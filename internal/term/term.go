// Package term turns the URLs Argus prints into clickable hyperlinks,
// where there is a terminal to click them in.
//
// Every address Argus prints is one somebody is meant to open, and
// selecting a URL out of a terminal by hand is a small, repeated
// annoyance. Terminals have carried OSC 8 hyperlinks for years, so the
// address can simply be clicked instead.
//
// The escape sequence is only ever written to a stream that is genuinely
// a terminal. Anywhere else - a pipe, a log file, journald, docker logs -
// it is invisible rubbish somebody has to read past, so the plain URL
// goes out unchanged.
package term

import (
	"io"
	"os"

	"github.com/Tzomily-Anvar/argus/internal/config"
)

// Link renders url as a hyperlink whose visible text is the URL itself.
// That is what nearly every caller wants: the address stays readable for
// anyone copying it, and is clickable for everyone else.
func Link(w io.Writer, url string) string { return Linked(w, url, url) }

// Linked renders text as a hyperlink to url.
//
// w is the stream the result is about to be written to, and it is that
// stream which decides whether an escape sequence is written at all. It
// has to be passed in: some commands print to stdout and some to stderr,
// and a guard that looked at the wrong one would put escapes into a
// stream somebody had redirected to a file.
func Linked(w io.Writer, url, text string) string {
	if !printable(url) || !supported(w) {
		return text
	}
	// OSC 8, terminated with ST (ESC backslash) rather than BEL. BEL is
	// widely accepted but is the older, looser form; ST is what the
	// specification asks for and what terminals agree on.
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// printable rejects a URL that could end the escape sequence early.
//
// Not every URL printed is one Argus wrote: the device-flow address comes
// back from GitHub. A control character in it would leave the terminal
// treating the rest of the line as part of the sequence, so such a URL is
// printed plainly instead.
func printable(url string) bool {
	if url == "" {
		return false
	}
	for _, r := range url {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// supported reports whether an escape sequence written to w would reach a
// terminal that wants it.
func supported(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	// The same nuance as interactive() in cmd/argus: a character device is
	// not necessarily a terminal. /dev/null is one too, and it is what a
	// process started with no terminal at all - launchd, cron, a container
	// run without -t - usually has on the other end.
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
		return false
	}
	// No TERM means nothing has claimed to be a terminal; dumb means one
	// that has said outright it cannot do this.
	if t := os.Getenv("TERM"); t == "" || t == "dumb" {
		return false
	}
	// NO_COLOR is about colour rather than hyperlinks, but anyone setting
	// it is asking for output without escape sequences in it, and reading
	// it that way costs nothing. An empty value counts as unset, which is
	// what the convention settled on.
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// In the container the output people read is `docker logs`, which is a
	// file on disk. Escape sequences there are noise in something nobody
	// clicks.
	return !config.InContainer()
}
