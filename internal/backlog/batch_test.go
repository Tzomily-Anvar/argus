package backlog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The pure half of the batch: what a request must look like, what each
// action does to a constructed ticket, the request a row becomes, and
// the inverse a reversal builds. No Jira anywhere near these; the
// server's handler tests hold the wire.

const batchSprintField = "customfield_10020"

func service() *Batch {
	return &Batch{
		cfg: BatchConfig{
			OperationsLabel: "Operations", LegacyLabels: []string{"Ops"},
			StoryLinkTypes: []string{"Blocks", "Relates"}, StoryLinkChild: "inward",
			ContainerTypes: []string{"Story"},
		},
		sprintField: batchSprintField,
		previews:    map[string]BatchPreview{},
		now:         func() time.Time { return time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC) },
	}
}

// ticket builds an issue the way Jira would send it.
func ticket(t *testing.T, key, typ string, fields map[string]any) jira.Issue {
	t.Helper()
	all := map[string]any{"summary": "About " + key, "issuetype": map[string]any{"name": typ},
		"updated": "2026-09-27T10:00:00.000+0000"}
	for k, v := range fields {
		all[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"id": "1" + strings.TrimLeft(key, "PRJ-"), "key": key, "fields": all})
	var is jira.Issue
	if err := json.Unmarshal(raw, &is); err != nil {
		t.Fatal(err)
	}
	return is
}

func sprints(entries ...map[string]any) map[string]any {
	return map[string]any{batchSprintField: entries}
}

func TestParseRefusesWhatItCannotPreview(t *testing.T) {
	b := service()
	for name, req := range map[string]BatchRequest{
		"an unknown action":         {Action: "status.set", Keys: []string{"PRJ-1"}},
		"no keys":                   {Action: ActionLabelsAdd, Keys: nil, Params: map[string]any{"labels": []any{"x"}}},
		"a key that is not a key":   {Action: ActionIssueDelete, Keys: []string{"10001"}},
		"a key twice":               {Action: ActionIssueDelete, Keys: []string{"PRJ-1", "PRJ-1"}},
		"a sprint with no id":       {Action: ActionSprintAssign, Keys: []string{"PRJ-1"}},
		"a sprint id of zero":       {Action: ActionSprintAssign, Keys: []string{"PRJ-1"}, Params: map[string]any{"sprint_id": 0.0}},
		"an epic that is not a key": {Action: ActionEpicSet, Keys: []string{"PRJ-1"}, Params: map[string]any{"epic_key": "epic"}},
		"a story with no key":       {Action: ActionStoryLink, Keys: []string{"PRJ-1"}},
		"no labels":                 {Action: ActionLabelsAdd, Keys: []string{"PRJ-1"}, Params: map[string]any{"labels": []any{}}},
		"a label with a space":      {Action: ActionLabelsAdd, Keys: []string{"PRJ-1"}, Params: map[string]any{"labels": []any{"two words"}}},
		"nothing to remove":         {Action: ActionLabelsRemove, Keys: []string{"PRJ-1"}, Params: map[string]any{"labels": []any{}}},
	} {
		_, err := b.parse(req)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if name == "an unknown action" && !errors.Is(err, ErrUnknownAction) {
			t.Errorf("%s: got %v, want ErrUnknownAction", name, err)
		}
		if name != "an unknown action" && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
	sp, err := b.parse(BatchRequest{Action: ActionSprintAssign, Keys: []string{"PRJ-1"}, Params: map[string]any{"sprint_id": 9.0}})
	if err != nil || sp.sprintID != 9 {
		t.Errorf("a JSON number for the sprint: %+v, %v", sp, err)
	}
	if sp, _ = b.parse(BatchRequest{Action: ActionOperationsMigrate, Keys: []string{"PRJ-1"}}); strings.Join(sp.add, ",") != "Operations" || strings.Join(sp.remove, ",") != "Ops" {
		t.Errorf("migrate = %+v", sp)
	}
}

func TestLabelsAreAddedWhereAbsentAndRemovedWherePresent(t *testing.T) {
	b := service()
	sp, _ := b.parse(BatchRequest{Action: ActionOperationsMigrate, Keys: []string{"PRJ-1"}})

	r := b.row(sp, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"Ops", "keep"}}), batchSprintField)
	if r.Skipped != "" || r.Before != "Ops, keep" || r.After != "keep, Operations" {
		t.Errorf("a legacy label: %+v", r)
	}
	if _, path, query, body := request(r); path != "/rest/api/3/issue/PRJ-1" || query != notifyQuery ||
		encoded(body) != `{"update":{"labels":[{"add":"Operations"},{"remove":"Ops"}]}}` {
		t.Errorf("request = %s %s %s", path, query, encoded(body))
	}
	r = b.row(sp, ticket(t, "PRJ-2", "Task", map[string]any{"labels": []string{"Operations"}}), batchSprintField)
	if r.Skipped != "already carries Operations" || r.After != "Operations" {
		t.Errorf("already migrated: %+v", r)
	}
	r = b.row(sp, ticket(t, "PRJ-3", "Task", map[string]any{"labels": []string{"Operations", "Ops"}}), batchSprintField)
	if r.Skipped != "" || encoded(request(r)) != `{"update":{"labels":[{"remove":"Ops"}]}}` {
		t.Errorf("both labels: %+v", r)
	}
	if r.Guard.IssueID != "13" || r.Guard.Updated.IsZero() {
		t.Errorf("guard = %+v", r.Guard)
	}
}

