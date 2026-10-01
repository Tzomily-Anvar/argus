// Package backlog is the read side of the Backlog tool: one sweep of a
// project's open tickets, kept in memory, and the views drawn over it.
//
// The sweep is the whole cost. A project's open backlog is a few hundred
// issues, fetched in pages the search endpoint hands out one token at a
// time, plus the board's sprints, the open epics and the personal inbox.
// Every view - the groups, the epics, the requests from outside the team
// - is a pure function over that one fetch and the acknowledgements and
// requesters held locally, so a request costs microseconds and a test needs no
// Jira.
//
// It reads and nothing else. Acknowledging a ticket writes a watermark
// to the store; the ticket in Jira is untouched. The sweep runs on the
// same interval as the GitHub one and backs off the same way when nobody
// has looked for a while, for the same reason: a backlog nobody is
// grooming does not need to be fresh.
package backlog

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
)

// Config is what one team's backlog looks like: the project, the
// thresholds, the spelling of the request label, and the conventions
// shared with the sprint report.
type Config struct {
	Project string
	BaseURL string

	// EpicProjects are other projects whose open Epics the picker offers
	// beside this project's, for a team whose epics live on more than
	// one board.
	EpicProjects []string

	StaleDays int
	NewDays   int

	// RequestLabel marks a ticket as a request from outside the team;
	// LegacyLabels are its older spellings, read as the same thing;
	// WorkLabels mark engineering work, which a request should not be
	// labelled as. A typical setup is an operations or support team
	// whose tickets engineering triages, but nothing here assumes so.
	RequestLabel string
	LegacyLabels []string
	WorkLabels   []string
	// UnrefinedStatuses are status names that count as unrefined, on top
	// of Jira's "new" category.
	UnrefinedStatuses []string
	InboxDays         int

	// StoryLinkTypes tie a Task to its Story where Jira's parent field
	// is taken by the Epic; ContainerTypes are the types that play the
	// Story. Both come from the sprint report's settings.
	StoryLinkTypes []string
	ContainerTypes []string

	// EpicClasses is the Run/Build split, matched against an epic's
	// summary. The Build class is the one the views ask about.
	EpicClasses map[string]*regexp.Regexp

	// PointsField, EstimateField and SprintField pin the custom field ids.
	// Left empty, each is resolved the way the sprint report resolves
	// them: the sprint and estimate fields by name, points by the board's
	// estimation field and then by name.
	PointsField       string
	EstimateField     string
	SprintField       string
	PointsFieldName   string
	EstimateFieldName string
	SprintFieldName   string
}

// Snapshot is one sweep's result, kept whole so the views can be drawn
// again when an acknowledgement or the roster changes without asking
// Jira anything.
type Snapshot struct {
	SweptAt  time.Time
	Building bool
	Error    string
	Warnings []string
	Issues   []jira.Issue
	Epics    []jira.Issue
	Sprints  []jira.Sprint
	Inbox    []Item
	Fields   Fields
}

// Service owns the Jira client, the store and the cached sweep.
type Service struct {
	client   *jira.Client
	store    store.Store
	cfg      Config
	interval time.Duration

	mu       sync.RWMutex
	snap     Snapshot
	fields   *Fields
	sweeping bool

	// lastRequest is when the backlog was last asked for, seeded with
	// the start time so a fresh process gets its grace before slowing.
	lastRequest time.Time
	trigger     chan struct{}
}

// NewService wires a service. Settings left empty in cfg are read from
// the configuration, so an existing wiring picks them up unchanged.
func NewService(client *jira.Client, st store.Store, cfg Config, interval time.Duration) *Service {
	if cfg.StaleDays <= 0 {
		cfg.StaleDays = config.BacklogStaleDays()
	}
	if cfg.NewDays <= 0 {
		cfg.NewDays = config.BacklogNewDays()
	}
	if cfg.RequestLabel == "" {
		cfg.RequestLabel = config.BacklogRequestLabel()
	}
	if cfg.LegacyLabels == nil {
		cfg.LegacyLabels = config.BacklogLegacyLabels()
	}
	if cfg.UnrefinedStatuses == nil {
		cfg.UnrefinedStatuses = config.BacklogUnrefinedStatuses()
	}
	if cfg.WorkLabels == nil {
		cfg.WorkLabels = config.BacklogWorkLabels()
	}
	if cfg.InboxDays <= 0 {
		cfg.InboxDays = config.BacklogInboxDays()
	}
	if len(cfg.StoryLinkTypes) == 0 {
		cfg.StoryLinkTypes = config.JiraStoryLinkTypes()
	}
	if cfg.ContainerTypes == nil {
		cfg.ContainerTypes = config.Strings("ARGUS_JIRA_CONTAINER_TYPES", []string{"Story"})
	}
	if cfg.PointsField == "" {
		cfg.PointsField = config.JiraPointsField()
	}
	if cfg.PointsFieldName == "" {
		cfg.PointsFieldName = config.JiraPointsFieldName()
	}
	if cfg.EstimateFieldName == "" {
		cfg.EstimateFieldName = config.JiraEstimateFieldName()
	}
	if cfg.SprintField == "" {
		cfg.SprintField = config.JiraSprintField()
	}
	if cfg.SprintFieldName == "" {
		cfg.SprintFieldName = config.JiraSprintFieldName()
	}
	if interval < time.Minute {
		interval = time.Minute
	}
	return &Service{
		client: client, store: st, cfg: cfg, interval: interval,
		lastRequest: time.Now(),
		// Depth one so several refresh clicks coalesce into one sweep.
		trigger: make(chan struct{}, 1),
	}
}

