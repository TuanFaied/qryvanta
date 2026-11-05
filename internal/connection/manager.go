package connection

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned when a profile ID does not exist.
var ErrNotFound = errors.New("connection profile not found")

// Manager is the entry point for creating, changing and deleting connection profiles.
// It keeps profiles and their credentials consistent: a failed operation never leaves a
// profile pointing at the wrong password, or a password without a profile.
type Manager struct {
	mu    sync.Mutex
	store ProfileStore
	creds CredentialStore
	log   *slog.Logger
	now   func() time.Time
}

// ProfileStore persists the full list of profiles. *Store is the file implementation.
type ProfileStore interface {
	Load() ([]Profile, error)
	Save([]Profile) error
}

// NewManager creates a manager. A nil logger discards log output.
func NewManager(store ProfileStore, creds CredentialStore, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{store: store, creds: creds, log: log, now: time.Now}
}

// List returns all profiles sorted by name (case-insensitive).
func (m *Manager) List() ([]Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(profiles, func(a, b Profile) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return profiles, nil
}

// Get returns one profile.
func (m *Manager) Get(id string) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profiles, err := m.store.Load()
	if err != nil {
		return Profile{}, err
	}
	i := indexOf(profiles, id)
	if i < 0 {
		return Profile{}, ErrNotFound
	}
	return profiles[i], nil
}

// Create saves a new profile. ID and timestamps are assigned by the manager.
// password is stored only for SQL Server authentication; it may be empty, in which case the
// user is asked for it when connecting.
func (m *Manager) Create(p Profile, password string) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profiles, err := m.store.Load()
	if err != nil {
		return Profile{}, err
	}
	p = p.Normalize()
	if err := validateUnique(p, profiles); err != nil {
		return Profile{}, err
	}
	id, err := newID()
	if err != nil {
		return Profile{}, err
	}
	now := m.now().UTC()
	p.ID, p.CreatedAt, p.UpdatedAt = id, now, now

	storedSecret := false
	if p.UsesPassword() && password != "" {
		if err := m.creds.Set(p.ID, password); err != nil {
			return Profile{}, err
		}
		storedSecret = true
	}
	if err := m.store.Save(append(profiles, p)); err != nil {
		if storedSecret {
			if delErr := m.creds.Delete(p.ID); delErr != nil {
				m.log.Error("could not remove credential after failed save", "profile_id", p.ID, "err", delErr)
			}
		}
		return Profile{}, err
	}
	m.log.Info("connection profile created", "profile_id", p.ID, "name", p.Name, "auth", p.Auth)
	return p, nil
}

// PasswordChange describes what Update does with the stored password.
type PasswordChange struct {
	// Set replaces the stored password with Value. When false, the password is kept.
	Set   bool
	Value string
}

// KeepPassword leaves the stored password unchanged.
var KeepPassword = PasswordChange{}

// NewPassword replaces the stored password. An empty value removes it.
func NewPassword(v string) PasswordChange { return PasswordChange{Set: true, Value: v} }

// Update replaces an existing profile's settings. ID and CreatedAt cannot be changed.
// Switching to Windows authentication always removes the stored password.
func (m *Manager) Update(p Profile, pw PasswordChange) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	profiles, err := m.store.Load()
	if err != nil {
		return Profile{}, err
	}
	i := indexOf(profiles, p.ID)
	if i < 0 {
		return Profile{}, ErrNotFound
	}
	old := profiles[i]
	p = p.Normalize()
	if err := validateUnique(p, profiles); err != nil {
		return Profile{}, err
	}
	p.CreatedAt = old.CreatedAt
	p.UpdatedAt = m.now().UTC()

	// Decide the credential change, remembering the previous secret so it can be restored.
	prev, prevErr := m.creds.Get(p.ID)
	hadPrev := prevErr == nil
	if prevErr != nil && !errors.Is(prevErr, ErrNoCredential) {
		return Profile{}, prevErr
	}

	var apply func() error
	switch {
	case !p.UsesPassword():
		apply = func() error { return m.creds.Delete(p.ID) }
	case pw.Set && pw.Value == "":
		apply = func() error { return m.creds.Delete(p.ID) }
	case pw.Set:
		apply = func() error { return m.creds.Set(p.ID, pw.Value) }
	}
	if apply != nil {
		if err := apply(); err != nil {
			return Profile{}, err
		}
	}

	profiles[i] = p
	if err := m.store.Save(profiles); err != nil {
		if apply != nil {
			m.restoreCredential(p.ID, prev, hadPrev)
		}
		return Profile{}, err
	}
	m.log.Info("connection profile updated", "profile_id", p.ID, "name", p.Name, "password_changed", apply != nil)
	return p, nil
}

func (m *Manager) restoreCredential(id, prev string, hadPrev bool) {
	var err error
	if hadPrev {
		err = m.creds.Set(id, prev)
	} else {
		err = m.creds.Delete(id)
	}
	if err != nil {
		m.log.Error("could not restore credential after failed save", "profile_id", id, "err", err)
	}
}

// Delete removes a profile and its stored credential. The credential is removed first, so a
// failure can leave a profile without a password (recoverable) but never an orphaned secret.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	profiles, err := m.store.Load()
	if err != nil {
		return err
	}
	i := indexOf(profiles, id)
	if i < 0 {
		return ErrNotFound
	}
	if err := m.creds.Delete(id); err != nil {
		return err
	}
	name := profiles[i].Name
	if err := m.store.Save(slices.Delete(profiles, i, i+1)); err != nil {
		return err
	}
	m.log.Info("connection profile deleted", "profile_id", id, "name", name)
	return nil
}

// HasPassword reports whether a password is stored for the profile.
func (m *Manager) HasPassword(id string) (bool, error) {
	_, err := m.creds.Get(id)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoCredential):
		return false, nil
	default:
		return false, err
	}
}

// Password returns the stored password for connecting. It must never be passed to the UI,
// logged, or included in errors.
func (m *Manager) Password(id string) (string, error) {
	return m.creds.Get(id)
}

func indexOf(profiles []Profile, id string) int {
	return slices.IndexFunc(profiles, func(p Profile) bool { return p.ID == id })
}

// validateUnique validates p and checks that no other profile has the same name.
func validateUnique(p Profile, existing []Profile) error {
	err := p.Validate()
	var ve *ValidationError
	if err != nil && !errors.As(err, &ve) {
		return err
	}
	for _, other := range existing {
		if other.ID != p.ID && strings.EqualFold(other.Name, p.Name) && p.Name != "" {
			if ve == nil {
				ve = &ValidationError{Fields: map[string]string{}}
			}
			ve.Fields["name"] = fmt.Sprintf("a connection named %q already exists", other.Name)
			break
		}
	}
	if ve != nil {
		return ve
	}
	return nil
}