func TestLabelsAreRemovedOnlyWherePresent(t *testing.T) {
	b := service()
	sp, err := b.parse(BatchRequest{Action: ActionLabelsRemove, Keys: []string{"PRJ-1"}, Params: map[string]any{"labels": []any{"Ops", "stale"}}})
	if err != nil || len(sp.add) != 0 || strings.Join(sp.remove, ",") != "Ops,stale" {
		t.Fatalf("parse = %+v, %v", sp, err)
	}
	r := b.row(sp, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"Ops", "keep"}}), batchSprintField)
	if r.Skipped != "" || r.Before != "Ops, keep" || r.After != "keep" ||
		encoded(request(r)) != `{"update":{"labels":[{"remove":"Ops"}]}}` {
		t.Errorf("carries one of them: %+v %s", r, encoded(request(r)))
	}
	r = b.row(sp, ticket(t, "PRJ-2", "Task", map[string]any{"labels": []string{"keep"}}), batchSprintField)
	if r.Skipped != "does not carry Ops, stale" || r.After != "keep" {
		t.Errorf("carries neither: %+v", r)
	}
	// Reversing a removal adds the label back, unless it is back already.
	w := store.WriteRecord{Operation: ActionLabelsRemove, Target: "PRJ-1", Before: "Ops, keep", After: "keep"}
	inv := b.inverse(w, nil, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"keep"}}), batchSprintField)
	if inv.Skipped != "" || inv.After != "keep, Ops" || encoded(request(inv)) != `{"update":{"labels":[{"add":"Ops"}]}}` {
		t.Errorf("reverse: %+v", inv)
	}
	inv = b.inverse(w, nil, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"keep", "Ops"}}), batchSprintField)
	if inv.Skipped == "" {
		t.Errorf("reverse of what is back already: %+v", inv)
	}
}

func encoded(v ...any) string {
	raw, _ := json.Marshal(v[len(v)-1])
	return string(raw)
}

func TestEpicAndStoryRowsSkipWhatIsAlreadySo(t *testing.T) {
	b := service()
	epic, _ := b.parse(BatchRequest{Action: ActionEpicSet, Keys: []string{"PRJ-1"}, Params: map[string]any{"epic_key": "PRJ-9"}})
	under := map[string]any{"parent": map[string]any{"key": "PRJ-9"}}
	if r := b.row(epic, ticket(t, "PRJ-1", "Task", under), batchSprintField); r.Skipped != "already under PRJ-9" {
		t.Errorf("already under: %+v", r)
	}
	if r := b.row(epic, ticket(t, "PRJ-2", "Epic", nil), batchSprintField); r.Skipped == "" {
		t.Errorf("an Epic under an Epic: %+v", r)
	}
	r := b.row(epic, ticket(t, "PRJ-3", "Task", map[string]any{"parent": map[string]any{"key": "PRJ-8"}}), batchSprintField)
	if r.Skipped != "" || r.Before != "PRJ-8" || r.After != "PRJ-9" || encoded(request(r)) != `{"fields":{"parent":{"key":"PRJ-9"}}}` {
		t.Errorf("a move between epics: %+v %s", r, encoded(request(r)))
	}

	story, _ := b.parse(BatchRequest{Action: ActionStoryLink, Keys: []string{"PRJ-1"}, Params: map[string]any{"story_key": "PRJ-5"}})
	linked := map[string]any{"issuelinks": []map[string]any{{"id": "77", "type": map[string]any{"name": "Relates"},
		"inwardIssue": map[string]any{"key": "PRJ-5"}}}}
	if r := b.row(story, ticket(t, "PRJ-1", "Task", linked), batchSprintField); r.Skipped != "already linked to PRJ-5" || r.Before != "PRJ-5" {
		t.Errorf("already linked: %+v", r)
	}
	if r := b.row(story, ticket(t, "PRJ-2", "Story", nil), batchSprintField); !strings.Contains(r.Skipped, "container") {
		t.Errorf("a container as a child: %+v", r)
	}
	r = b.row(story, ticket(t, "PRJ-3", "Task", nil), batchSprintField)
	if r.Skipped != "" || encoded(request(r)) != `{"inwardIssue":{"key":"PRJ-3"},"outwardIssue":{"key":"PRJ-5"},"type":{"name":"Blocks"}}` {
		t.Errorf("a link: %+v %s", r, encoded(request(r)))
	}
	b.cfg.StoryLinkChild = "outward"
	if r = b.row(story, ticket(t, "PRJ-3", "Task", nil), batchSprintField); !strings.Contains(encoded(request(r)), `"inwardIssue":{"key":"PRJ-5"}`) {
		t.Errorf("with the child outward: %s", encoded(request(r)))
	}
}

