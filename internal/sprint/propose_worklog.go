package sprint

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The worklog half of a proposal: logging effort for a person on a
// carried ticket, or correcting or removing an entry already there. Kept
// apart from the field edits because it builds an entry rather than
// setting a value, and because the one-entry-per-person rule and the
// comment Argus writes live here.

func (p proposer) worklog(req ChangeRequest, is jira.Issue) (Change, error) {
	row, carried := p.rows[is.Key]
	if !carried || row.State != StateCarried {
		return Change{}, fmt.Errorf("%s was not open when the sprint closed; time is logged only against carryover", is.Key)
	}
	if req.Op == OpWorklogAdd {
		// The guard for an add is the entries the issue already holds,
		// so a second person logging time between preview and apply is
		// noticed rather than duplicated.
		var held []string
		for _, w := range is.Fields.Worklog.Entries {
			held = append(held, w.ID)
		}
		c := p.change(req, is, held)
		c.Field = "worklog"
		if err := p.fillEntry(&c, req, is); err != nil {
			return Change{}, err
		}
		c.Reason = "carried over, more time logged"
		if !row.TimeLogged {
			c.Reason = "carried with no time logged"
		}
		return c, nil
	}

	if req.WorklogID == "" {
		return Change{}, errors.New("no worklog entry was named")
	}
	var entry *jira.WorklogEntry
	for i := range is.Fields.Worklog.Entries {
		if is.Fields.Worklog.Entries[i].ID == req.WorklogID {
			entry = &is.Fields.Worklog.Entries[i]
		}
	}
	if entry == nil {
		return Change{}, fmt.Errorf("%s has no worklog entry %s", is.Key, req.WorklogID)
	}
	if err := p.once(is.Key+" worklog "+entry.ID, fmt.Sprintf("entry %s on %s is already changed above", entry.ID, is.Key)); err != nil {
		return Change{}, err
	}

	c := p.change(req, is, entry.Seconds)
	c.Field = "worklog"
	c.WorklogID = entry.ID
	c.Guard.Entry = &WorklogGuard{
		ID: entry.ID, Updated: entry.Updated.Time, Seconds: entry.Seconds, Mentions: entry.Mentions(),
	}
	if req.Op == OpWorklogDelete {
		c.Hours = round2(float64(entry.Seconds) / 3600)
		c.Started = entry.Started.Time
		c.AfterLabel = fmt.Sprintf("remove %sh logged on %s", figure(c.Hours), entry.Started.Format("2006-01-02"))
		c.Reason = "carried over, an entry removed"
		if names := entry.Mentions(); len(names) == 1 {
			c.Person = names[0]
			c.PersonLabel = p.people[names[0]].Name
		}
		return c, nil
	}
	if err := p.fillEntry(&c, req, is); err != nil {
		return Change{}, err
	}
	c.Reason = "carried over, an entry corrected"
	return c, nil
}

// fillEntry sets the person, amount and date on an add or an update, and
// builds the body the gate accepts: started, timeSpentSeconds and a
// comment mentioning exactly one person. The hours are never written into
// the text; the number lives in one place.
func (p proposer) fillEntry(c *Change, req ChangeRequest, is jira.Issue) error {
	person, err := p.onRoster(req.Person, "person")
	if err != nil {
		return err
	}
	if req.Hours <= 0 {
		return errors.New("hours must be above zero")
	}
	started := req.Started
	if started.IsZero() {
		started = p.in.SprintCloses
	}
	if started.Before(p.in.SprintOpens) || started.After(p.in.SprintCloses) {
		return fmt.Errorf("%s is outside the sprint window, %s to %s",
			started.Format("2006-01-02"), p.in.SprintOpens.Format("2006-01-02"), p.in.SprintCloses.Format("2006-01-02"))
	}
	// One entry per person per issue is the invariant that lets an entry
	// be read back without guessing whose hours are whose.
	if err := p.once(is.Key+" "+person.AccountID,
		fmt.Sprintf("%s already has an entry proposed for %s; one entry per person", is.Key, person.Name)); err != nil {
		return err
	}

	seconds := int(math.Round(req.Hours * 3600))
	c.Person = person.AccountID
	c.PersonLabel = person.Name
	c.Hours = req.Hours
	c.Started = started
	c.After = map[string]any{
		"started":          started.Format(jiraStarted),
		"timeSpentSeconds": seconds,
		"comment":          mentionComment(person, req.Note),
	}
	c.AfterLabel = fmt.Sprintf("%s, %sh on %s", person.Name, figure(req.Hours), started.Format("2006-01-02"))
	if p.in.HoursPerPoint > 0 {
		c.AfterLabel += fmt.Sprintf(" (%s points)", figure(req.Hours/p.in.HoursPerPoint))
	}
	return nil
}

// mentionComment is the Atlassian Document Format comment Argus writes:
// one mention node, then the operator's note if there was one. The note
// is text beside the name and is never read back as data.
func mentionComment(person store.Person, note string) map[string]any {
	content := []any{map[string]any{
		"type":  "mention",
		"attrs": map[string]any{"id": person.AccountID, "text": "@" + person.Name},
	}}
	if note = strings.TrimSpace(note); note != "" {
		content = append(content, map[string]any{"type": "text", "text": " " + note})
	}
	return map[string]any{
		"type": "doc", "version": 1,
		"content": []any{map[string]any{"type": "paragraph", "content": content}},
	}
}

// figure prints a number the way a person would write it: 3, not
// 3.000000, and 2.5 rather than 2.50.
func figure(n float64) string { return strconv.FormatFloat(round2(n), 'f', -1, 64) }
