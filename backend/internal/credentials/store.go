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
	Tokens         map[string]string            `json:"tokens"`                    // URL -> encrypted token
	ProjectSecrets map[string]map[string]string `json:"project_secrets,omitempty"` // project_id -> { key -> encrypted_value }
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

// SaveProjectSecret encrypts and stores a secret for a project.
// Existing values for the same key are overwritten.
func (s *Store) SaveProjectSecret(projectID, key, value string) error {
	if s.key == nil {
		return ErrNoKey
	}
	if projectID == "" || key == "" {
		return errors.New("projectID and key required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if creds == nil {
		creds = &credentials{}
	}
	if creds.Tokens == nil {
		creds.Tokens = make(map[string]string)
	}
	if creds.ProjectSecrets == nil {
		creds.ProjectSecrets = make(map[string]map[string]string)
	}
	if creds.ProjectSecrets[projectID] == nil {
		creds.ProjectSecrets[projectID] = make(map[string]string)
	}
	encrypted, err := s.encrypt(value)
	if err != nil {
		return err
	}
	creds.ProjectSecrets[projectID][key] = encrypted
	return s.save(creds)
}

// GetProjectSecret returns the decrypted value for a project's secret.
func (s *Store) GetProjectSecret(projectID, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if creds == nil || creds.ProjectSecrets == nil {
		return "", ErrNotFound
	}
	enc, ok := creds.ProjectSecrets[projectID][key]
	if !ok {
		return "", ErrNotFound
	}
	return s.decrypt(enc)
}

// ListProjectSecretKeys returns the key names (NOT values) for a project.
// Order is not guaranteed.
func (s *Store) ListProjectSecretKeys(projectID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if creds == nil || creds.ProjectSecrets == nil || creds.ProjectSecrets[projectID] == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(creds.ProjectSecrets[projectID]))
	for k := range creds.ProjectSecrets[projectID] {
		out = append(out, k)
	}
	return out, nil
}

// GetProjectSecrets returns ALL decrypted secrets for a project.
// Used by the build runner to write a .env file.
func (s *Store) GetProjectSecrets(projectID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if creds == nil || creds.ProjectSecrets == nil || creds.ProjectSecrets[projectID] == nil {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(creds.ProjectSecrets[projectID]))
	for k, enc := range creds.ProjectSecrets[projectID] {
		v, err := s.decrypt(enc)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// DeleteProjectSecret removes a secret. Returns ErrNotFound if absent.
func (s *Store) DeleteProjectSecret(projectID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	if creds == nil || creds.ProjectSecrets == nil || creds.ProjectSecrets[projectID] == nil {
		return ErrNotFound
	}
	if _, ok := creds.ProjectSecrets[projectID][key]; !ok {
		return ErrNotFound
	}
	delete(creds.ProjectSecrets[projectID], key)
	return s.save(creds)
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
