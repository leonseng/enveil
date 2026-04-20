package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leonzalion/enveil/internal/store"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.enveil")
	password := []byte("hunter2")

	s, err := store.Init(path, password)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Add("stripe", "key", "sk_live_123")
	s.Add("postgres", "url", "postgres://localhost/db")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := store.Open(path, password)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	val, err := s2.Resolve("stripe/key")
	if err != nil || val != "sk_live_123" {
		t.Fatalf("Resolve stripe/key: got %q %v", val, err)
	}

	val, err = s2.Resolve("postgres/url")
	if err != nil || val != "postgres://localhost/db" {
		t.Fatalf("Resolve postgres/url: got %q %v", val, err)
	}
}

func TestWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.enveil")

	s, err := store.Init(path, []byte("correct"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Add("item", "field", "value")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err = store.Open(path, []byte("wrong"))
	if err == nil {
		t.Fatal("expected error with wrong password, got nil")
	}
}

func TestTamperedCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.enveil")

	s, err := store.Init(path, []byte("password"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Add("item", "field", "value")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Corrupt the file by flipping bytes near the end.
	data, _ := os.ReadFile(path)
	data[len(data)-5] ^= 0xFF
	os.WriteFile(path, data, 0600)

	_, err = store.Open(path, []byte("password"))
	if err == nil {
		t.Fatal("expected error with tampered ciphertext, got nil")
	}
}

func TestRekey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.enveil")
	oldPass := []byte("oldpassword")
	newPass := []byte("newpassword")

	s, err := store.Init(path, oldPass)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Add("stripe", "key", "sk_live_abc")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.Rekey(newPass); err != nil {
		t.Fatalf("Rekey: %v", err)
	}

	// Should open with new password and retain secrets.
	s2, err := store.Open(path, newPass)
	if err != nil {
		t.Fatalf("Open with new password: %v", err)
	}
	val, err := s2.Resolve("stripe/key")
	if err != nil || val != "sk_live_abc" {
		t.Fatalf("Resolve after Rekey: got %q %v", val, err)
	}

	// Should NOT open with old password.
	_, err = store.Open(path, oldPass)
	if err == nil {
		t.Fatal("expected error opening with old password after Rekey, got nil")
	}
}

func TestDeleteAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.enveil")

	s, err := store.Init(path, []byte("pass"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Add("a", "k1", "v1")
	s.Add("a", "k2", "v2")
	s.Add("b", "k1", "v3")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, _ := store.Open(path, []byte("pass"))
	keys := s2.List()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	deleted := s2.Delete("a", "k1")
	if !deleted {
		t.Fatal("expected Delete to return true")
	}

	notDeleted := s2.Delete("a", "k1") // already gone
	if notDeleted {
		t.Fatal("expected Delete to return false for missing key")
	}

	if err := s2.Save(); err != nil {
		t.Fatalf("Save after delete: %v", err)
	}

	s3, _ := store.Open(path, []byte("pass"))
	if len(s3.List()) != 2 {
		t.Fatalf("expected 2 keys after delete, got %d", len(s3.List()))
	}
}
