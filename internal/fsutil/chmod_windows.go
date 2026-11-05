package fsutil

// On Windows, File.Chmod only toggles the read-only attribute and access is governed by the
// user profile ACLs, so a failure here is not a reason to abort the write.
func isUnsupportedChmod(error) bool { return true }
