//go:build !darwin && !linux

package ghauth

// Everywhere else keeps the file. Windows has a credential store, but
// nothing here has been able to test against it, and shipping an
// untested path for storing a credential is worse than shipping none.
func systemKeyring() keyring { return nil }
