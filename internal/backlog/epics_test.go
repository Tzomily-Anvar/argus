package backlog

import "testing"

// The picker offers this project's Epics, and other projects' when the
// team names them; the project's own key is never repeated.
func TestEpicJQLNamesTheProjectsAsked(t *testing.T) {
	if got := epicJQL("ABC", nil); got != "project = ABC AND issuetype = Epic AND statusCategory != Done ORDER BY created DESC" {
		t.Errorf("one project: %s", got)
	}
	if got := epicJQL("ABC", []string{"XYZ", " abc ", "", "QRS"}); got != "project in (ABC, XYZ, QRS) AND issuetype = Epic AND statusCategory != Done ORDER BY created DESC" {
		t.Errorf("several projects: %s", got)
	}
}
