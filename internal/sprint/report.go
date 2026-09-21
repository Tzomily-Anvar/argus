package sprint

import "time"

// Report is everything a sprint report shows. It is computed, never
// stored wholesale: the inputs are Jira plus the capacity a human
// entered, and keeping only those means a report can always be rebuilt.
type Report struct {
	Sprint   SprintInfo  `json:"sprint"`
	Summary  Summary     `json:"summary"`
	People   []Person    `json:"people"`
	Stories  []Story     `json:"stories_concluded"`
	Epics    []EpicGroup `json:"epics"`
	Carry    []Row       `json:"carryover"`
	Flags    []Flag      `json:"flags"`
	Warnings []string    `json:"warnings,omitempty"`

	GeneratedAt time.Time `json:"generated_at"`
}

// SprintInfo identifies the sprint. Both numbers are kept: Jira's id is
// unambiguous and is what links back to Jira, the number is what people
// call it and the only one a published report shows.
type SprintInfo struct {
	JiraID      int64     `json:"jira_id"`
	Number      int       `json:"number"`
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Starts      time.Time `json:"starts"`
	Ends        time.Time `json:"ends"`
	Provisional bool      `json:"provisional"`
	BoardID     int64     `json:"board_id"`
	BrowseURL   string    `json:"browse_url"`
}

// Summary is the headline. Delivered excludes container types, so it is
// the work itself rather than the rollups above it.
type Summary struct {
	BaselineTotal  float64 `json:"baseline_total"`
	CapacityTotal  float64 `json:"capacity_total"`
	DeliveredTotal float64 `json:"delivered_total"`

	PlannedDaysOff   float64 `json:"planned_days_off"`
	UnplannedDaysOff float64 `json:"unplanned_days_off"`

	// Say/do by ticket count, not points: the question is how much of what
	// was promised got done, and a count answers it without an estimate
	// having to be right.
	Promised  int `json:"promised"`
	Injected  int `json:"injected"`
	Completed int `json:"completed"`

	IssueCount int `json:"issue_count"`
	DoneCount  int `json:"done_count"`

	// Delivered points belonging to nobody, because the issue was
	// unassigned. Shown so the per-person numbers can be seen not to sum
	// to the total, rather than the difference going quietly missing.
	UnattributedPoints float64 `json:"unattributed_points"`
}

// Person is one team member's sprint.
type Person struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`

	Baseline  float64 `json:"baseline"`
	Capacity  float64 `json:"capacity"`
	Delivered float64 `json:"delivered"`
	Delta     float64 `json:"delta"`

	PlannedDaysOff   float64 `json:"planned_days_off"`
	UnplannedDaysOff float64 `json:"unplanned_days_off"`

	// Note is the human explanation of a delta. Local by default; it
	// reaches a published page only when explicitly chosen.
	Note string `json:"note,omitempty"`

	// Registered is false for someone who delivered work but is not on the
	// roster - a contractor, or somebody nobody has added yet. Their
	// delivery still counts; they just have no baseline to compare it to.
	Registered bool `json:"registered"`

	Rows []Row `json:"rows"`
}

// Story is a container issue that finished this sprint. Its points are a
// rollup of the work beneath it, so they are reported here rather than
// counted as anyone's capacity.
type Story struct {
	Key     string  `json:"key"`
	Summary string  `json:"summary"`
	Points  float64 `json:"points"`
	Epic    string  `json:"epic,omitempty"`
	URL     string  `json:"url"`
}

// EpicGroup is delivery grouped by the epic it belonged to, which is how
// stakeholders read a sprint - by what moved, not by who moved it.
//
// Key and URL are empty for work that belongs to no epic: there is
// nothing in Jira to open, so the group is a name and nothing more.
type EpicGroup struct {
	Key        string  `json:"key,omitempty"`
	Name       string  `json:"name"`
	Class      string  `json:"class,omitempty"` // Run, Build, or whatever a team configures
	Points     float64 `json:"points"`
	Share      float64 `json:"share"` // of delivered points, 0-1
	IssueCount int     `json:"issue_count"`
	URL        string  `json:"url,omitempty"`
}

// Row is one issue, in the shape every table renders.
type Row struct {
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee,omitempty"`
	Points    float64   `json:"points"`
	HasPoints bool      `json:"has_points"`
	Done      bool      `json:"done"`
	Epic      string    `json:"epic,omitempty"`
	EpicKey   string    `json:"epic_key,omitempty"`
	Created   time.Time `json:"created"`
	Resolved  time.Time `json:"resolved,omitempty"`
	URL       string    `json:"url"`
}

// Flag is something a person should look at. Flags are deliberately few
// and specific: a list long enough to scroll is a list nobody reads, and
// every entry here names one fixable thing.
type Flag struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
	URL     string `json:"url,omitempty"`

	// JQL lets someone verify the whole set in Jira rather than trusting
	// the number. Trust in a report comes from being able to check it.
	JQL string `json:"jql,omitempty"`
}

// Flag kinds.
const (
	FlagDoneNoEstimate      = "done_no_estimate"
	FlagDoneUnassigned      = "done_unassigned"
	FlagContainerWithPoints = "container_with_points"
	FlagDeliveredByStranger = "delivered_by_unregistered"
	FlagCapacityUnreviewed  = "capacity_unreviewed"
)
