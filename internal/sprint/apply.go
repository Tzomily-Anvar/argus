package sprint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The apply is the one place a previewed change set becomes requests to
// Jira. It is sequential, one row at a time, and every row is judged
// twice on the way: once here, by re-reading the issue and comparing it
// with the guard the preview recorded (guard.go), and once in the Jira
// client, by the gate that refuses anything not on its list. Nothing here
// can widen what the gate allows; it can only decline to ask.

// The refusals an apply makes before anything is written. Each leaves the
// change set held, except that a set already taken is gone by design.
var (
	ErrWritesDisabled = errors.New("writes to Jira are off for this deployment; set ARGUS_SPRINT_ALLOW_WRITES to switch them on")
	ErrExpired        = errors.New("that preview has expired or was already applied; build it again")
	ErrDigestMismatch = errors.New("that is not the preview the server built; build it again")
)

// worklogQuery is the query every worklog write carries. The gate in the
// Jira client holds the same string and refuses any other.
const worklogQuery = "notifyUsers=false&adjustEstimate=leave"

// Why a batch stopped early, in the words the result shows.
const (
	stoppedAuth = "stopped after three consecutive permission failures; nothing further was attempted"
	stoppedRate = "stopped after a second rate limit from Jira; nothing further was attempted"
)

// RowResult is what became of one previewed change.
type RowResult struct {
	Key         string `json:"key"`
	Op          string `json:"op"`
	Outcome     string `json:"outcome"` // applied | skipped | failed
	Reason      string `json:"reason,omitempty"`
	PersonLabel string `json:"person_label,omitempty"`
	AfterLabel  string `json:"after_label"`
}

// Result is the outcome of an apply, one row per change in preview order,
// so the person reads the same table they approved with a verdict beside
// each line.
type Result struct {
	ChangeSet string      `json:"change_set"`
	Rows      []RowResult `json:"rows"`
	Applied   int         `json:"applied"`
	Skipped   int         `json:"skipped"`
	Failed    int         `json:"failed"`

	// Stopped says why the batch ended before its last row, when it did.
	Stopped string `json:"stopped,omitempty"`
}

// Apply writes a held change set to Jira, once.
//
// Everything that can refuse does so before a single request is made:
// writes off for this deployment, a set that has expired or was already
// applied, a digest that is not the one the server computed. Only then is
// the set taken, so a double-click finds nothing to apply. The result
// lists every change in preview order with its outcome, and the sprint's
// report is swept again afterwards so the screen agrees with Jira rather
// than with the preview.
func (s *Service) Apply(ctx context.Context, id, digest string) (Result, error) {
	if !s.cfg.EnableWrites {
		return Result{}, ErrWritesDisabled
	}
	held := s.held()
	cs := held.Get(id)
	if cs == nil {
		return Result{}, ErrExpired
	}
	if digest == "" || digest != cs.Digest {
		return Result{}, ErrDigestMismatch
	}
	f, err := s.resolveFields()
	if err != nil {
		return Result{}, err
	}
	if cs = held.Take(id); cs == nil {
		return Result{}, ErrExpired
	}

	a := &applier{
		s: s, cs: *cs, points: f.points,
		screens:  map[string]map[string]string{},
		board:    s.boardOf(cs.SprintJiraID),
		viaBoard: map[string]bool{},
		touched:  map[string]bool{}, added: map[string][]string{},
	}
	a.preflight()
	res := a.run(ctx)
	s.clearApplied(ctx, cs.SprintJiraID, cs.Changes, res.Rows)
	s.rebuild(cs.SprintJiraID)
	return res, nil
}

