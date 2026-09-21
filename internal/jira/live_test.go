package jira_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// A live check against a real Jira site. Skipped unless credentials are
// present, so `go test ./...` stays offline on any machine and in CI.
//
//	ARGUS_JIRA_BASE_URL=https://site.atlassian.net \
//	ARGUS_JIRA_EMAIL=you@example.com \
//	ARGUS_JIRA_TOKEN=... \
//	ARGUS_JIRA_PROJECT=ABC \
//	go test ./internal/jira/ -run Live -v
func TestLiveSprint(t *testing.T) {
	base, email := os.Getenv("ARGUS_JIRA_BASE_URL"), os.Getenv("ARGUS_JIRA_EMAIL")
	token, project := os.Getenv("ARGUS_JIRA_TOKEN"), os.Getenv("ARGUS_JIRA_PROJECT")
	if base == "" || email == "" || token == "" || project == "" {
		t.Skip("set ARGUS_JIRA_BASE_URL, _EMAIL, _TOKEN and _PROJECT to run the live check")
	}

	c, err := jira.New(base, email, token, 8, 45)
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	sprintField, err := c.FieldID("Sprint")
	if err != nil {
		t.Fatalf("resolving the Sprint field: %v", err)
	}
	pointsField, err := c.FieldID("Story Points")
	if err != nil {
		t.Fatalf("resolving the Story Points field: %v", err)
	}
	t.Logf("field ids: Sprint=%s  Story Points=%s", sprintField, pointsField)
	if sprintField == "" {
		t.Fatal("no field named Sprint on this site")
	}

	sp, err := c.ResolveSprint(project, sprintField, 0)
	if err != nil {
		t.Fatalf("resolving the open sprint: %v", err)
	}
	t.Logf("open sprint: id=%d number=%d name=%q state=%s provisional=%v",
		sp.ID, sp.Number, sp.Name, sp.State, sp.Provisional)
	t.Logf("window: %s to %s", sp.StartDate.Format("2006-01-02"), sp.EndDate.Format("2006-01-02"))

	issues, err := c.Search(
		fmt.Sprintf("project = %s AND sprint = %d", project, sp.ID),
		[]string{"summary", "issuetype", "status", "assignee", "created", "resolutiondate", "parent", pointsField},
		0)
	if err != nil {
		t.Fatalf("searching the sprint: %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("the open sprint has no issues, which is almost certainly wrong")
	}

	// Read from configuration rather than hardcoded: these are one team's
	// workflow, and the whole point is that they differ per site.
	done := []string{"Done"}
	if v := os.Getenv("ARGUS_JIRA_DONE_STATUSES"); v != "" {
		done = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				done = append(done, s)
			}
		}
	}
	t.Logf("counting as delivered: %v", done)
	var total, delivered float64
	byType, byStatus := map[string]int{}, map[string]int{}
	nDone, unestimated := 0, 0
	for _, is := range issues {
		byType[is.Fields.IssueType.Name]++
		byStatus[is.Fields.Status.Name]++
		p, ok := is.Number(pointsField)
		if !ok {
			unestimated++
		}
		total += p
		if is.IsDone(done) {
			nDone++
			delivered += p
		}
	}
	t.Logf("issues: %d (%d done, %d with no estimate)", len(issues), nDone, unestimated)
	t.Logf("points: %.1f total, %.1f delivered", total, delivered)
	t.Logf("by type: %v", byType)
	t.Logf("by status: %v", byStatus)
}
