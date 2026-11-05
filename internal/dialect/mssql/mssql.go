// Package mssql is the Microsoft SQL Server dialect: connection handling through
// github.com/microsoft/go-mssqldb and SQL Server specific queries.
package mssql

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	// Named pipes and shared memory let Qryvanta reach local instances whose TCP listener is
	// disabled, which is the default for SQL Server Express. The driver tries TCP first and
	// falls back to these on Windows; on other platforms the packages are no-ops.
	_ "github.com/microsoft/go-mssqldb/namedpipe"
	_ "github.com/microsoft/go-mssqldb/sharedmemory"

	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/security"
)

// Driver implements connection.Driver for SQL Server and Azure SQL.
type Driver struct{}

// New returns the SQL Server driver.
func New() Driver { return Driver{} }

var _ connection.Driver = Driver{}

// Open builds a connector from the profile. It does not contact the server.
func (Driver) Open(p connection.Profile, secret string, opts connection.OpenOptions) (*sql.DB, error) {
	cfg, err := Config(p, secret, opts)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(mssqldb.NewConnectorConfig(cfg)), nil
}

// Config converts a profile into the driver's configuration.
//
// The DSN is assembled with net/url (which escapes every component) and then parsed by the
// driver's own msdsn.Parse, so the driver applies its usual TLS and default handling. Values
// are never concatenated into a connection string.
func Config(p connection.Profile, secret string, opts connection.OpenOptions) (msdsn.Config, error) {
	u, err := dsnURL(p, secret, opts)
	if err != nil {
		return msdsn.Config{}, err
	}
	cfg, err := msdsn.Parse(u.String())
	if err != nil {
		// msdsn errors describe the offending parameter, not the password; scrub anyway and
		// never echo the DSN.
		msg := err.Error()
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, security.Mask)
		}
		return msdsn.Config{}, fmt.Errorf("invalid connection settings: %s", security.RedactString(msg))
	}
	return cfg, nil
}

func dsnURL(p connection.Profile, secret string, opts connection.OpenOptions) (*url.URL, error) {
	host, instance, _ := strings.Cut(p.Server, `\`)
	if host == "" {
		return nil, errors.New("server is required")
	}
	if host == "." || strings.EqualFold(host, "(local)") {
		host = "localhost"
	}
	if p.Port > 0 {
		host = host + ":" + strconv.Itoa(p.Port)
	}

	u := &url.URL{Scheme: "sqlserver", Host: host}
	if instance != "" {
		u.Path = "/" + instance
	}
	switch p.Auth {
	case connection.AuthSQL:
		u.User = url.UserPassword(p.Username, secret)
	case connection.AuthWindows:
		// No user: go-mssqldb uses integrated authentication (SSPI on Windows) as the
		// current OS user.
	default:
		return nil, fmt.Errorf("unsupported authentication method %q", p.Auth)
	}

	q := url.Values{}
	if p.Database != "" {
		q.Set(msdsn.Database, p.Database)
	}
	switch p.Encrypt {
	case connection.EncryptStrict:
		q.Set(msdsn.Encrypt, "strict")
	case connection.EncryptOptional:
		q.Set(msdsn.Encrypt, "false")
	default:
		q.Set(msdsn.Encrypt, "true")
	}
	if p.TrustServerCertificate {
		q.Set(msdsn.TrustServerCertificate, "true")
	}
	if p.ReadOnlyIntent {
		q.Set(msdsn.ApplicationIntent, "ReadOnly")
	}
	if opts.ConnectTimeout > 0 {
		secs := int(opts.ConnectTimeout.Seconds())
		if secs < 1 {
			secs = 1
		}
		q.Set(msdsn.DialTimeout, strconv.Itoa(secs))
	}
	appName := opts.AppName
	if appName == "" {
		appName = "Qryvanta"
	}
	q.Set(msdsn.AppName, appName)
	u.RawQuery = q.Encode()
	return u, nil
}

// serverInfoQuery reads properties every login can see. It needs no VIEW SERVER STATE.
const serverInfoQuery = `SELECT
	CAST(@@SERVERNAME AS nvarchar(256)),
	CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
	CAST(SERVERPROPERTY('ProductLevel') AS nvarchar(128)),
	CAST(SERVERPROPERTY('Edition') AS nvarchar(128)),
	CAST(SERVERPROPERTY('EngineEdition') AS int),
	DB_NAME(),
	SUSER_SNAME()`

// ServerInfo runs a small metadata query, which also verifies that login succeeded.
func (Driver) ServerInfo(ctx context.Context, db *sql.DB) (connection.ServerInfo, error) {
	var (
		info                                connection.ServerInfo
		serverName, version, level, edition sql.NullString
		database, login                     sql.NullString
		engineEdition                       sql.NullInt64
	)
	err := db.QueryRowContext(ctx, serverInfoQuery).Scan(
		&serverName, &version, &level, &edition, &engineEdition, &database, &login)
	if err != nil {
		return connection.ServerInfo{}, convertError(err)
	}
	info.ServerName = serverName.String
	info.ProductVersion = version.String
	info.ProductLevel = level.String
	info.Edition = edition.String
	info.EngineEdition = int(engineEdition.Int64)
	// 5 = Azure SQL Database, 8 = Azure SQL Managed Instance, 6/11 = Synapse.
	switch info.EngineEdition {
	case 5, 6, 8, 11:
		info.Cloud = true
	}
	info.Database = database.String
	info.Login = login.String
	return info, nil
}

// convertError maps driver errors to connection.ServerError so callers can inspect the SQL
// Server error number without depending on go-mssqldb.
func convertError(err error) error {
	if err == nil {
		return nil
	}
	var me mssqldb.Error
	if errors.As(err, &me) {
		return &connection.ServerError{Number: me.Number, State: me.State, Class: me.Class, Message: me.Message}
	}
	if isCertificateError(err) {
		return fmt.Errorf("%w. %s", err, connection.CertificateHint)
	}
	return err
}

// isCertificateError reports TLS certificate validation failures. go-mssqldb wraps some TLS
// errors as text, so the message is checked as well as the error chain.
func isCertificateError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) ||
		strings.Contains(err.Error(), "x509: ")
}