// applier is the state of one apply: what the edit screens allow, which
// issues this run has already written to, and how the batch is faring.
type applier struct {
	s      *Service
	cs     ChangeSet
	points string

	// screens is, per issue type and operation, why a field edit cannot
	// be made on that type: the story points field is not on the edit
	// screen, or the account may not assign. Filled once per type before
	// the loop, because "3 of 10 failed" after the fact is worse than
	// "the Bug edit screen does not carry Story Points" before it.
	screens map[string]map[string]string

	// board is the board the sprint belongs to, and viaBoard the issue
	// types whose points go through the board's estimation endpoint
	// because the field is not on their edit screen. That is how the
	// backlog view sets them, and it works where a plain edit would not.
	board    int64
	viaBoard map[string]bool

	// touched is the issues this run has written to, by id. A second
	// change on the same issue would otherwise fail its updated check
	// against the first, so for those the guard falls back to the field
	// itself. added is the worklog ids this run created, by issue, for
	// the same reason.
	touched map[string]bool
	added   map[string][]string

	auth, limited int
	stopped       string
}

// preflight asks Jira which fields may be edited, once per distinct issue
// type among the field edits, and records what cannot.
func (a *applier) preflight() {
	for _, c := range a.cs.Changes {
		if c.Op != OpPointsSet && c.Op != OpAssigneeSet {
			continue
		}
		if _, done := a.screens[c.Type]; done {
			continue
		}
		cannot := map[string]string{}
		a.screens[c.Type] = cannot
		meta, err := a.s.client.EditMeta(c.Key)
		if err != nil {
			cannot[OpPointsSet] = "could not read the edit screen: " + err.Error()
			cannot[OpAssigneeSet] = cannot[OpPointsSet]
			continue
		}
		if meta[a.points] == nil {
			// Not on the edit screen. The board's estimation endpoint sets
			// the same field without needing it there, which is how the
			// backlog view manages; that route is taken when the board is
			// known, and only then is the row refused.
			if a.board > 0 {
				a.viaBoard[c.Type] = true
			} else {
				cannot[OpPointsSet] = fmt.Sprintf("the %s edit screen does not carry the story points field and no board is known to set it through; fix the screen in Jira or deselect these rows", typeLabel(c.Type))
			}
		}
		if meta["assignee"] == nil {
			cannot[OpAssigneeSet] = fmt.Sprintf("this account cannot assign a %s; the edit screen does not offer the assignee", typeLabel(c.Type))
		}
	}
}

func typeLabel(t string) string {
	if t == "" {
		return "issue"
	}
	return t
}

// run applies the changes in order, recording every attempt as it goes,
// so a crash mid-batch still leaves a record of what landed.
func (a *applier) run(ctx context.Context) Result {
	res := Result{ChangeSet: a.cs.ID, Rows: make([]RowResult, 0, len(a.cs.Changes))}
	for _, c := range a.cs.Changes {
		row := RowResult{Key: c.Key, Op: c.Op, PersonLabel: c.PersonLabel, AfterLabel: c.AfterLabel}
		if a.stopped != "" {
			// Not attempted, so not recorded: the log holds what was
			// tried, and the result says why the rest was not.
			row.Outcome, row.Reason = store.OutcomeSkipped, "not attempted"
			res.Skipped++
			res.Rows = append(res.Rows, row)
			continue
		}
		outcome, reason, created := a.one(c)
		if err := a.record(ctx, c, outcome, reason, created); err != nil {
			reason = strings.TrimSpace(reason + " (the audit row could not be written: " + err.Error() + ")")
		}
		row.Outcome, row.Reason = outcome, reason
		switch outcome {
		case store.OutcomeApplied:
			res.Applied++
		case store.OutcomeSkipped:
			res.Skipped++
		default:
			res.Failed++
		}
		res.Rows = append(res.Rows, row)
	}
	res.Stopped = a.stopped
	return res
}

