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

	// CapacityReview says whether the availability behind these figures
	// has been confirmed by a person, and whether they changed anything.
	CapacityReview CapacityReview `json:"capacity_review"`

	GeneratedAt time.Time `json:"generated_at"`
}

// Capacity review states. Three, because a sprint with no capacity rows
// is two different situations and reporting them as one is a lie: nobody
// has opened it, or somebody opened it and everybody was available.
const (
	ReviewNone          = "not_reviewed"
	ReviewAdjusted      = "adjusted"
	ReviewNoAdjustments = "no_adjustments"
)

// CapacityReview is the state of one sprint's availability.
type CapacityReview struct {
	State string `json:"state"`

	// ReviewedAt is when a person confirmed it, absent while nobody has.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	// Adjusted counts the people who were away for any part of the
	// sprint, so "no adjustments" can be shown as the positive statement
	// it is rather than as an empty table.
	Adjusted int `json:"adjusted"`
}

// SprintInfo identifies the sprint. Both numbers are kept: Jira's id is
// unambiguous and is what links back to Jira, the number is what people
// call it and the only one a published report shows.
type SprintInfo struct {
	JiraID int64     `json:"jira_id"`
	Number int       `json:"number"`
	Name   string    `json:"name"`
	State  string    `json:"state"`
	Starts time.Time `json:"starts"`
	Ends   time.Time `json:"ends"`

	// CountsFrom and CountsUntil are the stretch of time this sprint
	// claims work in. They are not Starts and Ends: a sprint is completed
	// when somebody clicks Complete Sprint, which is routinely days after
	// the date it was meant to end, and that click is what decides which
	// sprint a finished ticket belongs to.
	CountsFrom  time.Time `json:"counts_from"`
	CountsUntil time.Time `json:"counts_until"`

	Provisional bool   `json:"provisional"`
	BoardID     int64  `json:"board_id"`
	BrowseURL   string `json:"browse_url"`
}

// Summary is the headline. Delivered excludes container types, so it is
// the work itself rather than the rollups above it.
type Summary struct {
	BaselineTotal  float64 `json:"baseline_total"`
	CapacityTotal  float64 `json:"capacity_total"`
	DeliveredTotal float64 `json:"delivered_total"`

	PlannedDaysOff   float64 `json:"planned_days_off"`
	UnplannedDaysOff float64 `json:"unplanned_days_off"`

	// ShortfallFromAbsence is how much of the gap between capacity and
	// delivery is accounted for by absence nobody could plan around.
	//
	// Capacity is reduced by planned leave only, because that is what a
	// team knew about when it committed. Unplanned absence has to land
	// somewhere, and it lands here: it explains part of a shortfall
	// rather than quietly shrinking the number the shortfall is measured
	// against. It never exceeds the shortfall itself, so it cannot
	// explain away more than actually went missing.
	ShortfallFromAbsence float64 `json:"shortfall_from_absence"`

	// Say/do by ticket count, not points: the question is how much of what
	// was promised got done, and a count answers it without an estimate
	// having to be right.
	Promised  int `json:"promised"`
	Injected  int `json:"injected"`
	Completed int `json:"completed"`

	IssueCount int `json:"issue_count"`
	DoneCount  int `json:"done_count"`

	// CarriedOver is the work still open when this sprint closed.
	CarriedOver int `json:"carried_over"`

	// CarriedActive is how much of the carryover somebody actually picked
	// up, and NeverStarted how much sat untouched all sprint. Reporting
	// them as one number reads as though a sprint attempted work it never
	// began.
	CarriedActive int `json:"carried_active"`
	NeverStarted  int `json:"never_started"`

	// FinishedFromEarlier counts the work this sprint finished that was
	// already under way before it opened. The sprint gets the credit,
	// which is the simpler and more defensible rule, but it should not
	// read as though all of that effort was spent here.
	FinishedFromEarlier int `json:"finished_from_earlier"`

	// FinishedEarlier counts issues still on this sprint's board that had
	// already finished before it opened. They are neither delivered here
	// nor carried out of here.
	FinishedEarlier int `json:"finished_earlier"`

	// PriorPointsDeducted is the credit handed back to earlier sprints
	// for work they had already been paid for. Shown rather than folded
	// in, so a total that looks low can be seen to be low for a reason.
	PriorPointsDeducted float64 `json:"prior_points_deducted"`

	// Delivered points belonging to nobody, because the issue was
	// unassigned and nobody logged time against it. Shown so the
	// per-person numbers can be seen not to sum to the total, rather than
	// the difference going quietly missing.
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

	// ShortfallFromAbsence is the part of a negative delta that unplanned
	// absence accounts for. Zero when they met their capacity: absence
	// only needs explaining when something is missing.
	ShortfallFromAbsence float64 `json:"shortfall_from_absence"`

	// Note is the human explanation of a delta. Local by default; it
	// reaches a published page only when explicitly chosen.
	Note string `json:"note,omitempty"`

	// OnRoster is false for someone who delivered work here without being
	// on the team's roster - a contractor, another team's engineer, or
	// somebody nobody has added yet.
	OnRoster bool `json:"on_roster"`

	// Measured is true only for somebody on the roster, opted in, and with
	// a baseline set. It is the one flag that decides whether Baseline,
	// Capacity and Delta above mean anything, and whether this person
	// counts towards the sprint's capacity totals.
	//
	// Delivery is not gated on it. Points delivered by anybody are part of
	// what the sprint delivered; being measured against a baseline is a
	// separate question, and the answer for a contractor is no.
	Measured bool `json:"measured"`

	Rows []Row `json:"rows"`
}

