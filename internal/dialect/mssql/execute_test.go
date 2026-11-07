package mssql

import (
	"errors"
	"strings"
	"testing"
	"time"

	mssqldb "github.com/microsoft/go-mssqldb"
)

func TestConvertValue(t *testing.T) {
	ts := time.Date(2026, 1, 31, 16, 45, 0, 123456700, time.UTC)
	tests := []struct {
		dbType string
		in     any
		want   any
	}{
		{"INT", nil, nil},
		{"DECIMAL", []byte("105.00"), "105.00"},
		{"MONEY", []byte("-12.3400"), "-12.3400"},
		{"BIGINT", int64(9007199254740991), int64(9007199254740991)},
		{"BIGINT", int64(9007199254740993), "9007199254740993"},
		{"BIGINT", int64(-9007199254740993), "-9007199254740993"},
		{"DATE", ts, "2026-01-31"},
		{"DATETIME2", ts, "2026-01-31T16:45:00.1234567"},
		{"DATETIME2", time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), "2026-01-31T00:00:00"},
		{"TIME", ts, "16:45:00.1234567"},
		{"DATETIMEOFFSET", ts, "2026-01-31T16:45:00.1234567Z"},
		{"VARBINARY", []byte{0xde, 0xad, 0xbe, 0xef}, "0xDEADBEEF"},
		{"NVARCHAR", "naïve", "naïve"},
		{"BIT", true, true},
		{"REAL", float32(1.5), float64(1.5)},
	}
	for _, tt := range tests {
		if got := ConvertValue(tt.dbType, tt.in); got != tt.want {
			t.Errorf("ConvertValue(%s, %#v) = %#v, want %#v", tt.dbType, tt.in, got, tt.want)
		}
	}
}

func TestConvertUniqueIdentifier(t *testing.T) {
	// SQL Server stores the first three groups little-endian.
	raw := []byte{0x78, 0x56, 0x34, 0x12, 0x34, 0x12, 0x78, 0x56, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}
	if got := ConvertValue("UNIQUEIDENTIFIER", raw); got != "12345678-1234-5678-1234-56789ABCDEF0" {
		t.Errorf("guid = %v", got)
	}
}

func TestConvertLongBinaryIsCapped(t *testing.T) {
	got := ConvertValue("VARBINARY", make([]byte, 2000)).(string)
	if !strings.HasSuffix(got, "… (2000 bytes)") || len(got) > 2*maxBinaryBytes+40 {
		t.Errorf("long binary not capped: len %d, suffix %q", len(got), got[len(got)-20:])
	}
}

func TestToMessage(t *testing.T) {
	m, ok := toMessage(mssqldb.Error{Number: 0, Class: 0, Message: "Hello", LineNo: 2}, "info", 1)
	if !ok || m.Kind != "info" || m.Text != "Hello" || m.Line != 2 || m.AfterResultSet != 1 {
		t.Errorf("print notice = %+v", m)
	}
	m, _ = toMessage(mssqldb.Error{Number: 50000, Class: 16, State: 1, Message: "boom", LineNo: 3, ProcName: "p"}, "info", 0)
	if m.Kind != "error" || m.Number != 50000 || m.Severity != 16 || m.Procedure != "p" {
		t.Errorf("severity-16 notice must be an error: %+v", m)
	}
	m, ok = toMessage(errors.New("connection reset; password=abc"), "error", 0)
	if !ok || m.Kind != "error" || strings.Contains(m.Text, "abc") {
		t.Errorf("plain error = %+v", m)
	}
}
