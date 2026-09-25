package server_test

// The worklog read behind the Carryover disclosure, against the same
// stand-in Jira as the preview tests: it fails the test if anything
// tries to write, and the entries it answers with are the ones the
// report already fetched.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

func TestWorklogNeedsTheReportFirst(t *testing.T) {
	srv := previewServer(t)
	rec := call(t, srv, http.MethodGet, "/api/sprint/worklog?sprint=21&key=ABC-2", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "open its report first") {
		t.Errorf("error = %q", body["error"])
	}
}

func TestWorklogAnswersFromTheCachedReport(t *testing.T) {
	srv := previewServer(t)
	if rec := call(t, srv, http.MethodGet, "/api/sprint/report?sprint=21", ""); rec.Code != http.StatusOK {
		t.Fatalf("building the report: %d %s", rec.Code, rec.Body.String())
	}

	rec := call(t, srv, http.MethodGet, "/api/sprint/worklog?sprint=21&key=ABC-2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []sprint.WorklogView `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	// Nothing is logged on the stand-in's open ticket, and the answer
	// has to say so with an empty array rather than a null.
	if body.Entries == nil || len(body.Entries) != 0 {
		t.Errorf("entries = %#v, want an empty list", body.Entries)
	}
	if !strings.Contains(rec.Body.String(), `"entries":[]`) {
		t.Errorf("the wire form should carry an empty array: %s", rec.Body.String())
	}

	if rec := call(t, srv, http.MethodGet, "/api/sprint/worklog?sprint=21&key=ABC-9", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a key not in the sprint: status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, srv, http.MethodGet, "/api/sprint/worklog?sprint=21", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("no key at all: status = %d, want 400", rec.Code)
	}
}