// one judges and, if nothing has moved, writes a single change. It
// returns the outcome, the reason when there is one, and the id Jira gave
// a new worklog entry, which a reversal needs.
func (a *applier) one(c Change) (outcome, reason, created string) {
	if why := a.screens[c.Type][c.Op]; why != "" {
		return store.OutcomeSkipped, why, ""
	}
	ref := c.Guard.IssueID
	if ref == "" {
		ref = c.Key
	}
	is, err := a.s.client.GetIssue(ref, []string{"updated", "status", "assignee", "worklog", a.points})
	if err != nil {
		a.failed(err)
		return store.OutcomeFailed, "could not re-read the issue: " + err.Error(), ""
	}
	if why := conflict(c, is, a.points, a.s.cfg.Rules, a.touched[is.ID], a.added[is.ID]); why != "" {
		return store.OutcomeSkipped, why, ""
	}

	method, path, query, body := a.request(c)
	var answer struct {
		ID string `json:"id"`
	}
	if err := a.s.client.Write(method, path, query, body, &answer); err != nil {
		a.failed(err)
		return store.OutcomeFailed, err.Error(), ""
	}
	a.auth = 0
	a.touched[is.ID] = true
	if c.Op == OpWorklogAdd {
		a.added[is.ID] = append(a.added[is.ID], answer.ID)
	}
	return store.OutcomeApplied, "", answer.ID
}

// failed keeps count of the faults that mean the rest of the batch would
// fail the same way. Three permission failures in a row say the token
// cannot write at all; a second rate limit says Jira wants us to stop.
// Anything else is a fault on one issue and the loop carries on.
func (a *applier) failed(err error) {
	switch jira.StatusCode(err) {
	case http.StatusUnauthorized, http.StatusForbidden:
		if a.auth++; a.auth >= 3 {
			a.stopped = stoppedAuth
		}
	case http.StatusTooManyRequests:
		if a.limited++; a.limited >= 2 {
			a.stopped = stoppedRate
		}
	default:
		a.auth = 0
	}
}

// request is the request one change becomes, choosing the board's
// estimation endpoint for points on a type whose edit screen lacks the
// field. The value goes as a string, the way the board sends it; nil
// clears, which is the reversal of a write.
func (a *applier) request(c Change) (method, path, query string, body any) {
	if c.Op == OpPointsSet && a.viaBoard[c.Type] {
		var value any
		if f, ok := c.After.(float64); ok {
			value = strconv.FormatFloat(f, 'f', -1, 64)
		}
		return http.MethodPut, "/rest/agile/1.0/issue/" + c.Key + "/estimation",
			"boardId=" + strconv.FormatInt(a.board, 10), map[string]any{"value": value}
	}
	return request(c, a.points)
}

// boardOf is the board the sprint's cached report names, zero when the
// report is not held.
func (s *Service) boardOf(sprintID int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if entry := s.reports[sprintID]; entry != nil {
		return entry.report.Sprint.BoardID
	}
	return 0
}

// request is the exact request an operation becomes, in the shape the
// gate accepts and nothing more. An operation this does not know becomes
// a request with no method, which the gate refuses.
func request(c Change, points string) (method, path, query string, body any) {
	issue := "/rest/api/3/issue/" + c.Key
	switch c.Op {
	case OpPointsSet:
		return http.MethodPut, issue, "", map[string]any{"fields": map[string]any{points: c.After}}
	case OpAssigneeSet:
		return http.MethodPut, issue, "", map[string]any{"fields": map[string]any{"assignee": c.After}}
	case OpWorklogAdd:
		return http.MethodPost, issue + "/worklog", worklogQuery, c.After
	case OpWorklogUpdate:
		return http.MethodPut, issue + "/worklog/" + c.WorklogID, worklogQuery, c.After
	case OpWorklogDelete:
		return http.MethodDelete, issue + "/worklog/" + c.WorklogID, worklogQuery, nil
	}
	return "", "", "", nil
}

// rebuild throws the sprint's report away and sweeps it again, so what
// the screen shows next is Jira after the writes rather than the preview.
// Synchronous, so the result reaches the browser with the report already
// agreeing with it. If the sweep fails the next open pays for one, which
// is the honest outcome after a write.
func (s *Service) rebuild(sprintID int64) {
	s.mu.Lock()
	delete(s.reports, sprintID)
	s.mu.Unlock()
	s.refresh(sprintID)
}
