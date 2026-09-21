// Command argus serves a read-only overview of a GitHub organisation.
//
// It sweeps in the background and serves from memory, so the dashboard is
// instant whenever you open it. Leave the container running and it stays
// warm; that is what the non-default port is for.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

func main() {
	log.SetFlags(log.Ltime)

	// The final image is distroless: no shell, no curl. So the container
	// healthcheck runs this binary with -healthcheck, which just probes
	// the local server and reports via exit status.
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit 0 if healthy")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}

	if err := run(); err != nil {
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

	port := config.Port()
	enabled := rules.Enabled()
	log.Printf("argus: org %s, %d/%d rules enabled, refreshing every %s",
		org, len(enabled), len(rules.All()), interval)
	if bind := config.Bind(); bind == "127.0.0.1" || bind == "localhost" {
		log.Printf("argus: http://localhost:%s (loopback only)", port)
	} else {
		log.Printf("argus: listening on %s:%s", bind, port)
	}

	return server.Run(config.Addr(), server.New(cache))
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
