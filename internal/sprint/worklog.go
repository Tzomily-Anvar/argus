package sprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// ErrUnknownKey is returned when a worklog is asked for against a key
// that is not among the sprint's issues. Distinct from ErrNoReport so
// the HTTP layer can say "no such ticket here" rather than "open the
// report first" to somebody who has the report open.
var ErrUnknownKey = errors.New("not among the sprint's issues")

// WorklogPerson is one person a worklog entry is credited to.
type WorklogPerson struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// WorklogView is one existing worklog entry as the Carryover disclosure
// shows it: who recorded it, who it credits, how much, when, and what
// was written beside the names.
//
// People comes from the @mentions in the comment, which on a board where
// the lead logs everybody's time is the attribution; Author is the
// account that made the request and is kept so the two can be told
// apart. An entry mentioning nobody credits its author, and People is
// then empty rather than a guess.
type WorklogView struct {
	ID          string          `json:"id"`
	Author      string          `json:"author"`
	AuthorLabel string          `json:"author_label"`
	People      []WorklogPerson `json:"people"`
	Hours       float64         `json:"hours"`
	Started     time.Time       `json:"started"`
	Note        string          `json:"note"`

	// Window says where the entry falls against the sprint being looked
	// at: before it opened, inside it, or after it closed. Only the inside
	// entries credit this sprint; the others are shown so the figure above
	// them can be understood, and marked so they are not mistaken for it.
	Window string `json:"window,omitempty"`
}

// Worklog lists what is logged on one issue of a sprint whose report is
// cached. Nothing is asked of Jira: the entries came in with the issues
// when the report was built, which is also why a sprint nobody has
// opened is ErrNoReport. Labels come from the report's people, then from
// the entry's own author, and fall back to the account id; the browser
// holds the roster and can finish the job for anyone left.
func (s *Service) Worklog(sprintNumber int, key string) ([]WorklogView, error) {
	entry, found := s.cachedSprint(sprintNumber)
	if !found {
		return nil, fmt.Errorf("%w: sprint %d; open its report first", ErrNoReport, sprintNumber)
	}
	for _, is := range entry.issues {
		if is.Key == key {
			return worklogViews(is, labelsOf(entry.report), entry.report.Sprint.CountsFrom, entry.report.Sprint.CountsUntil), nil
		}
	}
	return nil, fmt.Errorf("%s is %w", key, ErrUnknownKey)
}

// cachedSprint finds the built report for a sprint by the sprint's own
// number. Not through sprintIDs, which is only filled by a resolve; the
// cached entry knows which sprint it is. The same lookup Propose makes.
func (s *Service) cachedSprint(sprintNumber int) (cached, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.reports {
		if sprintNumber > 0 && c.sprint.Number == sprintNumber && c.buildErr == nil && len(c.issues) > 0 {
			return *c, true
		}
	}
	return cached{}, false
}

// labelsOf is account id to name for everyone the report names.
func labelsOf(rep Report) map[string]string {
	out := make(map[string]string, len(rep.People))
	for _, p := range rep.People {
		out[p.AccountID] = p.Name
	}
	return out
}

// worklogViews reads an issue's entries into views. Pure: the issue and
// the labels are all it looks at.
func worklogViews(is jira.Issue, labels map[string]string, opens, closes time.Time) []WorklogView {
	// A slice rather than nil: this crosses to a browser as JSON, and
	// null where an array was promised is a crash in the panel.
	out := make([]WorklogView, 0, len(is.Fields.Worklog.Entries))
	for _, w := range is.Fields.Worklog.Entries {
		v := WorklogView{
			ID:      w.ID,
			Hours:   round2(float64(w.Seconds) / 3600),
			Started: w.Started.Time,
			Note:    commentText(w.Comment),
			People:  []WorklogPerson{},
			Window:  windowOf(w.Started.Time, opens, closes),
		}
		if w.Author != nil {
			v.Author = w.Author.AccountID
			v.AuthorLabel = w.Author.DisplayName
			if v.AuthorLabel == "" {
				v.AuthorLabel = labels[w.Author.AccountID]
			}
		}
		for _, m := range w.MentionShares() {
			label := labels[m.ID]
			if label == "" && w.Author != nil && m.ID == w.Author.AccountID {
				label = w.Author.DisplayName
			}
			if label == "" {
				label = m.ID
			}
			v.People = append(v.People, WorklogPerson{ID: m.ID, Label: label})
		}
		out = append(out, v)
	}
	return out
}

// commentText is the comment's prose with the mentions taken out, so the
// note reads as what was typed beside the names rather than repeating
// them. The comment is an Atlassian Document Format tree; only its text
// nodes are read, with a space where a paragraph or line break was.
func commentText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var root textNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	var b strings.Builder
	root.collect(&b)
	return strings.Join(strings.Fields(b.String()), " ")
}

// textNode is the slice of an ADF node this file needs: the type, the
// text and the children. Its own type rather than the Jira package's,
// which keeps its node private and reads mentions rather than prose.
type textNode struct {
	Type    string     `json:"type"`
	Text    string     `json:"text"`
	Content []textNode `json:"content"`
}

func (n textNode) collect(b *strings.Builder) {
	switch n.Type {
	case "mention":
		return
	case "text":
		b.WriteString(n.Text)
	case "hardBreak", "paragraph":
		b.WriteString(" ")
	}
	for _, c := range n.Content {
		c.collect(b)
	}
}

// windowOf places a moment against a sprint's counting window. Empty when
// no window was given, so the same view serves a caller with no sprint.
func windowOf(at, opens, closes time.Time) string {
	switch {
	case opens.IsZero() || closes.IsZero():
		return ""
	case at.Before(opens):
		return "before"
	case at.After(closes):
		return "after"
	default:
		return "inside"
	}
}
