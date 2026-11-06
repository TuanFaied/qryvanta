package connection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/TuanFaied/qryvanta/internal/security"
)

// Driver opens database handles for one database product. It is implemented by dialect
// packages (internal/dialect/mssql); this package defines it because it is the consumer.
type Driver interface {
	// Open returns a handle for the profile. It must not contact the server; Pool pings it.
	// secret is the password for SQL authentication and empty otherwise.
	Open(p Profile, secret string, opts OpenOptions) (*sql.DB, error)
	// ServerInfo describes the server and session behind db.
	ServerInfo(ctx context.Context, db *sql.DB) (ServerInfo, error)
}

// OpenOptions carries application settings that affect how a connection is opened.
type OpenOptions struct {
	// ConnectTimeout applies when the profile does not set its own.
	ConnectTimeout time.Duration
	// AppName is reported to the server (visible in sys.dm_exec_sessions.program_name).
	AppName string
}

// ServerInfo is what a successful connection test reports.
type ServerInfo struct {
	ServerName     string `json:"serverName"`
	ProductVersion string `json:"productVersion"`
	ProductLevel   string `json:"productLevel"`
	Edition        string `json:"edition"`
	EngineEdition  int    `json:"engineEdition"`
	// Cloud is true for Azure SQL Database and Azure SQL Managed Instance.
	Cloud    bool   `json:"cloud"`
	Database string `json:"database"`
	Login    string `json:"login"`
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	Server ServerInfo `json:"server"`
	// LatencyMs is the measured time, in milliseconds, to open the connection and run the
	// server-info query.
	LatencyMs int64 `json:"latencyMs"`
	// Access is the login's write access in the connection's database. It is nil when no
	// AccessChecker is configured.
	Access *WriteAccess `json:"access"`
}

// ErrPasswordRequired is returned when a SQL-authentication profile has no stored password.
var ErrPasswordRequired = errors.New("a password is required for this connection")

// ErrNotConnected is returned when a profile has no open connection.
var ErrNotConnected = errors.New("not connected")

// CertificateHint is appended by drivers to TLS certificate validation errors, so CLI users
// see what to do. Front ends with their own guidance can strip it with TrimCertificateHint.
const CertificateHint = "The server's TLS certificate is not trusted by this computer: install a " +
	"trusted certificate on the server, or, for a local development server with a self-signed " +
	"certificate, enable \"Trust server certificate\" for this connection"

// TrimCertificateHint removes CertificateHint from an error message.
func TrimCertificateHint(msg string) string {
	return strings.TrimSuffix(msg, ". "+CertificateHint)
}

// ServerError is a database error reported by the server, with secrets removed.
type ServerError struct {
	Number  int32  `json:"number"`
	State   uint8  `json:"state"`
	Class   uint8  `json:"class"`
	Message string `json:"message"`
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("SQL Server error %d (state %d, class %d): %s", e.Number, e.State, e.Class, e.Message)
}

// Pool limits for each connected profile. Debugging sessions need few connections; small
// pools also keep Qryvanta from adding noticeable load to shared servers.
const (
	MaxOpenConnsPerProfile = 4
	MaxIdleConnsPerProfile = 2
	ConnMaxIdleTime        = 5 * time.Minute
)

// Pool keeps one *sql.DB per connected profile.
type Pool struct {
	manager *Manager
	driver  Driver
	opts    func() OpenOptions
	log     *slog.Logger
	access  AccessChecker

	mu    sync.Mutex
	conns map[string]*pooled
}

type pooled struct {
	db     *sql.DB
	server ServerInfo
	since  time.Time
}

// NewPool creates a pool. opts is called on every open so changed settings take effect on the
// next connection. A nil logger discards log output.
func NewPool(manager *Manager, driver Driver, opts func() OpenOptions, log *slog.Logger) *Pool {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Pool{manager: manager, driver: driver, opts: opts, log: log, conns: map[string]*pooled{}}
}

// SetAccessChecker makes connection tests report the login's write access. Call it before
// the pool is used.
func (p *Pool) SetAccessChecker(c AccessChecker) { p.access = c }

// CheckAccess runs the write-access check on db. A failed check is reported as
// AccessUnknown with a sanitized message, never as an error, because a connection whose
// permissions cannot be read is still usable.
func (p *Pool) CheckAccess(ctx context.Context, db *sql.DB, database string) *WriteAccess {
	if p.access == nil {
		return nil
	}
	res, err := p.access.WriteAccess(ctx, db, database)
	if err != nil {
		p.log.Warn("write-access check failed", "database", database, "err", err)
		return &WriteAccess{Database: database, Level: AccessUnknown, Reasons: []AccessReason{}, Error: sanitize(err, "").Error()}
	}
	return &res
}

// CheckAccessFor runs the write-access check for a connected profile.
func (p *Pool) CheckAccessFor(ctx context.Context, id, database string) (*WriteAccess, error) {
	db, err := p.DB(id)
	if err != nil {
		return nil, err
	}
	return p.CheckAccess(ctx, db, database), nil
}

// Test opens a temporary connection to a saved profile, reports the server, and closes it.
func (p *Pool) Test(ctx context.Context, id string) (TestResult, error) {
	prof, secret, err := p.credentials(id)
	if err != nil {
		return TestResult{}, err
	}
	return p.test(ctx, prof, secret)
}