// Story is a container issue that concluded this sprint.
//
// Concluded means more than its own status: a Story is finished when the
// Story is done and so is everything linked beneath it. One marked done
// over work still in flight has not finished, and the sprint that closes
// the last of that work is the one that gets to claim it.
//
// Its points are a rollup of the work beneath it, so they are reported
// here and never counted as delivery.
type Story struct {
	Key     string  `json:"key"`
	Summary string  `json:"summary"`
	Points  float64 `json:"points"`
	Epic    string  `json:"epic,omitempty"`
	URL     string  `json:"url"`

	// LinkedPoints is the work underneath added up, and LinkedCount how
	// many items that was. Shown beside the Story's own figure because
	// the two disagreeing is the interesting case.
	LinkedPoints float64 `json:"linked_points"`
	LinkedCount  int     `json:"linked_count"`

	// ConcludedAt is when the last of it finished, which is what decided
	// that this sprint rather than another one claims it.
	ConcludedAt time.Time `json:"concluded_at"`
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

// How an issue stands relative to the sprint being reported on.
const (
	// StateConcluded means it reached a Done status inside this sprint's
	// window. This is the only state that is delivery.
	StateConcluded = "concluded"

	// StateCarried means it was still open when the sprint shut, whether
	// or not it has finished since.
	StateCarried = "carried"

	// StateFinishedEarlier means it had already finished before this
	// sprint opened and is still sitting on its board. Jira's sprint
	// field is cumulative, so this is common and is neither delivery here
	// nor carryover from here.
	StateFinishedEarlier = "finished_earlier"
)

// Row is one issue, in the shape every table renders.
type Row struct {
	Key       string  `json:"key"`
	Summary   string  `json:"summary"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	Assignee  string  `json:"assignee,omitempty"`
	Points    float64 `json:"points"`
	HasPoints bool    `json:"has_points"`
	Done      bool    `json:"done"`
	Epic      string  `json:"epic,omitempty"`
	EpicKey   string  `json:"epic_key,omitempty"`

	// State is how this issue stands relative to the sprint. It is not
	// the same question as Done: an issue can be done now and have been
	// open throughout the sprint being reported on.
	State string `json:"state"`

	// Credited is what this sprint counts for the issue, which is not its
	// size. A ticket that finishes here gives back what earlier sprints
	// were already credited for it; one still open earns only what was
	// worked on here. Across every sprint the issue touched, these sum to
	// its points and never more.
	Credited float64 `json:"credited"`

	// PriorPoints is what was handed back to earlier sprints.
	PriorPoints float64 `json:"prior_points,omitempty"`

	// UsedEstimate marks a figure that came from the planning estimate
	// because the actual was never filled in.
	UsedEstimate bool `json:"used_estimate,omitempty"`

	// ConcludedAt is when it last reached a Done status, zero while it
	// has not. This is what decides which sprint counts it.
	ConcludedAt time.Time `json:"concluded_at,omitempty"`

	// Active is whether anybody picked it up during this sprint. An open
	// ticket nobody touched is not the same as one somebody worked on and
	// could not finish, and only the second is work that happened.
	Active bool `json:"active"`

	// StartedEarlier marks work already under way before this sprint
	// opened.
	StartedEarlier bool `json:"started_earlier,omitempty"`

	// TimeLogged is whether anybody logged any time against it at all.
	TimeLogged bool `json:"time_logged"`

	// HoursLogged is the time logged inside this sprint's window.
	HoursLogged float64 `json:"hours_logged,omitempty"`

	Created  time.Time `json:"created"`
	Resolved time.Time `json:"resolved,omitempty"`
	URL      string    `json:"url"`
}

// Flag is something a person should look at. Flags are deliberately few
// and specific: a list long enough to scroll is a list nobody reads, and
// every entry here names one fixable thing.
type Flag struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
	URL     string `json:"url,omitempty"`

	// Keys carries the tickets behind a flag that speaks for several at
	// once. A flag that would otherwise be nine rows is one row and a
	// list, because nine rows is how a list stops being read.
	Keys []string `json:"keys,omitempty"`

	// JQL lets someone verify the whole set in Jira rather than trusting
	// the number. Trust in a report comes from being able to check it.
	JQL string `json:"jql,omitempty"`
}

// Flag kinds.
const (
	FlagDoneNoEstimate     = "done_no_estimate"
	FlagDoneUnassigned     = "done_unassigned"
	FlagEstimateFallback   = "estimate_used"
	FlagDeliveredOffRoster = "delivered_off_roster"
	FlagNoBaseline         = "no_baseline"
	FlagCapacityUnreviewed = "capacity_unreviewed"
	FlagCarriedNoWorklog   = "carried_no_worklog"

	// The container flags. These replaced one that fired on every Story
	// carrying points, which is normal data for a team that rolls up, and
	// so named nothing anybody could act on.
	FlagStoryPointsMismatch  = "story_points_mismatch"
	FlagStoryNothingSized    = "story_work_unsized"
	FlagStoryNoWork          = "story_no_work"
	FlagStoryWorkDoneNotShut = "story_work_done_not_closed"
)
