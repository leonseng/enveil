package store

import (
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

const (
	keyLen = 32 // ChaCha20-Poly1305 key length
	saltLen = 16
)

// KDFParams holds Argon2id tuning parameters stored alongside the ciphertext.
type KDFParams struct {
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	SaltHex     string `json:"salt"`
}

// DefaultKDFParams returns sensible Argon2id defaults (≈64 MiB RAM, 3 passes).
func DefaultKDFParams() KDFParams {
	return KDFParams{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
	}
}

// NewSalt generates a fresh random salt and encodes it into params.
func NewSalt() ([saltLen]byte, error) {
	var salt [saltLen]byte
	_, err := rand.Read(salt[:])
	return salt, err
}

// DeriveKey runs Argon2id with the given password, salt, and params.
func DeriveKey(password []byte, params KDFParams, salt []byte) []byte {
	return argon2.IDKey(password, salt, params.Iterations, params.Memory, params.Parallelism, keyLen)
}
