package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/connection"
)

// ConnectionService exposes connection management to the frontend.
//
// Passwords flow one way only: the frontend may send a new password in ConnectionInput, but
// no method ever returns one. Responses report HasPassword instead.
type ConnectionService struct {
	core *app.Core
}

// NewConnectionService creates the service.
func NewConnectionService(core *app.Core) *ConnectionService {
	return &ConnectionService{core: core}
}

// ConnectionDTO is a saved connection as the UI sees it.
type ConnectionDTO struct {
	Profile     connection.Profile `json:"profile"`
	HasPassword bool               `json:"hasPassword"`
	Connected   bool               `json:"connected"`
}

// PasswordAction says what to do with the stored password when saving or testing.
type PasswordAction string

const (
	// PasswordKeep keeps the stored password (none, for a new connection).
	PasswordKeep PasswordAction = "keep"
	// PasswordSet uses ConnectionInput.Password.
	PasswordSet PasswordAction = "set"
	// PasswordClear removes the stored password.
	PasswordClear PasswordAction = "clear"
)

// ConnectionInput is what the connection dialog submits. Profile.ID is empty for a new
// connection.
type ConnectionInput struct {
	Profile        connection.Profile `json:"profile"`
	PasswordAction PasswordAction     `json:"passwordAction"`
	Password       string             `json:"password"`
}

// ErrorCode classifies connection failures so the UI can show specific guidance.
type ErrorCode string

const (
	ErrPasswordRequired ErrorCode = "password_required"
	ErrLoginFailed      ErrorCode = "login_failed"
	ErrCertificate      ErrorCode = "certificate"
	ErrTimeout          ErrorCode = "timeout"
	ErrCancelled        ErrorCode = "cancelled"
	ErrServer           ErrorCode = "server"
	ErrNetwork          ErrorCode = "network"
	ErrNotFound         ErrorCode = "not_found"
)

// ConnectionError is a failure the UI can display. Message never contains secrets.
type ConnectionError struct {
	Code ErrorCode `json:"code"`
	// ServerErrorNumber is the SQL Server error number, when the server reported one.
	ServerErrorNumber int32  `json:"serverErrorNumber"`
	Message           string `json:"message"`
}

// SaveConnectionResult is returned by Save. Validation problems are data, not errors.
type SaveConnectionResult struct {
	OK          bool              `json:"ok"`
	Connection  *ConnectionDTO    `json:"connection"`
	FieldErrors map[string]string `json:"fieldErrors"`
}

// TestConnectionResult is returned by Test.
type TestConnectionResult struct {
	OK          bool                    `json:"ok"`
	Server      connection.ServerInfo   `json:"server"`
	LatencyMs   int64                   `json:"latencyMs"`
	Access      *connection.WriteAccess `json:"access"`
	Error       *ConnectionError        `json:"error"`
	FieldErrors map[string]string       `json:"fieldErrors"`
}

// ConnectResult is returned by Connect.
type ConnectResult struct {
	OK     bool                  `json:"ok"`
	Server connection.ServerInfo `json:"server"`
	Error  *ConnectionError      `json:"error"`
}

