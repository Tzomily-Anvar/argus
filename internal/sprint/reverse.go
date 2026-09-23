package sprint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The audit row, written by the apply and read back by the reversal. Both
// halves live here so that what one writes and what the other expects
// cannot drift apart.

// ErrNoWrites is returned when a reversal is asked for against a change
// set the audit log holds no applied row for: nothing landed, so there is
// nothing to undo.
var ErrNoWrites = errors.New("no applied writes are recorded for that change set")

// ---- writing a row ----------------------------------------------------

// record writes the audit row for one attempted change.
func (a *applier) record(ctx context.Context, c Change, outcome, reason, created string) error {
	return a.s.store.AppendWrite(ctx, store.WriteRecord{
		Operation: c.Op, Target: c.Key, Actor: a.s.cfg.Actor,
		ChangeSet: a.cs.ID, Outcome: outcome,
		Before: before(c), After: after(c),
		Note: a.note(c, outcome, reason, created),
	})
}

// before is what the field or entry held, as the log records it; after is
// what was written. Points as a figure, an assignee as an account id, a
// worklog as hours. Empty is empty.
func before(c Change) string {
	switch c.Op {
	case OpPointsSet:
		n, has := asNumber(c.Guard.Was)
		return held(n, has)
	case OpAssigneeSet:
		return accountOf(c.Guard.Was)
	case OpWorklogUpdate, OpWorklogDelete:
		if c.Guard.Entry != nil {
			return figure(float64(c.Guard.Entry.Seconds) / 3600)
		}
	}
	return ""
}

func after(c Change) string {
	switch c.Op {
	case OpPointsSet:
		n, has := asNumber(c.After)
		return held(n, has)
	case OpAssigneeSet:
		return accountOf(c.After)
	case OpWorklogAdd, OpWorklogUpdate:
		return figure(c.Hours)
	}
	return ""
}

// note is the row's free text: the reason the change was proposed, the
// digest that ties it to what was approved, and the facts a reversal
// needs that the fixed columns do not carry - the sprint, the issue id,
// the worklog entry, the person and the date. Each is "name value", one
// per clause, which is what noteFacts reads back.
func (a *applier) note(c Change, outcome, reason, created string) string {
	parts := []string{c.Reason, "digest " + a.cs.Digest, fmt.Sprintf("sprint %d", a.cs.SprintJiraID)}
	if c.Guard.IssueID != "" {
		parts = append(parts, "issue "+c.Guard.IssueID)
	}
	switch c.Op {
	case OpWorklogAdd:
		if created != "" {
			parts = append(parts, "entry "+created)
		}
	case OpWorklogUpdate, OpWorklogDelete:
		parts = append(parts, "entry "+c.WorklogID)
	}
	if c.Person != "" {
		parts = append(parts, "person "+c.Person)
	}
	if !c.Started.IsZero() {
		parts = append(parts, "started "+c.Started.UTC().Format(time.RFC3339))
	}
	if reason != "" {
		parts = append(parts, outcome+": "+reason)
	}
	return strings.Join(parts, "; ")
}

// noteFacts reads the "name value" clauses back. Anything else in the
// note - the reason, the outcome - is prose and is left alone.
func noteFacts(note string) map[string]string {
	facts := map[string]string{}
	for _, part := range strings.Split(note, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), " ")
		if !found {
			continue
		}
		switch name {
		case "digest", "sprint", "issue", "entry", "person", "started":
			facts[name] = strings.TrimSpace(value)
		}
	}
	return facts
}

// ---- reading the rows back as a reversal --------------------------------

// Reverse builds a preview that undoes a change set, from the audit rows
// it left. It is Propose fed from the log instead of from a report: the
// same held set, the same digest, the same approval and the same apply,
// and it never writes anything itself.
//
// Each applied row becomes its inverse, guarded on the value the log says
// Argus wrote, so a field somebody has since changed by hand fails its
// guard and is left alone: they had a reason, and the reversal is not
// entitled to overrule it.
func (s *Service) Reverse(ctx context.Context, changeSetID string) (ChangeSet, error) {
	all, err := s.store.ListWrites(ctx, 0)
	if err != nil {
		return ChangeSet{}, err
	}
	rows := appliedRows(all, changeSetID)
	if len(rows) == 0 {
		return ChangeSet{}, fmt.Errorf("%w: %s", ErrNoWrites, changeSetID)
	}
	f, err := s.resolveFields()
	if err != nil {
		return ChangeSet{}, err
	}
	people, err := s.store.ListPeople(ctx, true)
	if err != nil {
		return ChangeSet{}, err
	}
	return s.held().Put(reversal(rows, f.points, people, time.Now().UTC(), s.cfg.Actor)), nil
}

// appliedRows picks the rows of one change set that landed, oldest first,
// so the reversal reads in the order the writes were made. The store
// lists newest first and its interface was deliberately not widened for
// this: a filter over a list that will never be long.
func appliedRows(all []store.WriteRecord, changeSetID string) []store.WriteRecord {
	var rows []store.WriteRecord
	if changeSetID == "" {
		return nil
	}
	for i := len(all) - 1; i >= 0; i-- {
		if w := all[i]; w.ChangeSet == changeSetID && w.Outcome == store.OutcomeApplied {
			rows = append(rows, w)
		}
	}
	return rows
}

