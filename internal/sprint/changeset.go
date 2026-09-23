package sprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The operations a change set can carry. They are the names the Jira
// client's permitted list uses, so a change is spoken of in one
// vocabulary from the preview, through the request it becomes, to the
// audit row it leaves behind.
const (
	OpPointsSet     = "points.set"
	OpAssigneeSet   = "assignee.set"
	OpWorklogAdd    = "worklog.add"
	OpWorklogUpdate = "worklog.update"
	OpWorklogDelete = "worklog.delete"
)

// ChangeSetLifetime is how long a preview stays valid. Long enough to
// read a table of a dozen rows and press a button, short enough that a
// tab left open over lunch cannot apply figures from a Jira that has
// since moved on.
const ChangeSetLifetime = 15 * time.Minute

// ChangeSet is a proposal. It is the only thing that can ever be applied,
// and it is computed rather than stored: the inputs are a report plus
// what a human typed, and keeping only those means a proposal can always
// be rebuilt.
//
// ID is assigned when the set is held by the service, not by Propose: a
// random value is the one thing a pure function cannot produce.
type ChangeSet struct {
	ID           string    `json:"id"`
	Digest       string    `json:"digest"`
	SprintJiraID int64     `json:"sprint_jira_id"`
	BuiltAt      time.Time `json:"built_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	ReportBuilt  time.Time `json:"report_built"` // the report's GeneratedAt
	Actor        string    `json:"actor"`        // the configured Jira account

	Changes []Change `json:"changes"`

	// Skipped is what was asked for and is NOT being changed, with the
	// reason. A silent exclusion is indistinguishable from a bug.
	Skipped []Skip `json:"skipped"`
}

// Change is one operation on one issue.
type Change struct {
	Key     string `json:"key"`
	URL     string `json:"url"`
	Summary string `json:"summary"` // for reading; never written anywhere
	Type    string `json:"type"`
	Status  string `json:"status"`

	Op    string `json:"op"`
	Field string `json:"field"` // the resolved points field id, "assignee" or "worklog"

	// After is what will be written, in the shape the Jira client's gate
	// accepts. AfterLabel is how it reads on screen and in the log: an
	// account id or an ADF document means nothing to anybody.
	After      any    `json:"after"`
	AfterLabel string `json:"after_label"`

	// Reason names the convention that produced this row, in the words
	// the flag already uses: "finished with no estimate".
	Reason string `json:"reason"`

	// The worklog operations name a person, an amount and a date. The
	// account id is what is written; the label is what is read.
	Person      string    `json:"person,omitempty"`
	PersonLabel string    `json:"person_label,omitempty"`
	Hours       float64   `json:"hours,omitempty"`
	Started     time.Time `json:"started,omitzero"`
	WorklogID   string    `json:"worklog_id,omitempty"` // update and delete

	Guard Guard `json:"guard"`
}

// Guard is what makes an apply provably the thing that was previewed.
// Recorded when the preview is built, re-checked against Jira at the
// moment of writing, and the change is skipped if reality has moved.
//
// An apply writes only where Jira still holds exactly Was. For a field
// edit Was is nil, which is the write-to-blank rule. For a worklog add it
// is the ids of the entries the issue held at preview time, so somebody
// else logging time in the meantime is a reason to look again rather
// than a reason to add a duplicate. For an update or delete it is the
// entry's seconds, and Entry carries the rest of what the entry was.
type Guard struct {
	// Jira's numeric issue id, not the key: a key moves when an issue is
	// moved between projects, and the id does not.
	IssueID string `json:"issue_id"`

	// fields.updated as it was at preview time. Any edit by anybody
	// changes it, which is the cheapest possible "has this moved".
	Updated time.Time `json:"updated"`

	Was    any    `json:"was"`
	Status string `json:"status"`

	// Entry is the worklog entry an update or delete acts on, as it stood
	// at preview time. Absent for every other operation.
	Entry *WorklogGuard `json:"entry,omitempty"`
}

// WorklogGuard is one existing worklog entry, reduced to what decides
// whether it has changed since the preview.
type WorklogGuard struct {
	ID       string    `json:"id"`
	Updated  time.Time `json:"updated"`
	Seconds  int       `json:"seconds"`
	Mentions []string  `json:"mentions"`
}

// ChangeRequest is one thing a person asked for: a value typed against a
// key. Which fields matter depends on Op; the rest are ignored.
type ChangeRequest struct {
	Key string `json:"key"`
	Op  string `json:"op"`

	Points   *float64 `json:"points,omitempty"`   // points.set
	Assignee string   `json:"assignee,omitempty"` // assignee.set, an account id

	// The worklog operations. Hours rather than seconds, because hours is
	// what the person is remembering; the conversion is done once, here.
	// Started left zero means the sprint's close.
	Person    string    `json:"person,omitempty"` // an account id on the roster
	Hours     float64   `json:"hours,omitempty"`
	Started   time.Time `json:"started,omitzero"`
	Note      string    `json:"note,omitempty"`
	WorklogID string    `json:"worklog_id,omitempty"` // update and delete
}

// ProposalInputs are everything Propose needs beyond the report and its
// issues. Explicit, like Inputs, so the computation is a pure function
// and can be tested without a Jira or a database anywhere near it.
type ProposalInputs struct {
	Requests      []ChangeRequest
	People        []store.Person // the roster, active and not
	Rules         Rules
	PointsField   string
	HoursPerPoint float64

	// The stretch of time the sprint claims work in, from Inputs.window.
	// A worklog entry dated outside it would credit a different sprint.
	SprintOpens  time.Time
	SprintCloses time.Time

	Now   time.Time
	Actor string
}

// Skip is one request that produced no change, and why, in words the
// person who typed it can act on.
type Skip struct {
	Key    string `json:"key"`
	Op     string `json:"op"`
	Person string `json:"person,omitempty"`
	Reason string `json:"reason"`
}

// digested is the part of a change that the digest covers: what will be
// written, to what, and the state it was previewed against. The labels
// and the reason are for reading and are left out, so a wording change
// cannot invalidate a preview somebody is looking at.
type digested struct {
	Op      string `json:"op"`
	IssueID string `json:"issue_id"`
	Field   string `json:"field"`
	After   any    `json:"after"`
	Guard   Guard  `json:"guard"`
}

// Digest is a sha256 over the canonical JSON of the change list, in
// order. The client sends it back with an apply, and a mismatch means
// what the browser rendered is not what the server proposed: a row
// dropped by a UI bug would otherwise be applied without anybody having
// seen it. The encoding is canonical because struct fields marshal in
// declaration order and map keys marshal sorted.
func Digest(changes []Change) string {
	list := make([]digested, 0, len(changes))
	for _, c := range changes {
		list = append(list, digested{
			Op: c.Op, IssueID: c.Guard.IssueID, Field: c.Field, After: c.After, Guard: c.Guard,
		})
	}
	// The values here all came from JSON or from this package, so the
	// only way this fails is a programming error, and an empty digest
	// matches nothing, which is the safe failure.
	raw, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
