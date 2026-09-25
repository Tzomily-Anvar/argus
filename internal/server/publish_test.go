package server_test

// The publish routes over the same stand-in Jira as the apply tests,
// with a stand-in Confluence beside it: what the panel is told, the
// statuses each refusal answers with, and one page created and updated
// through the routes.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/page"
	"github.com/Tzomily-Anvar/argus/internal/publish"
	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

// wiki is the Confluence half: one page at most, created by the first
// publish and updated by the next, and a version that a test can move.
type wiki struct {
	mu      sync.Mutex
	title   string
	body    string
	version int
	writes  int
}

func (k *wiki) serve(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	defer k.mu.Unlock()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	page := func() map[string]any {
		return map[string]any{"id": "9001", "title": k.title, "parentId": "200", "status": "current",
			"version": map[string]any{"number": k.version},
			"body":    map[string]any{"storage": map[string]any{"value": k.body}},
			"_links":  map[string]any{"webui": "/spaces/TEAM/pages/9001"}}
	}
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		Title   string
		Body    struct{ Value string }
		Version struct{ Number int }
	}
	_ = json.Unmarshal(raw, &req)
	switch r.Method + " " + r.URL.Path {
	case "GET /wiki/api/v2/pages":
		results := []any{}
		if k.version > 0 && r.URL.Query().Get("title") == k.title {
			results = append(results, page())
		}
		answer(200, map[string]any{"results": results})
	case "GET /wiki/api/v2/pages/9001":
		answer(200, page())
	case "POST /wiki/api/v2/pages":
		k.writes++
		k.title, k.body, k.version = req.Title, req.Body.Value, 1
		answer(200, page())
	case "PUT /wiki/api/v2/pages/9001":
		k.writes++
		if req.Version.Number != k.version+1 {
			answer(409, map[string]any{"errors": []any{}})
			return
		}
		k.title, k.body, k.version = req.Title, req.Body.Value, req.Version.Number
		answer(200, page())
	default:
		http.Error(w, "not here", http.StatusNotFound)
	}
}

// publishingServer wires the sprint routes with a publisher, over one
// stand-in site that answers Jira and Confluence paths.
func publishingServer(t *testing.T, enable, configured bool) (*server.Server, *wiki, store.Store) {
	t.Helper()
	if enable {
		t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "1")
	} else {
		t.Setenv("ARGUS_SPRINT_ALLOW_WRITES", "0")
	}
	fake := &standIn{t: t, points: map[string]any{}, updated: previewUpdated}
	k := &wiki{}
	fake.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wiki/") {
			k.serve(w, r)
			return
		}
		fake.serve(w, r)
	}))
	t.Cleanup(fake.srv.Close)

	client, err := jira.New(fake.srv.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_ = st.PutPerson(t.Context(), store.Person{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true})
	svc := sprint.NewService(client, st, sprint.Config{
		Project: "ABC", BaseURL: fake.srv.URL, Rules: sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
		Actor: "operator@example.com", EnableWrites: enable,
	})
	cfg := publish.Config{Title: "Sprint {{.Number}} Report", LiveSuffix: "(live)", Actor: "operator@example.com", EnableWrites: enable}
	if configured {
		cfg.SpaceID, cfg.ParentPageID = "100", "200"
	}
	srv := server.New(nil)
	srv.SprintRoutes(svc, st, publish.New(client, svc, st, cfg))
	return srv, k, st
}

func previewPage(t *testing.T, srv *server.Server, body string) (*httptest.ResponseRecorder, publish.Preview) {
	t.Helper()
	rec := call(t, srv, http.MethodPost, "/api/sprint/publish/preview", body)
	var p publish.Preview
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return rec, p
}

