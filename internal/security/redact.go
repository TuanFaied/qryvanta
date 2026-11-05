// Package security holds helpers that keep secrets out of logs, errors and exports.
package security

import (
	"regexp"
	"strings"
)

// Mask is the replacement text for redacted values.
const Mask = "[REDACTED]"

// sensitiveKeyParts are matched case-insensitively against attribute and field names after
// removing separators, so "Password", "db_password", "connection-string" and "apiKey" all match.
var sensitiveKeyParts = []string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"apikey",
	"credential",
	"connectionstring",
	"connstr",
	"dsn",
}

// IsSensitiveKey reports whether a field or attribute name probably holds a secret.
func IsSensitiveKey(key string) bool {
	k := normalizeKey(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

func normalizeKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// secretPairPattern matches key=value pairs in connection strings and URLs, for example
// "Password=abc;" , "pwd = 'a;b'", "password=abc&" or "token:xyz".
// Values can be quoted with ' or " (quotes may contain separators), or unquoted, in which
// case they end at ';', '&', whitespace, or the end of the string.
var secretPairPattern = regexp.MustCompile(
	`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)(\s*[=:]\s*)('[^']*'|"[^"]*"|[^;&\s]*)`,
)

// userInfoPattern matches credentials embedded in URLs: scheme://user:password@host.
var userInfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+:)[^@\s]*@`)

// RedactString removes secrets embedded in free text such as connection strings,
// URLs, or driver error messages.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	s = secretPairPattern.ReplaceAllString(s, "${1}${2}"+Mask)
	s = userInfoPattern.ReplaceAllString(s, "${1}"+Mask+"@")
	return s
}
