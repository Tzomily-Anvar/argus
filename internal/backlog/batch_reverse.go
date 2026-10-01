package backlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The audit row read back as a reversal. What the apply writes into a
// row's note and what this reads out of it live in the same package so
// the two cannot drift apart; the note's shape is spelled out on
// finish, in batch_apply.go.

// rawLink is an issue link with the one field the typed IssueLink leaves
// out: its id, which is what removing it needs.
type rawLink struct {
	ID   string `json:"id"`
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
	Inward  *struct{ Key string } `json:"inwardIssue"`
	Outward *struct{ Key string } `json:"outwardIssue"`
}

func rawLinks(is jira.Issue) []rawLink {
	var links []rawLink
	_ = json.Unmarshal(is.Raw["issuelinks"], &links)
	return links
}

// linkID finds the link of the given type to the other issue, "" when
// the ticket holds none.
func linkID(is jira.Issue, typ, other string) string {
	for _, l := range rawLinks(is) {
		far := l.Outward
		if far == nil {
			far = l.Inward
		}
		if l.Type.Name == typ && far != nil && far.Key == other {
			return l.ID
		}
	}
	return ""
}

func hasLink(is jira.Issue, id string) bool {
	for _, l := range rawLinks(is) {
		if l.ID == id {
			return true
		}
	}
	return false
}

// inverse is the row that undoes one applied audit row, judged against
// the ticket as it is now. What Argus wrote is known from the row; if
// the ticket no longer holds it, somebody changed it on purpose and the
// reversal leaves it alone.
func (b *Batch) inverse(w store.WriteRecord, facts map[string]string, is jira.Issue, sprintField string) BatchRow {
	r := BatchRow{
		Key: is.Key, Summary: is.Fields.Summary, Type: is.Fields.IssueType.Name,
		Guard: Guard{IssueID: is.ID, Updated: is.Fields.Updated.Time},
	}
	switch w.Operation {
	case ActionLabelsAdd, ActionLabelsRemove, ActionLabelsMigrate, ActionRequestLabel:
		was, wrote := splitLabels(w.Before), splitLabels(w.After)
		labelsRow(&r, is.Fields.Labels, except(was, wrote), except(wrote, was))
		if r.Skipped != "" {
			r.Skipped = "the labels no longer hold what was written"
		}
	case ActionEpicSet:
		current := parentKey(is)
		r.Before = current
		if current != w.After {
			r.After, r.Skipped = current, fmt.Sprintf("the parent is now %q, not the %s that was set", current, w.After)
			break
		}
		was := w.Before
		r.After, r.plan.parent = was, &was
	case ActionStoryLink:
		r.Before = w.After
		switch id := facts["link"]; {
		case id == "":
			r.Skipped = "the log does not record the link Jira created"
		case !hasLink(is, id):
			r.Skipped = "the link is no longer on the ticket"
		default:
			r.plan.unlink = id
		}
	case ActionSprintAssign:
		current := currentSprint(is, sprintField)
		if current != nil {
			r.Before, r.plan.fromSprint = current.Name, current.ID
		}
		if current == nil || strconv.FormatInt(current.ID, 10) != facts["sprint"] {
			r.After, r.Skipped = r.Before, "no longer in the sprint it was moved to"
			break
		}
		r.After = w.Before
		if from, err := strconv.ParseInt(facts["from"], 10, 64); err == nil && from > 0 {
			r.plan.sprintID = from
		} else {
			r.plan.toBacklog = true
		}
	}
	return r
}

func splitLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ", ")
}

// except is what is in a and not in b.
func except(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// noteFacts reads the "name value" clauses of a note back. The reason
// and the outcome are prose and are left alone.
func noteFacts(note string) map[string]string {
	facts := map[string]string{}
	for _, part := range strings.Split(note, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(part), " ")
		if !found {
			if strings.TrimSpace(part) == "backlog" {
				facts["backlog"] = "yes"
			}
			continue
		}
		switch name {
		case "digest", "issue", "link", "type", "sprint", "from", "reverses":
			facts[name] = strings.TrimSpace(value)
		}
	}
	return facts
}

// Reverse builds a preview that undoes a batch from the audit rows it
// left, against the tickets as they are now. Nothing is written. A
// delete has no reversal and says so; a reversal is not reversed again,
// because the way back from there is the original action.
func (b *Batch) Reverse(ctx context.Context, batchID string) (BatchPreview, error) {
	all, err := b.store.ListWrites(ctx, 0)
	if err != nil {
		return BatchPreview{}, err
	}
	rows := appliedRows(all, batchID)
	if len(rows) == 0 {
		return BatchPreview{}, fmt.Errorf("%w: %s", ErrNoWrites, batchID)
	}
	if rows[0].Operation == ActionIssueDelete {
		return BatchPreview{}, ErrIrreversible
	}
	if noteFacts(rows[0].Note)["reverses"] != "" {
		return BatchPreview{}, ErrIsReversal
	}
	sprintField, err := b.resolveSprintField()
	if err != nil {
		return BatchPreview{}, err
	}
	p := BatchPreview{Action: rows[0].Operation, Reverses: batchID, Rows: make([]BatchRow, 0, len(rows))}
	for _, w := range rows {
		facts := noteFacts(w.Note)
		ref := facts["issue"]
		if ref == "" {
			ref = w.Target
		}
		is, err := b.client.GetIssue(ref, b.fields(sprintField))
		if err != nil {
			return BatchPreview{}, fmt.Errorf("reading %s: %w", w.Target, err)
		}
		p.Rows = append(p.Rows, b.inverse(w, facts, is, sprintField))
	}
	return b.hold(p), nil
}

// appliedRows picks the rows of one batch that landed, oldest first. The
// store lists newest first, and its interface was not widened for a
// filter over a list that will never be long.
func appliedRows(all []store.WriteRecord, batchID string) []store.WriteRecord {
	if batchID == "" {
		return nil
	}
	var rows []store.WriteRecord
	for i := len(all) - 1; i >= 0; i-- {
		if w := all[i]; w.ChangeSet == batchID && w.Outcome == store.OutcomeApplied {
			rows = append(rows, w)
		}
	}
	return rows
}
