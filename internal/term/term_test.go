package term_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/term"
)

const url = "http://argus.localhost:18474"

// want is the sequence a terminal has to receive, written out by hand so
// that a change to how it is built has to be justified against the
// specification rather than against itself.
const want = "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"

// terminal opens something that is genuinely a character device.
//
// The controlling terminal if there is one, and otherwise the pty
// multiplexer, which can be opened anywhere a pty can be allocated. A
// test binary's own stdout is a pipe, so it cannot stand in for either.
func terminal(t *testing.T) *os.File {
	t.Helper()
	for _, name := range []string{"/dev/tty", "/dev/ptmx"} {
		f, err := os.OpenFile(name, os.O_RDWR, 0)
		if err == nil {
			t.Cleanup(func() { f.Close() })
			return f
		}
	}
	t.Skip("no character device to stand in for a terminal")
	return nil
}

// allowed puts the environment in the state where a hyperlink is wanted,
// so that each test below can take away the one thing it is about.
func allowed(t *testing.T) {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("ARGUS_IN_CONTAINER", "0")
}

func TestLinkOnATerminal(t *testing.T) {
	allowed(t)
	got := term.Link(terminal(t), url)
	if got != want {
		t.Fatalf("link not wrapped as OSC 8:\n got %q\nwant %q", got, want)
	}
}

func TestLinkedUsesItsOwnText(t *testing.T) {
	allowed(t)
	got := term.Linked(terminal(t), url, "the dashboard")
	want := "\x1b]8;;" + url + "\x1b\\the dashboard\x1b]8;;\x1b\\"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The important half: everywhere that is not a terminal gets the plain
// URL, because that is where an escape sequence would be read by a person
// or parsed by a machine rather than rendered.
func TestPlainWhereItIsNotWanted(t *testing.T) {
	regular, err := os.Create(filepath.Join(t.TempDir(), "argus.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	pipe, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	defer w.Close()

	for _, c := range []struct {
		name string
		w    func(*testing.T) *os.File
		env  map[string]string
	}{
		{name: "a log file", w: func(*testing.T) *os.File { return regular }},
		{name: "/dev/null", w: func(*testing.T) *os.File { return devNull }},
		{name: "a pipe", w: func(*testing.T) *os.File { return w }},
		{name: "TERM unset", w: terminal, env: map[string]string{"TERM": ""}},
		{name: "TERM=dumb", w: terminal, env: map[string]string{"TERM": "dumb"}},
		{name: "NO_COLOR set", w: terminal, env: map[string]string{"NO_COLOR": "1"}},
		{name: "in the container", w: terminal, env: map[string]string{"ARGUS_IN_CONTAINER": "1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			allowed(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if got := term.Link(c.w(t), url); got != url {
				t.Fatalf("escape sequence written to %s: %q", c.name, got)
			}
		})
	}
}

// Anything that is not a file cannot be checked, so it is treated as not
// a terminal.
func TestPlainForAnyOtherWriter(t *testing.T) {
	allowed(t)
	if got := term.Link(&bytes.Buffer{}, url); got != url {
		t.Fatalf("got %q, want the plain URL", got)
	}
}

// A URL Argus did not write itself - the device-flow address comes back
// from GitHub - must not be able to end the sequence early.
func TestControlCharactersAreNotWrapped(t *testing.T) {
	allowed(t)
	for _, bad := range []string{"", "https://example.com/\x1b]8;;", "https://example.com/\n"} {
		if got := term.Link(terminal(t), bad); got != bad {
			t.Fatalf("wrapped %q as %q", bad, got)
		}
	}
}
