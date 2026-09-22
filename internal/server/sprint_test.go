package server_test

// The review endpoint is the one place a sprint can be recorded as having
// been checked, so it is worth holding to its shape. It needs no Jira
// client: a review is a local fact, and recomputing an empty report cache
// touches nothing remote.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/server"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
)

func reviewServer(t *testing.T) (*server.Server, store.Store) {
	t.Helper()

	st, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ends := time.Date(2026, 1, 19, 17, 0, 0, 0, time.UTC)
	if err := st.PutSprint(context.Background(), store.Sprint{
		JiraID: 744, Label: "Sprint 21", Number: 21, EndsAt: &ends,
	}); err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}

	srv := server.New(nil)
	srv.SprintRoutes(sprint.NewService(nil, st, sprint.Config{}), st)
	return srv, st
}

func put(t *testing.T, srv *server.Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestReviewEndpoint(t *testing.T) {
	srv, st := reviewServer(t)
	ctx := context.Background()

	rec := put(t, srv, "/api/sprint/review", `{"sprint_jira_id":744,"reviewed":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if !body["saved"] {
		t.Errorf("answer = %v, want saved", body)
	}

	at, err := st.CapacityReviewedAt(ctx, 744)
	if err != nil {
		t.Fatalf("CapacityReviewedAt: %v", err)
	}
	if at.IsZero() {
		t.Error("the review was not stored")
	}

	// Reopening puts it back to unreviewed rather than to a third state.
	if rec := put(t, srv, "/api/sprint/review", `{"sprint_jira_id":744,"reviewed":false}`); rec.Code != http.StatusOK {
		t.Fatalf("reopening: status = %d, body %s", rec.Code, rec.Body.String())
	}
	if at, _ := st.CapacityReviewedAt(ctx, 744); !at.IsZero() {
		t.Error("reopening did not clear the review")
	}

	// A sprint nobody has swept is a mistake worth reporting, not a
	// review to store quietly against nothing.
	if rec := put(t, srv, "/api/sprint/review", `{"sprint_jira_id":999,"reviewed":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown sprint: status = %d, want 400", rec.Code)
	}
}
