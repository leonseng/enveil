package agent_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/leonzalion/enveil/internal/agent"
	"github.com/leonzalion/enveil/internal/store"
	"github.com/leonzalion/enveil/internal/verify"
)

// mockResolver resolves from an in-memory map.
type mockResolver struct {
	secrets map[string]string
}

func (m *mockResolver) Resolve(key string) (string, error) {
	v, ok := m.secrets[key]
	if !ok {
		return "", &missingError{key}
	}
	return v, nil
}

type missingError struct{ key string }

func (e *missingError) Error() string { return "secret not found: " + e.key }

func startAgent(t *testing.T, r agent.Resolver) (socketPath string, shutdown func()) {
	t.Helper()
	a := agent.NewWithSocket(r, verify.Noop{}, t.TempDir()+"/agent.sock", t.TempDir()+"/.enveil-agent.env")
	ready := make(chan struct{})
	go func() {
		close(ready)
		a.Start() //nolint:errcheck
	}()
	<-ready
	time.Sleep(20 * time.Millisecond) // let goroutine reach Accept
	return a.SocketPath(), a.Shutdown
}

func sendRequest(t *testing.T, sockPath string, req agent.Request) agent.Response {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	b, _ := json.Marshal(req)
	conn.Write(append(b, '\n'))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response from agent")
	}
	var resp agent.Response
	json.Unmarshal(scanner.Bytes(), &resp)
	return resp
}

func TestAgentResolve(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{"stripe/key": "sk_live_abc"}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if resp.Value != "sk_live_abc" {
		t.Fatalf("expected sk_live_abc, got %q", resp.Value)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}

func TestAgentMissingSecret(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "missing/key"})
	if resp.Error == "" {
		t.Fatal("expected error for missing key, got none")
	}
}

func TestAgentShutdownCleansUp(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sockDir := t.TempDir()
	sockPath := sockDir + "/agent.sock"
	envFile := t.TempDir() + "/.enveil-agent.env"

	a := agent.NewWithSocket(r, verify.Noop{}, sockPath, envFile)
	ready := make(chan struct{})
	go func() {
		close(ready)
		a.Start() //nolint:errcheck
	}()
	<-ready
	time.Sleep(20 * time.Millisecond)

	// Verify socket and env file exist.
	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("socket not found before shutdown: %v", err)
	}
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("env file not found before shutdown: %v", err)
	}

	a.Shutdown()
	time.Sleep(20 * time.Millisecond)

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket still exists after shutdown")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatal("env file still exists after shutdown")
	}
}

func TestAgentEnvFileContent(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sockPath := t.TempDir() + "/agent.sock"
	envFile := t.TempDir() + "/.enveil-agent.env"

	a := agent.NewWithSocket(r, verify.Noop{}, sockPath, envFile)
	ready := make(chan struct{})
	go func() {
		close(ready)
		a.Start() //nolint:errcheck
	}()
	<-ready
	time.Sleep(20 * time.Millisecond)
	defer a.Shutdown()

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("env file not readable: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ENVEIL_AUTH_SOCK="+sockPath) {
		t.Fatalf("env file missing ENVEIL_AUTH_SOCK: %s", content)
	}
	if !strings.Contains(content, "ENVEIL_AGENT_PID=") {
		t.Fatalf("env file missing ENVEIL_AGENT_PID: %s", content)
	}
}

// TestAgentReloadAfterSecretAdded verifies that secrets written to the store
// after the agent starts are resolved correctly (stale cache regression test).
func TestAgentReloadAfterSecretAdded(t *testing.T) {
	storePath := t.TempDir() + "/.enveil"
	password := []byte("test123")

	// Initialise an empty store.
	s, err := store.Init(storePath, password)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Start the agent with the empty store.
	sock, shutdown := startAgentWithStore(t, s)
	defer shutdown()

	// Add a secret AFTER the agent has started.
	s2, err := store.Open(storePath, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s2.Add("postgres", "url", "postgres://localhost/db")
	if err := s2.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The agent should reload from disk and resolve the new secret.
	resp := sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "postgres/url"})
	if resp.Error != "" {
		t.Fatalf("expected successful resolve after reload, got error: %s", resp.Error)
	}
	if resp.Value != "postgres://localhost/db" {
		t.Fatalf("expected postgres://localhost/db, got %q", resp.Value)
	}
}

func startAgentWithStore(t *testing.T, r agent.Resolver) (socketPath string, shutdown func()) {
	t.Helper()
	a := agent.NewWithSocket(r, verify.Noop{}, t.TempDir()+"/agent.sock", t.TempDir()+"/.enveil-agent.env")
	ready := make(chan struct{})
	go func() {
		close(ready)
		a.Start() //nolint:errcheck
	}()
	<-ready
	time.Sleep(20 * time.Millisecond)
	return a.SocketPath(), a.Shutdown
}
