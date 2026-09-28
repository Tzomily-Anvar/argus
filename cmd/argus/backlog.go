package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/backlog"
	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// setUpBacklog wires the backlog tool, or reports why it stayed off.
//
// It takes the store the sprint report opened, when it opened one, so
// the two tools running together share one data directory or database
// rather than each opening the same files. With the sprint report off
// it opens the store itself, and returns it so main can close it.
//
// Absent Jira configuration is not an error here either: the tool is
// off unless ARGUS_TOOLS names it, and a person who named it without
// configuring Jira gets one line saying which setting is missing.
func setUpBacklog(ctx context.Context, shared store.Store) (*backlog.Service, *backlog.Batch, store.Store, error) {
	if !config.ToolEnabled("backlog") {
		return nil, nil, shared, nil
	}

	base, err := config.JiraBaseURL()
	if err != nil {
		log.Printf("argus: backlog off (%v)", err)
		return nil, nil, shared, nil
	}
	email, token, err := config.JiraCredentials()
	if err != nil {
		log.Printf("argus: backlog off (%v)", err)
		return nil, nil, shared, nil
	}
	project, err := config.JiraProject()
	if err != nil {
		log.Printf("argus: backlog off (%v)", err)
		return nil, nil, shared, nil
	}

	st := shared
	if st == nil {
		if st, err = openStore(ctx); err != nil {
			return nil, nil, nil, err
		}
		if err := st.Migrate(ctx); err != nil {
			return nil, nil, nil, err
		}
		// The sprint report runs retention when it opens the store;
		// with it off, this is the only tool with a store to keep tidy.
		go retain(ctx, st)
	}

	// A client of its own, and one that never has writes switched on:
	// this side of the tool only reads, and the gate in the client is
	// what makes that a property rather than a promise.
	client, err := jira.New(base, email, token, config.Concurrency(), config.HTTPTimeout())
	if err != nil {
		return nil, nil, nil, err
	}

	// The bulk bar gets a client of its own, whose gate NewBatch opens as
	// far as the settings allow: writes, and deletes only when asked for
	// separately. The read side above keeps a client that can do neither.
	writer, err := jira.New(base, email, token, 1, config.HTTPTimeout())
	if err != nil {
		return nil, nil, nil, err
	}
	batch := backlog.NewBatch(writer, st, backlog.BatchConfig{Actor: email,
		EnableWrites:    config.SprintWritesAllowed(),
		AllowDelete:     config.BacklogDeleteAllowed(),
		OperationsLabel: config.BacklogOperationsLabel(),
		LegacyLabels:    config.BacklogLegacyLabels(),
		StoryLinkTypes:  config.JiraStoryLinkTypes(),
		StoryLinkChild:  config.BacklogStoryLinkChild(),
		ContainerTypes:  config.Strings("ARGUS_JIRA_CONTAINER_TYPES", []string{"Story"}),
		SprintField:     config.JiraSprintField(),
		SprintFieldName: config.String("ARGUS_JIRA_SPRINT_FIELD_NAME", "Sprint"),
		PointsField:     config.JiraPointsField(),
	})

	interval := time.Duration(config.Int("ARGUS_REFRESH_MINUTES", 15)) * time.Minute
	svc := backlog.NewService(client, st, backlog.Config{
		Project:         project,
		BaseURL:         strings.TrimRight(base, "/"),
		StaleDays:       config.BacklogStaleDays(),
		NewDays:         config.BacklogNewDays(),
		OperationsLabel: config.BacklogOperationsLabel(),
		LegacyLabels:    config.BacklogLegacyLabels(),
		InboxDays:       config.BacklogInboxDays(),
		StoryLinkTypes:  config.JiraStoryLinkTypes(),
		ContainerTypes:  config.Strings("ARGUS_JIRA_CONTAINER_TYPES", []string{"Story"}),
		EpicClasses:     epicClasses(),
		EpicProjects:    config.BacklogEpicProjects(),
	}, interval)
	svc.Start()

	log.Printf("argus: backlog on (project %s, stale after %d days, new for %d days, inbox %d days)",
		project, config.BacklogStaleDays(), config.BacklogNewDays(), config.BacklogInboxDays())
	return svc, batch, st, nil
}
