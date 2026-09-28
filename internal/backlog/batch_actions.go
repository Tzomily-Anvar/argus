package backlog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// What each action does to one ticket, and the request that becomes.
// Pure over an issue the caller has already read, so every skip rule
// can be tested against a constructed issue with no Jira near it.

// spec is a parsed request: the action and what it was given.
type spec struct {
	action     string
	sprintID   int64
	sprintName string
	epic       string
	story      string
	add        []string // labels to add where absent
	remove     []string // labels to remove where present
}

// plan is the write one row becomes. At most one of its parts is set;
// a sprint move is sent in a group rather than per row.
type plan struct {
	add, remove []string
	parent      *string // set the parent; "" clears it
	link        *linkPlan
	unlink      string // the id of a link to remove
	sprintID    int64
	toBacklog   bool
	fromSprint  int64 // where a move came from, for the record
	delete      bool
}

type linkPlan struct{ typ, inward, outward string }

// The queries the gate insists on, spelled again here so the request
// built and the request judged cannot drift apart unnoticed.
const (
	notifyQuery = "notifyUsers=false"
	deleteQuery = "deleteSubtasks=true"
	maxMove     = 50
)

var issueKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)

// parse checks the request before a single ticket is read: the action
// exists, its parameter is there and well formed, the keys look like
// keys and none is selected twice.
func (b *Batch) parse(req BatchRequest) (spec, error) {
	sp := spec{action: req.Action}
	switch req.Action {
	case ActionSprintAssign:
		id, ok := number(req.Params["sprint_id"])
		if !ok || id <= 0 {
			return sp, invalid("sprint_id must be the id of a sprint")
		}
		sp.sprintID = id
	case ActionEpicSet:
		if sp.epic, _ = req.Params["epic_key"].(string); !issueKey.MatchString(sp.epic) {
			return sp, invalid("epic_key must be an issue key")
		}
	case ActionStoryLink:
		if sp.story, _ = req.Params["story_key"].(string); !issueKey.MatchString(sp.story) {
			return sp, invalid("story_key must be an issue key")
		}
		if len(b.cfg.StoryLinkTypes) == 0 {
			return sp, invalid("no story link type is configured; set ARGUS_JIRA_STORY_LINK_TYPES")
		}
	case ActionLabelsAdd:
		labels, err := labelList(req.Params["labels"])
		if err != nil {
			return sp, err
		}
		sp.add = labels
	case ActionOperationsLabel:
		sp.add = []string{b.cfg.OperationsLabel}
	case ActionOperationsMigrate:
		sp.add, sp.remove = []string{b.cfg.OperationsLabel}, b.cfg.LegacyLabels
	case ActionIssueDelete:
	default:
		return sp, fmt.Errorf("%w: %q", ErrUnknownAction, req.Action)
	}
	if len(req.Keys) == 0 {
		return sp, invalid("no tickets are selected")
	}
	seen := map[string]bool{}
	for _, k := range req.Keys {
		if !issueKey.MatchString(k) {
			return sp, invalid("%q is not an issue key", k)
		}
		if seen[k] {
			return sp, invalid("%s is selected twice", k)
		}
		seen[k] = true
	}
	return sp, nil
}

// number reads an id from the shapes JSON and callers hand it in.
func number(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), n == float64(int64(n))
	case int:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	}
	return 0, false
}

