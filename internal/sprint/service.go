package sprint

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
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

	// inputs counts changes to the locally held half of a report -
	// baselines, capacity, capacity reviews. A sweep that started before
	// a save must not install figures computed before it, and comparing
	// this counter either side of the computation is how that is caught.
	inputs uint64

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

	// StoryLinkTypes are the issue link types that tie a container to the
	// work beneath it. Left empty, NewService reads the setting.
	StoryLinkTypes []string

	// HoursPerPoint is what one point is worth in logged time, which is
	// how a sprint gets credit for work on a ticket that finished
	// somewhere else. Left zero, NewService reads the setting.
	HoursPerPoint float64
}

type fields struct {
	sprint   string
	points   string
	estimate string

	// categories maps a status id to new, indeterminate or done. It is
	// resolved once alongside the field ids because it changes about as
	// often - which is to say when somebody edits the workflow.
	categories map[string]string
}

type cached struct {
	report   Report
	builtAt  time.Time
	building bool
	buildErr error

	// The Jira half of the inputs, kept so that a changed baseline or
	// capacity can be folded in without asking Jira again. A sprint's
	// issues are a few hundred small structs and only the handful of
	// sprints somebody has opened are held, which is a cheap price for
	// making a saved edit appear at once instead of after a sweep.
	sprint jira.Sprint
	issues []jira.Issue
	fields *fields

	// The dated half: status histories, the issues linked beneath this
	// sprint's containers, and when the previous sprint actually closed.
	// All of it is Jira's answer rather than a local input, so it is kept
	// beside the issues and reused when a baseline changes.
	changes  map[string][]jira.StatusChange
	linked   map[string]jira.Issue
	previous time.Time
}

