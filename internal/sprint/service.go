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

	// trendFilling guards the background sweep behind the calibration
	// trend, so a browser polling every few seconds asks for one run of
	// builds rather than a new one each time it asks.
	trendFilling bool

	// Sprint number to Jira id. Resolving a sprint costs a round trip, and
	// paying it before every cache lookup would make a cached report as
	// slow as a fresh one - which is the whole thing the cache exists to
	// avoid. The mapping does not change once a sprint exists.
	sprintIDs map[int]int64

	// changes holds the previews built and not yet applied or expired.
	// In memory on purpose: a preview is an in-flight intention rather
	// than a record of anything, and losing it on a restart is right.
	changes *changeSets

	// declared is the last reading of what Jira's configuration says,
	// kept so a sweep whose read fails builds on the previous reading,
	// dated and marked stale, rather than on a built-in default that
	// would look exactly like a successful read.
	declared Declared
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

	// WorklogAttribution is who a logged entry is credited to, author or
	// mention. Left empty, NewService reads the setting.
	WorklogAttribution string

	// HoursPerDay is a working day in hours, for a breakdown written in
	// days beside a name. Left zero, NewService reads the setting.
	HoursPerDay float64

	// AbsenceCost is how a day off is priced, point or share. Left empty,
	// NewService reads the setting.
	AbsenceCost string

	// Actor is the account any write would be made as - the Jira email
	// the client authenticates with. Named on every preview so the person
	// approving it sees whose name the edits will carry.
	Actor string

	// EnableWrites is ARGUS_SPRINT_ALLOW_WRITES, read once by the caller.
	// When true the Jira client's gate is opened as soon as the site's
	// points field is known, and Apply is allowed to run. This is the one
	// place the setting reaches the client; nothing consults it per
	// request, where it could also be forgotten.
	EnableWrites bool

	// PointsField pins the points field id, and PointsFieldName is the
	// deprecated name to resolve it by where no board declares one. Left
	// empty, NewService reads the settings. The board's own estimation
	// field is what normally decides it, per sprint, in declare.
	PointsField     string
	PointsFieldName string
}

