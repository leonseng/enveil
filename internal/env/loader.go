package env

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
)

// refPattern matches secret://item/field references.
var refPattern = regexp.MustCompile(`^secret://([^/]+)/([^/]+)$`)

// SecretRef is a parsed secret reference from an env var value.
type SecretRef struct {
	// VarName is the environment variable that contained the reference.
	VarName string
	// Key is the store key in "item/field" format.
	Key string
}

// Load parses a .env file and merges it with the current process environment.
// Shell env wins: if a var is already set in the process env, the .env value is ignored.
// Returns the merged env as a slice of "KEY=VALUE" strings and all secret refs found.
func Load(dotenvPath string) ([]string, []SecretRef, error) {
	fileVars, err := godotenv.Read(dotenvPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", dotenvPath, err)
	}

	// Start with the current process environment (shell wins).
	existing := make(map[string]string)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			existing[parts[0]] = parts[1]
		}
	}

	// Merge: .env vars that are NOT already in the shell env.
	merged := make(map[string]string)
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range fileVars {
		if _, already := existing[k]; !already {
			merged[k] = v
		}
	}

	// Collect all secret refs.
	var refs []SecretRef
	for k, v := range merged {
		if m := refPattern.FindStringSubmatch(v); m != nil {
			refs = append(refs, SecretRef{
				VarName: k,
				Key:     m[1] + "/" + m[2],
			})
		}
	}

	// Build env slice.
	env := make([]string, 0, len(merged))
	for k, v := range merged {
		env = append(env, k+"="+v)
	}

	return env, refs, nil
}

// IsSecretRef reports whether value is a secret:// reference.
func IsSecretRef(value string) bool {
	return refPattern.MatchString(value)
}

// ParseRef extracts the store key from a secret:// reference.
// Returns ("", false) if value is not a reference.
func ParseRef(value string) (key string, ok bool) {
	m := refPattern.FindStringSubmatch(value)
	if m == nil {
		return "", false
	}
	return m[1] + "/" + m[2], true
}
