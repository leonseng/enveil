package run_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leonzalion/enveil/internal/env"
)

// These tests exercise the env loading and ref resolution logic without actually
// calling syscall.Exec (which would replace the test process).

func TestApplyResolvedViaEnv(t *testing.T) {
	// Write a .env with a mix of refs and plain values.
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("STRIPE_KEY=enveil://stripe/key\nPLAIN=hello\n"), 0644)

	_, refs, err := env.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify the ref was found.
	found := false
	for _, r := range refs {
		if r.VarName == "STRIPE_KEY" && r.Key == "stripe/key" {
			found = true
		}
	}
	if !found {
		t.Fatal("STRIPE_KEY ref not found")
	}
}

func TestStrictModeNoPassthrough(t *testing.T) {
	// Confirm IsSecretRef rejects non-refs and accepts refs correctly.
	if env.IsSecretRef("plaintext") {
		t.Fatal("plain value should not be a secret ref")
	}
	if !env.IsSecretRef("enveil://stripe/key") {
		t.Fatal("enveil:// value should be a secret ref")
	}
	if env.IsSecretRef("enveil://no-field") {
		t.Fatal("malformed secret ref (no field) should not match")
	}
}

func TestShellEnvWinsInRun(t *testing.T) {
	os.Setenv("ENVEIL_RUN_TEST_VAR", "from_shell")
	t.Cleanup(func() { os.Unsetenv("ENVEIL_RUN_TEST_VAR") })

	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("ENVEIL_RUN_TEST_VAR=from_dotenv\n"), 0644)

	envSlice, _, err := env.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, kv := range envSlice {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 && parts[0] == "ENVEIL_RUN_TEST_VAR" {
			if parts[1] != "from_shell" {
				t.Fatalf("expected from_shell, got %q", parts[1])
			}
			return
		}
	}
	t.Fatal("ENVEIL_RUN_TEST_VAR not found in env slice")
}
