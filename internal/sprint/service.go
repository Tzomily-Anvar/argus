package sprint

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// Service is what the HTTP layer talks to. It owns the Jira client, the
// store, and a small in-memory cache of built reports.
//
// The caching shape follows what the interface promises. The sprint list
// is fetched live, because it costs about half a second and caching it
// would buy a staleness bug instead. A built report is cached, because
// building one costs several seconds and a person reading it should not
// pay that twice.
type Service struct {
	client *jira.Client
	store  store.Store
	cfg    Config

	mu       sync.RWMutex
	reports  map[int64]*cached
	fieldIDs *fields

	// Sprint number to Jira id. Resolving a sprint costs a round trip, and
	// paying it before every cache lookup would make a cached report as
	// slow as a fresh one - which is the whole thing the cache exists to
	// avoid. The mapping does not change once a sprint exists.
	sprintIDs map[int]int64
}

type Config struct {
	Project          string
	BaseURL          string
	Rules            Rules
	SprintLengthDays int
	EpicClasses      map[string]*regexp.Regexp

	// RecentSprints is how many closed sprints appear beside the current
	// one. Future sprints are never listed: they are empty shells nobody
	// reports on.
	RecentSprints int
}

type fields struct {
	sprint string
	points string
}

type cached struct {
	report   Report
	builtAt  time.Time
	building bool
	buildErr error
}

func NewService(client *jira.Client, st store.Store, cfg Config) *Service {
	if cfg.RecentSprints <= 0 {
		cfg.RecentSprints = 4
	}
	return &Service{
		client: client, store: st, cfg: cfg,
		reports:   map[int64]*cached{},
		sprintIDs: map[int]int64{},
	}
}

// Option is one entry in the sprint dropdown.
type Option struct {
	JiraID  int64     `json:"jira_id"`
	Number  int       `json:"number"`
	Name    string    `json:"name"`
	State   string    `json:"state"`
	Starts  time.Time `json:"starts,omitempty"`
	Ends    time.Time `json:"ends,omitempty"`
	Current bool      `json:"current"`

	// HasReport marks a sprint already swept, so the dropdown can show
	// which ones open instantly. It comes from the database, which is why
	// the list can render before Jira answers.
	HasReport bool       `json:"has_report"`
	ReportAt  *time.Time `json:"report_at,omitempty"`
}

// resolveFields looks up the custom field ids once. They cannot be
// hardcoded - a story points field has a different id on every site - and
// they do not change while the process runs.
func (s *Service) resolveFields() (*fields, error) {
	s.mu.RLock()
	f := s.fieldIDs
	s.mu.RUnlock()
	if f != nil {
		return f, nil
	}

	sprintID, err := s.client.FieldID("Sprint")
	if err != nil {
		return nil, fmt.Errorf("resolving the Sprint field: %w", err)
	}
	if sprintID == "" {
		return nil, fmt.Errorf("no field named Sprint on this Jira site")
	}
	pointsID, err := s.client.FieldID("Story Points")
	if err != nil {
		return nil, fmt.Errorf("resolving the Story Points field: %w", err)
	}

	f = &fields{sprint: sprintID, points: pointsID}
	s.mu.Lock()
	s.fieldIDs = f
	s.mu.Unlock()
	return f, nil
}

// Sprints lists sprints for the picker, marking which already have a
// stored report.
//
// With all false it returns the current sprint and the few before it,
// which is what almost every visit wants. With all true it returns the
// whole board, for the times you need one from months ago.
func (s *Service) Sprints(ctx context.Context, all bool) ([]Option, error) {
	f, err := s.resolveFields()
	if err != nil {
		return nil, err
	}

	current, err := s.client.ResolveSprint(s.cfg.Project, f.sprint, 0)
	if err != nil {
		return nil, err
	}

	list := []jira.Sprint{current}
	if current.OriginBoardID > 0 {
		// One call gives the whole board. Future sprints are excluded:
		// they hold nothing and reporting on them is meaningless.
		siblings, err := s.client.BoardSprints(current.OriginBoardID, "active,closed")
		if err == nil {
			list = siblings
		}
	}

	known := map[int64]time.Time{}
	if stats, err := s.store.ListStats(ctx, 0); err == nil {
		for _, st := range stats {
			known[st.SprintJiraID] = st.RecordedAt
		}
	}

	limit := s.cfg.RecentSprints
	if all {
		limit = len(list)
	}

	out := make([]Option, 0, limit+1)
	for _, sp := range list {
		if len(out) >= limit {
			break
		}
		o := Option{
			JiraID: sp.ID, Number: sp.Number, Name: sp.Name, State: sp.State,
			Starts: sp.StartDate.Time, Ends: sp.EndDate.Time,
			Current: sp.ID == current.ID,
		}
		if at, ok := known[sp.ID]; ok {
			o.HasReport = true
			o.ReportAt = &at
		}
		out = append(out, o)
	}
	return out, nil
}

// Invalidate discards cached reports so the next read rebuilds them.
//
// Baselines and capacity are inputs to a report, not part of the sweep,
// so changing one has to invalidate what was computed from it. Without
// this a saved baseline lands in storage and never reaches the screen -
// which looks exactly like the save having failed.
//
// Only the derived reports are dropped; the sprint-number-to-id mapping
// is kept, since that never changes.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.reports = map[int64]*cached{}
	s.mu.Unlock()
}

// ReportResult carries a report plus how fresh it is, so the UI can say
// "as of" rather than implying the numbers are live.
type ReportResult struct {
	Report   Report    `json:"report"`
	BuiltAt  time.Time `json:"built_at"`
	Building bool      `json:"building"`
	Stale    bool      `json:"stale"`
}

