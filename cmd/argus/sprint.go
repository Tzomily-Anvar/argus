package main

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
	"github.com/Tzomily-Anvar/argus/internal/store/pgstore"
)

// setUpSprint wires the sprint report, or reports why it stayed off.
//
// Absent Jira configuration is not an error: most people running Argus
// want the pull request tool and nothing else, and they should not have
// to read a failure about a tool they never asked for.
func setUpSprint(ctx context.Context) (*sprint.Service, store.Store, error) {
	// Switched off unless asked for. Checked before anything else, so a
	// deployment that only wants pull request triage opens no database,
	// makes no Jira call, and reads no message about a tool it never
	// enabled.
	if !config.ToolEnabled("sprint") {
		return nil, nil, nil
	}

	base, err := config.JiraBaseURL()
	if err != nil {
		log.Printf("argus: sprint report off (%v)", err)
		return nil, nil, nil
	}
	email, token, err := config.JiraCredentials()
	if err != nil {
		log.Printf("argus: sprint report off (%v)", err)
		return nil, nil, nil
	}
	project, err := config.JiraProject()
	if err != nil {
		log.Printf("argus: sprint report off (%v)", err)
		return nil, nil, nil
	}

	st, err := openStore(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		return nil, nil, err
	}

	client, err := jira.New(base, email, token, config.Concurrency(), config.HTTPTimeout())
	if err != nil {
		return nil, nil, err
	}

	svc := sprint.NewService(client, st, sprint.Config{
		Project: project,
		BaseURL: strings.TrimRight(base, "/"),
		Rules: sprint.Rules{
			Done:               config.JiraDoneStatuses(),
			Container:          config.Strings("ARGUS_JIRA_CONTAINER_TYPES", []string{"Story"}),
			EstimatedOnResolve: config.Strings("ARGUS_JIRA_ESTIMATED_ON_RESOLVE", []string{"Bug"}),
			ExcludedFromSayDo:  config.JiraExcludedTypes(),
		},
		SprintLengthDays: config.JiraSprintLengthDays(),
		EpicClasses:      epicClasses(),
		RecentSprints:    config.Int("ARGUS_SPRINT_RECENT", 4),
	})

	log.Printf("argus: sprint report on (project %s, %d recent sprints)",
		project, config.Int("ARGUS_SPRINT_RECENT", 4))
	go retain(ctx, st)
	return svc, st, nil
}

// openStore picks a backend. Files by default, so nobody has to run a
// database to try the tool; Postgres when a connection string is given.
func openStore(ctx context.Context) (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		log.Printf("argus: storage = postgres")
		return pgstore.New(ctx, dsn)
	}
	dir := config.String("ARGUS_DATA_DIR", "/data")
	log.Printf("argus: storage = files in %s", dir)
	return jsonstore.New(dir)
}

// retain drops per-person rows past the retention window. Trends live on
// in the aggregates, which carry no personal data; a sprint tool should
// not quietly become a permanent record of who was away when.
func retain(ctx context.Context, st store.Store) {
	years := config.Int("ARGUS_SPRINT_RETAIN_YEARS", 3)
	if years <= 0 {
		return
	}
	cutoff := time.Now().UTC().AddDate(-years, 0, 0)
	res, err := st.Prune(ctx, cutoff)
	if err != nil {
		log.Printf("argus: retention failed: %v", err)
		return
	}
	if res.CapacityRows > 0 || res.WriteRows > 0 {
		log.Printf("argus: retention removed %d capacity rows and %d write-log entries older than %d years",
			res.CapacityRows, res.WriteRows, years)
	}
}

// epicClasses reads the Run/Build style split. A team labels these
// however it likes, so the classes and their patterns are both config.
func epicClasses() map[string]*regexp.Regexp {
	raw := config.Strings("ARGUS_JIRA_EPIC_CLASSES", []string{`Run:^\[?run\]?`, `Build:^\[?build\]?`})
	out := map[string]*regexp.Regexp{}
	for _, entry := range raw {
		name, pattern, found := strings.Cut(entry, ":")
		if !found || name == "" || pattern == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			log.Printf("argus: epic class %q has an invalid pattern: %v", name, err)
			continue
		}
		out[strings.TrimSpace(name)] = re
	}
	return out
}
