package server

// The bulk bar's routes against a stand-in Jira that accepts the writes
// the gate lists and records exactly what it was sent. In package server
// because the routes are registered by the integrator, not exported.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/backlog"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const sprintFieldID = "customfield_10020"

type seen struct{ Method, Path, Query, Body string }

// backlogStandIn holds four tickets and keeps what the writes do to
// them, so a re-read after a write sees what Jira would hold.
type backlogStandIn struct {
	t   *testing.T
	srv *httptest.Server

	mu     sync.Mutex
	writes []seen
	labels map[string][]string
	links  map[string][]map[string]any
	status int // the answer to every write; 0 means success
	nextID int
}

var tickets = map[string]struct{ id, typ string }{
	"PRJ-1": {"101", "Task"}, "PRJ-2": {"102", "Task"}, "PRJ-3": {"103", "Epic"}, "PRJ-4": {"104", "Task"},
}

func newBacklogStandIn(t *testing.T) *backlogStandIn {
	t.Helper()
	f := &backlogStandIn{t: t, labels: map[string][]string{"PRJ-1": {"Ops"}}, links: map[string][]map[string]any{}, nextID: 500}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *backlogStandIn) issue(key string) map[string]any {
	tk := tickets[key]
	labels := f.labels[key]
	if labels == nil {
		labels = []string{}
	}
	links := f.links[key]
	if links == nil {
		links = []map[string]any{}
	}
	return map[string]any{"id": tk.id, "key": key, "fields": map[string]any{
		"summary": "About " + key, "issuetype": map[string]any{"name": tk.typ},
		"updated": "2026-09-27T10:00:00.000+0000", "labels": labels, "issuelinks": links,
	}}
}

