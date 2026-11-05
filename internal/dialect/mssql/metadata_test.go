package mssql

import (
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	tests := map[string]string{
		"Sales":          "[Sales]",
		"My DB":          "[My DB]",
		"a]b":            "[a]]b]",
		"x]; DROP TABLE": "[x]]; DROP TABLE]",
		"[already]":      "[[already]]]",
		"Ünïcödé":        "[Ünïcödé]",
	}
	for in, want := range tests {
		got, err := QuoteIdent(in)
		if err != nil || got != want {
			t.Errorf("QuoteIdent(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", 129), "a\x00b"} {
		if _, err := QuoteIdent(bad); err == nil {
			t.Errorf("QuoteIdent(%q) succeeded", bad)
		}
	}
	if _, err := QuoteIdent(strings.Repeat("é", 128)); err != nil {
		t.Errorf("128 characters must be allowed (length is in characters, not bytes): %v", err)
	}
}

func TestFormatType(t *testing.T) {
	tests := []struct {
		name                   string
		maxLength, prec, scale int
		want                   string
	}{
		{"int", 4, 10, 0, "int"},
		{"nvarchar", 200, 0, 0, "nvarchar(100)"},
		{"nvarchar", -1, 0, 0, "nvarchar(max)"},
		{"varchar", 20, 0, 0, "varchar(20)"},
		{"varbinary", -1, 0, 0, "varbinary(max)"},
		{"char", 2, 0, 0, "char(2)"},
		{"nchar", 10, 0, 0, "nchar(5)"},
		{"decimal", 9, 10, 2, "decimal(10,2)"},
		{"numeric", 9, 18, 0, "numeric(18,0)"},
		{"datetime2", 6, 19, 0, "datetime2(0)"},
		{"time", 5, 16, 7, "time(7)"},
		{"date", 3, 10, 0, "date"},
		{"uniqueidentifier", 16, 0, 0, "uniqueidentifier"},
	}
	for _, tt := range tests {
		if got := FormatType(tt.name, tt.maxLength, tt.prec, tt.scale); got != tt.want {
			t.Errorf("FormatType(%s, %d, %d, %d) = %q, want %q", tt.name, tt.maxLength, tt.prec, tt.scale, got, tt.want)
		}
	}
}
