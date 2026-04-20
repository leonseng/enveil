package verify_test

import (
	"os"
	"testing"

	"github.com/leonzalion/enveil/internal/verify"
)

func TestNoopVerifier(t *testing.T) {
	v := verify.Noop{}
	ok, err := v.Verify(0)
	if err != nil || !ok {
		t.Fatalf("Noop.Verify should always return true, got %v %v", ok, err)
	}
}

func TestInodeVerifierSelf(t *testing.T) {
	v, err := verify.NewInodeVerifier()
	if err != nil {
		t.Fatalf("NewInodeVerifier: %v", err)
	}

	selfPID := uint32(os.Getpid())
	ok, err := v.Verify(selfPID)
	if err != nil {
		t.Fatalf("Verify self: %v", err)
	}
	if !ok {
		t.Fatal("Verify self: expected true, got false")
	}
}

func TestInodeVerifierFakePID(t *testing.T) {
	v, err := verify.NewInodeVerifier()
	if err != nil {
		t.Fatalf("NewInodeVerifier: %v", err)
	}

	// PID 1 (init) will have a different executable.
	ok, err := v.Verify(1)
	if err != nil {
		// Some systems may deny access to /proc/1/exe; that's a legitimate rejection.
		t.Logf("Verify PID 1: expected rejection, got error (acceptable): %v", err)
		return
	}
	if ok {
		t.Fatal("Verify PID 1: expected false (different exe), got true")
	}
}