// labelList reads the labels to add: non-empty, and without spaces,
// which Jira refuses with a message about the field rather than the
// label.
func labelList(v any) ([]string, error) {
	var out []string
	items, _ := v.([]any)
	for _, it := range items {
		s, _ := it.(string)
		s = strings.TrimSpace(s)
		if s == "" || strings.ContainsAny(s, " \t\n") {
			return nil, invalid("a label must be one word, not %q", s)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, invalid("labels must name at least one label")
	}
	return out, nil
}

// row says what the action does to one ticket, or why it is left alone.
func (b *Batch) row(sp spec, is jira.Issue, sprintField string) BatchRow {
	r := BatchRow{
		Key: is.Key, Summary: is.Fields.Summary, Type: is.Fields.IssueType.Name,
		Guard: Guard{IssueID: is.ID, Updated: is.Fields.Updated.Time},
	}
	switch sp.action {
	case ActionLabelsAdd, ActionOperationsLabel, ActionOperationsMigrate:
		labelsRow(&r, is.Fields.Labels, sp.add, sp.remove)
	case ActionEpicSet:
		switch current := parentKey(is); {
		case isEpic(is):
			r.Skipped = "an Epic cannot be put under another Epic"
		case is.Key == sp.epic:
			r.Skipped = "is the Epic itself"
		case current == sp.epic:
			r.Before, r.After, r.Skipped = current, current, "already under "+sp.epic
		default:
			r.Before, r.After, r.plan.parent = current, sp.epic, &sp.epic
		}
	case ActionStoryLink:
		linked := b.storyLinks(is)
		r.Before = strings.Join(linked, ", ")
		switch {
		case is.Key == sp.story:
			r.Skipped = "is the Story itself"
		case b.isContainer(is):
			r.Skipped = fmt.Sprintf("a %s is a container, not work under a Story", is.Fields.IssueType.Name)
		case contains(linked, sp.story):
			r.After, r.Skipped = r.Before, "already linked to "+sp.story
		default:
			r.After = sp.story
			r.plan.link = b.linkFor(is.Key, sp.story)
		}
	case ActionSprintAssign:
		current := currentSprint(is, sprintField)
		if current != nil {
			r.Before, r.plan.fromSprint = current.Name, current.ID
		}
		if current != nil && current.ID == sp.sprintID {
			r.After, r.Skipped = current.Name, "already in "+current.Name
			break
		}
		r.After, r.plan.sprintID = sp.sprintName, sp.sprintID
	case ActionIssueDelete:
		// The summary goes in Before so the audit row holds the one
		// thing about the ticket that survives the write.
		r.Before = is.Fields.Summary
		if isEpic(is) {
			r.Skipped = "an Epic is not deleted in bulk; the work beneath it would be orphaned"
			break
		}
		r.After, r.plan.delete = "(deleted)", true
	}
	return r
}

// labelsRow adds what is absent and removes what is present, and skips
// a ticket where that comes to nothing.
func labelsRow(r *BatchRow, have []string, add, remove []string) {
	r.Before = joinLabels(have)
	after := make([]string, 0, len(have)+len(add))
	for _, l := range have {
		if contains(remove, l) {
			r.plan.remove = append(r.plan.remove, l)
		} else {
			after = append(after, l)
		}
	}
	for _, l := range add {
		if !contains(have, l) {
			r.plan.add = append(r.plan.add, l)
			after = append(after, l)
		}
	}
	r.After = joinLabels(after)
	if len(r.plan.add)+len(r.plan.remove) == 0 {
		r.Skipped = "already carries " + joinLabels(add)
	}
}

// linkFor is the link a child gets to its Story, the way this team
// makes them: the type written is the first configured, and which end
// the child sits at is the setting.
func (b *Batch) linkFor(child, story string) *linkPlan {
	l := &linkPlan{typ: b.cfg.StoryLinkTypes[0], inward: child, outward: story}
	if b.cfg.StoryLinkChild == "outward" {
		l.inward, l.outward = story, child
	}
	return l
}

// storyLinks are the keys at the far end of the issue's links of the
// configured story types, from either side.
func (b *Batch) storyLinks(is jira.Issue) []string {
	var keys []string
	for _, l := range is.Fields.Links {
		if other := l.Other(); other != nil && contains(b.cfg.StoryLinkTypes, l.Type.Name) {
			keys = append(keys, other.Key)
		}
	}
	return keys
}

func (b *Batch) isContainer(is jira.Issue) bool {
	return isEpic(is) || contains(b.cfg.ContainerTypes, is.Fields.IssueType.Name)
}

func isEpic(is jira.Issue) bool { return strings.EqualFold(is.Fields.IssueType.Name, "Epic") }

func parentKey(is jira.Issue) string {
	if is.Fields.Parent == nil {
		return ""
	}
	return is.Fields.Parent.Key
}

// currentSprint is the sprint the ticket is on now: the last entry in
// the field that is not closed. The field is cumulative and never
// forgets a sprint, so a closed one is history rather than membership.
func currentSprint(is jira.Issue, sprintField string) *jira.Sprint {
	var current *jira.Sprint
	for _, s := range is.SprintsOn(sprintField) {
		if !strings.EqualFold(s.State, "closed") {
			s := s
			current = &s
		}
	}
	return current
}

// request is the exact request one row becomes, in the shape the gate
// accepts. A sprint move is not built here; the apply sends it for a
// group of rows at once.
func request(r BatchRow) (method, path, query string, body any) {
	issue := "/rest/api/3/issue/" + r.Key
	p := r.plan
	switch {
	case p.delete:
		return http.MethodDelete, issue, deleteQuery, nil
	case p.parent != nil:
		var parent any
		if *p.parent != "" {
			parent = map[string]string{"key": *p.parent}
		}
		return http.MethodPut, issue, notifyQuery, map[string]any{"fields": map[string]any{"parent": parent}}
	case p.link != nil:
		return http.MethodPost, "/rest/api/3/issueLink", "", map[string]any{
			"type":         map[string]string{"name": p.link.typ},
			"inwardIssue":  map[string]string{"key": p.link.inward},
			"outwardIssue": map[string]string{"key": p.link.outward},
		}
	case p.unlink != "":
		return http.MethodDelete, "/rest/api/3/issueLink/" + p.unlink, "", nil
	}
	ops := make([]map[string]string, 0, len(p.add)+len(p.remove))
	for _, l := range p.add {
		ops = append(ops, map[string]string{"add": l})
	}
	for _, l := range p.remove {
		ops = append(ops, map[string]string{"remove": l})
	}
	return http.MethodPut, issue, notifyQuery, map[string]any{"update": map[string]any{"labels": ops}}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