func TestPublishStatusSaysWhatIsMissing(t *testing.T) {
	fake := newStandIn(t)
	srv, _ := writableServer(t, fake, false) // no publisher wired at all
	rec := call(t, srv, http.MethodGet, "/api/sprint/publish/status", "")
	var st publish.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	if st.Configured || st.Allowed || st.Setting != "ARGUS_SPRINT_ALLOW_WRITES" || len(st.Sections) != len(page.All) ||
		!strings.Contains(st.Reason, "ARGUS_CONFLUENCE_SPACE_ID") {
		t.Errorf("status = %+v", st)
	}

	srv, _, _ = publishingServer(t, true, false)
	rec, _ = previewPage(t, srv, `{"sprint":21}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) {
		t.Errorf("an unconfigured preview is a 200 saying so: %d %s", rec.Code, rec.Body.String())
	}

	srv, _, _ = publishingServer(t, true, true)
	if rec, _ = previewPage(t, srv, `{"sprint":21}`); rec.Code != http.StatusConflict {
		t.Errorf("a preview before the report is opened: %d, want 409", rec.Code)
	}
}

func TestPublishIsForbiddenWhileWritesAreOff(t *testing.T) {
	srv, k, _ := publishingServer(t, false, true)
	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("report: %d", rec.Code)
	}
	rec, p := previewPage(t, srv, `{"sprint":21,"sections":["summary"]}`)
	if rec.Code != http.StatusOK || p.Action != "create" || len(p.Sections) != 1 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	rec = call(t, srv, http.MethodPost, "/api/sprint/publish", `{"id":"`+p.ID+`","digest":"`+p.Digest+`"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Errorf("publish: %d %s, want 403 naming the setting", rec.Code, rec.Body.String())
	}
	if k.writes != 0 {
		t.Error("the stand-in saw a write with the setting off")
	}
}

func TestPublishCreatesThenRefusesAMovedPage(t *testing.T) {
	srv, k, st := publishingServer(t, true, true)
	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("report: %d", rec.Code)
	}
	_, p := previewPage(t, srv, `{"sprint":21}`)
	if rec := call(t, srv, http.MethodPost, "/api/sprint/publish", `{"id":"`+p.ID+`","digest":"x"}`); rec.Code != http.StatusConflict {
		t.Errorf("wrong digest: %d", rec.Code)
	}
	rec := call(t, srv, http.MethodPost, "/api/sprint/publish", `{"id":"`+p.ID+`","digest":"`+p.Digest+`"}`)
	var res publish.Result
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || res.Action != "create" || res.PageID != "9001" || res.Version != 1 || !strings.HasSuffix(res.URL, "/wiki/spaces/TEAM/pages/9001") {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	if k.writes != 1 || k.title != "Sprint 21 Report" || strings.Count(k.body, page.MarkerStart) != 1 {
		t.Errorf("the page after the create: %+v", k)
	}
	if rec := call(t, srv, http.MethodPost, "/api/sprint/publish", `{"id":"`+p.ID+`","digest":"`+p.Digest+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("a second publish of the same preview: %d, want 409", rec.Code)
	}
	if logged := writesLogged(t, srv, ""); len(logged) != 1 || logged[0].Operation != "page.create" || logged[0].Target != "9001" || logged[0].ChangeSet != p.ID {
		t.Errorf("audit rows = %+v", logged)
	}
	if sp, err := st.GetSprint(t.Context(), 744); err != nil || sp.ConfluencePageID != "9001" {
		t.Errorf("the page id should be on the sprint: %+v, %v", sp, err)
	}

	rec, p = previewPage(t, srv, `{"sprint":21}`)
	if rec.Code != http.StatusOK || p.Action != "update" || p.PageID != "9001" || p.Version != 1 {
		t.Fatalf("second preview: %d %+v", rec.Code, p)
	}
	k.mu.Lock()
	k.version = 2
	k.mu.Unlock()
	rec = call(t, srv, http.MethodPost, "/api/sprint/publish", `{"id":"`+p.ID+`","digest":"`+p.Digest+`"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "version 2") {
		t.Errorf("a moved page: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if k.writes != 1 {
		t.Error("nothing should be sent to a page that moved")
	}
}
