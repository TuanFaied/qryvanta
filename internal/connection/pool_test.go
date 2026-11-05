package connection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDriver implements Driver without a database. Open returns a real *sql.DB backed by a
// connector that is never dialled, so Pool's pool settings and Close calls are exercised.
type fakeDriver struct {
	opened   atomic.Int32
	lastOpts OpenOptions
	lastPass string
	infoErr  error
	delay    time.Duration
}

func (f *fakeDriver) Open(_ Profile, secret string, opts OpenOptions) (*sql.DB, error) {
	f.opened.Add(1)
	f.lastOpts, f.lastPass = opts, secret
	return sql.OpenDB(nopConnector{}), nil
}

func (f *fakeDriver) ServerInfo(ctx context.Context, _ *sql.DB) (ServerInfo, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ServerInfo{}, ctx.Err()
		}
	}
	if f.infoErr != nil {
		return ServerInfo{}, f.infoErr
	}
	return ServerInfo{ServerName: "DB1", ProductVersion: "16.0.4135.4", Login: "reader"}, nil
}

type nopConnector struct{}

func (nopConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("not dialled")
}
func (nopConnector) Driver() driver.Driver { return nil }

func newTestPool(t *testing.T) (*Pool, *Manager, *fakeDriver) {
	t.Helper()
	m, _, _ := newTestManager(t)
	d := &fakeDriver{}
	p := NewPool(m, d, func() OpenOptions { return OpenOptions{ConnectTimeout: 7 * time.Second, AppName: "Qryvanta"} }, nil)
	t.Cleanup(func() { _ = p.Close() })
	return p, m, d
}

func TestPoolTestUsesStoredPasswordAndClosesHandle(t *testing.T) {
	p, m, d := newTestPool(t)
	prof, _ := m.Create(validSQLProfile(), secret)

	res, err := p.Test(context.Background(), prof.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Server.ProductVersion != "16.0.4135.4" || res.LatencyMs < 0 {
		t.Errorf("unexpected result %+v", res)
	}
	if d.lastPass != secret || d.lastOpts.ConnectTimeout != 7*time.Second {
		t.Errorf("driver got password=%v timeout=%v", d.lastPass == secret, d.lastOpts.ConnectTimeout)
	}
	if p.IsConnected(prof.ID) {
		t.Error("Test must not keep the connection open")
	}
}

func TestPoolProfileTimeoutOverridesDefault(t *testing.T) {
	p, m, d := newTestPool(t)
	prof := validSQLProfile()
	prof.ConnectTimeoutSeconds = 42
	saved, _ := m.Create(prof, secret)
	if _, err := p.Test(context.Background(), saved.ID); err != nil {
		t.Fatal(err)
	}
	if d.lastOpts.ConnectTimeout != 42*time.Second {
		t.Errorf("timeout = %v, want 42s", d.lastOpts.ConnectTimeout)
	}
}

func TestPoolRequiresPasswordForSQLAuth(t *testing.T) {
	p, m, d := newTestPool(t)
	prof, _ := m.Create(validSQLProfile(), "")
	if _, err := p.Test(context.Background(), prof.ID); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("Test = %v, want ErrPasswordRequired", err)
	}
	if _, err := p.TestProfile(context.Background(), validSQLProfile(), ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("TestProfile = %v, want ErrPasswordRequired", err)
	}
	if d.opened.Load() != 0 {
		t.Error("driver opened without a password")
	}
}

func TestPoolWindowsAuthSendsNoSecret(t *testing.T) {
	p, _, d := newTestPool(t)
	_, err := p.TestProfile(context.Background(), Profile{Name: "w", Server: "db1", Auth: AuthWindows}, "typed-anyway")
	if err != nil {
		t.Fatal(err)
	}
	if d.lastPass != "" {
		t.Error("Windows authentication passed a password to the driver")
	}
}

func TestPoolConnectReusesAndDisconnects(t *testing.T) {
	p, m, d := newTestPool(t)
	prof, _ := m.Create(validSQLProfile(), secret)
	ctx := context.Background()

	if _, err := p.Connect(ctx, prof.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Connect(ctx, prof.ID); err != nil {
		t.Fatal(err)
	}
	if d.opened.Load() != 1 {
		t.Errorf("opened %d handles, want 1 (reuse)", d.opened.Load())
	}
	db, err := p.DB(prof.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := db.Stats().MaxOpenConnections; got != MaxOpenConnsPerProfile {
		t.Errorf("MaxOpenConnections = %d", got)
	}
	if err := p.Disconnect(prof.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.DB(prof.ID); !errors.Is(err, ErrNotConnected) {
		t.Errorf("DB after disconnect = %v", err)
	}
	if err := p.Disconnect(prof.ID); err != nil {
		t.Errorf("second disconnect = %v", err)
	}
}

func TestPoolErrorsNeverContainThePassword(t *testing.T) {
	p, m, d := newTestPool(t)
	prof, _ := m.Create(validSQLProfile(), secret)

	d.infoErr = errors.New("login failed: server=db1;password=" + secret + "; raw " + secret)
	_, err := p.Test(context.Background(), prof.ID)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks password: %v", err)
	}

	d.infoErr = &ServerError{Number: 18456, State: 1, Class: 14, Message: "Login failed. Tried " + secret}
	_, err = p.Test(context.Background(), prof.ID)
	var se *ServerError
	if !errors.As(err, &se) || se.Number != 18456 {
		t.Fatalf("server error number lost: %v", err)
	}
	if strings.Contains(se.Message, secret) {
		t.Fatalf("server error message leaks password: %q", se.Message)
	}
}

func TestPoolTimesOut(t *testing.T) {
	m, _, _ := newTestManager(t)
	d := &fakeDriver{delay: time.Minute}
	p := NewPool(m, d, func() OpenOptions { return OpenOptions{} }, nil)
	prof := validSQLProfile()
	prof.ConnectTimeoutSeconds = 1
	saved, _ := m.Create(prof, secret)

	start := time.Now()
	_, err := p.Test(context.Background(), saved.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "within 1s") {
		t.Errorf("error should state the timeout: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("timeout not applied")
	}
}

func TestPoolCancellation(t *testing.T) {
	m, _, _ := newTestManager(t)
	d := &fakeDriver{delay: time.Minute}
	p := NewPool(m, d, func() OpenOptions { return OpenOptions{ConnectTimeout: time.Minute} }, nil)
	saved, _ := m.Create(validSQLProfile(), secret)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err := p.Test(ctx, saved.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