// TestProfile tests an unsaved profile (for example while the user is filling in the
// connection dialog). password is used as given; nothing is stored.
func (p *Pool) TestProfile(ctx context.Context, prof Profile, password string) (TestResult, error) {
	prof = prof.Normalize()
	if err := prof.Validate(); err != nil {
		return TestResult{}, err
	}
	if !prof.UsesPassword() {
		password = ""
	} else if password == "" {
		return TestResult{}, ErrPasswordRequired
	}
	return p.test(ctx, prof, password)
}

func (p *Pool) test(ctx context.Context, prof Profile, secret string) (TestResult, error) {
	start := time.Now()
	db, info, err := p.open(ctx, prof, secret)
	if err != nil {
		p.log.Info("connection test failed", "profile_id", prof.ID, "name", prof.Name, "err", err)
		return TestResult{}, err
	}
	latency := time.Since(start)
	access := p.CheckAccess(ctx, db, info.Database)
	_ = db.Close()
	p.log.Info("connection test succeeded", "profile_id", prof.ID, "name", prof.Name,
		"server_version", info.ProductVersion, "latency_ms", latency.Milliseconds())
	return TestResult{Server: info, LatencyMs: latency.Milliseconds(), Access: access}, nil
}

// Connect opens (or reuses) the pooled connection for a profile.
func (p *Pool) Connect(ctx context.Context, id string) (ServerInfo, error) {
	p.mu.Lock()
	if c, ok := p.conns[id]; ok {
		p.mu.Unlock()
		return c.server, nil
	}
	p.mu.Unlock()

	prof, secret, err := p.credentials(id)
	if err != nil {
		return ServerInfo{}, err
	}
	db, info, err := p.open(ctx, prof, secret)
	if err != nil {
		return ServerInfo{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.conns[id]; ok {
		// Another caller connected first; keep theirs.
		_ = db.Close()
		return existing.server, nil
	}
	p.conns[id] = &pooled{db: db, server: info, since: time.Now()}
	p.log.Info("connected", "profile_id", id, "name", prof.Name, "server_version", info.ProductVersion)
	return info, nil
}

// DB returns the open handle for a connected profile.
func (p *Pool) DB(id string) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.conns[id]
	if !ok {
		return nil, ErrNotConnected
	}
	return c.db, nil
}

// IsConnected reports whether a profile has an open pooled connection.
func (p *Pool) IsConnected(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.conns[id]
	return ok
}

// Disconnect closes a profile's pooled connection. Disconnecting a profile that is not
// connected is not an error.
func (p *Pool) Disconnect(id string) error {
	p.mu.Lock()
	c, ok := p.conns[id]
	delete(p.conns, id)
	p.mu.Unlock()
	if !ok {
		return nil
	}
	p.log.Info("disconnected", "profile_id", id)
	return c.db.Close()
}

// Close disconnects every profile.
func (p *Pool) Close() error {
	p.mu.Lock()
	conns := p.conns
	p.conns = map[string]*pooled{}
	p.mu.Unlock()
	var errs []error
	for id, c := range conns {
		if err := c.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Pool) credentials(id string) (Profile, string, error) {
	prof, err := p.manager.Get(id)
	if err != nil {
		return Profile{}, "", err
	}
	if !prof.UsesPassword() {
		return prof, "", nil
	}
	secret, err := p.manager.Password(id)
	if errors.Is(err, ErrNoCredential) {
		return Profile{}, "", ErrPasswordRequired
	}
	if err != nil {
		return Profile{}, "", err
	}
	return prof, secret, nil
}

// open creates the handle, verifies it with the server-info query and applies pool limits.
func (p *Pool) open(ctx context.Context, prof Profile, secret string) (*sql.DB, ServerInfo, error) {
	opts := p.opts()
	timeout := opts.ConnectTimeout
	if prof.ConnectTimeoutSeconds > 0 {
		timeout = time.Duration(prof.ConnectTimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	opts.ConnectTimeout = timeout

	db, err := p.driver.Open(prof, secret, opts)
	if err != nil {
		return nil, ServerInfo{}, sanitize(err, secret)
	}
	db.SetMaxOpenConns(MaxOpenConnsPerProfile)
	db.SetMaxIdleConns(MaxIdleConnsPerProfile)
	db.SetConnMaxIdleTime(ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	info, err := p.driver.ServerInfo(ctx, db)
	if err != nil {
		_ = db.Close()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ServerInfo{}, fmt.Errorf("could not connect within %s: %w", timeout, sanitize(err, secret))
		}
		return nil, ServerInfo{}, sanitize(err, secret)
	}
	return db, info, nil
}

// sanitize removes the password and connection-string secrets from an error while keeping
// structured server errors (number, state, class) and sentinel errors intact.
func sanitize(err error, secret string) error {
	if err == nil {
		return nil
	}
	var se *ServerError
	if errors.As(err, &se) {
		clean := *se
		clean.Message = scrub(se.Message, secret)
		return &clean
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &sanitizedError{msg: scrub(err.Error(), secret), cause: ctxCause(err)}
	}
	return &sanitizedError{msg: scrub(err.Error(), secret)}
}

func ctxCause(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return context.DeadlineExceeded
}

func scrub(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, security.Mask)
	}
	return security.RedactString(s)
}

// sanitizedError replaces a driver error whose text may have contained secrets. Only
// context sentinels are kept in the chain, so errors.Is(err, context.Canceled) still works.
type sanitizedError struct {
	msg   string
	cause error
}

func (e *sanitizedError) Error() string { return e.msg }
func (e *sanitizedError) Unwrap() error { return e.cause }
