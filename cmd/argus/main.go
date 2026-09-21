// Command argus serves a read-only overview of a GitHub organisation.
//
// It sweeps in the background and serves from memory, so the dashboard is
// instant whenever you open it. Leave the container running and it stays
// warm; that is what the non-default port is for.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
	"github.com/Tzomily-Anvar/argus/internal/ghauth"
	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
	"github.com/Tzomily-Anvar/argus/internal/term"
)

func main() {
	log.SetFlags(log.Ltime)

	// The final image is distroless: no shell, no curl. So the container
	// healthcheck runs this binary with -healthcheck, which just probes
	// the local server and reports via exit status.
	// Subcommands come before flags so the common ones read as words
	// rather than switches: `argus login`, not `argus -login`.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		if err := command(os.Args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "\n  argus: %s\n\n", err)
			os.Exit(1)
		}
		return
	}

	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit 0 if healthy")
	flag.Usage = usage
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}

	// Configuration is loaded before anything reads a setting.
	if _, err := config.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  argus: %s\n\n", err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		// Someone who has never configured Argus needs a first step, not
		// the name of a setting they have no file to put it in. Installed
		// from a package manager there is no repository to go and read.
		var missing *config.Missing
		if errors.As(err, &missing) && config.Loaded() == "" {
			firstRun()
			os.Exit(1)
		}
		// Configuration problems are the common case here and deserve a
		// readable message, not a stack trace.
		fmt.Fprintf(os.Stderr, "\n  argus: %s\n\n", err)
		os.Exit(1)
	}
}

func run() error {
	org, err := config.Org()
	if err != nil {
		return err
	}
	client, err := gh.NewFromEnv()
	if err != nil {
		return err
	}

	interval := time.Duration(config.Int("ARGUS_REFRESH_MINUTES", 15)) * time.Minute
	cache := sweep.New(client, interval)
	cache.Start()

	sprintSvc, st, err := setUpSprint(context.Background())
	if err != nil {
		return err
	}
	if st != nil {
		defer st.Close()
	}

	port := config.Port()
	enabled := rules.Enabled()
	log.Printf("argus: org %s, %d/%d rules enabled, refreshing every %s",
		org, len(enabled), len(rules.All()), interval)
	if bind := config.Bind(); bind == "127.0.0.1" || bind == "localhost" {
		// This line goes wherever log goes, which under launchd is a file
		// and under systemd is the journal. term.Link is given that same
		// stream so it can see that, and leaves the address plain there.
		log.Printf("argus: %s (loopback only)",
			term.Link(log.Writer(), "http://localhost:"+port))
	} else {
		log.Printf("argus: listening on %s:%s", bind, port)
	}

	srv := server.New(cache)
	if sprintSvc != nil {
		srv.SprintRoutes(sprintSvc, st)
	}
	return server.Run(config.Addr(), srv)
}

// probe returns 0 if the local server answers /healthz.
func probe() int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + config.Port() + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// command runs a subcommand.
func command(name string) error {
	// init is the one command that must work before configuration exists.
	if name != "init" {
		if _, err := config.Load(); err != nil {
			return err
		}
	}
	switch name {
	case "init":
		return initConfig()
	case "doctor":
		return doctor()
	case "login":
		return appSession(true)
	case "logout":
		return appSession(false)
	case "service":
		action := "install"
		if len(os.Args) > 2 {
			action = os.Args[2]
		}
		if action != "install" && action != "uninstall" {
			return fmt.Errorf("usage: argus service [install|uninstall]")
		}
		return serviceCommand(action)
	case "setup":
		return setup()
	case "version", "--version":
		fmt.Println(versionString())
		return nil
	case "help":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("unknown command %q", name)
}

// appSource builds the App token source from configuration.
func appSource() *ghauth.Source {
	return ghauth.NewSource(config.GitHubAppClientID(), config.DataDir())
}

// appSession signs in to, or out of, the configured GitHub App.
//
// This runs in the foreground on purpose. A device code expires in
// fifteen minutes, and one printed by a detached container would sit
// unread in the logs until it did.
func appSession(login bool) error {
	id := config.GitHubAppClientID()
	if id == "" {
		return fmt.Errorf(
			"no GitHub App configured. Set ARGUS_GITHUB_APP_CLIENT_ID, or use a personal access token")
	}
	src := appSource()

	if !login {
		if err := src.Logout(); err != nil {
			return err
		}
		fmt.Println("Signed out. The session has been removed.")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()

	err := src.Login(ctx, func(code ghauth.DeviceCode) {
		fmt.Printf("\n  Open %s\n  and enter this code:\n\n      %s\n\n  Waiting...\n",
			term.Link(os.Stdout, code.VerificationURI), code.UserCode)
	})
	if err != nil {
		switch {
		case errors.Is(err, ghauth.ErrExpired):
			return fmt.Errorf("the code expired before it was entered. Run this again")
		case errors.Is(err, ghauth.ErrDenied):
			return fmt.Errorf("authorisation was declined")
		}
		return err
	}

	fmt.Printf("\n  Signed in. The session is kept in %s\n\n", src.SessionPath())
	return nil
}