type fields struct {
	sprint   string
	points   string
	estimate string

	// pinned reports that points came from ARGUS_JIRA_POINTS_FIELD, which
	// wins over anything a board declares.
	pinned bool

	// catalogue is every status on the site with its category, and
	// categories the same keyed by id. Resolved once alongside the field
	// ids because they change about as often - which is to say when
	// somebody edits the workflow.
	catalogue  []jira.Status
	categories map[string]string

	// done is the delivered set in force for one sweep and declared what
	// Jira said for it. Both are read again on every sweep, so they live
	// on the copy build makes rather than on the cached original.
	done     []string
	declared Declared
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
	if cfg.WorklogAttribution == "" {
		cfg.WorklogAttribution = config.JiraWorklogAttribution()
	}
	if cfg.HoursPerDay <= 0 {
		cfg.HoursPerDay = config.HoursPerDay()
	}
	if cfg.AbsenceCost == "" {
		cfg.AbsenceCost = config.SprintAbsenceCost()
	}
	if cfg.PointsField == "" {
		cfg.PointsField = config.JiraPointsField()
	}
	if cfg.PointsFieldName == "" {
		cfg.PointsFieldName = config.JiraPointsFieldName()
	}
	return &Service{
		client: client, store: st, cfg: cfg,
		reports:   map[int64]*cached{},
		sprintIDs: map[int]int64{},
		changes:   newChangeSets(func() time.Time { return time.Now().UTC() }),
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
	// Points: the pinned id, or the deprecated name lookup. Either is
	// only the starting point - the board a sprint belongs to declares
	// the field it estimates in, and declare weighs the two per sweep.
	pointsID, pinned := s.cfg.PointsField, s.cfg.PointsField != ""
	if !pinned {
		if pointsID, err = s.client.FieldID(s.cfg.PointsFieldName); err != nil {
			return nil, fmt.Errorf("resolving the %s field: %w", s.cfg.PointsFieldName, err)
		}
	}
	// The estimate is optional. A site without one simply has no fallback
	// for a finished ticket whose actual was never filled in, which is
	// worse but is not a reason to refuse to build a report.
	estimateID, err := s.client.FieldID(config.JiraEstimateFieldName())
	if err != nil {
		return nil, fmt.Errorf("resolving the %s field: %w", config.JiraEstimateFieldName(), err)
	}

	catalogue, err := s.client.Statuses()
	if err != nil {
		return nil, fmt.Errorf("reading the status catalogue: %w", err)
	}
	categories := make(map[string]string, len(catalogue))
	for _, st := range catalogue {
		categories[st.ID] = st.Category.Key
	}

	f = &fields{sprint: sprintID, points: pointsID, pinned: pinned, estimate: estimateID,
		catalogue: catalogue, categories: categories}
	s.mu.Lock()
	if s.fieldIDs == nil && s.cfg.EnableWrites {
		// The gate opens here and nowhere else: once, under the lock so
		// two callers resolving at the same moment do not both do it,
		// and only now, because the gate has to know which custom field
		// id "points" means on this site before it can judge a body.
		s.client.AllowWrites(f.points)
	}
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

// CalibrationTrend is the estimate-against-actual figures for the run of
// sprints up to and including one, oldest first.
//
// It is assembled from built reports rather than from a stored aggregate.
// Which sprint a ticket counts in is decided by the dating rules in
// Build, so a figure frozen at the time of a sweep would go on reporting
// the answer an older rule gave - and the dating has already changed once.
//
// Only sprints already swept come back. The rest are built in the
// background and the answer says so, because a first visit would
// otherwise block on several Jira sweeps at once. The trend ends at the
// sprint being read rather than at today: a report from three sprints ago
// should show the run as it stood then.
func (s *Service) CalibrationTrend(ctx context.Context, sprintNumber, span int) (CalibrationTrend, error) {
	if span <= 0 {
		span = s.cfg.RecentSprints + 2
	}

	f, err := s.resolveFields()
	if err != nil {
		return CalibrationTrend{}, err
	}
	target, err := s.client.ResolveSprint(s.cfg.Project, f.sprint, sprintNumber)
	if err != nil {
		return CalibrationTrend{}, err
	}

	list := []jira.Sprint{target}
	if target.OriginBoardID > 0 {
		if siblings, err := s.client.BoardSprints(target.OriginBoardID, "active,closed"); err == nil {
			list = siblings
		}
	}

	run := make([]jira.Sprint, 0, len(list))
	for _, sp := range list {
		if sp.Number > 0 && sp.Number <= target.Number {
			run = append(run, sp)
		}
	}
	sort.Slice(run, func(i, j int) bool { return run[i].Number < run[j].Number })
	if len(run) > span {
		run = run[len(run)-span:]
	}

	// Always a slice, never nil: this crosses to a browser as JSON, and a
	// null where an array was promised is a crash in the panel rather
	// than an empty chart.
	out := CalibrationTrend{Sprints: make([]CalibrationSprint, 0, len(run))}
	var missing []jira.Sprint
	for _, sp := range run {
		s.mu.RLock()
		entry := s.reports[sp.ID]
		s.mu.RUnlock()
		if entry == nil || entry.buildErr != nil || entry.builtAt.IsZero() {
			missing = append(missing, sp)
			continue
		}
		c := entry.report.Calibration
		// The run is the shape of the thing; the tickets behind one
		// sprint belong to that sprint's own report.
		c.Diverged = nil
		out.Sprints = append(out.Sprints, CalibrationSprint{
			Number: sp.Number, Name: sp.Name, Calibration: c,
		})
	}

	if len(missing) > 0 {
		out.Building = true
		s.fillTrend(missing, f)
	}
	return out, nil
}

// fillTrend sweeps the sprints a trend is missing, one at a time in the
// background.
//
// Sequentially on purpose. Each sweep is several requests against Jira,
// and firing six at once to draw a chart nobody is waiting on is how a
// dashboard gets a team rate limited.
func (s *Service) fillTrend(missing []jira.Sprint, f *fields) {
	s.mu.Lock()
	if s.trendFilling {
		s.mu.Unlock()
		return
	}
	s.trendFilling = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.trendFilling = false
			s.mu.Unlock()
		}()
		for _, sp := range missing {
			// Not the caller's context: it belongs to a request that has
			// already been answered, and cancelling this on its return
			// would mean the trend never fills in at all.
			_, _ = s.build(context.Background(), sp, f)
		}
	}()
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
		WorklogAttribution: s.cfg.WorklogAttribution, HoursPerDay: s.cfg.HoursPerDay,
		AbsenceCost: s.cfg.AbsenceCost,
		EpicClasses: s.cfg.EpicClasses, SprintLengthDays: s.cfg.SprintLengthDays,
		CapacityReviewedAt: reviewedAt,
	}
	if got.fields != nil {
		in.PointsField = got.fields.points
		in.EstimateField = got.fields.estimate
		in.SprintField = got.fields.sprint
		in.StatusCategories = got.fields.categories
		in.Declared = got.fields.declared
		// What Jira declared fills a gap and never overrules a person:
		// the done set only where no override names one, the sprint
		// length only where the setting is unset.
		if len(got.fields.done) > 0 {
			in.Rules.Done = got.fields.done
		}
		if in.SprintLengthDays <= 0 && in.Declared.SprintLengthDays > 0 {
			in.SprintLengthDays = in.Declared.SprintLengthDays
		}
	}
	rep := Build(in)
	if sp.OriginBoardID == 0 && got.fields != nil && !got.fields.pinned {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"This sprint belongs to no board, so the points field could not be read from a board's configuration; it was resolved by the name %q instead.",
			s.cfg.PointsFieldName))
	}
	return rep
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

	// What Jira declares is read again on every sweep, so a status added
	// or a board changed since the last one is seen rather than assumed
	// away. The reading rides on a copy of the field ids for this build.
	f, err := s.declare(sp, f)
	if err != nil {
		return Report{}, err
	}

	// Jira's sprint field is cumulative, so this returns every issue that
	// has ever been in the sprint rather than the work that happened in
	// it. Narrowing it in JQL is not possible - Jira has no "finished
	// during" - so everything comes back and the dating below decides.
	fields := []string{"summary", "issuetype", "status", "assignee", "created", "updated",
		"resolutiondate", "parent", "labels", "issuelinks", "timespent", "worklog", f.points, f.sprint}
	if f.estimate != "" {
		fields = append(fields, f.estimate)
	}
	// Where the board's field was held back, both are fetched so the
	// report can say how many issues each one is populated on.
	if held := f.declared.BoardField; held != "" && held != f.points {
		fields = append(fields, held)
	}
	issues, err := s.client.Search(
		fmt.Sprintf("project = %s AND sprint = %d", s.cfg.Project, sp.ID), fields, 0)
	if err != nil {
		return Report{}, err
	}
	f.declared.BoardFieldPopulated = populated(issues, f.declared.BoardField)
	f.declared.NamedFieldPopulated = populated(issues, f.declared.NamedField)

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
	got.previous, f.declared.SprintLengthDays = s.previousClose(sp)
	f.declared.SprintLengthSetting = s.cfg.SprintLengthDays
	if s.cfg.SprintLengthDays <= 0 && f.declared.SprintLengthDays > 0 {
		config.Declared("ARGUS_JIRA_SPRINT_LENGTH_DAYS", strconv.Itoa(f.declared.SprintLengthDays),
			"sprint dates", time.Now().UTC())
	}
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
// when it cannot be worked out, and how many working days the board's
// most recently closed sprint ran - the sprint length Jira declares.
//
// Jira leaves a gap of a few minutes between one sprint being completed
// and the next being started, and work does finish in it - one ticket
// worth five points did, in the sprints this was measured on. Without
// this the gap is a hole that work falls into and no sprint claims.
func (s *Service) previousClose(sp jira.Sprint) (time.Time, int) {
	if sp.OriginBoardID == 0 || sp.Number <= 0 {
		return time.Time{}, 0
	}
	siblings, err := s.client.BoardSprints(sp.OriginBoardID, "closed")
	if err != nil {
		return time.Time{}, 0
	}
	var best, latest jira.Sprint
	for _, sib := range siblings {
		if sib.CompleteDate.IsZero() {
			continue
		}
		if latest.CompleteDate.IsZero() || sib.CompleteDate.After(latest.CompleteDate.Time) {
			latest = sib
		}
		if sib.ID == sp.ID || !sib.CompleteDate.Before(sp.StartDate.Time) {
			continue
		}
		if best.CompleteDate.IsZero() || sib.CompleteDate.After(best.CompleteDate.Time) {
			best = sib
		}
	}
	return best.CompleteDate.Time, workingDays(latest.StartDate.Time, latest.EndDate.Time)
}

