package backlog

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The apply is the one place a held preview becomes requests to Jira.
// Sequential, one row at a time - or one group of fifty for a sprint
// move - and every row is judged twice on the way: here, by re-reading
// the ticket and comparing it with the guard, and in the Jira client, by
// the gate. Nothing here can widen what the gate allows.

// Why a batch stopped early, in the words the result shows.
const (
	stoppedAuth = "stopped after three consecutive permission failures; nothing further was attempted"
	stoppedRate = "stopped after a second rate limit from Jira; nothing further was attempted"
)

// RowResult is what became of one previewed row.
type RowResult struct {
	Key     string `json:"key"`
	Outcome string `json:"outcome"` // applied | skipped | failed
	Reason  string `json:"reason,omitempty"`
}

// BatchResult is the outcome of an apply, one row per ticket in preview
// order, so the person reads the table they approved with a verdict
// beside each line.
type BatchResult struct {
	Batch   string      `json:"batch"`
	Action  string      `json:"action"`
	Rows    []RowResult `json:"rows"`
	Applied int         `json:"applied"`
	Skipped int         `json:"skipped"`
	Failed  int         `json:"failed"`
	Stopped string      `json:"stopped,omitempty"`
}

// Apply writes a held preview to Jira, once.
//
// Everything that can refuse does so before a single request is made:
// writes off, deletes off for a delete, a preview expired or already
// applied, a digest that is not the server's, and for a delete a
// confirmation that is not the number of tickets going. Only then is the
// preview taken, so a double-click finds nothing to apply.
func (b *Batch) Apply(ctx context.Context, id, digest, confirm string) (BatchResult, error) {
	if !b.cfg.EnableWrites {
		return BatchResult{}, ErrWritesDisabled
	}
	p := b.Get(id)
	if p == nil {
		return BatchResult{}, ErrExpired
	}
	if digest == "" || digest != p.Digest {
		return BatchResult{}, ErrDigestMismatch
	}
	if p.Irreversible {
		if !b.cfg.AllowDelete {
			return BatchResult{}, ErrDeletesDisabled
		}
		// Typing the count is the confirmation: it cannot be pressed by
		// reflex, and it is wrong the moment the person has a different
		// number in mind than the preview does.
		if strings.TrimSpace(confirm) != strconv.Itoa(p.Changes) {
			return BatchResult{}, ErrConfirm
		}
	}
	if p = b.take(id); p == nil {
		return BatchResult{}, ErrExpired
	}
	a := &applier{b: b, p: *p}
	return a.run(ctx), nil
}

// applier is the state of one apply: the preview and how the batch is
// faring.
type applier struct {
	b *Batch
	p BatchPreview

	auth, limited int
	stopped       string
}

// run applies the rows in order. A row the preview already skipped is
// reported and not attempted; a row after a stop is reported as not
// attempted; everything else is attempted and recorded, one audit row
// per ticket, as it goes.
func (a *applier) run(ctx context.Context) BatchResult {
	res := BatchResult{Batch: a.p.ID, Action: a.p.Action, Rows: make([]RowResult, 0, len(a.p.Rows))}
	add := func(rr RowResult) {
		switch rr.Outcome {
		case store.OutcomeApplied:
			res.Applied++
		case store.OutcomeSkipped:
			res.Skipped++
		default:
			res.Failed++
		}
		res.Rows = append(res.Rows, rr)
	}
	rows := a.p.Rows
	for i := 0; i < len(rows); {
		r := rows[i]
		switch {
		case r.Skipped != "":
			add(RowResult{Key: r.Key, Outcome: store.OutcomeSkipped, Reason: r.Skipped})
		case a.stopped != "":
			add(RowResult{Key: r.Key, Outcome: store.OutcomeSkipped, Reason: "not attempted"})
		case r.plan.sprintID > 0 || r.plan.toBacklog:
			// One request for up to fifty consecutive rows bound for the
			// same place. Each is still guarded on its own first.
			j := i + 1
			for j < len(rows) && j-i < maxMove && rows[j].Skipped == "" &&
				rows[j].plan.sprintID == r.plan.sprintID && rows[j].plan.toBacklog == r.plan.toBacklog {
				j++
			}
			for _, rr := range a.move(ctx, rows[i:j]) {
				add(rr)
			}
			i = j
			continue
		default:
			add(a.one(ctx, r))
		}
		i++
	}
	res.Stopped = a.stopped
	return res
}

