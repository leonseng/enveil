package main

import (
	"bytes"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/leonzalion/enveil/internal/agent"
	"github.com/leonzalion/enveil/internal/store"
	"github.com/leonzalion/enveil/internal/verify"
)

// startTestAgent initialises a real agent backed by s over a temp socket.
// The socket path is set in ENVEIL_AUTH_SOCK for the duration of the test.
func startTestAgent(t *testing.T, s *store.Store) {
	t.Helper()
	sockPath := t.TempDir() + "/agent.sock"
	envFile := t.TempDir() + "/.enveil-agent.env"
	a := agent.NewWithSocket(s, verify.Noop{}, sockPath, envFile)

	ready := make(chan struct{})
	go func() {
		close(ready)
		a.Start() //nolint:errcheck
	}()
	<-ready
	time.Sleep(20 * time.Millisecond)
	t.Cleanup(a.Shutdown)

	t.Setenv("ENVEIL_AUTH_SOCK", sockPath)
}

// captureStdout runs f and returns whatever was written to os.Stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })

	f()
	w.Close()

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func initStore(t *testing.T) (*store.Store, string, []byte) {
	t.Helper()
	sp := t.TempDir() + "/.enveil"
	pw := []byte("test123")
	s, err := store.Init(sp, pw)
	if err != nil {
		t.Fatalf("store.Init: %v", err)
	}
	return s, sp, pw
}

// TestDialAgentUnreachable verifies dialAgent returns an error when no agent is running.
func TestDialAgentUnreachable(t *testing.T) {
	t.Setenv("ENVEIL_AUTH_SOCK", t.TempDir()+"/nonexistent.sock")
	_, err := dialAgent(agent.Request{Op: agent.OpList})
	if err == nil {
		t.Fatal("expected error when agent is unreachable, got nil")
	}
}

// TestSecretListViaAgent verifies secret list uses the agent without a password prompt.
func TestSecretListViaAgent(t *testing.T) {
	s, _, _ := initStore(t)
	s.Add("stripe", "key", "sk_live_abc")
	s.Add("postgres", "url", "postgres://localhost/db")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	startTestAgent(t, s)

	out := captureStdout(t, func() {
		resp, err := dialAgent(agent.Request{Op: agent.OpList})
		if err != nil {
			t.Fatalf("dialAgent: %v", err)
		}
		if resp.Error != "" {
			t.Fatalf("agent error: %s", resp.Error)
		}
		for _, k := range resp.Keys {
			os.Stdout.WriteString(k + "\n")
		}
	})

	lines := strings.Fields(out)
	sort.Strings(lines)
	want := []string{"postgres/url", "stripe/key"}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("key[%d]: want %q, got %q", i, w, lines[i])
		}
	}
}

// TestSecretAddViaAgent verifies secret add sends OpAdd and the secret is then resolvable.
func TestSecretAddViaAgent(t *testing.T) {
	s, _, _ := initStore(t)
	startTestAgent(t, s)

	resp, err := dialAgent(agent.Request{Op: agent.OpAdd, Item: "stripe", Field: "key", Value: "sk_live_abc"})
	if err != nil {
		t.Fatalf("dialAgent OpAdd: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error on add: %s", resp.Error)
	}

	// Verify the new secret is resolvable via the agent.
	resp, err = dialAgent(agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if err != nil {
		t.Fatalf("dialAgent OpResolve: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error on resolve: %s", resp.Error)
	}
	if resp.Value != "sk_live_abc" {
		t.Fatalf("expected sk_live_abc, got %q", resp.Value)
	}
}

// TestSecretDeleteViaAgent verifies secret delete sends OpDelete and the secret is gone.
func TestSecretDeleteViaAgent(t *testing.T) {
	s, _, _ := initStore(t)
	s.Add("stripe", "key", "sk_live_abc")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	startTestAgent(t, s)

	resp, err := dialAgent(agent.Request{Op: agent.OpDelete, Item: "stripe", Field: "key"})
	if err != nil {
		t.Fatalf("dialAgent OpDelete: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error on delete: %s", resp.Error)
	}

	// Verify the secret is gone.
	resp, err = dialAgent(agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if err != nil {
		t.Fatalf("dialAgent OpResolve: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected error resolving deleted key, got none")
	}
}

// TestSecretDeleteMissingViaAgent verifies OpDelete returns an error for a non-existent key.
func TestSecretDeleteMissingViaAgent(t *testing.T) {
	s, _, _ := initStore(t)
	startTestAgent(t, s)

	resp, err := dialAgent(agent.Request{Op: agent.OpDelete, Item: "stripe", Field: "key"})
	if err != nil {
		t.Fatalf("dialAgent: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected error deleting non-existent key, got none")
	}
}

// TestSecretRotateViaAgent verifies secret rotate sends OpRotate and the new value is resolvable.
func TestSecretRotateViaAgent(t *testing.T) {
	s, _, _ := initStore(t)
	s.Add("stripe", "key", "sk_live_old")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	startTestAgent(t, s)

	resp, err := dialAgent(agent.Request{Op: agent.OpRotate, Item: "stripe", Field: "key", Value: "sk_live_new"})
	if err != nil {
		t.Fatalf("dialAgent OpRotate: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error on rotate: %s", resp.Error)
	}

	resp, err = dialAgent(agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if err != nil {
		t.Fatalf("dialAgent OpResolve: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("agent error on resolve: %s", resp.Error)
	}
	if resp.Value != "sk_live_new" {
		t.Fatalf("expected sk_live_new, got %q", resp.Value)
	}
}

// TestSecretListFallback verifies that dialAgent returns an error when the agent is
// not running, which triggers the direct store fallback path.
func TestSecretListFallback(t *testing.T) {
	// Point to a socket that doesn't exist — no agent running.
	t.Setenv("ENVEIL_AUTH_SOCK", t.TempDir()+"/no-agent.sock")

	_, err := dialAgent(agent.Request{Op: agent.OpList})
	if err == nil {
		t.Fatal("expected dialAgent to fail when agent is not running")
	}

	// The fallback path (store.Open + List) is exercised separately in store_test.go.
	// Here we just confirm the branch condition (err != nil) is correctly triggered.
}
