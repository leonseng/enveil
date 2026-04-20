package env_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leonzalion/enveil/internal/env"
)

func writeDotenv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRefScanning(t *testing.T) {
	p := writeDotenv(t, `
STRIPE_KEY=enveil://stripe/key
DB_URL=enveil://db/url
PLAIN=hello
`)
	envSlice, refs, err := env.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(refs) < 2 {
		t.Fatalf("expected at least 2 refs, got %d", len(refs))
	}

	_ = envSlice
	refMap := make(map[string]string)
	for _, r := range refs {
		refMap[r.VarName] = r.Key
	}
	if refMap["STRIPE_KEY"] != "stripe/key" {
		t.Errorf("STRIPE_KEY ref: got %q", refMap["STRIPE_KEY"])
	}
	if refMap["DB_URL"] != "db/url" {
		t.Errorf("DB_URL ref: got %q", refMap["DB_URL"])
	}
}

func TestShellEnvWins(t *testing.T) {
	// Temporarily set a variable in the process environment.
	os.Setenv("ENVEIL_TEST_PRIORITY", "shell_value")
	t.Cleanup(func() { os.Unsetenv("ENVEIL_TEST_PRIORITY") })

	p := writeDotenv(t, "ENVEIL_TEST_PRIORITY=dotenv_value\n")
	envSlice, _, err := env.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, kv := range envSlice {
		if kv == "ENVEIL_TEST_PRIORITY=shell_value" {
			return // pass
		}
	}
	t.Fatal("shell env should win over .env value")
}

func TestNonRefPassthrough(t *testing.T) {
	p := writeDotenv(t, "PLAIN_VAR=just_a_value\n")
	_, refs, err := env.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, r := range refs {
		if r.VarName == "PLAIN_VAR" {
			t.Fatal("non-ref value should not appear in refs")
		}
	}
}

func TestParseRef(t *testing.T) {
	key, ok := env.ParseRef("enveil://stripe/key")
	if !ok || key != "stripe/key" {
		t.Fatalf("ParseRef: got %q %v", key, ok)
	}

	_, ok = env.ParseRef("not_a_ref")
	if ok {
		t.Fatal("ParseRef should return false for non-ref")
	}
}
