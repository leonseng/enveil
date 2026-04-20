package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/leonzalion/enveil/internal/verify"
)

// Resolver resolves a secret reference key to its plaintext value.
type Resolver interface {
	Resolve(key string) (string, error)
}

// Lister returns all item/field key names in the store (no values).
type Lister interface {
	List() []string
}

// Mutator supports adding, deleting, and persisting secrets in the store.
type Mutator interface {
	Add(item, field, value string)
	Delete(item, field string) bool
	Save() error
}

// Agent is a running socket server that resolves secrets.
type Agent struct {
	socketPath string
	envFile    string
	resolver   Resolver
	verifier   verify.Verifier
	listener   net.Listener
}

// SocketPath returns the Unix socket path for the given UID.
func SocketPath(uid int) string {
	return fmt.Sprintf("/tmp/enveil-agent-%d.sock", uid)
}

// EnvFilePath returns the path to the agent env file.
func EnvFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".enveil-agent.env")
}

// New creates an Agent but does not start listening.
func New(resolver Resolver, verifier verify.Verifier) *Agent {
	return &Agent{
		socketPath: SocketPath(os.Getuid()),
		envFile:    EnvFilePath(),
		resolver:   resolver,
		verifier:   verifier,
	}
}

// NewWithSocket creates an Agent with explicit socket and env file paths (for testing).
func NewWithSocket(resolver Resolver, verifier verify.Verifier, socketPath, envFile string) *Agent {
	return &Agent{
		socketPath: socketPath,
		envFile:    envFile,
		resolver:   resolver,
		verifier:   verifier,
	}
}

// SocketPath returns the socket path this agent is bound to.
func (a *Agent) SocketPath() string { return a.socketPath }

// Start binds the socket, writes the env file, and begins the accept loop.
// It blocks until shutdown.
func (a *Agent) Start() error {
	// Prevent other processes (even same-UID) from reading /proc/<pid>/environ
	// or memory-dumping this process.
	setNoDump()

	// Clean up any stale socket file.
	os.Remove(a.socketPath)

	ln, err := net.Listen("unix", a.socketPath)
	if err != nil {
		return fmt.Errorf("binding socket: %w", err)
	}
	// Restrict socket permissions to owner only.
	if err := os.Chmod(a.socketPath, 0600); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	a.listener = ln

	if err := a.writeEnvFile(); err != nil {
		ln.Close()
		return fmt.Errorf("writing env file: %w", err)
	}

	// Handle signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		a.Shutdown()
	}()

	log.Printf("enveil agent listening on %s", a.socketPath)
	return a.acceptLoop()
}

// Shutdown closes the listener and removes the socket + env file.
func (a *Agent) Shutdown() {
	if a.listener != nil {
		a.listener.Close()
	}
	os.Remove(a.socketPath)
	os.Remove(a.envFile)
}

func (a *Agent) acceptLoop() error {
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			// Listener closed — clean shutdown.
			return nil
		}
		go a.handleConn(conn)
	}
}

func (a *Agent) handleConn(conn net.Conn) {
	defer conn.Close()

	// Obtain peer credentials before reading any data.
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}

	pid, err := peerPID(uc)
	if err != nil {
		log.Printf("peercred error: %v", err)
		return
	}

	ok, err = a.verifier.Verify(pid)
	if err != nil || !ok {
		log.Printf("caller verification failed for pid %d: %v", pid, err)
		return
	}

	// Reload from disk so secrets added after agent startup are visible.
	if r, ok := a.resolver.(interface{ Reload() error }); ok {
		if err := r.Reload(); err != nil {
			log.Printf("store reload error: %v", err)
			writeResponse(conn, Response{Error: "agent: store reload failed"})
			return
		}
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			writeResponse(conn, Response{Error: "invalid request"})
			continue
		}

		switch req.Op {
		case OpResolve:
			val, err := a.resolver.Resolve(req.Ref)
			if err != nil {
				writeResponse(conn, Response{Error: err.Error()})
			} else {
				writeResponse(conn, Response{Value: val})
			}
		case OpList:
			lister, ok := a.resolver.(Lister)
			if !ok {
				writeResponse(conn, Response{Error: "list not supported by resolver"})
			} else {
				writeResponse(conn, Response{Keys: lister.List()})
			}
		case OpAdd, OpRotate:
			mutator, ok := a.resolver.(Mutator)
			if !ok {
				writeResponse(conn, Response{Error: "mutation not supported by resolver"})
				continue
			}
			mutator.Add(req.Item, req.Field, req.Value)
			if err := mutator.Save(); err != nil {
				writeResponse(conn, Response{Error: fmt.Sprintf("saving store: %v", err)})
			} else {
				writeResponse(conn, Response{})
			}
		case OpDelete:
			mutator, ok := a.resolver.(Mutator)
			if !ok {
				writeResponse(conn, Response{Error: "mutation not supported by resolver"})
				continue
			}
			if !mutator.Delete(req.Item, req.Field) {
				writeResponse(conn, Response{Error: fmt.Sprintf("secret %s/%s not found", req.Item, req.Field)})
			} else if err := mutator.Save(); err != nil {
				writeResponse(conn, Response{Error: fmt.Sprintf("saving store: %v", err)})
			} else {
				writeResponse(conn, Response{})
			}
		default:
			writeResponse(conn, Response{Error: fmt.Sprintf("unknown op %q", req.Op)})
		}
	}
}

func (a *Agent) writeEnvFile() error {
	content := fmt.Sprintf(
		"export ENVEIL_AUTH_SOCK=%s\nexport ENVEIL_AGENT_PID=%d\n",
		a.socketPath, os.Getpid(),
	)
	return os.WriteFile(a.envFile, []byte(content), 0600)
}

func writeResponse(conn net.Conn, resp Response) {
	b, err := encodeResponse(resp)
	if err != nil {
		return
	}
	conn.Write(b)
}
