package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrNoKey       = errors.New("encryption key not set")
	ErrInvalidKey  = errors.New("encryption key must be 32 bytes")
	ErrNotFound    = errors.New("credential not found")
	ErrDecryptFail = errors.New("failed to decrypt credential")
)

// Store manages encrypted credential storage
type Store struct {
	path string
	key  []byte
	mu   sync.RWMutex
}

// credentials is the internal structure stored on disk
type credentials struct {
	Tokens map[string]string `json:"tokens"` // URL -> encrypted token
}

// NewStore creates a new credential store
// key should be 32 bytes for AES-256
func NewStore(path string, key []byte) (*Store, error) {
	if len(key) == 0 {
		// Allow nil key for public-only repos
		return &Store{path: path, key: nil}, nil
	}
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	return &Store{path: path, key: key}, nil
}

// SaveToken encrypts and saves a token for a repository URL
func (s *Store) SaveToken(repoURL, token string) error {
	if s.key == nil {
		return ErrNoKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if creds == nil {
		creds = &credentials{Tokens: make(map[string]string)}
	}

	encrypted, err := s.encrypt(token)
	if err != nil {
		return err
	}

	creds.Tokens[repoURL] = encrypted
	return s.save(creds)
}

// GetToken retrieves and decrypts a token for a repository URL
func (s *Store) GetToken(repoURL string) (string, error) {
	if s.key == nil {
		return "", ErrNoKey
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}

	encrypted, ok := creds.Tokens[repoURL]
	if !ok {
		return "", ErrNotFound
	}

	return s.decrypt(encrypted)
}

// HasToken checks if a token exists for a repository URL
func (s *Store) HasToken(repoURL string) bool {
	if s.key == nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return false
	}

	_, ok := creds.Tokens[repoURL]
	return ok
}

// DeleteToken removes a token for a repository URL
func (s *Store) DeleteToken(repoURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	delete(creds.Tokens, repoURL)
	return s.save(creds)
}

// globalKey returns the reserved key used to store a provider-wide token.
// The __provider:name prefix cannot collide with a real repo URL (no scheme).
func globalKey(provider string) string {
	return "__provider:" + provider
}

// SaveGlobalToken stores a token for a provider (e.g. "github") so it can be
// reused across every repo from that host.
func (s *Store) SaveGlobalToken(provider, token string) error {
	return s.SaveToken(globalKey(provider), token)
}

// GetGlobalToken retrieves the provider-wide token.
func (s *Store) GetGlobalToken(provider string) (string, error) {
	return s.GetToken(globalKey(provider))
}

// HasGlobalToken reports whether a provider-wide token is stored.
func (s *Store) HasGlobalToken(provider string) bool {
	return s.HasToken(globalKey(provider))
}

// DeleteGlobalToken removes the provider-wide token.
func (s *Store) DeleteGlobalToken(provider string) error {
	return s.DeleteToken(globalKey(provider))
}

// load reads credentials from disk
func (s *Store) load() (*credentials, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	if creds.Tokens == nil {
		creds.Tokens = make(map[string]string)
	}

	return &creds, nil
}

// save writes credentials to disk
func (s *Store) save(creds *credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0600)
}

// encrypt encrypts plaintext using AES-256-GCM
func (s *Store) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts ciphertext using AES-256-GCM
func (s *Store) decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return "", ErrDecryptFail
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptFail
	}

	return string(plaintext), nil
}
