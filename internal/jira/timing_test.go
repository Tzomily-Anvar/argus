package jira_test

import (
	"os"
	"testing"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/jira"
)

// How long does the sprint dropdown actually cost? The answer decides
// whether the list is worth caching or should just be fetched live.
func TestLiveSprintListTiming(t *testing.T) {
	base, email := os.Getenv("ARGUS_JIRA_BASE_URL"), os.Getenv("ARGUS_JIRA_EMAIL")
	token, project := os.Getenv("ARGUS_JIRA_TOKEN"), os.Getenv("ARGUS_JIRA_PROJECT")
	if base == "" || email == "" || token == "" || project == "" {
		t.Skip("needs live Jira credentials")
	}
	c, _ := jira.New(base, email, token, 8, 45)

	sprintField, err := c.FieldID("Sprint")
	if err != nil {
		t.Fatalf("fields: %v", err)
	}

	start := time.Now()
	open, err := c.ResolveSprint(project, sprintField, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Logf("resolve open sprint:      %s", time.Since(start).Round(time.Millisecond))
	t.Logf("  board id: %d", open.OriginBoardID)

	if open.OriginBoardID == 0 {
		t.Skip("no board id on the sprint; cannot list siblings")
	}

	for _, states := range []string{"active,closed", "active", ""} {
		start = time.Now()
		sprints, err := c.BoardSprints(open.OriginBoardID, states)
		took := time.Since(start).Round(time.Millisecond)
		if err != nil {
			t.Logf("list (state=%-14q) FAILED after %s: %v", states, took, err)
			continue
		}
		t.Logf("list (state=%-14q) %s — %d sprints", states, took, len(sprints))
		for i, s := range sprints {
			if i >= 4 {
				break
			}
			t.Logf("    %d  number=%d  state=%-7s  %s", s.ID, s.Number, s.State,
				s.EndDate.Format("2006-01-02"))
		}
	}
}
