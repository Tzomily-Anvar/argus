package publish_test

// A stand-in site that serves one closed sprint on its Jira side and
// takes page reads and the two page writes on its Confluence side,
// recording exactly what it was sent. The Jira half is the least a
// report needs; the Confluence half is a map of pages with versions a
// test can move.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/publish"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

const (
	pointsField = "customfield_10016"
	sprintField = "customfield_10020"
	space       = "100"
	parent      = "200"
)

type sent struct{ Method, Path, Body string }

type fakePage struct {
	ID, Title, ParentID, Body string
	Version                   int
}

type standIn struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	pages    map[string]*fakePage
	writes   []sent
	searches []string
	conflict bool // answer every update with 409
}

func newStandIn(t *testing.T) *standIn {
	t.Helper()
	f := &standIn{t: t, pages: map[string]*fakePage{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *standIn) put(p fakePage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[p.ID] = &p
}

func (f *standIn) page(id string) fakePage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.pages[id]; p != nil {
		return *p
	}
	return fakePage{}
}

func (f *standIn) sent() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent{}, f.writes...)
}

func (f *standIn) searched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.searches...)
}

func pageJSON(p *fakePage) map[string]any {
	return map[string]any{
		"id": p.ID, "title": p.Title, "parentId": p.ParentID, "status": "current",
		"version": map[string]any{"number": p.Version},
		"body":    map[string]any{"storage": map[string]any{"representation": "storage", "value": p.Body}},
		"_links":  map[string]any{"webui": "/spaces/TEAM/pages/" + p.ID},
	}
}

func issue(id, key, status string, points any) map[string]any {
	fields := map[string]any{
		"summary": key, "issuetype": map[string]any{"name": "Task"}, "status": map[string]any{"name": status},
		"created": "2026-01-06T09:00:00.000+0000", "updated": "2026-01-18T09:00:00.000+0000",
		"assignee":  map[string]any{"accountId": "acc-a", "displayName": "Person A"},
		sprintField: []map[string]any{{"id": 744, "name": "Sprint 21", "state": "closed"}},
	}
	if status == "Done" {
		fields["resolutiondate"] = "2026-01-10T09:00:00.000+0000"
	}
	if points != nil {
		fields[pointsField] = points
	}
	return map[string]any{"id": id, "key": key, "fields": fields}
}

func (f *standIn) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	body, _ := io.ReadAll(r.Body)
	id := strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/")
	switch {
	case r.URL.Path == "/rest/api/3/field":
		answer(200, []map[string]any{{"id": pointsField, "name": "Story Points", "custom": true}, {"id": sprintField, "name": "Sprint", "custom": true}})
	case r.URL.Path == "/rest/api/3/status":
		answer(200, []any{})
	case r.URL.Path == "/rest/agile/1.0/sprint/744":
		answer(200, map[string]any{"id": 744, "name": "Sprint 21", "state": "closed", "originBoardId": 0,
			"startDate": "2026-01-05T09:00:00.000+0000", "endDate": "2026-01-19T09:00:00.000+0000", "completeDate": "2026-01-19T10:00:00.000+0000"})
	case r.URL.Path == "/rest/api/3/search/jql":
		answer(200, map[string]any{"issues": []any{issue("10001", "ABC-1", "Done", 3), issue("10002", "ABC-2", "In Progress", nil)}, "isLast": true})
	case r.URL.Path == "/rest/api/3/changelog/bulkfetch":
		answer(200, map[string]any{"issueChangeLogs": []any{}})

	case r.Method == http.MethodGet && r.URL.Path == "/wiki/api/v2/pages":
		if r.URL.Query().Get("space-id") != space {
			f.t.Errorf("a search outside the configured space: %s", r.URL.RawQuery)
		}
		title := r.URL.Query().Get("title")
		f.searches = append(f.searches, title)
		results := []any{}
		for _, p := range f.pages {
			if p.Title == title {
				results = append(results, pageJSON(p))
			}
		}
		answer(200, map[string]any{"results": results})
	case r.Method == http.MethodGet && id != r.URL.Path:
		if p := f.pages[id]; p != nil {
			answer(200, pageJSON(p))
		} else {
			answer(404, map[string]any{"errors": []any{}})
		}
	case r.Method == http.MethodPost && r.URL.Path == "/wiki/api/v2/pages":
		f.writes = append(f.writes, sent{r.Method, r.URL.Path, string(body)})
		var req struct {
			Title, ParentID string
			Body            struct{ Value string }
		}
		_ = json.Unmarshal(body, &req)
		p := &fakePage{ID: fmt.Sprintf("900%d", len(f.pages)+1), Title: req.Title, ParentID: req.ParentID, Body: req.Body.Value, Version: 1}
		f.pages[p.ID] = p
		answer(200, pageJSON(p))
	case r.Method == http.MethodPut && id != r.URL.Path:
		f.writes = append(f.writes, sent{r.Method, r.URL.Path, string(body)})
		var req struct {
			Title   string
			Body    struct{ Value string }
			Version struct{ Number int }
		}
		_ = json.Unmarshal(body, &req)
		p := f.pages[id]
		if p == nil || f.conflict || req.Version.Number != p.Version+1 {
			answer(409, map[string]any{"errors": []map[string]any{{"title": "version conflict"}}})
			return
		}
		p.Title, p.Body, p.Version = req.Title, req.Body.Value, req.Version.Number
		answer(200, pageJSON(p))
	default:
		f.t.Errorf("the stand-in got %s %s, which nothing here should make", r.Method, r.URL)
		http.Error(w, "not here", http.StatusNotFound)
	}
}

// wired builds the services the way cmd/argus does and opens the report.
func wired(t *testing.T, fake *standIn, writes, configured bool) (*publish.Service, store.Store) {
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
	_ = st.PutPerson(t.Context(), store.Person{AccountID: "acc-a", Name: "Person A", Baseline: 10, Active: true})
	svc := sprint.NewService(client, st, sprint.Config{
		Project: "ABC", BaseURL: fake.srv.URL, Rules: sprint.Rules{Done: []string{"Done"}, Container: []string{"Story"}},
		Actor: "operator@example.com", EnableWrites: writes,
	})
	cfg := publish.Config{Title: "Sprint {{.Number}} Report", LiveSuffix: "(live)", Project: "ABC", Actor: "operator@example.com", EnableWrites: writes}
	if configured {
		cfg.SpaceID, cfg.ParentPageID = space, parent
	}
	if _, err := svc.Report(t.Context(), 21, false); err != nil {
		t.Fatalf("opening the report: %v", err)
	}
	return publish.New(client, svc, st, cfg), st
}
