package connection

import (
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

// ErrNoCredential is returned when no secret is stored for a profile.
var ErrNoCredential = errors.New("no stored credential")

// CredentialStore keeps connection secrets outside the application's files.
type CredentialStore interface {
	// Set stores or replaces the secret for a profile.
	Set(profileID, secret string) error
	// Get returns the secret, or ErrNoCredential.
	Get(profileID string) (string, error)
	// Delete removes the secret. Deleting a missing secret is not an error.
	Delete(profileID string) error
}

// KeyringService is the service name used in the OS credential store. In Windows Credential
// Manager, entries appear as generic credentials named "Qryvanta:connection:<profile id>".
const KeyringService = "Qryvanta"

// KeyringStore stores secrets in the operating system's credential manager:
// Windows Credential Manager, the macOS Keychain, or the Secret Service on Linux.
type KeyringStore struct{}

// NewKeyringStore returns a store backed by the OS credential manager.
func NewKeyringStore() KeyringStore { return KeyringStore{} }

func keyringUser(profileID string) string { return "connection:" + profileID }

func (KeyringStore) Set(profileID, secret string) error {
	if err := keyring.Set(KeyringService, keyringUser(profileID), secret); err != nil {
		// The error never includes the secret itself.
		return fmt.Errorf("store credential in OS credential manager: %w", err)
	}
	return nil
}

func (KeyringStore) Get(profileID string) (string, error) {
	s, err := keyring.Get(KeyringService, keyringUser(profileID))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoCredential
	}
	if err != nil {
		return "", fmt.Errorf("read credential from OS credential manager: %w", err)
	}
	return s, nil
}

func (KeyringStore) Delete(profileID string) error {
	err := keyring.Delete(KeyringService, keyringUser(profileID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete credential from OS credential manager: %w", err)
	}
	return nil
}

// MemoryStore is an in-memory CredentialStore for tests. The Fail* fields inject errors.
type MemoryStore struct {
	mu      sync.Mutex
	secrets map[string]string

	FailSet    error
	FailGet    error
	FailDelete error
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{secrets: map[string]string{}} }

func (m *MemoryStore) Set(profileID, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailSet != nil {
		return m.FailSet
	}
	m.secrets[profileID] = secret
	return nil
}

func (m *MemoryStore) Get(profileID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailGet != nil {
		return "", m.FailGet
	}
	s, ok := m.secrets[profileID]
	if !ok {
		return "", ErrNoCredential
	}
	return s, nil
}

func (m *MemoryStore) Delete(profileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailDelete != nil {
		return m.FailDelete
	}
	delete(m.secrets, profileID)
	return nil
}

// Len returns the number of stored secrets.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.secrets)
}
