package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/chacha20poly1305"
)

const storeVersion = 1

// envelope is the on-disk JSON structure.
type envelope struct {
	Version    int       `json:"version"`
	KDFParams  KDFParams `json:"kdf_params"`
	NonceHex   string    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
}

// Store is a decrypted in-memory secret store.
type Store struct {
	path     string
	password []byte
	secrets  map[string]string
}

// Open reads, decrypts, and parses the store file.
func Open(path string, password []byte) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading store: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing store: %w", err)
	}
	if env.Version != storeVersion {
		return nil, fmt.Errorf("unsupported store version %d", env.Version)
	}

	salt, err := hex.DecodeString(env.KDFParams.SaltHex)
	if err != nil {
		return nil, fmt.Errorf("decoding salt: %w", err)
	}

	key := DeriveKey(password, env.KDFParams, salt)
	defer zeroBytes(key)

	nonce, err := hex.DecodeString(env.NonceHex)
	if err != nil {
		return nil, fmt.Errorf("decoding nonce: %w", err)
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	plaintext, err := aead.Open(nil, nonce, env.Ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: wrong password or corrupted store")
	}

	var secrets map[string]string
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return nil, fmt.Errorf("parsing secrets: %w", err)
	}

	return &Store{
		path:     path,
		password: password,
		secrets:  secrets,
	}, nil
}

// Init creates a new empty encrypted store file at path.
func Init(path string, password []byte) (*Store, error) {
	s := &Store{
		path:     path,
		password: password,
		secrets:  map[string]string{},
	}
	return s, s.save()
}

// Resolve returns the plaintext value for a secret key, or an error if not found.
func (s *Store) Resolve(key string) (string, error) {
	v, ok := s.secrets[key]
	if !ok {
		return "", fmt.Errorf("secret %q not found", key)
	}
	return v, nil
}

// Add sets a secret. Call Save to persist.
func (s *Store) Add(item, field, value string) {
	s.secrets[item+"/"+field] = value
}

// Delete removes a secret. Returns false if it didn't exist.
func (s *Store) Delete(item, field string) bool {
	key := item + "/" + field
	if _, ok := s.secrets[key]; !ok {
		return false
	}
	delete(s.secrets, key)
	return true
}

// List returns all item/field keys in the store (no values).
func (s *Store) List() []string {
	keys := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		keys = append(keys, k)
	}
	return keys
}

// Save re-encrypts and writes the store to disk.
func (s *Store) Save() error {
	return s.save()
}

func (s *Store) save() error {
	params := DefaultKDFParams()

	salt, err := NewSalt()
	if err != nil {
		return err
	}
	params.SaltHex = hex.EncodeToString(salt[:])

	key := DeriveKey(s.password, params, salt[:])
	defer zeroBytes(key)

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	plaintext, err := json.Marshal(s.secrets)
	if err != nil {
		return err
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	env := envelope{
		Version:    storeVersion,
		KDFParams:  params,
		NonceHex:   hex.EncodeToString(nonce),
		Ciphertext: ciphertext,
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}

	// Write atomically via temp file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