func (f *backlogStandIn) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if v != nil {
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	write := func() bool {
		f.writes = append(f.writes, seen{r.Method, path, r.URL.RawQuery, string(body)})
		if f.status != 0 {
			answer(f.status, map[string]any{"errorMessages": []string{"no"}})
			return false
		}
		return true
	}
	switch {
	case r.Method == http.MethodGet && path == "/rest/api/3/field":
		answer(200, []map[string]any{{"id": sprintFieldID, "name": "Sprint", "custom": true}})
	case r.Method == http.MethodGet && path == "/rest/agile/1.0/sprint/9":
		answer(200, map[string]any{"id": 9, "name": "Sprint 9", "state": "active"})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/rest/api/3/issue/"):
		ref := strings.TrimPrefix(path, "/rest/api/3/issue/")
		for key, tk := range tickets {
			if ref == key || ref == tk.id {
				answer(200, f.issue(key))
				return
			}
		}
		answer(404, map[string]any{"errorMessages": []string{"no such issue"}})
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/rest/api/3/issue/"):
		if !write() {
			return
		}
		var edit struct {
			Update struct {
				Labels []map[string]string `json:"labels"`
			} `json:"update"`
		}
		_ = json.Unmarshal(body, &edit)
		key := strings.TrimPrefix(path, "/rest/api/3/issue/")
		for _, op := range edit.Update.Labels {
			if l := op["add"]; l != "" {
				f.labels[key] = append(f.labels[key], l)
			}
			if l := op["remove"]; l != "" {
				var kept []string
				for _, have := range f.labels[key] {
					if have != l {
						kept = append(kept, have)
					}
				}
				f.labels[key] = kept
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/rest/api/3/issue/"):
		if write() {
			w.WriteHeader(http.StatusNoContent)
		}
	case r.Method == http.MethodPost && path == "/rest/api/3/issueLink":
		if !write() {
			return
		}
		var link struct {
			Type    map[string]string `json:"type"`
			Inward  map[string]string `json:"inwardIssue"`
			Outward map[string]string `json:"outwardIssue"`
		}
		_ = json.Unmarshal(body, &link)
		f.nextID++
		id := strconv.Itoa(f.nextID)
		f.links[link.Inward["key"]] = append(f.links[link.Inward["key"]], map[string]any{"id": id, "type": link.Type, "outwardIssue": map[string]any{"key": link.Outward["key"]}})
		f.links[link.Outward["key"]] = append(f.links[link.Outward["key"]], map[string]any{"id": id, "type": link.Type, "inwardIssue": map[string]any{"key": link.Inward["key"]}})
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/rest/api/3/issueLink/"):
		if write() {
			w.WriteHeader(http.StatusNoContent)
		}
	case r.Method == http.MethodPost && (path == "/rest/agile/1.0/sprint/9/issue" || path == "/rest/agile/1.0/backlog/issue"):
		if write() {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		f.t.Errorf("the stand-in Jira got %s %s, which nothing here should make", r.Method, r.URL)
		http.Error(w, "not here", http.StatusNotFound)
	}
}

func (f *backlogStandIn) sent() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen{}, f.writes...)
}

func (f *backlogStandIn) fail(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// batchServer wires the routes the way the integrator will, with both
// settings read once into the service.
func batchServer(t *testing.T, fake *backlogStandIn, writes, deletes bool) (*Server, store.Store) {
	t.Helper()
	client, err := jira.New(fake.srv.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	b := backlog.NewBatch(client, st, backlog.BatchConfig{
		Actor: "operator@example.com", EnableWrites: writes, AllowDelete: deletes,
		RequestLabel:   "Operations",
		StoryLinkTypes: []string{"Blocks"}, StoryLinkChild: "inward", ContainerTypes: []string{"Story"},
		SprintField: sprintFieldID,
	})
	srv := New(nil)
	srv.backlogBatchRoutes(b)
	return srv, st
}

func hit(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func previewBatch(t *testing.T, srv *Server, body string) backlog.BatchPreview {
	t.Helper()
	rec := hit(t, srv, http.MethodPost, "/api/backlog/batch/preview", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var p backlog.BatchPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func applyBatch(t *testing.T, srv *Server, p backlog.BatchPreview, confirm string) (*httptest.ResponseRecorder, backlog.BatchResult) {
	t.Helper()
	rec := hit(t, srv, http.MethodPost, "/api/backlog/batch/"+p.ID+"/apply", `{"digest":"`+p.Digest+`","confirm":"`+confirm+`"}`)
	var res backlog.BatchResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec, res
}

func TestBatchPreviewRefusesAnUnknownAction(t *testing.T) {
	srv, _ := batchServer(t, newBacklogStandIn(t), true, false)
	if rec := hit(t, srv, http.MethodPost, "/api/backlog/batch/preview", `{"action":"status.set","keys":["PRJ-1"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	if rec := hit(t, srv, http.MethodPost, "/api/backlog/batch/preview", `{"action":"labels.add","keys":["PRJ-1"],"params":{"labels":["two words"]}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad label: status = %d, want 400", rec.Code)
	}
}

func TestBatchApplyIsRefusedWhileWritesAreOff(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, _ := batchServer(t, fake, false, false)
	p := previewBatch(t, srv, `{"action":"labels.add","keys":["PRJ-1"],"params":{"labels":["triage"]}}`)
	if p.Allowed || p.Changes != 1 {
		t.Errorf("preview = %+v", p)
	}
	rec, _ := applyBatch(t, srv, p, "")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Fatalf("status = %d, want 403 naming the setting; body %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("the stand-in saw writes with the setting off: %+v", got)
	}
}

func TestBatchAddsLabelsAndReversesThem(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, st := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"labels.add","keys":["PRJ-1","PRJ-2"],"params":{"labels":["triage"]}}`)
	if p.Changes != 2 || p.Rows[0].Before != "Ops" || p.Rows[0].After != "Ops, triage" || p.Rows[1].After != "triage" {
		t.Fatalf("preview = %+v", p.Rows)
	}
	if rec, _ := applyBatch(t, srv, backlog.BatchPreview{ID: p.ID, Digest: "wrong"}, ""); rec.Code != http.StatusConflict {
		t.Fatalf("wrong digest: status = %d, want 409", rec.Code)
	}

	rec, res := applyBatch(t, srv, p, "")
	if rec.Code != http.StatusOK || res.Applied != 2 || res.Failed != 0 || res.Stopped != "" {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	want := []seen{
		{http.MethodPut, "/rest/api/3/issue/PRJ-1", "notifyUsers=false", `{"update":{"labels":[{"add":"triage"}]}}`},
		{http.MethodPut, "/rest/api/3/issue/PRJ-2", "notifyUsers=false", `{"update":{"labels":[{"add":"triage"}]}}`},
	}
	if got := fake.sent(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the stand-in saw %+v, want %+v", got, want)
	}
	if rec, _ := applyBatch(t, srv, p, ""); rec.Code != http.StatusConflict {
		t.Errorf("second apply: status = %d, want 409", rec.Code)
	}
	logged, _ := st.ListWrites(t.Context(), 10)
	if len(logged) != 2 || logged[1].Operation != "labels.add" || logged[1].Target != "PRJ-1" || logged[1].Before != "Ops" ||
		logged[1].After != "Ops, triage" || logged[1].ChangeSet != p.ID || logged[1].Outcome != store.OutcomeApplied ||
		logged[1].Actor != "operator@example.com" {
		t.Errorf("audit rows = %+v", logged)
	}

	// The reversal removes what was added, judged against the tickets now.
	rec = hit(t, srv, http.MethodPost, "/api/backlog/batch/reverse", `{"batch":"`+p.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reverse: %d %s", rec.Code, rec.Body.String())
	}
	var back backlog.BatchPreview
	_ = json.Unmarshal(rec.Body.Bytes(), &back)
	if back.Reverses != p.ID || back.Changes != 2 || back.Rows[0].Before != "Ops, triage" || back.Rows[0].After != "Ops" {
		t.Fatalf("reversal = %+v", back)
	}
	if rec, res = applyBatch(t, srv, back, ""); rec.Code != http.StatusOK || res.Applied != 2 {
		t.Fatalf("applying the reversal: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 4 || got[2].Body != `{"update":{"labels":[{"remove":"triage"}]}}` {
		t.Errorf("the reversal sent %+v", got[2:])
	}
	if rec = hit(t, srv, http.MethodPost, "/api/backlog/batch/reverse", `{"batch":"`+back.ID+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("reversing a reversal: %d, want 409", rec.Code)
	}
	if rec = hit(t, srv, http.MethodPost, "/api/backlog/batch/reverse", `{"batch":"nothing"}`); rec.Code != http.StatusNotFound {
		t.Errorf("reversing an unknown batch: %d, want 404", rec.Code)
	}
}

// A migration is one edit per ticket that carries the old label; a
// ticket without it is not given the new one, it is left alone.
func TestBatchMigratesTheLegacyLabelInOneEdit(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, _ := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"labels.migrate","keys":["PRJ-1","PRJ-2"],"params":{"from":["Ops"],"to":"Operations"}}`)
	if p.Changes != 1 || p.Skipped != 1 || p.Rows[1].Skipped != "does not carry Ops" {
		t.Fatalf("preview = %+v", p.Rows)
	}
	if rec, res := applyBatch(t, srv, p, ""); rec.Code != http.StatusOK || res.Applied != 1 || res.Skipped != 1 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	got := fake.sent()
	if len(got) != 1 || got[0].Body != `{"update":{"labels":[{"add":"Operations"},{"remove":"Ops"}]}}` {
		t.Errorf("the stand-in saw %+v", got)
	}
}

func TestBatchAssignsASprintInOneRequest(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, st := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"sprint.assign","keys":["PRJ-1","PRJ-2"],"params":{"sprint_id":9}}`)
	if p.Changes != 2 || p.Rows[0].After != "Sprint 9" || p.Rows[0].Before != "" {
		t.Fatalf("preview = %+v", p.Rows)
	}
	rec, res := applyBatch(t, srv, p, "")
	if rec.Code != http.StatusOK || res.Applied != 2 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 1 || got[0] != (seen{http.MethodPost, "/rest/agile/1.0/sprint/9/issue", "", `{"issues":["PRJ-1","PRJ-2"]}`}) {
		t.Errorf("the stand-in saw %+v", got)
	}
	if logged, _ := st.ListWrites(t.Context(), 10); len(logged) != 2 || !strings.Contains(logged[0].Note, "from backlog") || !strings.Contains(logged[0].Note, "sprint 9") {
		t.Errorf("audit rows = %+v", logged)
	}
}

func TestBatchLinksToAStory(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, st := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"story.link","keys":["PRJ-1","PRJ-3"],"params":{"story_key":"PRJ-2"}}`)
	if p.Changes != 1 || p.Skipped != 1 || p.Rows[1].Skipped == "" {
		t.Fatalf("an Epic is not linked as a child: %+v", p.Rows)
	}
	if rec, res := applyBatch(t, srv, p, ""); rec.Code != http.StatusOK || res.Applied != 1 || res.Skipped != 1 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	got := fake.sent()
	if len(got) != 1 || got[0].Body != `{"inwardIssue":{"key":"PRJ-1"},"outwardIssue":{"key":"PRJ-2"},"type":{"name":"Blocks"}}` {
		t.Fatalf("the stand-in saw %+v", got)
	}
	if logged, _ := st.ListWrites(t.Context(), 10); len(logged) != 1 || !strings.Contains(logged[0].Note, "link 501") {
		t.Fatalf("the link id must be read back into the row: %+v", logged)
	}
	rec := hit(t, srv, http.MethodPost, "/api/backlog/batch/reverse", `{"batch":"`+p.ID+`"}`)
	var back backlog.BatchPreview
	_ = json.Unmarshal(rec.Body.Bytes(), &back)
	if rec.Code != http.StatusOK || back.Changes != 1 {
		t.Fatalf("reverse: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ = applyBatch(t, srv, back, ""); rec.Code != http.StatusOK {
		t.Fatalf("applying the reversal: %d %s", rec.Code, rec.Body.String())
	}
	if got = fake.sent(); len(got) != 2 || got[1] != (seen{http.MethodDelete, "/rest/api/3/issueLink/501", "", ""}) {
		t.Errorf("the reversal sent %+v", got[1:])
	}
}

func TestBatchDeleteNeedsTheFlagAndTheCount(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, _ := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"issue.delete","keys":["PRJ-1","PRJ-3"]}`)
	if !p.Irreversible || p.DeleteAllowed || p.Changes != 1 || p.Skipped != 1 || p.Rows[0].Before != "About PRJ-1" {
		t.Fatalf("preview = %+v", p)
	}
	rec, _ := applyBatch(t, srv, p, "1")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ARGUS_BACKLOG_ALLOW_DELETE") {
		t.Fatalf("without the flag: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 0 {
		t.Fatalf("a refused delete wrote: %+v", got)
	}

	srv, st := batchServer(t, fake, true, true)
	p = previewBatch(t, srv, `{"action":"issue.delete","keys":["PRJ-1","PRJ-3"]}`)
	if !p.DeleteAllowed {
		t.Fatalf("preview = %+v", p)
	}
	if rec, _ = applyBatch(t, srv, p, "2"); rec.Code != http.StatusConflict {
		t.Fatalf("the wrong count: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ = applyBatch(t, srv, p, ""); rec.Code != http.StatusConflict {
		t.Fatalf("no count: %d %s", rec.Code, rec.Body.String())
	}
	rec, res := applyBatch(t, srv, p, "1")
	if rec.Code != http.StatusOK || res.Applied != 1 || res.Skipped != 1 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	if got := fake.sent(); len(got) != 1 || got[0] != (seen{http.MethodDelete, "/rest/api/3/issue/PRJ-1", "deleteSubtasks=true", ""}) {
		t.Errorf("the stand-in saw %+v", got)
	}
	logged, _ := st.ListWrites(t.Context(), 10)
	if len(logged) != 1 || logged[0].Operation != "issue.delete" || logged[0].Before != "About PRJ-1" || logged[0].After != "(deleted)" {
		t.Errorf("audit rows = %+v", logged)
	}
	if rec = hit(t, srv, http.MethodPost, "/api/backlog/batch/reverse", `{"batch":"`+p.ID+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("reversing a delete: %d, want 409; body %s", rec.Code, rec.Body.String())
	}
}

func TestBatchStopsAfterThreePermissionFailures(t *testing.T) {
	fake := newBacklogStandIn(t)
	srv, st := batchServer(t, fake, true, false)
	p := previewBatch(t, srv, `{"action":"labels.add","keys":["PRJ-1","PRJ-2","PRJ-3","PRJ-4"],"params":{"labels":["triage"]}}`)
	fake.fail(http.StatusForbidden)

	rec, res := applyBatch(t, srv, p, "")
	if rec.Code != http.StatusOK || res.Failed != 3 || res.Skipped != 1 || !strings.Contains(res.Stopped, "three consecutive permission failures") {
		t.Fatalf("result = %+v", res)
	}
	if last := res.Rows[3]; last.Outcome != store.OutcomeSkipped || last.Reason != "not attempted" {
		t.Errorf("the fourth row = %+v", last)
	}
	if got := fake.sent(); len(got) != 3 {
		t.Errorf("the stand-in saw %d writes, want the three that were refused", len(got))
	}
	if logged, _ := st.ListWrites(t.Context(), 10); len(logged) != 3 || logged[0].Outcome != store.OutcomeFailed {
		t.Errorf("audit rows = %+v", logged)
	}
}
