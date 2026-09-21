package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/term"
)

// Running at login, the way the container does.
//
// The value of Argus is that the answer is already there when you look,
// which needs it sweeping while nobody is watching. A container gets that
// from restart: unless-stopped; a binary run from a terminal loses it the
// moment the window closes.
//
// So the binary can install itself as a user service - launchd on macOS,
// systemd --user on Linux. Both are per-user and unprivileged: nothing
// here needs or asks for root.

// A service inherits none of the shell environment, so the unit carries
// the configuration file it was installed with. Without this, someone
// whose config lives anywhere but the default path installs a service
// that starts and then immediately complains it is not configured.
const launchdLabel = "dev.jomily.argus"

func serviceCommand(action string) error {
	switch runtime.GOOS {
	case "darwin":
		return launchdService(action)
	case "linux":
		return systemdService(action)
	case "windows":
		return windowsService(action)
	}
	return fmt.Errorf("installing a service is not supported on %s; run `argus` yourself, "+
		"or use the Docker path", runtime.GOOS)
}

// dashboardURL is the address the service serves on. The argus.localhost
// form resolves to the loopback address without an /etc/hosts entry, so
// it works as printed; Windows is the exception and says localhost.
func dashboardURL() string { return "http://argus.localhost:" + config.Port() }

func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// ---- macOS ----------------------------------------------------------

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func launchdService(action string) error {
	path, err := launchdPlistPath()
	if err != nil {
		return err
	}

	if action == "uninstall" {
		_ = exec.Command("launchctl", "unload", path).Run()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("  Removed %s\n  Argus will no longer start at login.\n", path)
		return nil
	}

	bin, err := selfPath()
	if err != nil {
		return err
	}
	logDir := config.DataDir()
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string></array>
  <key>EnvironmentVariables</key>
  <dict><key>ARGUS_CONFIG</key><string>%s</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, launchdLabel, bin, config.ConfigFile(),
		filepath.Join(logDir, "argus.log"),
		filepath.Join(logDir, "argus.log"))

	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", path).Run()
	if out, err := exec.Command("launchctl", "load", path).CombinedOutput(); err != nil {
		return fmt.Errorf("loading the service: %v: %s", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf(`
  Installed %s

  Argus now starts at login and keeps sweeping in the background, so the
  dashboard is ready whenever you open it:

      %s

  Logs:    %s
  Remove:  argus service uninstall

`, path, term.Link(os.Stdout, dashboardURL()), filepath.Join(logDir, "argus.log"))
	return nil
}

// ---- Linux ----------------------------------------------------------

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "argus.service"), nil
}

func systemdService(action string) error {
	path, err := systemdUnitPath()
	if err != nil {
		return err
	}

	if action == "uninstall" {
		_ = exec.Command("systemctl", "--user", "disable", "--now", "argus").Run()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("  Removed %s\n  Argus will no longer start at login.\n", path)
		return nil
	}

	bin, err := selfPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	unit := fmt.Sprintf(`[Unit]
Description=Argus - read-only GitHub dashboard
After=network-online.target

[Service]
Environment=ARGUS_CONFIG=%s
ExecStart=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, config.ConfigFile(), bin)

	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", "argus").CombinedOutput(); err != nil {
		return fmt.Errorf("enabling the service: %v: %s", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf(`
  Installed %s

  Argus now starts at login and keeps sweeping in the background:

      %s

  Logs:    journalctl --user -u argus -f
  Remove:  argus service uninstall

`, path, term.Link(os.Stdout, dashboardURL()))
	return nil
}

// ---- Windows --------------------------------------------------------
//
// Task Scheduler rather than a Windows service: a service would need an
// installer and a privileged account, and Argus only ever needs to run
// as the person whose GitHub access it is using.

// Untested: this compiles and is written from the documented behaviour
// of schtasks, but nobody has yet run Argus on Windows.
const windowsTaskName = "Argus"

func windowsService(action string) error {
	if action == "uninstall" {
		out, err := exec.Command("schtasks", "/delete", "/tn", windowsTaskName, "/f").CombinedOutput()
		if err != nil {
			return fmt.Errorf("removing the task: %v: %s", err, strings.TrimSpace(string(out)))
		}
		fmt.Printf("  Removed the %s task.\n  Argus will no longer start at login.\n", windowsTaskName)
		return nil
	}

	bin, err := selfPath()
	if err != nil {
		return err
	}
	out, err := exec.Command("schtasks", "/create", "/tn", windowsTaskName,
		"/tr", bin, "/sc", "onlogon", "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating the task: %v: %s", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf(`
  Created the %s scheduled task.

  Argus now starts when you log in, so the dashboard is ready whenever
  you open it:

      %s

  Remove:  argus service uninstall

`, windowsTaskName, term.Link(os.Stdout, "http://localhost:"+config.Port()))
	return nil
}

// serviceInstalled reports whether a unit has been written, so `argus
// doctor` can name what is answering rather than just that something is.
func serviceInstalled() bool {
	var path string
	var err error
	switch runtime.GOOS {
	case "darwin":
		path, err = launchdPlistPath()
	case "linux":
		path, err = systemdUnitPath()
	default:
		return false
	}
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
