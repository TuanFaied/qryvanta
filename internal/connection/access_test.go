package connection

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestLevelFromReasons(t *testing.T) {
	tests := []struct {
		reasons []AccessReason
		want    AccessLevel
	}{
		{nil, AccessReadOnly},
		{[]AccessReason{{Kind: "indirect", Text: "EXECUTE"}}, AccessMayWrite},
		{[]AccessReason{{Kind: "indirect"}, {Kind: "direct"}}, AccessCanWrite},
		{[]AccessReason{{Kind: "direct"}}, AccessCanWrite},
	}
	for _, tt := range tests {
		if got := LevelFromReasons(tt.reasons); got != tt.want {
			t.Errorf("LevelFromReasons(%v) = %s, want %s", tt.reasons, got, tt.want)
		}
	}
}

type fakeAccess struct {
	res   WriteAccess
	err   error
	gotDB string
}

func (f *fakeAccess) WriteAccess(_ context.Context, _ *sql.DB, database string) (WriteAccess, error) {
	f.gotDB = database
	return f.res, f.err
}

func TestPoolTestReportsAccess(t *testing.T) {
	p, m, _ := newTestPool(t)
	fa := &fakeAccess{res: WriteAccess{Level: AccessCanWrite, Reasons: []AccessReason{{Kind: "direct", Text: "member of the db_owner database role"}}}}
	p.SetAccessChecker(fa)
	prof, _ := m.Create(validSQLProfile(), secret)

	res, err := p.Test(context.Background(), prof.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Access == nil || res.Access.Level != AccessCanWrite || fa.gotDB != "" {
		// fakeDriver reports no database; the check must run against what the server reported.
		t.Fatalf("Access = %+v, checked db %q", res.Access, fa.gotDB)
	}
}

func TestPoolAccessFailureIsUnknownNotError(t *testing.T) {
	p, m, _ := newTestPool(t)
	p.SetAccessChecker(&fakeAccess{err: errors.New("VIEW SERVER STATE denied; password=" + secret)})
	prof, _ := m.Create(validSQLProfile(), secret)

	res, err := p.Test(context.Background(), prof.ID)
	if err != nil {
		t.Fatalf("a failed permission check must not fail the connection test: %v", err)
	}
	if res.Access == nil || res.Access.Level != AccessUnknown || res.Access.Error == "" {
		t.Fatalf("Access = %+v", res.Access)
	}
	if strings.Contains(res.Access.Error, secret) {
		t.Fatalf("access error leaks the password: %q", res.Access.Error)
	}
}

func TestPoolWithoutCheckerReportsNoAccess(t *testing.T) {
	p, m, _ := newTestPool(t)
	prof, _ := m.Create(validSQLProfile(), secret)
	if res, _ := p.Test(context.Background(), prof.ID); res.Access != nil {
		t.Fatalf("Access = %+v, want nil without a checker", res.Access)
	}
	if _, err := p.CheckAccessFor(context.Background(), prof.ID, "x"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("CheckAccessFor(not connected) = %v", err)
	}
}
