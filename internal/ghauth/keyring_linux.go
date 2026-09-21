package ghauth

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// Linux: the Secret Service, via secret-tool from libsecret.
//
// Unlike macOS this one reads the secret from standard input, so nothing
// sensitive reaches the process table. It needs a running Secret Service
// - a desktop session, in practice - so a headless machine falls back to
// the file, which is the correct answer there rather than an error.

type linuxKeyring struct{}

func systemKeyring() keyring {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil
	}
	// Without a session bus there is no Secret Service to talk to.
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return nil
	}
	return linuxKeyring{}
}

func (linuxKeyring) describe() string { return "the system keyring" }

func (linuxKeyring) get() (string, error) {
	out, err := exec.Command("secret-tool", "lookup",
		"service", keyringService, "account", keyringAccount).Output()
	if err != nil {
		var ee *exec.ExitError
		if errorsAs(err, &ee) {
			return "", nil // no such item
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (linuxKeyring) set(secret string) error {
	cmd := exec.Command("secret-tool", "store", "--label=Argus GitHub App session",
		"service", keyringService, "account", keyringAccount)
	cmd.Stdin = bytes.NewReader([]byte(secret))
	return cmd.Run()
}

func (linuxKeyring) del() error {
	err := exec.Command("secret-tool", "clear",
		"service", keyringService, "account", keyringAccount).Run()
	var ee *exec.ExitError
	if errorsAs(err, &ee) {
		return nil
	}
	return err
}
