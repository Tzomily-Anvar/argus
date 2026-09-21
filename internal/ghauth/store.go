package ghauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Persisting the session.
//
// This is the cost of the App path, and it is worth stating plainly: the
// pull request tool otherwise keeps nothing on disk. A refresh token must
// outlive the process or every restart would mean signing in again, so
// the file exists and SECURITY.md says so.
//
// Where it goes depends on the machine. If the operating system offers a
// secret store, the session lives there and no file is written at all.
// Otherwise it is a 0600 file on the same volume as the sprint report's
// data, which is already excluded from version control. A session found
// in a file on a machine that has since gained a keyring is moved into
// it, so nobody has to sign in again to benefit.

// Store keeps a token, in the system keyring where there is one.
type Store struct {
	path string
	ring keyring
	mu   sync.Mutex
}

// NewStore keeps the session in dir. useKeyring asks for the system
// secret store instead, and only the caller can answer that: the keyring
// holds a single item for the machine, so a Store pointed at anywhere
// but the usual data directory must not touch it. A second instance, or
// a test handed a temporary directory, would otherwise read and
// overwrite the real session despite having been told to keep its own
// somewhere separate.
//
// It is a parameter rather than something worked out here, because
// deciding it here would mean a package-level default that has to be set
// before anything constructs a Store - and the one path that forgot
// would fail by quietly using a file, which is the failure nobody
// notices until their session will not load.
func NewStore(dir string, useKeyring bool) *Store {
	s := &Store{path: filepath.Join(dir, "github-app-session.json")}
	if useKeyring {
		s.ring = systemKeyring()
	}
	return s
}

// Path is the file the session would use, for the fallback case.
func (s *Store) Path() string { return s.path }

// Location describes where the session actually is, for the messages
// that tell someone what was just written on their behalf.
func (s *Store) Location() string {
	if s.ring != nil {
		return s.ring.describe()
	}
	return s.path
}

// Load returns the stored token. A missing file is not an error: it means
// nobody has signed in yet, which is the normal state on first run.
func (s *Store) Load() (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ring != nil {
		raw, err := s.ring.get()
		if err != nil {
			return Token{}, fmt.Errorf("reading the session from %s: %w", s.ring.describe(), err)
		}
		if raw != "" {
			var t Token
			if err := json.Unmarshal([]byte(raw), &t); err != nil {
				return Token{}, fmt.Errorf("the stored session is unreadable: %w", err)
			}
			return t, nil
		}
		// Nothing in the keyring. There may still be a file from before
		// this machine had one, or from an older Argus.
	}

	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Token{}, nil
	}
	if err != nil {
		return Token{}, fmt.Errorf("reading the session: %w", err)
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return Token{}, fmt.Errorf("the session file is unreadable: %w", err)
	}

	// Move it in, and take the plaintext copy off the disk. A failure
	// here is not worth refusing the session over: the file still works.
	if s.ring != nil {
		if err := s.ring.set(string(b)); err == nil {
			_ = os.Remove(s.path)
		}
	}
	return t, nil
}

// Save writes a token atomically. GitHub rotates refresh tokens, so a
// half-written file is a lost session rather than a stale one - the old
// refresh token is already invalid by the time this is called.
func (s *Store) Save(t Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}

	if s.ring != nil {
		// On one line, deliberately. The macOS keychain hex-encodes any
		// value containing a newline and hands the hex back on read,
		// which turns a saved session into something that silently fails
		// to parse.
		compact, err := json.Marshal(t)
		if err != nil {
			return err
		}
		if err := s.ring.set(string(compact)); err != nil {
			return fmt.Errorf("saving the session to %s: %w", s.ring.describe(), err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".session.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// Clear removes the session, for signing out.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear both: a session may predate the keyring, and signing out has
	// to mean it everywhere.
	if s.ring != nil {
		if err := s.ring.del(); err != nil {
			return err
		}
	}
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
