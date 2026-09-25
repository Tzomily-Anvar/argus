package jira

import (
	"net/http"
	"testing"
)

const estimation = "/rest/agile/1.0/issue/ABC-123/estimation"

// The board's estimation endpoint sets the points field without it being
// on the edit screen. Its body is one key, a string that reads as a
// number, or null to clear; its query names a board, any board.
func TestEstimationIsTheBoardsWayOfSettingPoints(t *testing.T) {
	mustAllow(t, assertPermitted(http.MethodPut, estimation, "boardId=42", map[string]any{"value": "6.2"}, on), "a string estimate")
	mustAllow(t, assertPermitted(http.MethodPut, estimation, "boardId=1", map[string]any{"value": nil}, on), "a clear")
	if _, ok := assertPermitted(http.MethodPut, estimation, "boardId=42", map[string]any{"value": "6.2"}, off).(*WritesDisabledError); !ok {
		t.Error("with writes off the estimation write must be refused as disabled")
	}
	refused := []struct {
		name  string
		query string
		body  any
		path  string
	}{
		{"a number, not a string", "boardId=42", map[string]any{"value": 6.2}, estimation},
		{"a negative estimate", "boardId=42", map[string]any{"value": "-1"}, estimation},
		{"not a number", "boardId=42", map[string]any{"value": "six"}, estimation},
		{"a second key", "boardId=42", map[string]any{"value": "6", "fields": map[string]any{}}, estimation},
		{"no body", "boardId=42", nil, estimation},
		{"no board", "", map[string]any{"value": "6"}, estimation},
		{"a board that is not a number", "boardId=abc", map[string]any{"value": "6"}, estimation},
		{"an extra parameter", "boardId=42&notifyUsers=false", map[string]any{"value": "6"}, estimation},
		{"beneath the estimation", "boardId=42", map[string]any{"value": "6"}, estimation + "/x"},
		{"the agile issue itself", "boardId=42", map[string]any{"value": "6"}, "/rest/agile/1.0/issue/ABC-123"},
	}
	for _, c := range refused {
		if err := assertPermitted(http.MethodPut, c.path, c.query, c.body, on); err == nil {
			t.Errorf("%s was allowed", c.name)
		}
	}
}
