package connection

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secret = "S3cr3t-P@ssw0rd!"

func newTestManager(t *testing.T) (*Manager, *MemoryStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	creds := NewMemoryStore()
	m := NewManager(NewStore(path), creds, nil)
	m.now = func() time.Time { return time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC) }
	return m, creds, path
}

func TestCreateStoresPasswordOnlyInCredentialStore(t *testing.T) {
	m, creds, path := newTestManager(t)
	p, err := m.Create(validSQLProfile(), secret)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.CreatedAt.IsZero() || p.Encrypt != EncryptMandatory {
		t.Errorf("manager did not assign id/timestamps/defaults: %+v", p)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(strings.ToLower(string(data)), "password") {
		t.Fatalf("connections file contains password data:\n%s", data)
	}
	got, err := m.Password(p.ID)
	if err != nil || got != secret {
		t.Fatalf("Password() = %q, %v", got, err)
	}
	if creds.Len() != 1 {
		t.Errorf("credential count = %d, want 1", creds.Len())
	}
}

func TestCreateWindowsAuthNeverStoresPassword(t *testing.T) {
	m, creds, _ := newTestManager(t)
	p, err := m.Create(Profile{Name: "win", Server: "db1", Auth: AuthWindows}, "should-be-ignored")
	if err != nil {
		t.Fatal(err)
	}
	if creds.Len() != 0 {
		t.Error("Windows authentication profile stored a password")
	}
	if has, _ := m.HasPassword(p.ID); has {
		t.Error("HasPassword = true for Windows authentication")
	}
}

func TestCreateRejectsInvalidAndDuplicateNames(t *testing.T) {
	m, creds, _ := newTestManager(t)
	if _, err := m.Create(validSQLProfile(), secret); err != nil {
		t.Fatal(err)
	}
	dup := validSQLProfile()
	dup.Name = "LOCAL DEV"
	_, err := m.Create(dup, "other")
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Fields["name"], "already exists") {
		t.Fatalf("duplicate name: err = %v", err)
	}
	if _, err := m.Create(Profile{Name: "x"}, "pw"); !errors.As(err, &ve) {
		t.Fatalf("invalid profile: err = %v", err)
	}
	if creds.Len() != 1 {
		t.Errorf("rejected profiles stored credentials: %d", creds.Len())
	}
}

// flakyStore wraps a ProfileStore and fails Save when failSave is set.
type flakyStore struct {
	ProfileStore
	failSave bool
}

func (s *flakyStore) Save(p []Profile) error {
	if s.failSave {
		return errors.New("disk full")
	}
	return s.ProfileStore.Save(p)
}

func newFlakyManager(t *testing.T) (*Manager, *MemoryStore, *flakyStore) {
	t.Helper()
	store := &flakyStore{ProfileStore: NewStore(filepath.Join(t.TempDir(), FileName))}
	creds := NewMemoryStore()
	return NewManager(store, creds, nil), creds, store
}

func TestCreateRollsBackCredentialWhenSaveFails(t *testing.T) {
	m, creds, store := newFlakyManager(t)
	store.failSave = true
	if _, err := m.Create(validSQLProfile(), secret); err == nil {
		t.Fatal("expected an error")
	}
	if creds.Len() != 0 {
		t.Error("credential left behind after failed create")
	}
}

func TestCreateFailsCleanlyWhenCredentialStoreFails(t *testing.T) {
	m, creds, _ := newTestManager(t)
	creds.FailSet = errors.New("credential manager locked")
	if _, err := m.Create(validSQLProfile(), secret); err == nil {
		t.Fatal("expected an error")
	}
	profiles, _ := m.List()
	if len(profiles) != 0 {
		t.Error("profile saved even though its password could not be stored")
	}
}

func TestUpdatePasswordChanges(t *testing.T) {
	m, _, _ := newTestManager(t)
	p, _ := m.Create(validSQLProfile(), secret)

	p.Database = "Sales"
	if _, err := m.Update(p, KeepPassword); err != nil {
		t.Fatal(err)
	}
	if pw, _ := m.Password(p.ID); pw != secret {
		t.Error("KeepPassword changed the password")
	}

	if _, err := m.Update(p, NewPassword("new-one")); err != nil {
		t.Fatal(err)
	}
	if pw, _ := m.Password(p.ID); pw != "new-one" {
		t.Error("NewPassword did not replace the password")
	}

	if _, err := m.Update(p, NewPassword("")); err != nil {
		t.Fatal(err)
	}
	if has, _ := m.HasPassword(p.ID); has {
		t.Error("empty NewPassword did not remove the password")
	}

	got, _ := m.Get(p.ID)
	if got.Database != "Sales" || !got.CreatedAt.Equal(p.CreatedAt) {
		t.Errorf("unexpected stored profile %+v", got)
	}
}

func TestUpdateToWindowsAuthRemovesPassword(t *testing.T) {
	m, creds, _ := newTestManager(t)
	p, _ := m.Create(validSQLProfile(), secret)
	p.Auth = AuthWindows
	updated, err := m.Update(p, KeepPassword)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "" || creds.Len() != 0 {
		t.Errorf("switching to Windows auth kept username or password: %+v, creds=%d", updated, creds.Len())
	}
}

func TestUpdateRestoresCredentialWhenSaveFails(t *testing.T) {
	m, creds, store := newFlakyManager(t)
	withPw, _ := m.Create(validSQLProfile(), secret)
	noPw, _ := m.Create(Profile{Name: "No password yet", Server: "db2", Auth: AuthSQL, Username: "u"}, "")
	store.failSave = true

	if _, err := m.Update(withPw, NewPassword("replacement")); err == nil {
		t.Fatal("expected save failure")
	}
	if pw, _ := m.Password(withPw.ID); pw != secret {
		t.Errorf("password after failed update = %q, want original", pw)
	}

	if _, err := m.Update(noPw, NewPassword("first")); err == nil {
		t.Fatal("expected save failure")
	}
	if has, _ := m.HasPassword(noPw.ID); has || creds.Len() != 1 {
		t.Error("failed update left a new credential behind")
	}
}

func TestDeleteRemovesProfileAndCredential(t *testing.T) {
	m, creds, _ := newTestManager(t)
	a, _ := m.Create(validSQLProfile(), secret)
	b, _ := m.Create(Profile{Name: "Another", Server: "db2", Auth: AuthSQL, Username: "u"}, "pw2")

	if err := m.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(deleted) = %v, want ErrNotFound", err)
	}
	if has, _ := m.HasPassword(a.ID); has {
		t.Error("credential not deleted")
	}
	if pw, _ := m.Password(b.ID); pw != "pw2" || creds.Len() != 1 {
		t.Error("deleting one profile affected another")
	}
	if err := m.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteKeepsProfileWhenCredentialDeleteFails(t *testing.T) {
	m, creds, _ := newTestManager(t)
	p, _ := m.Create(validSQLProfile(), secret)
	creds.FailDelete = errors.New("access denied")
	if err := m.Delete(p.ID); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := m.Get(p.ID); err != nil {
		t.Error("profile removed although its credential could not be deleted")
	}
}

func TestListSortsByName(t *testing.T) {
	m, _, _ := newTestManager(t)
	for _, name := range []string{"zeta", "Alpha", "beta"} {
		if _, err := m.Create(Profile{Name: name, Server: "db", Auth: AuthWindows}, ""); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "Alpha,beta,zeta" {
		t.Errorf("order = %v", names)
	}
}

func TestStoreRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion":42,"profiles":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Load() = %v", err)
	}
}