func TestSprintAndDeleteRows(t *testing.T) {
	b := service()
	sp, _ := b.parse(BatchRequest{Action: ActionSprintAssign, Keys: []string{"PRJ-1"}, Params: map[string]any{"sprint_id": 9.0}})
	sp.sprintName = "Sprint 9"
	was := sprints(map[string]any{"id": 4, "name": "Sprint 4", "state": "closed"}, map[string]any{"id": 8, "name": "Sprint 8", "state": "active"})
	r := b.row(sp, ticket(t, "PRJ-1", "Task", was), batchSprintField)
	if r.Skipped != "" || r.Before != "Sprint 8" || r.After != "Sprint 9" || r.plan.sprintID != 9 || r.plan.fromSprint != 8 {
		t.Errorf("from another sprint: %+v %+v", r, r.plan)
	}
	if r = b.row(sp, ticket(t, "PRJ-2", "Task", sprints(map[string]any{"id": 4, "name": "Sprint 4", "state": "closed"})), batchSprintField); r.Before != "" || r.plan.fromSprint != 0 {
		t.Errorf("a closed sprint is history, not membership: %+v", r)
	}
	if r = b.row(sp, ticket(t, "PRJ-3", "Task", sprints(map[string]any{"id": 9, "name": "Sprint 9", "state": "future"})), batchSprintField); r.Skipped != "already in Sprint 9" {
		t.Errorf("already there: %+v", r)
	}

	del, _ := b.parse(BatchRequest{Action: ActionIssueDelete, Keys: []string{"PRJ-1"}})
	r = b.row(del, ticket(t, "PRJ-1", "Task", nil), batchSprintField)
	method, path, query, body := request(r)
	if r.Skipped != "" || r.Before != "About PRJ-1" || method != http.MethodDelete || path != "/rest/api/3/issue/PRJ-1" || query != deleteQuery || body != nil {
		t.Errorf("a delete: %+v -> %s %s?%s", r, method, path, query)
	}
	if r = b.row(del, ticket(t, "PRJ-2", "Epic", nil), batchSprintField); r.Skipped == "" {
		t.Errorf("an Epic must not be deleted in bulk: %+v", r)
	}
}

func TestPreviewsExpireAndAreTakenOnce(t *testing.T) {
	b := service()
	p := b.hold(BatchPreview{Action: ActionLabelsAdd, Rows: []BatchRow{{Key: "PRJ-1"}, {Key: "PRJ-2", Skipped: "x"}}})
	if p.Changes != 1 || p.Skipped != 1 || p.Digest == "" || len(p.ID) != 32 {
		t.Fatalf("held = %+v", p)
	}
	if b.Get(p.ID) == nil || b.take(p.ID) == nil || b.take(p.ID) != nil || b.Get(p.ID) != nil {
		t.Error("a preview is taken once")
	}
	p = b.hold(BatchPreview{Action: ActionLabelsAdd})
	b.now = func() time.Time { return time.Date(2026, 9, 28, 9, 15, 0, 0, time.UTC) }
	if b.Get(p.ID) != nil {
		t.Error("a preview outlived its fifteen minutes")
	}
	other := b.hold(BatchPreview{Action: ActionLabelsAdd, Rows: []BatchRow{{Key: "PRJ-1", After: "x"}}})
	if other.Digest == digest(BatchPreview{Action: ActionLabelsAdd, Rows: []BatchRow{{Key: "PRJ-1", After: "y"}}}) {
		t.Error("the digest must change with what will be written")
	}
}

