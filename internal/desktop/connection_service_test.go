package desktop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

const secret = "UI-S3cret!"

// fakeDriver records the secret it was given and returns a configurable result.
type fakeDriver struct {
	gotSecret string
	err       error
}

func (f *fakeDriver) Open(_ connection.Profile, s string, _ connection.OpenOptions) (*sql.DB, error) {
	f.gotSecret = s
	return sql.OpenDB(nopConnector{}), nil
}

func (f *fakeDriver) ServerInfo(context.Context, *sql.DB) (connection.ServerInfo, error) {
	if f.err != nil {
		return connection.ServerInfo{}, f.err
	}
	return connection.ServerInfo{ServerName: "DB1", ProductVersion: "16.0.4305.5", Login: "reader", Database: "Sales"}, nil
}

type nopConnector struct{}

func (nopConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("not dialled")
}
func (nopConnector) Driver() driver.Driver { return nil }

func newConnService(t *testing.T) (*ConnectionService, *fakeDriver) {
	t.Helper()
	drv := &fakeDriver{}
	core, err := app.New(app.Options{
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		DisableFileLog: true,
		Credentials:    connection.NewMemoryStore(),
		Driver:         drv,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return NewConnectionService(core), drv
}

func sqlInput() ConnectionInput {
	return ConnectionInput{
		Profile:        connection.Profile{Name: "Sales", Server: "db1", Database: "Sales", Auth: connection.AuthSQL, Username: "reader"},
		PasswordAction: PasswordSet,
		Password:       secret,
	}
}

func TestSaveCreateAndListNeverReturnPasswords(t *testing.T) {
	s, _ := newConnService(t)
	res, err := s.Save(sqlInput())
	if err != nil || !res.OK || res.Connection == nil {
		t.Fatalf("Save = %+v, %v", res, err)
	}
	if !res.Connection.HasPassword || res.Connection.Profile.ID == "" {
		t.Errorf("unexpected DTO %+v", res.Connection)
	}

	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	for _, v := range []any{res, list} {
		data, _ := json.Marshal(v)
		if strings.Contains(string(data), secret) {
			t.Fatalf("response contains the password: %s", data)
		}
		if strings.Contains(strings.ToLower(string(data)), `"password"`) {
			t.Fatalf("response has a password field: %s", data)
		}
	}
}

func TestSaveReturnsFieldErrorsAsData(t *testing.T) {
	s, _ := newConnService(t)
	in := sqlInput()
	in.Profile.Server = ""
	in.Profile.Username = ""
	res, err := s.Save(in)
	if err != nil || res.OK {
		t.Fatalf("Save = %+v, %v", res, err)
	}
	if res.FieldErrors["server"] == "" || res.FieldErrors["username"] == "" {
		t.Errorf("FieldErrors = %v", res.FieldErrors)
	}
}

func TestSaveUpdatePasswordActions(t *testing.T) {
	s, drv := newConnService(t)
	created, _ := s.Save(sqlInput())
	id := created.Connection.Profile.ID

	// Keep: the stored password is still used.
	upd := ConnectionInput{Profile: created.Connection.Profile, PasswordAction: PasswordKeep}
	upd.Profile.Database = "Archive"
	res, err := s.Save(upd)
	if err != nil || !res.OK || !res.Connection.HasPassword || res.Connection.Profile.Database != "Archive" {
		t.Fatalf("keep: %+v, %v", res, err)
	}
	if r, _ := s.Connect(context.Background(), id); !r.OK || drv.gotSecret != secret {
		t.Fatalf("connect after keep: %+v secretOK=%v", r, drv.gotSecret == secret)
	}

	// Saving changed settings disconnects the stale handle.
	if list, _ := s.List(); !list[0].Connected {
		t.Fatal("expected connected")
	}
	if res, _ := s.Save(upd); !res.OK {
		t.Fatal("save failed")
	}
	if list, _ := s.List(); list[0].Connected {
		t.Error("saving a connection must close its open handle")
	}

	// Clear: the password is removed.
	upd.PasswordAction = PasswordClear
	if res, _ := s.Save(upd); res.Connection.HasPassword {
		t.Error("clear did not remove the password")
	}
}

func TestTestUsesStoredPasswordOnlyWhenKept(t *testing.T) {
	s, drv := newConnService(t)
	created, _ := s.Save(sqlInput())

	in := ConnectionInput{Profile: created.Connection.Profile, PasswordAction: PasswordKeep}
	res, err := s.Test(context.Background(), in)
	if err != nil || !res.OK || drv.gotSecret != secret {
		t.Fatalf("Test(keep) = %+v, %v", res, err)
	}
	if res.Server.ProductVersion != "16.0.4305.5" {
		t.Errorf("server info not returned: %+v", res.Server)
	}

	in.PasswordAction = PasswordSet
	in.Password = "typed-in-dialog"
	if res, _ := s.Test(context.Background(), in); !res.OK || drv.gotSecret != "typed-in-dialog" {
		t.Fatalf("Test(set) used %q", drv.gotSecret)
	}

	in.PasswordAction = PasswordClear
	res, _ = s.Test(context.Background(), in)
	if res.OK || res.Error == nil || res.Error.Code != ErrPasswordRequired {
		t.Fatalf("Test(clear) = %+v", res)
	}
}

func TestTestReportsValidationAndClassifiedErrors(t *testing.T) {
	s, drv := newConnService(t)
	bad := sqlInput()
	bad.Profile.Encrypt = connection.EncryptStrict
	bad.Profile.TrustServerCertificate = true
	res, err := s.Test(context.Background(), bad)
	if err != nil || res.OK || res.FieldErrors["trustServerCertificate"] == "" {
		t.Fatalf("Test(invalid) = %+v, %v", res, err)
	}

	cases := []struct {
		err  error
		code ErrorCode
		num  int32
	}{
		{&connection.ServerError{Number: 18456, Class: 14, Message: "Login failed for user 'reader'."}, ErrLoginFailed, 18456},
		{&connection.ServerError{Number: 4060, Message: "Cannot open database"}, ErrLoginFailed, 4060},
		{&connection.ServerError{Number: 208, Message: "Invalid object name"}, ErrServer, 208},
		{errors.New("TLS Handshake failed: x509: certificate signed by unknown authority. " + connection.CertificateHint), ErrCertificate, 0},
		{errors.New("dial tcp 10.0.0.1:1433: connectex: No connection could be made"), ErrNetwork, 0},
		{context.Canceled, ErrCancelled, 0},
	}
	for _, c := range cases {
		drv.err = c.err
		res, _ := s.Test(context.Background(), sqlInput())
		if res.OK || res.Error == nil || res.Error.Code != c.code || res.Error.ServerErrorNumber != c.num {
			t.Errorf("%v: got %+v, want code %s number %d", c.err, res.Error, c.code, c.num)
		}
		if c.code == ErrCertificate && strings.Contains(res.Error.Message, "Trust server certificate") {
			t.Errorf("certificate message still contains the CLI hint: %q", res.Error.Message)
		}
	}
}

func TestConnectDisconnectDelete(t *testing.T) {
	s, _ := newConnService(t)
	created, _ := s.Save(sqlInput())
	id := created.Connection.Profile.ID

	if r, _ := s.Connect(context.Background(), id); !r.OK || r.Server.Database != "Sales" {
		t.Fatalf("Connect = %+v", r)
	}
	if err := s.Disconnect(id); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(); list[0].Connected {
		t.Error("still connected after Disconnect")
	}
	if _, err := s.Connect(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Error("connection not deleted")
	}
	if r, _ := s.Connect(context.Background(), id); r.OK || r.Error.Code != ErrNotFound {
		t.Errorf("Connect(deleted) = %+v", r)
	}
	if err := s.Delete(id); err != nil {
		t.Errorf("deleting twice should be harmless: %v", err)
	}
}
