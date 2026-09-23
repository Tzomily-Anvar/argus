package server_test

// The close-out draft: what lets a half-finished close survive a reload
// or a restart. Nothing here goes near Jira at all.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store"
)

func TestDraftIsEmptyBeforeAnythingIsQueued(t *testing.T) {
	srv, _ := closeoutServer(t)
	rec := call(t, srv, http.MethodGet, "/api/sprint/draft?sprint=744", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var d store.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.SprintJiraID != 744 || len(d.Requests) != 0 {
		t.Errorf("draft = %+v, want the sprint and nothing queued", d)
	}
	// An array on the wire, not null: the panel was promised a list.
	if !strings.Contains(rec.Body.String(), `"requests":[]`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestDraftRoundTripsAndIsDiscarded(t *testing.T) {
	srv, st := closeoutServer(t)

	rec := call(t, srv, http.MethodPut, "/api/sprint/draft", `{"sprint_jira_id":744,"requests":[
		{"key":"ABC-1","op":"points.set","points":3},
		{"key":"ABC-1","op":"assignee.set","assignee":"acc-a"},
		{"key":"ABC-2","op":"worklog.add","person":"acc-a","hours":4.5,"started":"2026-01-16T09:00:00Z","note":"pairing"}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = call(t, srv, http.MethodGet, "/api/sprint/draft?sprint=744", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, body %s", rec.Code, rec.Body.String())
	}
	var d store.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Requests) != 3 || d.UpdatedAt.IsZero() {
		t.Fatalf("draft = %+v", d)
	}
	if d.Requests[0].Points == nil || *d.Requests[0].Points != 3 || d.Requests[1].Points != nil {
		t.Errorf("points must survive as typed or as absent: %+v", d.Requests[:2])
	}
	if r := d.Requests[2]; r.Person != "acc-a" || r.Hours != 4.5 || r.Note != "pairing" || r.Started.IsZero() {
		t.Errorf("worklog row = %+v", r)
	}

	// A second save is the whole queue, so it replaces the first.
	rec = call(t, srv, http.MethodPut, "/api/sprint/draft", `{"sprint_jira_id":744,"requests":[{"key":"ABC-2","op":"points.set","points":2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT: status = %d", rec.Code)
	}
	if held, err := st.GetDraft(t.Context(), 744); err != nil || len(held.Requests) != 1 || held.Requests[0].Key != "ABC-2" {
		t.Errorf("the second save should replace the first, got %+v, err %v", held, err)
	}

	rec = call(t, srv, http.MethodDelete, "/api/sprint/draft?sprint=744", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetDraft(t.Context(), 744); err == nil {
		t.Error("the draft should be gone after Discard")
	}
	rec = call(t, srv, http.MethodGet, "/api/sprint/draft?sprint=744", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"requests":[]`) {
		t.Errorf("after Discard the draft reads as empty again: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, srv, http.MethodDelete, "/api/sprint/draft?sprint=744", ""); rec.Code != http.StatusOK {
		t.Errorf("discarding twice is quiet, got %d", rec.Code)
	}
}

func TestDraftRefusesWhatItCannotUse(t *testing.T) {
	srv, _ := closeoutServer(t)
	cases := []struct {
		name, method, path, body string
	}{
		{"GET with no sprint", http.MethodGet, "/api/sprint/draft", ""},
		{"GET with a sprint of zero", http.MethodGet, "/api/sprint/draft?sprint=0", ""},
		{"GET with a sprint that is not a number", http.MethodGet, "/api/sprint/draft?sprint=twenty-one", ""},
		{"DELETE with no sprint", http.MethodDelete, "/api/sprint/draft", ""},
		{"PUT with no sprint", http.MethodPut, "/api/sprint/draft", `{"requests":[]}`},
		{"PUT with a body it cannot read", http.MethodPut, "/api/sprint/draft", `{"sprint_jira_id":`},
		{"PUT with requests that do not decode", http.MethodPut, "/api/sprint/draft", `{"sprint_jira_id":744,"requests":[{"key":"ABC-1","points":"three"}]}`},
		{"PUT for a sprint the store has never seen", http.MethodPut, "/api/sprint/draft", `{"sprint_jira_id":999,"requests":[]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(t, srv, c.method, c.path, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}
