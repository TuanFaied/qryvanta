//go:build !windows

package fsutil

func isUnsupportedChmod(error) bool { return false }
