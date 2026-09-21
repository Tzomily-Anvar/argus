package ghauth

import "errors"

// Keeping the session in the operating system's own secret store.
//
// What this buys, stated precisely, because it is easy to overclaim:
// the session is encrypted at rest, is unreadable while the keychain is
// locked, and is not picked up by anything that copies or synchronises
// the configuration directory. What it does NOT buy is isolation from
// other processes running as you - on macOS any of them can read the
// item back through `security` without a prompt, exactly as any of them
// could read a 0600 file. SECURITY.md says so in as many words.
//
// Both backends are the platform's own command line tool rather than a
// library, which keeps Argus at zero new dependencies and CGO_ENABLED=0.

const (
	keyringService = "dev.jomily.argus"
	keyringAccount = "github-app-session"
)

// errNoKeyring means this machine has no usable secret store, which is
// ordinary: a Linux box with no desktop session has none, and then the
// file is the right answer rather than a failure.
var errNoKeyring = errors.New("no system keyring available")

type keyring interface {
	get() (string, error) // returns "" when there is no entry
	set(secret string) error
	del() error
	describe() string
}
