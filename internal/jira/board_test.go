package jira_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// A stand-in that declares one project's statuses under two issue types,
// with the same statuses listed under both, and one board that estimates
// in a field and arranges the statuses into three columns.
func declaringJira(t *testing.T) *jira.Client {
	t.Helper()
	status := func(id, name, cat string) map[string]any {
		return map[string]any{"id": id, "name": name, "statusCategory": map[string]any{"key": cat}}
	}
	shared := []any{
		status("1", "To Do", "new"),
		status("2", "In Progress", "indeterminate"),
		status("3", "Ready for production", "done"),
		status("4", "Done", "done"),
	}
	answer := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/project/ABC/statuses":
			answer(w, []map[string]any{
				{"id": "10001", "name": "Task", "statuses": shared},
				{"id": "10002", "name": "Bug", "statuses": append(append([]any{}, shared...), status("5", "Superseded", "done"))},
			})
		case "GET /rest/agile/1.0/board/7/configuration":
			answer(w, map[string]any{
				"id": 7, "name": "ABC board",
				"estimation": map[string]any{"type": "field", "field": map[string]any{
					"fieldId": "customfield_10016", "displayName": "Story Points",
				}},
				"columnConfig": map[string]any{"columns": []map[string]any{
					{"name": "To Do", "statuses": []map[string]any{{"id": "1"}}},
					{"name": "In Progress", "statuses": []map[string]any{{"id": "2"}, {"id": "3"}}},
					{"name": "Done", "statuses": []map[string]any{{"id": "4"}, {"id": "5"}}},
				}},
			})
		case "GET /rest/agile/1.0/board/8/configuration":
			answer(w, map[string]any{"id": 8, "estimation": map[string]any{"type": "issueCount"}})
		default:
			t.Errorf("the stand-in got %s %s", r.Method, r.URL)
			http.Error(w, "not here", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := jira.New(srv.URL, "operator@example.com", "token", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestProjectStatusesAreDeduplicatedAcrossTypes(t *testing.T) {
	statuses, err := declaringJira(t).ProjectStatuses("ABC")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 5 {
		t.Fatalf("got %d statuses, want the 5 distinct ones rather than the 9 listed", len(statuses))
	}
	if got := strings.Join(jira.DoneStatusNames(statuses), ","); got != "Ready for production,Done,Superseded" {
		t.Errorf("done-category names = %q", got)
	}
}

func TestBoardConfigurationReadsEstimationAndColumns(t *testing.T) {
	c := declaringJira(t)
	b, err := c.BoardConfiguration(7)
	if err != nil {
		t.Fatal(err)
	}
	if b.PointsField() != "customfield_10016" || b.Estimation.Field.DisplayName != "Story Points" {
		t.Errorf("estimation field = %q (%q)", b.PointsField(), b.Estimation.Field.DisplayName)
	}
	if len(b.Columns) != 3 || b.Columns[1].Name != "In Progress" || strings.Join(b.Columns[1].StatusIDs, ",") != "2,3" {
		t.Errorf("columns = %+v", b.Columns)
	}

	counted, err := c.BoardConfiguration(8)
	if err != nil {
		t.Fatal(err)
	}
	if !counted.EstimatesByCount() || counted.PointsField() != "" {
		t.Errorf("a board estimating by issue count has no points field, got %q", counted.PointsField())
	}
}
