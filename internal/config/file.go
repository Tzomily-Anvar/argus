package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Loading configuration from a file.
//
// Installed from a package manager there is no repository and no .env
// beside the binary, so settings have to live somewhere the user owns and
// Argus has to find them itself. Docker keeps working unchanged: compose
// passes everything as environment variables, which always win.
//
// Order, first match wins:
//
//	environment variables        set by compose, CI, or a one-off override
//	$ARGUS_CONFIG                an explicit path
//	<config dir>/config.env      written by `argus init`
//	./.env                       so a checkout behaves as it always has

// loaded records the file Load used, so a failure afterwards can tell
// "configured wrong" apart from "never configured at all" - which want
// very different advice.
var loaded string

// Loaded reports the configuration file in use, or "" if there is none.
func Loaded() string { return loaded }

// Load reads the configuration file into the environment, without
// overwriting anything already set. Returns the file it used, or "".
func Load() (string, error) {
	for _, path := range candidates() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := loadFile(path); err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		loaded = path
		break
	}
	// A setting under a former name is honoured after both layers are
	// in, and said out loud: the rename is somebody's to make, not
	// something to paper over for ever.
	for _, notice := range FormerNames(Core()) {
		fmt.Fprintf(os.Stderr, "argus: %s\n", notice)
	}
	return loaded, nil
}

// ActiveFile returns the configuration file Load would use, or the one
// that would be created if there is none.
//
// Not the same as ConfigFile: inside a checkout the file being read is
// ./.env, and `argus config` that read and wrote the file under your home
// directory instead would be reporting on, and editing, a file the server
// never looks at.
func ActiveFile() string {
	for _, path := range candidates() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ConfigFile()
}

func candidates() []string {
	return []string{
		os.Getenv("ARGUS_CONFIG"),
		filepath.Join(ConfigDir(), "config.env"),
		".env",
	}
}

func loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}

		// An environment variable wins, but only if it says something.
		// Compose declares "${ARGUS_GITHUB_TOKEN:-}" so the variable
		// exists in the container even when the host never set it; an
		// empty value there would otherwise silently beat the file and
		// leave someone staring at a token that is plainly present.
		if v, already := os.LookupEnv(key); already && strings.TrimSpace(v) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// parseLine reads one line of a configuration file.
//
// Every reader of the file goes through here - Load, and the `argus
// config` commands that have to say what the file currently holds. Two
// parsers that disagreed by a trimmed quote would have `argus config
// list` confidently reporting a value the server never saw.
func parseLine(raw string) (key, value string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`), true
}

// WriteStarter creates a configuration file, and refuses to overwrite one.
func WriteStarter(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; edit it, or delete it first", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(starter), 0o600)
}
