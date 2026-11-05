package connection

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// The keyring adapter is tested against go-keyring's in-memory mock, so tests never touch the
// developer's real credential manager.
func TestKeyringStore(t *testing.T) {
	keyring.MockInit()
	s := NewKeyringStore()

	if _, err := s.Get("p1"); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("Get(missing) = %v, want ErrNoCredential", err)
	}
	if err := s.Set("p1", "pw1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("p1"); err != nil || got != "pw1" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	// Entries are namespaced by service and profile id.
	if got, err := keyring.Get(KeyringService, "connection:p1"); err != nil || got != "pw1" {
		t.Fatalf("raw keyring entry = %q, %v", got, err)
	}
	if err := s.Delete("p1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("p1"); err != nil {
		t.Errorf("deleting a missing credential returned %v", err)
	}
}

func TestKeyringStoreWrapsBackendErrors(t *testing.T) {
	keyring.MockInitWithError(errors.New("backend unavailable"))
	t.Cleanup(keyring.MockInit)
	s := NewKeyringStore()
	if err := s.Set("p1", "super-secret-value"); err == nil {
		t.Fatal("expected error")
	} else if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("error leaks the secret: %v", err)
	}
	if _, err := s.Get("p1"); err == nil || errors.Is(err, ErrNoCredential) {
		t.Fatalf("Get error = %v, want backend error", err)
	}
}