func TestInverseUndoesWhatTheLogSays(t *testing.T) {
	b := service()
	facts := noteFacts("digest d; issue 11; type Blocks; link 77; sprint 9; from 8; applied")
	if facts["link"] != "77" || facts["from"] != "8" || facts["sprint"] != "9" || facts["type"] != "Blocks" {
		t.Fatalf("facts = %v", facts)
	}

	labels := store.WriteRecord{Operation: ActionOperationsMigrate, Target: "PRJ-1", Before: "Ops, keep", After: "keep, Operations"}
	r := b.inverse(labels, nil, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"keep", "Operations"}}), batchSprintField)
	if r.Skipped != "" || encoded(request(r)) != `{"update":{"labels":[{"add":"Ops"},{"remove":"Operations"}]}}` {
		t.Errorf("labels back: %+v %s", r, encoded(request(r)))
	}
	if r = b.inverse(labels, nil, ticket(t, "PRJ-1", "Task", map[string]any{"labels": []string{"keep", "Ops"}}), batchSprintField); r.Skipped == "" {
		t.Errorf("labels already put back by hand: %+v", r)
	}

	epic := store.WriteRecord{Operation: ActionEpicSet, Target: "PRJ-1", Before: "", After: "PRJ-9"}
	r = b.inverse(epic, nil, ticket(t, "PRJ-1", "Task", map[string]any{"parent": map[string]any{"key": "PRJ-9"}}), batchSprintField)
	if r.Skipped != "" || encoded(request(r)) != `{"fields":{"parent":null}}` {
		t.Errorf("a parent cleared: %+v %s", r, encoded(request(r)))
	}
	if r = b.inverse(epic, nil, ticket(t, "PRJ-1", "Task", map[string]any{"parent": map[string]any{"key": "PRJ-8"}}), batchSprintField); r.Skipped == "" {
		t.Errorf("a parent changed by hand: %+v", r)
	}

	link := store.WriteRecord{Operation: ActionStoryLink, Target: "PRJ-1", After: "PRJ-5"}
	present := map[string]any{"issuelinks": []map[string]any{{"id": "77", "type": map[string]any{"name": "Blocks"}, "outwardIssue": map[string]any{"key": "PRJ-5"}}}}
	r = b.inverse(link, facts, ticket(t, "PRJ-1", "Task", present), batchSprintField)
	if method, path, _, _ := request(r); r.Skipped != "" || method != http.MethodDelete || path != "/rest/api/3/issueLink/77" {
		t.Errorf("a link removed: %+v", r)
	}
	if r = b.inverse(link, facts, ticket(t, "PRJ-1", "Task", nil), batchSprintField); r.Skipped == "" {
		t.Errorf("a link already gone: %+v", r)
	}
	if r = b.inverse(link, map[string]string{}, ticket(t, "PRJ-1", "Task", present), batchSprintField); r.Skipped == "" {
		t.Errorf("a link the log does not know: %+v", r)
	}

	move := store.WriteRecord{Operation: ActionSprintAssign, Target: "PRJ-1", Before: "Sprint 8", After: "Sprint 9"}
	in9 := sprints(map[string]any{"id": 9, "name": "Sprint 9", "state": "active"})
	if r = b.inverse(move, facts, ticket(t, "PRJ-1", "Task", in9), batchSprintField); r.Skipped != "" || r.plan.sprintID != 8 || r.After != "Sprint 8" {
		t.Errorf("back to the previous sprint: %+v %+v", r, r.plan)
	}
	if r = b.inverse(move, noteFacts("sprint 9; from backlog"), ticket(t, "PRJ-1", "Task", in9), batchSprintField); !r.plan.toBacklog || r.plan.sprintID != 0 {
		t.Errorf("back to the backlog: %+v %+v", r, r.plan)
	}
	if r = b.inverse(move, facts, ticket(t, "PRJ-1", "Task", nil), batchSprintField); r.Skipped == "" {
		t.Errorf("moved elsewhere by hand: %+v", r)
	}
}