// Report returns a sprint's report, building it if necessary.
//
// A cached report comes back immediately and is refreshed in the
// background, which is what keeps opening a sprint you have looked at
// before instant. Only a sprint never swept costs a wait.
func (s *Service) Report(ctx context.Context, sprintNumber int, force bool) (ReportResult, error) {
	// Serve a known sprint from memory without asking Jira which sprint it
	// is. A sprint number maps to one id forever, so once seen it never
	// needs resolving again.
	if !force && sprintNumber > 0 {
		s.mu.RLock()
		id, known := s.sprintIDs[sprintNumber]
		var entry *cached
		if known {
			entry = s.reports[id]
		}
		s.mu.RUnlock()

		if entry != nil && entry.buildErr == nil {
			stale := time.Since(entry.builtAt) > 10*time.Minute
			if stale {
				go s.refresh(id)
			}
			return ReportResult{
				Report: entry.report, BuiltAt: entry.builtAt,
				Building: stale, Stale: stale,
			}, nil
		}
	}

	f, err := s.resolveFields()
	if err != nil {
		return ReportResult{}, err
	}
	sp, err := s.client.ResolveSprint(s.cfg.Project, f.sprint, sprintNumber)
	if err != nil {
		return ReportResult{}, err
	}

	s.mu.Lock()
	if sp.Number > 0 {
		s.sprintIDs[sp.Number] = sp.ID
	}
	entry := s.reports[sp.ID]
	s.mu.Unlock()

	if entry != nil && entry.buildErr == nil && !force {
		stale := time.Since(entry.builtAt) > 10*time.Minute
		if stale {
			go func() { _, _ = s.build(context.Background(), sp, f) }()
		}
		return ReportResult{
			Report: entry.report, BuiltAt: entry.builtAt,
			Building: stale, Stale: stale,
		}, nil
	}

	rep, err := s.build(ctx, sp, f)
	if err != nil {
		return ReportResult{}, err
	}
	return ReportResult{Report: rep, BuiltAt: time.Now().UTC()}, nil
}

// refresh rebuilds a sprint in the background, resolving it by id so the
// caller never waits.
func (s *Service) refresh(sprintID int64) {
	ctx := context.Background()
	f, err := s.resolveFields()
	if err != nil {
		return
	}
	sp, err := s.client.SprintByID(sprintID)
	if err != nil {
		return
	}
	_, _ = s.build(ctx, sp, f)
}

// build fetches and computes, then records the summary so trends have
// history without re-fetching years of Jira.
func (s *Service) build(ctx context.Context, sp jira.Sprint, f *fields) (Report, error) {
	s.mu.Lock()
	if s.reports[sp.ID] == nil {
		s.reports[sp.ID] = &cached{}
	}
	if s.reports[sp.ID].building {
		s.mu.Unlock()
		return s.reports[sp.ID].report, nil
	}
	s.reports[sp.ID].building = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.reports[sp.ID].building = false
		s.mu.Unlock()
	}()

	issues, err := s.client.Search(
		fmt.Sprintf("project = %s AND sprint = %d", s.cfg.Project, sp.ID),
		[]string{"summary", "issuetype", "status", "assignee", "created",
			"resolutiondate", "parent", "labels", f.points},
		0)
	if err != nil {
		return Report{}, err
	}

	people, _ := s.store.ListPeople(ctx, true)
	capacity, _ := s.store.ListCapacity(ctx, sp.ID)

	rep := Build(Inputs{
		Sprint: sp, Issues: issues, People: people, Capacity: capacity,
		Rules: s.cfg.Rules, PointsField: f.points,
		BaseURL: s.cfg.BaseURL, Project: s.cfg.Project,
		EpicClasses: s.cfg.EpicClasses, SprintLengthDays: s.cfg.SprintLengthDays,
	})

	s.mu.Lock()
	s.reports[sp.ID] = &cached{report: rep, builtAt: time.Now().UTC()}
	s.mu.Unlock()

	s.persist(ctx, sp, rep)
	return rep, nil
}

// persist records the sprint and its aggregate. Per-person rows are not
// written here: capacity is a human input, not something a sweep should
// overwrite.
func (s *Service) persist(ctx context.Context, sp jira.Sprint, rep Report) {
	ends := sp.EndDate.Time
	starts := sp.StartDate.Time
	_ = s.store.PutSprint(ctx, store.Sprint{
		JiraID: sp.ID, Label: sp.Name, Number: sp.Number,
		StartsAt: nonZero(starts), EndsAt: nonZero(ends), State: sp.State,
	})

	byClass := map[string]float64{}
	for _, e := range rep.Epics {
		if e.Class != "" {
			byClass[e.Class] += e.Points
		}
	}
	var storyPts float64
	for _, st := range rep.Stories {
		storyPts += st.Points
	}

	_ = s.store.PutStats(ctx, store.SprintStats{
		SprintJiraID:     sp.ID,
		BaselineTotal:    rep.Summary.BaselineTotal,
		CapacityTotal:    rep.Summary.CapacityTotal,
		DeliveredTotal:   rep.Summary.DeliveredTotal,
		PlannedDaysOff:   rep.Summary.PlannedDaysOff,
		UnplannedDaysOff: rep.Summary.UnplannedDaysOff,
		Promised:         rep.Summary.Promised,
		Injected:         rep.Summary.Injected,
		Completed:        rep.Summary.Completed,
		ByEpicClass:      byClass,
		StoriesDone:      len(rep.Stories),
		StoryPointsDone:  storyPts,
	})
}

func nonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// ParseSprintNumber reads a sprint number from a query string value.
func ParseSprintNumber(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
