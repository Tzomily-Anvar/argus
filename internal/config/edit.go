package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Editing the configuration file from the command line.
//
// The file is a document, not a database. Someone opened it, read the
// comments, and made decisions next to them; a writer that rewrote it
// from a parse tree would hand back a tidy file with all of that gone.
// So every change here is made line by line, and everything it does not
// need to touch it does not touch.
//
// The three cases, in the order they are tried:
//
//	the key is already set        replace that line, in place
//	the key is there commented    uncomment that line, in place
//	the key is not there at all   append it, under its own comment
//
// The middle case is the one that keeps the annotated template intact:
// `# ARGUS_PORT=18474` already has three lines of explanation above it,
// and turning it on where it stands keeps the explanation attached.

// Change describes what a write did, so the caller can say so plainly.
type Change struct {
	// Created reports that there was no configuration file and one was
	// written from the starter template.
	Created bool

	// Previous is the value that was in force in the file beforehand,
	// and Had says whether there was one at all.
	Previous string
	Had      bool

	// How the line was written.
	Replaced    bool // an active line was changed
	Uncommented bool // a commented-out line was brought back
	Appended    bool // the key was not in the file

	// Duplicates counts further active lines for the same key that were
	// commented out. The first line in the file is the one that takes
	// effect, so a second was doing nothing but misleading its reader.
	Duplicates int

	// Line is where the setting now lives, 1-based.
	Line int
}

// ReadFileValues parses a configuration file without touching the
// environment, returning the values it sets.
//
// Load cannot answer this afterwards: its whole job is to merge the file
// into the environment, and once it has, nothing can tell which layer a
// value came from. Provenance needs the layers kept apart.
//
// A missing file is not an error - it is the ordinary state of a fresh
// install, and the caller wants an empty map, not a failure.
func ReadFileValues(path string) (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := parseLine(line)
		if !ok {
			continue
		}
		// First wins, as in loadFile: it sets the variable and every
		// later line for the same key then finds it already set.
		if _, dup := out[key]; !dup {
			out[key] = value
		}
	}
	return out, nil
}

// lineFor matches a line setting a key, commented out or not.
func lineFor(key string) *regexp.Regexp {
	return regexp.MustCompile(`^(\s*)(#+\s*)?` + regexp.QuoteMeta(key) + `\s*=(.*)$`)
}

// SetInFile writes a setting to the configuration file, creating the file
// if there is none.
func SetInFile(path string, s Setting, value string) (Change, error) {
	var ch Change
	lines, crlf, created, err := readLines(path)
	if err != nil {
		return ch, err
	}
	ch.Created = created

	re := lineFor(s.Key)
	written := -1
	commented := -1
	for i, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		active := m[2] == ""
		switch {
		case active && written < 0:
			ch.Had, ch.Previous = true, strings.Trim(strings.TrimSpace(m[3]), `"'`)
			ch.Replaced = true
			lines[i] = m[1] + s.Key + "=" + quote(value)
			written = i
		case active:
			// A duplicate that was never in force. Commenting it out is
			// the only way the file stops disagreeing with itself.
			lines[i] = m[1] + "# " + strings.TrimSpace(line) +
				"    # superseded above, and never read"
			ch.Duplicates++
		case commented < 0:
			commented = i
		}
	}

	if written < 0 && commented >= 0 {
		m := re.FindStringSubmatch(lines[commented])
		lines[commented] = m[1] + s.Key + "=" + quote(value)
		ch.Uncommented = true
		written = commented
	}

	if written < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		if s.Desc != "" {
			lines = append(lines, "# "+s.Desc)
		}
		lines = append(lines, s.Key+"="+quote(value))
		ch.Appended = true
		written = len(lines) - 1
	}
	ch.Line = written + 1

	return ch, writeLines(path, lines, crlf)
}

// UnsetInFile comments out every line setting a key.
//
// Commented rather than deleted, because the old value is worth seeing -
// and because the next `argus config set` will find that line and turn it
// back on where it stands, rather than appending a second one further
// down the file.
func UnsetInFile(path string, s Setting) (Change, error) {
	var ch Change
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ch, fmt.Errorf("there is no configuration file at %s, so there is nothing to unset",
				path)
		}
		return ch, err
	}
	lines, crlf, _, err := readLines(path)
	if err != nil {
		return ch, err
	}

	re := lineFor(s.Key)
	for i, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil || m[2] != "" {
			continue
		}
		if !ch.Had {
			ch.Had, ch.Previous = true, strings.Trim(strings.TrimSpace(m[3]), `"'`)
			ch.Line = i + 1
		} else {
			ch.Duplicates++
		}
		lines[i] = m[1] + "# " + strings.TrimSpace(line)
	}
	if !ch.Had {
		return ch, nil
	}
	return ch, writeLines(path, lines, crlf)
}

// readLines reads the file, or the starter template if there is none.
func readLines(path string) (lines []string, crlf, created bool, err error) {
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		// The same annotated file `argus init` writes. Someone who opens
		// it after one `argus config set` should find the template they
		// would have got either way, not a single bare line.
		b, created, err = []byte(starter), true, nil
	default:
		return nil, false, false, err
	}
	text := string(b)
	if strings.Contains(text, "\r\n") {
		crlf = true
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	return strings.Split(text, "\n"), crlf, created, nil
}

// writeLines replaces the file atomically, so an interrupted write leaves
// the configuration that was there rather than half of a new one.
func writeLines(path string, lines []string, crlf bool) error {
	sep := "\n"
	if crlf {
		sep = "\r\n"
	}
	out := strings.Join(lines, sep)
	if !strings.HasSuffix(out, sep) {
		out += sep
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".config.%d.env", os.Getpid()))
	defer os.Remove(tmp)
	if err := os.WriteFile(tmp, []byte(out), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// quote wraps a value only when it would otherwise be read back wrong.
// The reader trims surrounding whitespace and quotes, so a value that
// ends in a space, or begins with a quote, needs them.
func quote(v string) string {
	if v == "" {
		return v
	}
	if v != strings.TrimSpace(v) || strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
		return `"` + v + `"`
	}
	return v
}
