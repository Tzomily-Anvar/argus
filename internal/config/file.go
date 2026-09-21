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
		return path, nil
	}
	return "", nil
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

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