// Config is the configuration in force, for the views and the handlers.
func (s *Service) Config() Config { return s.cfg }

// Start sweeps once now and then on the interval, or sooner when
// Refresh asks. Returns straight away.
func (s *Service) Start() {
	go func() {
		_ = s.Sweep(context.Background())
		idle := false
		for {
			wait := sweep.Interval(s.lastAsked(), time.Now(), s.interval)
			if wait != s.interval && !idle {
				log.Printf("backlog: nobody has looked for a while, sweeping every %s until someone does", wait)
			}
			idle = wait != s.interval
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-s.trigger:
				timer.Stop()
			}
			_ = s.Sweep(context.Background())
		}
	}()
}

func (s *Service) lastAsked() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRequest
}

// Touch records that somebody asked for the backlog. If the sweep had
// backed off, this is the request that wakes it.
func (s *Service) Touch() {
	now := time.Now()
	s.mu.Lock()
	was := s.lastRequest
	s.lastRequest = now
	s.mu.Unlock()
	if sweep.Interval(was, now, s.interval) != s.interval {
		s.Refresh()
	}
}

// Refresh asks for a sweep now. Non-blocking, and a no-op if one is
// already queued.
func (s *Service) Refresh() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Snapshot is the last sweep, with the flags that say how current it is.
func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snap
	snap.Building = s.sweeping
	return snap
}

// Sweep fetches everything the views need and installs it. A failure
// keeps the previous snapshot and records the error, so a Jira outage
// leaves a dated picture rather than an empty one.
func (s *Service) Sweep(ctx context.Context) error {
	s.mu.Lock()
	if s.sweeping {
		s.mu.Unlock()
		return nil
	}
	s.sweeping = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.sweeping = false
		s.mu.Unlock()
	}()

	started := time.Now()
	snap, err := s.fetch()
	if err != nil {
		log.Printf("backlog: sweep failed: %v", err)
		s.mu.Lock()
		s.snap.Error = err.Error()
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	log.Printf("backlog: %d open issues, %d epics, %d inbox items in %s",
		len(snap.Issues), len(snap.Epics), len(snap.Inbox), time.Since(started).Round(time.Millisecond))
	return nil
}

// fetch is one sweep's reads, in the order they depend on each other.
func (s *Service) fetch() (Snapshot, error) {
	f, warnings, err := s.resolve()
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Fields: *f, Warnings: warnings}

	fields := []string{"summary", "issuetype", "status", "created", "updated", "labels",
		"parent", "issuelinks", "reporter", "assignee", "priority", f.Sprint}
	if f.Points != "" {
		fields = append(fields, f.Points)
	}
	if f.Estimate != "" {
		fields = append(fields, f.Estimate)
	}
	// Newest first, because the top of the list is what a person grooming
	// the backlog has not seen yet.
	snap.Issues, err = s.client.Search(
		fmt.Sprintf("project = %s AND statusCategory != Done ORDER BY created DESC", s.cfg.Project), fields, 0)
	if err != nil {
		return Snapshot{}, err
	}
	// Every open epic, not only the ones with open work: the picker for
	// setting a ticket's epic has to offer the empty ones too.
	snap.Epics, err = s.client.Search(epicJQL(s.cfg.Project, s.cfg.EpicProjects),
		[]string{"summary", "status", "created", "updated"}, 0)
	if err != nil {
		return Snapshot{}, err
	}
	if f.BoardID > 0 {
		// Active and future: the sprints a ticket can be assigned to. A
		// board read that fails costs the picker, not the sweep.
		if sprints, err := s.client.BoardSprints(f.BoardID, "active,future"); err != nil {
			snap.Warnings = append(snap.Warnings, fmt.Sprintf("could not list the board's sprints: %v", err))
		} else {
			snap.Sprints = sprints
		}
	}
	items, inboxWarnings := s.fetchInbox()
	snap.Inbox = items
	snap.Warnings = append(snap.Warnings, inboxWarnings...)
	snap.SweptAt = time.Now().UTC()
	return snap, nil
}

// epicJQL selects the open Epics the picker offers: this project's, and
// any other project's the team named. This project's come first in the
// picker because they are listed first here and the sort is by creation
// within the set; the picker searches by text anyway.
func epicJQL(project string, others []string) string {
	keys := []string{project}
	for _, o := range others {
		if o = strings.TrimSpace(o); o != "" && !strings.EqualFold(o, project) {
			keys = append(keys, o)
		}
	}
	if len(keys) == 1 {
		return fmt.Sprintf("project = %s AND issuetype = Epic AND statusCategory != Done ORDER BY created DESC", project)
	}
	return fmt.Sprintf("project in (%s) AND issuetype = Epic AND statusCategory != Done ORDER BY created DESC", strings.Join(keys, ", "))
}