func NewService(client *jira.Client, st store.Store, cfg Config) *Service {
	if cfg.RecentSprints <= 0 {
		cfg.RecentSprints = 4
	}
	// Both of these have a working default and a setting, and a caller
	// that pins neither gets the setting. Reading them here rather than
	// demanding them from every caller means an existing wiring picks up
	// the setting without being rebuilt around it.
	if len(cfg.StoryLinkTypes) == 0 {
		cfg.StoryLinkTypes = config.JiraStoryLinkTypes()
	}
	if cfg.HoursPerPoint <= 0 {
		cfg.HoursPerPoint = config.HoursPerPoint()
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
	// The estimate is optional. A site without one simply has no fallback
	// for a finished ticket whose actual was never filled in, which is
	// worse but is not a reason to refuse to build a report.
	estimateID, err := s.client.FieldID(config.JiraEstimateFieldName())
	if err != nil {
		return nil, fmt.Errorf("resolving the %s field: %w", config.JiraEstimateFieldName(), err)
	}

	categories, err := s.client.StatusCategories()
	if err != nil {
		return nil, fmt.Errorf("reading the status catalogue: %w", err)
	}

	f = &fields{sprint: sprintID, points: pointsID, estimate: estimateID, categories: categories}
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

// Recompute folds a changed baseline, capacity or capacity review into
// every report already built, without asking Jira anything.
//
// This used to throw the cache away instead, and that is what made saving
// a number feel broken. Dropping a report means the next read pays a full
// sweep - several seconds against Jira - so the browser refetched, got
// nothing back yet, and redrew the field with the old value. The edit
// then appeared some time later, when the sweep landed.
//
// Nothing about Jira changes when somebody types a day off. Only the
// locally stored half does, and Build is a pure function, so recomputing
// from the issues already in memory costs microseconds. The figures are
// therefore right by the time the browser asks for them.
//
// The sprint-number-to-id mapping is left alone, since that never changes.
func (s *Service) Recompute(ctx context.Context) {
	s.mu.Lock()
	s.inputs++
	type job struct {
		sprint  jira.Sprint
		issues  []jira.Issue
		fetched fetched
		builtAt time.Time
	}
	jobs := make([]job, 0, len(s.reports))
	for id, c := range s.reports {
		if c.building {
			// A sweep is mid-flight. It reads the store itself and the
			// version check in install stops it installing figures from
			// before this change, so leaving it alone is correct.
			continue
		}
		if len(c.issues) == 0 {
			// Nothing to recompute from - a report built before this
			// process learned to keep the issues, or one still building.
			// Drop it so the next read rebuilds honestly.
			delete(s.reports, id)
			continue
		}
		jobs = append(jobs, job{
			sprint: c.sprint, issues: c.issues, builtAt: c.builtAt,
			fetched: fetched{
				fields: c.fields, changes: c.changes,
				linked: c.linked, previous: c.previous,
			},
		})
	}
	s.mu.Unlock()

	for _, j := range jobs {
		// The original build time is carried over, not refreshed. Nothing
		// was fetched here, so claiming the Jira figures are newer than
		// they are would be the same dishonesty the "as of" label exists
		// to avoid - and it would postpone the next real sweep.
		s.install(ctx, j.sprint, j.issues, j.fetched, j.builtAt)
	}
}

// fetched is everything one sweep took from Jira, kept together so a
// recomputation after a local edit does not have to ask again.
type fetched struct {
	fields   *fields
	changes  map[string][]jira.StatusChange
	linked   map[string]jira.Issue
	previous time.Time
}

// derive computes a report from what was fetched plus the locally stored
// inputs as they stand right now.
func (s *Service) derive(ctx context.Context, sp jira.Sprint, issues []jira.Issue, got fetched) Report {
	people, _ := s.store.ListPeople(ctx, true)
	capacity, _ := s.store.ListCapacity(ctx, sp.ID)
	reviewedAt, _ := s.store.CapacityReviewedAt(ctx, sp.ID)

	in := Inputs{
		Sprint: sp, Issues: issues, People: people, Capacity: capacity,
		Rules:   s.cfg.Rules,
		BaseURL: s.cfg.BaseURL, Project: s.cfg.Project,
		Changes: got.changes, Linked: got.linked, PreviousClose: got.previous,
		StoryLinkTypes: s.cfg.StoryLinkTypes, HoursPerPoint: s.cfg.HoursPerPoint,
		EpicClasses: s.cfg.EpicClasses, SprintLengthDays: s.cfg.SprintLengthDays,
		CapacityReviewedAt: reviewedAt,
	}
	if got.fields != nil {
		in.PointsField = got.fields.points
		in.EstimateField = got.fields.estimate
		in.SprintField = got.fields.sprint
		in.StatusCategories = got.fields.categories
	}
	return Build(in)
}

// install computes a report and caches it, retrying if somebody saved a
// baseline or a capacity while it was computing.
//
// Without that check a slow sweep can finish after a save and overwrite
// the correct figures with ones read from the store before it - the edit
// appears, then vanishes again, for up to the cache lifetime. The loop
// terminates because each pass is a pure recomputation over data already
// in memory and only repeats while somebody is actively saving.
func (s *Service) install(ctx context.Context, sp jira.Sprint, issues []jira.Issue, got fetched, builtAt time.Time) Report {
	for {
		s.mu.RLock()
		version := s.inputs
		s.mu.RUnlock()

		rep := s.derive(ctx, sp, issues, got)

		s.mu.Lock()
		if s.inputs == version {
			s.reports[sp.ID] = &cached{
				report: rep, builtAt: builtAt,
				sprint: sp, issues: issues, fields: got.fields,
				changes: got.changes, linked: got.linked, previous: got.previous,
			}
			s.mu.Unlock()
			return rep
		}
		s.mu.Unlock()
	}
}

// User resolves an account id to the person behind it.
//
// It lives here because the Jira client does, and because the roster
// import is assembled by the HTTP layer out of configuration this service
// deliberately does not hold: an organisation id is nothing to do with
// building a report.
func (s *Service) User(_ context.Context, accountID string) (jira.User, error) {
	if s.client == nil {
		return jira.User{}, fmt.Errorf("no Jira client, so account ids cannot be resolved to names")
	}
	return s.client.User(accountID)
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
		// The entry may have been replaced by install or dropped by a
		// concurrent Recompute, so this cannot assume it is still there.
		if entry := s.reports[sp.ID]; entry != nil {
			entry.building = false
		}
		s.mu.Unlock()
	}()

	// Jira's sprint field is cumulative, so this returns every issue that
	// has ever been in the sprint rather than the work that happened in
	// it. Narrowing it in JQL is not possible - Jira has no "finished
	// during" - so everything comes back and the dating below decides.
	fields := []string{"summary", "issuetype", "status", "assignee", "created", "updated",
		"resolutiondate", "parent", "labels", "issuelinks", "timespent", "worklog", f.points, f.sprint}
	if f.estimate != "" {
		fields = append(fields, f.estimate)
	}
	issues, err := s.client.Search(
		fmt.Sprintf("project = %s AND sprint = %d", s.cfg.Project, sp.ID), fields, 0)
	if err != nil {
		return Report{}, err
	}

	got, err := s.enrich(sp, f, issues)
	if err != nil {
		return Report{}, err
	}

	rep := s.install(ctx, sp, issues, got, time.Now().UTC())

	s.persist(ctx, sp, rep)
	return rep, nil
}

// enrich fetches what the issues alone cannot say: when work reached a
// Done status, what sits underneath this sprint's containers, and when
// the previous sprint really closed.
//
// The cost is three things beyond the search, and each is bounded:
//
//   - The status histories, in bulk. One request per thousand changelog
//     entries, which is two to five for a sprint of sixty issues. Asked
//     for the status field alone, so the response carries transitions
//     rather than every edit anybody ever made.
//   - The issues linked beneath the containers that are not in the sprint
//     themselves, in one search. A Story cannot be judged finished
//     without them.
//   - The previous sprint, one call, for the handover gap.
//
// The worklog is not in that list because a search returns it inline, so
// splitting delivery by who logged the work costs nothing at all. Only an
// issue whose log Jira truncated needs fetching on its own, which for
// this team has never happened.
func (s *Service) enrich(sp jira.Sprint, f *fields, issues []jira.Issue) (fetched, error) {
	got := fetched{fields: f}

	linked, err := s.linkedIssues(f, issues)
	if err != nil {
		return got, err
	}
	got.linked = linked

	ids := make([]string, 0, len(issues)+len(linked))
	seen := make(map[string]bool, len(issues)+len(linked))
	for _, is := range issues {
		if is.ID != "" && !seen[is.ID] {
			seen[is.ID] = true
			ids = append(ids, is.ID)
		}
	}
	for _, is := range linked {
		if is.ID != "" && !seen[is.ID] {
			seen[is.ID] = true
			ids = append(ids, is.ID)
		}
	}
	changes, err := s.client.StatusHistory(ids)
	if err != nil {
		return got, err
	}
	got.changes = changes
	got.previous = s.previousClose(sp)
	return got, nil
}

// linkedIssues fetches the work linked beneath this sprint's containers
// that the sprint does not already contain.
//
// Keyed by key rather than id because that is what a link carries. The
// issues already in hand are included, so the caller never has to decide
// which map to look in.
func (s *Service) linkedIssues(f *fields, issues []jira.Issue) (map[string]jira.Issue, error) {
	out := make(map[string]jira.Issue, len(issues))
	for _, is := range issues {
		out[is.Key] = is
	}

	var missing []string
	for _, is := range issues {
		if !s.cfg.Rules.IsContainer(is.Fields.IssueType.Name) {
			continue
		}
		for _, link := range is.Fields.Links {
			if !hasFold(s.cfg.StoryLinkTypes, link.Type.Name) {
				continue
			}
			other := link.Other()
			if other == nil || other.Key == "" {
				continue
			}
			if _, have := out[other.Key]; have {
				continue
			}
			if !s.cfg.Rules.IsWork(other.Fields.IssueType.Name) {
				continue
			}
			out[other.Key] = jira.Issue{}
			missing = append(missing, other.Key)
		}
	}
	for _, key := range missing {
		delete(out, key)
	}
	if len(missing) == 0 {
		return out, nil
	}

	sort.Strings(missing)
	fields := []string{"summary", "issuetype", "status", "assignee", "created", "updated",
		"resolutiondate", "parent", f.points}
	if f.estimate != "" {
		fields = append(fields, f.estimate)
	}
	// One search however many keys there are, because the work beneath a
	// sprint's Stories runs to a few dozen and asking for them one at a
	// time would be a few dozen round trips.
	found, err := s.client.Search(fmt.Sprintf("key in (%s)", strings.Join(missing, ", ")), fields, 0)
	if err != nil {
		return nil, err
	}
	for _, is := range found {
		out[is.Key] = is
	}
	return out, nil
}

// previousClose is when the sprint before this one was completed, zero
// when it cannot be worked out.
//
// Jira leaves a gap of a few minutes between one sprint being completed
// and the next being started, and work does finish in it - one ticket
// worth five points did, in the sprints this was measured on. Without
// this the gap is a hole that work falls into and no sprint claims.
func (s *Service) previousClose(sp jira.Sprint) time.Time {
	if sp.OriginBoardID == 0 || sp.Number <= 0 {
		return time.Time{}
	}
	siblings, err := s.client.BoardSprints(sp.OriginBoardID, "closed")
	if err != nil {
		return time.Time{}
	}
	var best jira.Sprint
	for _, sib := range siblings {
		if sib.ID == sp.ID || sib.CompleteDate.IsZero() {
			continue
		}
		if !sib.CompleteDate.Before(sp.StartDate.Time) {
			continue
		}
		if best.CompleteDate.IsZero() || sib.CompleteDate.After(best.CompleteDate.Time) {
			best = sib
		}
	}
	return best.CompleteDate.Time
}

// hasFold reports whether name appears in list, ignoring case and
// surrounding space.
func hasFold(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
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