// reversal is the pure part: audit rows in, a change set out. A row the
// log does not hold enough about to invert is a Skip with its reason.
func reversal(rows []store.WriteRecord, points string, people []store.Person, now time.Time, actor string) ChangeSet {
	cs := ChangeSet{
		BuiltAt: now, ExpiresAt: now.Add(ChangeSetLifetime), Actor: actor,
		Changes: []Change{}, Skipped: []Skip{},
	}
	names := map[string]string{}
	for _, p := range people {
		names[p.AccountID] = p.Name
	}
	for _, w := range rows {
		facts := noteFacts(w.Note)
		if cs.SprintJiraID == 0 {
			cs.SprintJiraID, _ = strconv.ParseInt(facts["sprint"], 10, 64)
		}
		c, err := inverse(w, facts, points, names)
		if err != nil {
			cs.Skipped = append(cs.Skipped, Skip{Key: w.Target, Op: w.Operation, Person: facts["person"], Reason: err.Error()})
			continue
		}
		cs.Changes = append(cs.Changes, c)
	}
	cs.Digest = Digest(cs.Changes)
	return cs
}

// inverse is the change that undoes one applied row.
func inverse(w store.WriteRecord, facts map[string]string, points string, names map[string]string) (Change, error) {
	c := Change{
		Key: w.Target, Op: w.Operation,
		Reason: fmt.Sprintf("reverses %s from change set %s", w.Operation, w.ChangeSet),
		Guard:  Guard{IssueID: facts["issue"]},
	}
	label := func(id string) string {
		if n := names[id]; n != "" {
			return n
		}
		return id
	}
	hours := func(s string) (float64, int, bool) {
		h, err := strconv.ParseFloat(s, 64)
		return h, int(math.Round(h * 3600)), err == nil && h > 0
	}
	started, _ := time.Parse(time.RFC3339, facts["started"])

	switch w.Operation {
	case OpPointsSet:
		// The value written is the guard; the value before it is what
		// goes back, and for an empty-to-value write that is a clear.
		wrote, err := strconv.ParseFloat(w.After, 64)
		if err != nil {
			return c, errors.New("the log does not say what was written")
		}
		c.Field, c.Guard.Was, c.AfterLabel = points, wrote, "(empty)"
		if was, err := strconv.ParseFloat(w.Before, 64); err == nil {
			c.After, c.AfterLabel = was, figure(was)
		}
	case OpAssigneeSet:
		if w.After == "" {
			return c, errors.New("the log does not say who was assigned")
		}
		c.Field, c.Guard.Was, c.AfterLabel = "assignee", map[string]string{"accountId": w.After}, "(unassigned)"
		if w.Before != "" {
			c.After, c.AfterLabel = map[string]string{"accountId": w.Before}, label(w.Before)
		}
	case OpWorklogAdd:
		h, seconds, ok := hours(w.After)
		if facts["entry"] == "" || !ok {
			return c, errors.New("the log does not record the entry Jira created")
		}
		c.Op, c.Field, c.WorklogID, c.Hours, c.Started = OpWorklogDelete, "worklog", facts["entry"], h, started
		c.Person, c.PersonLabel = facts["person"], label(facts["person"])
		c.Guard.Was, c.Guard.Entry = seconds, &WorklogGuard{ID: facts["entry"], Seconds: seconds, Mentions: []string{c.Person}}
		c.AfterLabel = fmt.Sprintf("remove %sh logged on %s", figure(h), started.Format("2006-01-02"))
	case OpWorklogUpdate:
		h, seconds, ok := hours(w.Before)
		_, wrote, wroteOK := hours(w.After)
		if facts["entry"] == "" || !ok || !wroteOK {
			return c, errors.New("the log does not record the hours before and after")
		}
		c.Field, c.WorklogID, c.Hours, c.Started = "worklog", facts["entry"], h, started
		c.Person, c.PersonLabel = facts["person"], label(facts["person"])
		c.After = map[string]any{"timeSpentSeconds": seconds}
		c.Guard.Was, c.Guard.Entry = wrote, &WorklogGuard{ID: facts["entry"], Seconds: wrote}
		c.AfterLabel = fmt.Sprintf("%sh again, from %sh", figure(h), w.After)
	case OpWorklogDelete:
		h, seconds, ok := hours(w.Before)
		if facts["person"] == "" {
			return c, errors.New("the removed entry named no single person, so this tool cannot re-add it")
		}
		if !ok || started.IsZero() {
			return c, errors.New("the log does not record the removed entry's hours and date")
		}
		c.Op, c.Field, c.Hours, c.Started = OpWorklogAdd, "worklog", h, started
		c.Person, c.PersonLabel = facts["person"], label(facts["person"])
		c.After = map[string]any{
			"started":          started.Format(jiraStarted),
			"timeSpentSeconds": seconds,
			"comment":          mentionComment(store.Person{AccountID: c.Person, Name: c.PersonLabel}, ""),
		}
		// No expectation about the issue's entries: this is built from
		// the log, not from a Jira read, so the guard has nothing to
		// hold the list to.
		c.Guard.Was = nil
		c.Reason += "; re-adds the entry under a new id, so it is not the entry that was removed"
		c.AfterLabel = fmt.Sprintf("%s, %sh on %s again", c.PersonLabel, figure(h), started.Format("2006-01-02"))
	default:
		return c, fmt.Errorf("%s is not an operation this tool can reverse", w.Operation)
	}
	return c, nil
}
