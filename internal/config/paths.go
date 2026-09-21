package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Where Argus keeps things, which depends on how it was installed.
//
// In a container the answer is /data, a mounted volume. Installed as a
// binary it has to be somewhere the user owns: /data needs root on a Mac,
// so defaulting to it would make the very first run of a brew install
// fail for everyone.
//
// Docker sets ARGUS_IN_CONTAINER, so the two cases are distinguished
// explicitly rather than guessed at from the filesystem.

// DefaultDataDir is where data would live with nothing overridden. The
// keyring holds a single item for the machine, so it belongs to this
// location and not to a directory someone has redirected Argus at.
func DefaultDataDir() string {
	if InContainer() {
		return "/data"
	}
	return filepath.Join(userDataHome(), "argus")
}

// DataDir is where sessions and the sprint report's storage live.
func DataDir() string {
	if v := String("ARGUS_DATA_DIR", ""); v != "" {
		return v
	}
	return defaultDataDir()
}

// ConfigDir is where the configuration file lives.
func ConfigDir() string {
	if v := String("ARGUS_CONFIG_DIR", ""); v != "" {
		return v
	}
	return defaultConfigDir()
}

// The defaults, without the override. `argus config` has to show what a
// setting would be if you had not set it, which the accessors above
// cannot say once you have.

func defaultDataDir() string {
	if InContainer() {
		return "/data"
	}
	return filepath.Join(userDataHome(), "argus")
}

func defaultConfigDir() string {
	if InContainer() {
		return "/data"
	}
	return filepath.Join(userConfigHome(), "argus")
}

// ConfigFile is the configuration file itself.
func ConfigFile() string {
	if v := String("ARGUS_CONFIG", ""); v != "" {
		return v
	}
	return filepath.Join(ConfigDir(), "config.env")
}

// InContainer reports whether Argus is running inside its own image.
// The Dockerfile sets it; nothing else should.
func InContainer() bool { return Bool("ARGUS_IN_CONTAINER", false) }

// userConfigHome follows each platform's own convention rather than
// inventing one: a dotfile in the home directory would be wrong on macOS
// and unwelcome on Linux.
func userConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if v := os.Getenv("AppData"); v != "" {
			return v
		}
		return filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(home, ".config")
}

func userDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if v := os.Getenv("LocalAppData"); v != "" {
			return v
		}
		return filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(home, ".local", "share")
}
