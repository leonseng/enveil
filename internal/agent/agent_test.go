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

func (m *mockResolver) List() []string {
	keys := make([]string, 0, len(m.secrets))
	for k := range m.secrets {
		keys = append(keys, k)
	}
	return keys
}

func (m *mockResolver) Add(item, field, value string) {
	m.secrets[item+"/"+field] = value
}

func (m *mockResolver) Delete(item, field string) bool {
	key := item + "/" + field
	if _, ok := m.secrets[key]; !ok {
		return false
	}
	delete(m.secrets, key)
	return true
}

func (m *mockResolver) Save() error { return nil }

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

func TestAgentList(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{
		"stripe/key":   "sk_live_abc",
		"postgres/url": "postgres://localhost/db",
	}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpList})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(resp.Keys), resp.Keys)
	}
}

func TestAgentListEmpty(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpList})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if len(resp.Keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(resp.Keys))
	}
}

func TestAgentAdd(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpAdd, Item: "stripe", Field: "key", Value: "sk_live_xyz"})
	if resp.Error != "" {
		t.Fatalf("unexpected error from OpAdd: %s", resp.Error)
	}

	// Verify the secret is now resolvable.
	resp = sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if resp.Error != "" {
		t.Fatalf("resolve after add failed: %s", resp.Error)
	}
	if resp.Value != "sk_live_xyz" {
		t.Fatalf("expected sk_live_xyz, got %q", resp.Value)
	}
}

func TestAgentDelete(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{"stripe/key": "sk_live_abc"}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpDelete, Item: "stripe", Field: "key"})
	if resp.Error != "" {
		t.Fatalf("unexpected error from OpDelete: %s", resp.Error)
	}

	// Verify the secret is gone.
	resp = sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if resp.Error == "" {
		t.Fatal("expected error resolving deleted key, got none")
	}
}

func TestAgentDeleteMissingKey(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpDelete, Item: "stripe", Field: "key"})
	if resp.Error == "" {
		t.Fatal("expected error deleting non-existent key, got none")
	}
}

func TestAgentRotate(t *testing.T) {
	r := &mockResolver{secrets: map[string]string{"stripe/key": "sk_live_old"}}
	sock, shutdown := startAgent(t, r)
	defer shutdown()

	resp := sendRequest(t, sock, agent.Request{Op: agent.OpRotate, Item: "stripe", Field: "key", Value: "sk_live_new"})
	if resp.Error != "" {
		t.Fatalf("unexpected error from OpRotate: %s", resp.Error)
	}

	// Verify the updated value.
	resp = sendRequest(t, sock, agent.Request{Op: agent.OpResolve, Ref: "stripe/key"})
	if resp.Error != "" {
		t.Fatalf("resolve after rotate failed: %s", resp.Error)
	}
	if resp.Value != "sk_live_new" {
		t.Fatalf("expected sk_live_new, got %q", resp.Value)
	}
}
