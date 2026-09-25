package publish

import (
	"strings"
	"testing"
	"time"
)

func TestDiffKeepsOrderAndNamesEveryLine(t *testing.T) {
	got := diffLines("a\nb\nc\nd", "a\nc\nx\nd")
	want := []DiffLine{{"same", "a"}, {"del", "b"}, {"same", "c"}, {"add", "x"}, {"same", "d"}}
	if len(got) != len(want) {
		t.Fatalf("diff = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if d := diffLines("", "a\nb"); len(d) != 2 || d[0].Kind != "add" || d[1].Kind != "add" {
		t.Errorf("a first publish is all additions, got %+v", d)
	}
	if d := diffLines("a", "a"); len(d) != 1 || d[0].Kind != "same" {
		t.Errorf("an unchanged block is all the same, got %+v", d)
	}
	if d := diffLines("", ""); len(d) != 0 {
		t.Errorf("nothing against nothing is nothing, got %+v", d)
	}
}

func TestInnerFindsEitherMarkerPair(t *testing.T) {
	if got := inner("x\n<!-- ARGUS:REPORT:START -->\nblock\n<!-- ARGUS:REPORT:END -->\ny"); got != "block" {
		t.Errorf("inner = %q", got)
	}
	if got := inner("<!-- SPRINT-REPORT:AUTO:START -->old<!-- SPRINT-REPORT:AUTO:END -->"); got != "old" {
		t.Errorf("legacy inner = %q", got)
	}
	if got := inner("<p>no markers</p>"); got != "" {
		t.Errorf("no markers should read as an empty block, got %q", got)
	}
}

// A preview lasts fifteen minutes and is taken once, like a change set.
func TestPreviewsExpireAndAreTakenOnce(t *testing.T) {
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	h := newPreviews(func() time.Time { return now })
	p := h.Put(Preview{Title: "x", ExpiresAt: now.Add(Lifetime)})
	if p.ID == "" || h.Get(p.ID) == nil {
		t.Fatal("a held preview must be retrievable by its id")
	}
	if h.Take(p.ID) == nil || h.Take(p.ID) != nil || h.Get(p.ID) != nil {
		t.Error("a preview is taken once")
	}
	p = h.Put(Preview{Title: "y", ExpiresAt: now.Add(Lifetime)})
	now = now.Add(Lifetime)
	if h.Get(p.ID) != nil {
		t.Error("a preview must be gone once its lifetime is up")
	}
}

func TestSectionSwitchesFollowTheStandingChoice(t *testing.T) {
	all := switches(nil)
	if len(all) != 9 || !all[0].On || all[0].Label == "" {
		t.Errorf("nil means every section on: %+v", all)
	}
	some := switches([]string{"summary", "people"})
	var on []string
	for _, s := range some {
		if s.On {
			on = append(on, s.ID)
		}
	}
	if strings.Join(on, ",") != "summary,people" {
		t.Errorf("on = %v", on)
	}
}