// one judges and, if nothing has moved, writes a single row.
func (a *applier) one(ctx context.Context, r BatchRow) RowResult {
	is, why, err := a.reread(r)
	if err != nil {
		return a.finish(ctx, r, store.OutcomeFailed, "could not re-read the ticket: "+err.Error(), nil)
	}
	if why != "" {
		return a.finish(ctx, r, store.OutcomeSkipped, why, nil)
	}
	method, path, query, body := request(r)
	if err := a.b.client.Write(method, path, query, body, nil); err != nil {
		a.failed(err)
		return a.finish(ctx, r, store.OutcomeFailed, err.Error(), nil)
	}
	a.auth = 0
	var facts []string
	if l := r.plan.link; l != nil {
		// Jira answers a link create with nothing, so the id a reversal
		// needs is read back off the ticket.
		facts = append(facts, "type "+l.typ)
		if again, err := a.b.client.GetIssue(is.ID, []string{"issuelinks"}); err == nil {
			if id := linkID(again, l.typ, r.After); id != "" {
				facts = append(facts, "link "+id)
			}
		}
	}
	return a.finish(ctx, r, store.OutcomeApplied, "", facts)
}

// move guards each row of a group, then sends the survivors in one
// request. A refused request fails them all, and counts once towards
// the stop rules.
func (a *applier) move(ctx context.Context, group []BatchRow) []RowResult {
	out := make([]RowResult, 0, len(group))
	var attempt []BatchRow
	for _, r := range group {
		_, why, err := a.reread(r)
		switch {
		case err != nil:
			out = append(out, a.finish(ctx, r, store.OutcomeFailed, "could not re-read the ticket: "+err.Error(), nil))
		case why != "":
			out = append(out, a.finish(ctx, r, store.OutcomeSkipped, why, nil))
		default:
			attempt = append(attempt, r)
		}
	}
	if len(attempt) == 0 {
		return out
	}
	path, facts := "/rest/agile/1.0/backlog/issue", []string{"backlog"}
	if id := attempt[0].plan.sprintID; id > 0 {
		path = fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", id)
		facts = []string{"sprint " + strconv.FormatInt(id, 10)}
	}
	keys := make([]string, 0, len(attempt))
	for _, r := range attempt {
		keys = append(keys, r.Key)
	}
	err := a.b.client.Write(http.MethodPost, path, "", map[string]any{"issues": keys}, nil)
	if err != nil {
		a.failed(err)
	} else {
		a.auth = 0
	}
	for _, r := range attempt {
		from := "from backlog"
		if r.plan.fromSprint > 0 {
			from = "from " + strconv.FormatInt(r.plan.fromSprint, 10)
		}
		if err != nil {
			out = append(out, a.finish(ctx, r, store.OutcomeFailed, err.Error(), nil))
			continue
		}
		out = append(out, a.finish(ctx, r, store.OutcomeApplied, "", append(facts, from)))
	}
	return out
}

// reread fetches the ticket as Jira holds it now and says what moved
// since the preview, or nothing if nothing did.
func (a *applier) reread(r BatchRow) (jira.Issue, string, error) {
	ref := r.Guard.IssueID
	if ref == "" {
		ref = r.Key
	}
	is, err := a.b.client.GetIssue(ref, []string{"updated"})
	if err != nil {
		a.failed(err)
		return is, "", err
	}
	if !r.Guard.Updated.IsZero() && !is.Fields.Updated.Time.Equal(r.Guard.Updated) {
		return is, fmt.Sprintf("edited in Jira since the preview (at %s, previewed at %s)",
			is.Fields.Updated.Format(time.RFC3339), r.Guard.Updated.Format(time.RFC3339)), nil
	}
	return is, "", nil
}

// failed keeps count of the faults that mean the rest of the batch would
// fail the same way: three permission failures in a row, or a second
// rate limit. Anything else is a fault on one ticket.
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

// finish records the audit row for one attempted ticket and returns its
// result. The row is written for every attempt, a failure or a skip
// included: a log that only holds successes cannot answer "what
// happened?".
func (a *applier) finish(ctx context.Context, r BatchRow, outcome, reason string, facts []string) RowResult {
	note := []string{"digest " + a.p.Digest, "issue " + r.Guard.IssueID}
	note = append(note, facts...)
	if a.p.Reverses != "" {
		note = append(note, "reverses "+a.p.Reverses)
	}
	if reason != "" {
		note = append(note, outcome+": "+reason)
	}
	err := a.b.store.AppendWrite(ctx, store.WriteRecord{
		Operation: a.p.Action, Target: r.Key, Actor: a.b.cfg.Actor,
		ChangeSet: a.p.ID, Outcome: outcome,
		Before: r.Before, After: r.After, Note: strings.Join(note, "; "),
	})
	if err != nil {
		reason = strings.TrimSpace(reason + " (the audit row could not be written: " + err.Error() + ")")
	}
	return RowResult{Key: r.Key, Outcome: outcome, Reason: reason}
}