// List returns all saved connections, sorted by name.
func (s *ConnectionService) List() ([]ConnectionDTO, error) {
	profiles, err := s.core.Connections().List()
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionDTO, 0, len(profiles))
	for _, p := range profiles {
		dto, err := s.dto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// Save creates (empty ID) or updates a connection.
func (s *ConnectionService) Save(in ConnectionInput) (SaveConnectionResult, error) {
	m := s.core.Connections()
	var (
		saved connection.Profile
		err   error
	)
	if in.Profile.ID == "" {
		pw := ""
		if in.PasswordAction == PasswordSet {
			pw = in.Password
		}
		saved, err = m.Create(in.Profile, pw)
	} else {
		change := connection.KeepPassword
		switch in.PasswordAction {
		case PasswordSet:
			change = connection.NewPassword(in.Password)
		case PasswordClear:
			change = connection.NewPassword("")
		}
		saved, err = m.Update(in.Profile, change)
		if err == nil {
			// Settings changed: the open handle (if any) was built from the old profile.
			_ = s.core.Pool().Disconnect(saved.ID)
		}
	}

	var ve *connection.ValidationError
	switch {
	case errors.As(err, &ve):
		return SaveConnectionResult{OK: false, FieldErrors: ve.Fields}, nil
	case errors.Is(err, connection.ErrNotFound):
		return SaveConnectionResult{}, errors.New("this connection no longer exists")
	case err != nil:
		return SaveConnectionResult{}, err
	}
	dto, err := s.dto(saved)
	if err != nil {
		return SaveConnectionResult{}, err
	}
	return SaveConnectionResult{OK: true, Connection: &dto, FieldErrors: map[string]string{}}, nil
}

// Delete disconnects and removes a connection and its stored password.
func (s *ConnectionService) Delete(id string) error {
	_ = s.core.Pool().Disconnect(id)
	err := s.core.Connections().Delete(id)
	if errors.Is(err, connection.ErrNotFound) {
		return nil
	}
	return err
}

// Test tries the settings in the dialog without saving them. With PasswordKeep and an
// existing connection, the stored password is used.
func (s *ConnectionService) Test(ctx context.Context, in ConnectionInput) (TestConnectionResult, error) {
	p := in.Profile.Normalize()
	if err := p.Validate(); err != nil {
		var ve *connection.ValidationError
		if errors.As(err, &ve) {
			return TestConnectionResult{FieldErrors: ve.Fields}, nil
		}
		return TestConnectionResult{}, err
	}

	password := ""
	switch in.PasswordAction {
	case PasswordSet:
		password = in.Password
	case PasswordKeep:
		if p.ID != "" && p.UsesPassword() {
			stored, err := s.core.Connections().Password(p.ID)
			if err != nil && !errors.Is(err, connection.ErrNoCredential) {
				return TestConnectionResult{}, err
			}
			password = stored
		}
	}

	res, err := s.core.Pool().TestProfile(ctx, p, password)
	if err != nil {
		return TestConnectionResult{Error: classify(err), FieldErrors: map[string]string{}}, nil
	}
	return TestConnectionResult{OK: true, Server: res.Server, LatencyMs: res.LatencyMs, Access: res.Access, FieldErrors: map[string]string{}}, nil
}

// CheckWriteAccess reports whether the login of a connected profile could modify data in
// database. The UI calls it when the connection or the current database changes.
func (s *ConnectionService) CheckWriteAccess(ctx context.Context, id, database string) (*connection.WriteAccess, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	res, err := s.core.Pool().CheckAccessFor(ctx, id, database)
	if errors.Is(err, connection.ErrNotConnected) {
		return &connection.WriteAccess{Database: database, Level: connection.AccessUnknown, Reasons: []connection.AccessReason{}, Error: "not connected"}, nil
	}
	return res, err
}

// Connect opens the pooled connection for a saved profile.
func (s *ConnectionService) Connect(ctx context.Context, id string) (ConnectResult, error) {
	info, err := s.core.Pool().Connect(ctx, id)
	if err != nil {
		return ConnectResult{Error: classify(err)}, nil
	}
	return ConnectResult{OK: true, Server: info}, nil
}

// Disconnect closes a profile's pooled connection.
func (s *ConnectionService) Disconnect(id string) error {
	return s.core.Pool().Disconnect(id)
}

func (s *ConnectionService) dto(p connection.Profile) (ConnectionDTO, error) {
	dto := ConnectionDTO{Profile: p, Connected: s.core.Pool().IsConnected(p.ID)}
	if p.UsesPassword() {
		has, err := s.core.Connections().HasPassword(p.ID)
		if err != nil {
			return ConnectionDTO{}, err
		}
		dto.HasPassword = has
	}
	return dto, nil
}

// SQL Server error numbers that mean the login itself was rejected.
var loginErrorNumbers = map[int32]bool{
	18456: true, // login failed
	18452: true, // untrusted domain (Windows authentication)
	18470: true, // login disabled
	18486: true, // account locked out
	18487: true, // password expired
	18488: true, // password must be changed
	4060:  true, // cannot open the requested database for this login
}

// classify turns a pool error into a UI error. The pool has already removed secrets.
func classify(err error) *ConnectionError {
	var se *connection.ServerError
	switch {
	case errors.Is(err, connection.ErrPasswordRequired):
		return &ConnectionError{Code: ErrPasswordRequired, Message: "Enter the password for this login."}
	case errors.Is(err, connection.ErrNotFound):
		return &ConnectionError{Code: ErrNotFound, Message: "This connection no longer exists."}
	case errors.Is(err, context.Canceled):
		return &ConnectionError{Code: ErrCancelled, Message: "The connection attempt was cancelled."}
	case errors.As(err, &se):
		code := ErrServer
		if loginErrorNumbers[se.Number] {
			code = ErrLoginFailed
		}
		return &ConnectionError{Code: code, ServerErrorNumber: se.Number, Message: se.Message}
	case errors.Is(err, context.DeadlineExceeded):
		return &ConnectionError{Code: ErrTimeout, Message: err.Error()}
	case strings.Contains(err.Error(), "x509: "):
		// The dialog shows its own guidance, so drop the driver's CLI-oriented hint.
		return &ConnectionError{Code: ErrCertificate, Message: connection.TrimCertificateHint(err.Error())}
	default:
		return &ConnectionError{Code: ErrNetwork, Message: fmt.Sprint(err)}
	}
}