// workingDays counts the weekdays from one date up to, not including,
// another: a sprint starting on a Monday and ending on the Monday two
// weeks later ran ten.
func workingDays(from, to time.Time) int {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return 0
	}
	n := 0
	for d := from.Truncate(24 * time.Hour); d.Before(to.Truncate(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			n++
		}
	}
	return n
}

// populated counts the issues carrying a value in a field.
func populated(issues []jira.Issue, field string) int {
	if field == "" {
		return 0
	}
	n := 0
	for _, is := range issues {
		if _, ok := is.Number(field); ok {
			n++
		}
	}
	return n
}

// declare reads what Jira's configuration says for this sweep: the
// project's statuses and their categories, and the board's estimation
// field and columns. Each read fills a gap and never overrules a person:
// the done set only where ARGUS_JIRA_DONE_STATUSES is unset, the points
// field only where nothing was pinned and the name lookup agrees or
// found nothing. A failed read keeps the last reading, dated and marked
// stale, because a default that looks detected cannot be told from a
// real one; only a first read with nothing behind it refuses.
func (s *Service) declare(sp jira.Sprint, base *fields) (*fields, error) {
	f := *base
	now := time.Now().UTC()
	s.mu.RLock()
	d := s.declared
	s.mu.RUnlock()
	ok := true

	// With an override the project's statuses are not read: the override
	// replaces the declared list, and the site's catalogue, already in
	// hand, says whether a named status exists at all.
	if len(s.cfg.Rules.Done) == 0 {
		statuses, err := s.client.ProjectStatuses(s.cfg.Project)
		switch {
		case err == nil:
			d.Statuses = statuses
		case len(d.Statuses) == 0 || !d.DoneFromJira:
			return nil, fmt.Errorf("reading the statuses of project %s: %w. Set ARGUS_JIRA_DONE_STATUSES to name the delivered statuses without that read", s.cfg.Project, err)
		default:
			ok = false
		}
		d.DoneFromJira = true
		f.done = jira.DoneStatusNames(d.Statuses)
		if len(f.done) == 0 {
			return nil, fmt.Errorf("no status in project %s declares Jira's done category, so nothing would count as delivered. Set ARGUS_JIRA_DONE_STATUSES", s.cfg.Project)
		}
	} else {
		d.Statuses, d.DoneFromJira, f.done = base.catalogue, false, s.cfg.Rules.Done
	}

	// The board's reading carries over a failed read only for the same
	// board; a sprint on another board starts from nothing.
	last := d
	d.BoardID, d.Estimation, d.BoardField, d.BoardFieldName, d.Columns = sp.OriginBoardID, "", "", "", nil
	if sp.OriginBoardID > 0 {
		b, err := s.client.BoardConfiguration(sp.OriginBoardID)
		switch {
		case err == nil:
			d.Estimation, d.BoardField, d.BoardFieldName, d.Columns =
				b.Estimation.Type, b.PointsField(), b.Estimation.Field.DisplayName, b.Columns
		case last.BoardID == sp.OriginBoardID:
			d.Estimation, d.BoardField, d.BoardFieldName, d.Columns =
				last.Estimation, last.BoardField, last.BoardFieldName, last.Columns
			ok = false
		default:
			ok = false
		}
	}

	d.NamedField, d.PointsFromJira = base.points, false
	switch {
	case base.pinned, d.BoardField == "":
		f.points = base.points
	case base.points == "" || base.points == d.BoardField:
		f.points, d.PointsFromJira = d.BoardField, true
	default:
		// Held back: the two disagree and every figure moves with the
		// choice, so the report says so and a person accepts it.
		f.points = base.points
	}
	if d.PointsFromJira && s.cfg.EnableWrites {
		s.client.AllowWrites(f.points)
	}

	if ok {
		d.ReadAt, d.Stale = now, false
	} else {
		d.Stale = true
	}
	f.declared = d
	s.mu.Lock()
	s.declared = d
	s.mu.Unlock()
	s.record(&f, ok)
	return &f, nil
}

// record tells the configuration registry what was read, so `argus
// config list` can show each value with its source and age.
func (s *Service) record(f *fields, ok bool) {
	note := func(key string, inForce bool, value, source string) {
		switch {
		case !inForce:
			config.Undeclare(key)
		case ok:
			config.Declared(key, value, source, f.declared.ReadAt)
		default:
			config.DeclaredStale(key)
		}
	}
	note("ARGUS_JIRA_DONE_STATUSES", f.declared.DoneFromJira, strings.Join(f.done, ", "), "status categories")
	note("ARGUS_JIRA_POINTS_FIELD", f.declared.PointsFromJira, f.points, "board estimation")
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
