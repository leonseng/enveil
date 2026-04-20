package run

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/leonzalion/enveil/internal/agent"
	"github.com/leonzalion/enveil/internal/env"
)

// Run resolves all secret:// references from dotenvPath, then exec's cmd with args.
// It never returns on success (syscall.Exec replaces the process).
func Run(dotenvPath string, cmd string, args []string) error {
	envSlice, refs, err := env.Load(dotenvPath)
	if err != nil {
		return fmt.Errorf("loading env: %w", err)
	}

	if len(refs) > 0 {
		resolved, err := resolveRefs(refs)
		if err != nil {
			return err
		}
		// Replace secret:// values in envSlice with resolved plaintext.
		envSlice = applyResolved(envSlice, resolved)
	}

	// Sanity check: no secret:// references should remain.
	for _, kv := range envSlice {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 && env.IsSecretRef(parts[1]) {
			return fmt.Errorf("unresolved secret reference in %s — agent may be unreachable", parts[0])
		}
	}

	setNoDump()

	return syscall.Exec(cmd, append([]string{cmd}, args...), envSlice)
}

// resolveRefs opens a single connection to the agent and batch-resolves all refs.
func resolveRefs(refs []env.SecretRef) (map[string]string, error) {
	sockPath := os.Getenv("ENVEIL_AUTH_SOCK")
	if sockPath == "" {
		sockPath = agent.SocketPath(os.Getuid())
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("agent not running — run: enveil agent start\n(dial %s: %w)", sockPath, err)
	}
	defer conn.Close()

	resolved := make(map[string]string, len(refs))
	scanner := bufio.NewScanner(conn)

	for _, ref := range refs {
		req := agent.Request{Op: agent.OpResolve, Ref: ref.Key}
		b, _ := json.Marshal(req)
		conn.Write(append(b, '\n'))

		if !scanner.Scan() {
			return nil, fmt.Errorf("agent closed connection unexpectedly")
		}
		var resp agent.Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			return nil, fmt.Errorf("invalid agent response: %w", err)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("agent error resolving %s: %s", ref.Key, resp.Error)
		}
		resolved[ref.VarName] = resp.Value
	}

	return resolved, nil
}

// applyResolved replaces secret:// values in the env slice with plaintext.
func applyResolved(envSlice []string, resolved map[string]string) []string {
	out := make([]string, len(envSlice))
	for i, kv := range envSlice {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			if val, ok := resolved[parts[0]]; ok {
				out[i] = parts[0] + "=" + val
				continue
			}
		}
		out[i] = kv
	}
	return out
}
