package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
	"github.com/Tzomily-Anvar/argus/internal/term"
)

// The commands a package-manager install needs.
//
// Installed from a tap there is no repository, so none of run.sh's
// helpers exist. Everything a person needs has to be in the binary:
// where to put configuration, whether the setup is right, and how to
// sign in.

// initConfig writes a starter configuration file.
func initConfig() error {
	path := config.ConfigFile()
	if err := config.WriteStarter(path); err != nil {
		return err
	}
	fmt.Printf(`
  Wrote %s

  Open it and set ARGUS_GITHUB_ORG to your organisation, or to your own
  username if your repositories live under your personal account. That is
  the only required setting; everything else has a working default.

  Then:
      argus doctor     check the setup
      argus            run it

`, path)
	return nil
}

// doctor reports on the setup and names whatever is missing.
//
// The equivalent of run.sh doctor, in the binary, because someone who
// installed from a package manager has no run.sh.
func doctor() error {
	green, red, yellow, dim, off := "\033[32m", "\033[31m", "\033[33m", "\033[2m", "\033[0m"
	ok := func(s string, d ...string) {
		fmt.Printf("  %s✓%s %-26s %s\n", green, off, s, strings.Join(d, " "))
	}
	bad := func(s, why, fix string) {
		fmt.Printf("  %s✗%s %-26s %s\n", red, off, s, why)
		if fix != "" {
			fmt.Printf("      %s%s%s\n", dim, fix, off)
		}
	}
	warn := func(s, why string) {
		fmt.Printf("  %s!%s %-26s %s\n", yellow, off, s, why)
	}

	fmt.Println("Argus setup")
	fmt.Println()

	if used, err := config.Load(); err != nil {
		bad("configuration", err.Error(), "")
	} else if used == "" {
		bad("configuration", "no file found",
			"run `argus init` to write one at "+config.ConfigFile())
	} else {
		ok("configuration", used)
	}

	org, err := config.Org()
	switch {
	case err != nil:
		bad("account", "not set", "set ARGUS_GITHUB_ORG in your configuration")
	case org == "your-org-here":
		bad("account", "still the placeholder",
			"set ARGUS_GITHUB_ORG to your organisation, or to your own username")
	default:
		// Which kind of account it is decides which endpoints Argus can
		// use - teams and the account-wide security feeds exist for an
		// organisation only - so it is worth saying out loud here rather
		// than leaving someone to infer it from a rule that looked
		// quiet.
		kind, kerr := accountKind(org)
		switch {
		case kerr != nil:
			ok("account", org)
			warn("account type", kerr.Error())
		default:
			ok("account", org, dim+kind.Label()+off)
		}
	}

	switch {
	case config.UseGitHubApp():
		src := appSource()
		if src.SignedIn() {
			ok("GitHub App", "signed in")
		} else {
			bad("GitHub App", "configured but not signed in", "run `argus login`")
		}
	default:
		if _, err := config.Token(); err != nil {
			bad("GitHub token", "none found",
				"set ARGUS_GITHUB_TOKEN, configure a GitHub App, or run `gh auth login`")
		} else {
			ok("GitHub token", "present")
		}
	}

	fmt.Printf("  %s·%s %-26s %s\n", dim, off, "data directory", config.DataDir())

	if config.ToolEnabled("sprint") {
		if _, err := config.JiraBaseURL(); err != nil {
			bad("sprint report", "enabled but Jira is not configured",
				"set ARGUS_JIRA_BASE_URL, _EMAIL, _TOKEN and _PROJECT")
		} else {
			ok("sprint report", "configured")
		}
	}

	addr := "http://localhost:" + config.Port() + "/healthz"
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(addr); err == nil {
		defer resp.Body.Close()
		var health struct {
			Container bool `json:"container"`
		}
		// An older Argus answers /healthz with plain text. Then there is
		// no way to tell what is on the port, and saying so is better
		// than naming the wrong one.
		what := "something already on this port"
		if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
			switch {
			case health.Container:
				what = "the Docker container"
			case serviceInstalled():
				what = "the background service"
			default:
				what = "a binary you started"
			}
		}
		dash := "http://argus.localhost:" + config.Port()
		ok("running", fmt.Sprintf("%s (%s)", term.Link(os.Stdout, dash), what))
	} else {
		warn("running", "nothing answering on port "+config.Port()+" yet")
	}

	fmt.Println()
	return nil
}

// accountKind asks GitHub whether a name is an organisation or a person.
//
// Doctor reports rather than fails on anything that goes wrong here: not
// being able to check is a different thing from the answer being bad,
// and someone who has not signed in yet should not be told their account
// is wrong.
func accountKind(name string) (gh.AccountKind, error) {
	client, err := gh.NewFromEnv()
	if err != nil {
		return gh.AccountUnknown, fmt.Errorf("not signed in, so this cannot be checked")
	}
	kind, err := client.AccountKindOf(name)
	if err != nil {
		return gh.AccountUnknown, fmt.Errorf("could not be checked: %v", err)
	}
	return kind, nil
}

// usage is printed for -help and for an unknown command.
func usage() {
	fmt.Fprintf(os.Stderr, `Argus - a read-only dashboard for a GitHub organisation or account.

  argus                run it
  argus setup          answer a few questions and be done
  argus init           write a starter configuration file
  argus doctor         check the setup and explain anything missing
  argus config         see and change any setting - `+"`argus config --help`"+`
  argus login          sign in to the configured GitHub App
  argus logout         forget that session
  argus service install    run at login, so it is always warm
  argus service uninstall  stop doing that
  argus version        which build this is

Every setting can be listed, explained and changed from the command line:

  argus config                 every setting, its value, and where it came from
  argus config get KEY         one setting, in full
  argus config set KEY VALUE   write it to the configuration file

A setting is read from the environment first, then the configuration
file, then the built-in default - so an exported variable quietly beats
anything in the file, and `+"`argus config`"+` will say when one is. The file is
$ARGUS_CONFIG, or ./.env in a checkout, or

    %s

Docs: %s
`, config.ConfigFile(), term.Link(os.Stderr, "https://github.com/Tzomily-Anvar/argus"))
}

// firstRun is what someone sees the very first time they run Argus with
// nothing set up: three commands, in the order they need them.
func firstRun() {
	// The distroless image has no shell, so none of these commands can
	// be typed there. A container is configured through compose instead,
	// and saying otherwise sends someone looking for a prompt that does
	// not exist.
	if config.InContainer() {
		fmt.Fprintf(os.Stderr, `
  Argus is not configured yet.

  Set ARGUS_GITHUB_ORG in your .env beside docker-compose.yml - your
  organisation, or your own username - then:

      ./run.sh up

  Copy .env.example to .env if you have not already. Everything except
  the account has a working default.

`)
		return
	}

	fmt.Fprintf(os.Stderr, `
  Argus is not configured yet.

  1.  argus init      write a starter configuration file and say where
                      it is, then open it and set the organisation or
                      username to sweep

  2.  argus login     sign in to GitHub
                      (or put a token in the file instead - either works)

  3.  argus doctor    check it over, and explain anything still missing

  Then run argus again.

`)
}
