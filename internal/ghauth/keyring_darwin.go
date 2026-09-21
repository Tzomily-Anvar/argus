package ghauth

import (
	"encoding/hex"
	"os/exec"
	"strings"
)

// macOS: the login keychain, via /usr/bin/security.
//
// The secret is passed as an argument because `security` has no way to
// read one from standard input - given a bare -w it prompts on the tty
// and asks for the value twice. That leaves the token visible in `ps`
// for the life of one short process, to processes running as this user
// only. Those same processes can read the finished keychain item anyway,
// so this costs nothing that was not already spent.

type macKeyring struct{}

func systemKeyring() keyring {
	if _, err := exec.LookPath("security"); err != nil {
		return nil
	}
	return macKeyring{}
}

func (macKeyring) describe() string { return "the macOS keychain" }

func (macKeyring) get() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", keyringAccount, "-s", keyringService, "-w").Output()
	if err != nil {
		// Exit status 44 is "the item does not exist", which is simply
		// nobody having signed in yet.
		var ee *exec.ExitError
		if errorsAs(err, &ee) {
			return "", nil
		}
		return "", err
	}
	return decodeSecurityOutput(strings.TrimRight(string(out), "\n")), nil
}

// `security` prints a password as hex whenever it contains anything it
// considers unprintable - a newline is enough. It gives no indication
// that it has done so, so a value that went in as JSON comes back as a
// wall of hex digits and parses as nothing. Argus stores the session on
// one line to avoid this, but a session written by an earlier version
// may still be sitting there hex-encoded.
func decodeSecurityOutput(s string) string {
	if len(s) < 2 || len(s)%2 != 0 || !strings.HasPrefix(s, "7b") {
		return s // not hex, or not hex of something starting "{"
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}

func (macKeyring) set(secret string) error {
	// -U updates in place rather than failing on an existing item, which
	// matters because GitHub rotates the refresh token on every renewal.
	return exec.Command("security", "add-generic-password",
		"-U", "-a", keyringAccount, "-s", keyringService,
		"-D", "application password",
		"-j", "Argus GitHub App session",
		"-w", secret).Run()
}

func (macKeyring) del() error {
	err := exec.Command("security", "delete-generic-password",
		"-a", keyringAccount, "-s", keyringService).Run()
	var ee *exec.ExitError
	if errorsAs(err, &ee) {
		return nil // already gone
	}
	return err
}
