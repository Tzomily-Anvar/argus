// Package store is where Argus keeps anything it must remember.
//
// Tool 1 remembers nothing - it sweeps GitHub and renders. The sprint
// report cannot work that way: a person's baseline capacity, who was
// away, and the notes explaining a delta have no source in Jira. They
// have to live somewhere, and that somewhere must not be the repository.
//
// Two backends implement this interface:
//
//   - jsonstore writes files to a volume. No setup, no container, and
//     enough for one team: a year is about twenty-six sprints.
//   - pgstore uses Postgres, for keeping years of history and querying
//     across it.
//
// The same test suite runs against both, so they cannot drift apart.
//
// WHAT IS HELD HERE IS PERSONAL DATA. Names, Jira account ids, how much
// someone was away, and free text about why their delivery differed from
// their baseline. It stays on the machine running Argus, it is excluded
// from version control, and SECURITY.md says so plainly. Anything added
// to these types inherits that responsibility.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup finds nothing. Callers are
// expected to treat it as a normal outcome rather than a failure.
var ErrNotFound = errors.New("not found")

// Person is a team member Argus knows about. The Jira account id is the
// join key; the display name is for reading.
type Person struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`

	// Baseline is the story points this person is expected to deliver in
	// a full sprint. It is a planning figure, not a target or a measure
	// of anyone's worth, and nothing here compares one person's to
	// another's.
	Baseline float64 `json:"baseline"`

	// Active is false for someone who has left, so history stays intact
	// without them appearing in new sprints.
	Active bool `json:"active"`

	UpdatedAt time.Time `json:"updated_at"`
}

// Sprint is one sprint, keyed by Jira's own id.
//
// Jira's id is the primary key because a sprint number is not unique:
// boards renumber, and two boards can each have a "Sprint 21". The
// number is kept as a label, and it is the only one of the two that
// reaches a published report.
type Sprint struct {
	JiraID    int64      `json:"jira_id"`
	Label     string     `json:"label"`
	Number    int        `json:"number"`
	StartsAt  *time.Time `json:"starts_at,omitempty"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	State     string     `json:"state"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Capacity is one person's availability for one sprint.
//
// Absence is split by whether it was foreseeable at planning time, not by
// why it happened. That split is what explains a delta: leave booked
// before the sprint started should already be in the plan, whereas
// absence that arose during it accounts for a shortfall nobody could have
// planned around.
//
// The reason is deliberately not recorded. "Unplanned" carries the entire
// analytical signal - a burst pipe and influenza have identical planning
// impact - while storing why someone was away would make this health
// data, which carries obligations that a sprint tool has no business
// taking on.
type Capacity struct {
	SprintJiraID int64  `json:"sprint_jira_id"`
	AccountID    string `json:"account_id"`

	// PlannedDaysOff was known before the sprint began: booked leave,
	// a public holiday, an agreed part-time arrangement.
	PlannedDaysOff float64 `json:"planned_days_off"`

	// UnplannedDaysOff arose during the sprint. This is the figure that
	// explains a delta the baseline could not have anticipated.
	UnplannedDaysOff float64 `json:"unplanned_days_off"`

	// Reviewed records whether a human has confirmed this figure for this
	// sprint. Capacity is pre-filled from the baseline, which is usually
	// right and occasionally very wrong; without this flag there is no way
	// to tell a confirmed number from an untouched default. The publish
	// preview warns when it is false.
	Reviewed bool `json:"reviewed"`

	// Note explains a delta. It is free text about a named person, so it
	// is local by default and never published unless explicitly chosen.
	Note string `json:"note,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

// DaysOff is the total absence, planned and unplanned together.
func (c Capacity) DaysOff() float64 { return c.PlannedDaysOff + c.UnplannedDaysOff }

// SprintStats is the summary kept for one finished sprint, so trends can
// be drawn without re-fetching years of Jira history.
type SprintStats struct {
	SprintJiraID    int64              `json:"sprint_jira_id"`
	BaselineTotal   float64            `json:"baseline_total"`
	CapacityTotal   float64            `json:"capacity_total"`
	DeliveredTotal  float64            `json:"delivered_total"`
	Promised        int                `json:"promised"`
	Injected        int                `json:"injected"`
	Completed       int                `json:"completed"`
	ByEpicClass     map[string]float64 `json:"by_epic_class"`
	StoriesDone     int                `json:"stories_done"`
	StoryPointsDone float64            `json:"story_points_done"`
	RecordedAt      time.Time          `json:"recorded_at"`
}

// WriteRecord is one change Argus made in Jira or Confluence.
//
// The tool can reassign a ticket, change its points, log work and publish
// a page. Anything that can change someone else's data should leave a
// record of what it changed and when, so a bulk edit can be explained -
// or reversed - afterwards.
type WriteRecord struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Operation string    `json:"operation"`
	Target    string    `json:"target"`
	Before    string    `json:"before,omitempty"`
	After     string    `json:"after,omitempty"`
	Actor     string    `json:"actor"`
	Note      string    `json:"note,omitempty"`
}

// Store is the whole persistence surface. Kept deliberately small: every
// method here has to be implemented twice and tested twice.
type Store interface {
	// People
	PutPerson(ctx context.Context, p Person) error
	GetPerson(ctx context.Context, accountID string) (Person, error)
	ListPeople(ctx context.Context, includeInactive bool) ([]Person, error)
	DeletePerson(ctx context.Context, accountID string) error

	// Sprints
	PutSprint(ctx context.Context, s Sprint) error
	GetSprint(ctx context.Context, jiraID int64) (Sprint, error)
	ListSprints(ctx context.Context, limit int) ([]Sprint, error)

	// Capacity
	PutCapacity(ctx context.Context, c Capacity) error
	ListCapacity(ctx context.Context, sprintJiraID int64) ([]Capacity, error)

	// Stats, for trends
	PutStats(ctx context.Context, s SprintStats) error
	ListStats(ctx context.Context, limit int) ([]SprintStats, error)

	// Write audit
	AppendWrite(ctx context.Context, w WriteRecord) error
	ListWrites(ctx context.Context, limit int) ([]WriteRecord, error)

	// Lifecycle
	Migrate(ctx context.Context) error
	Close() error
}
