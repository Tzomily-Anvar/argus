package page

import (
	"strings"
	"testing"
)

const block = MarkerStart + "\n<p>new block</p>\n" + MarkerEnd

func TestSpliceReplacesBetweenTheMarkers(t *testing.T) {
	body := "<p>intro the team wrote</p>\n" + MarkerStart + "<p>old block</p>" + MarkerEnd + "\n<h2>Notes</h2><p>their notes</p>"
	out, how, err := Splice(body, block)
	if err != nil {
		t.Fatal(err)
	}
	if how != Replaced {
		t.Errorf("how = %q, want %q", how, Replaced)
	}
	if !strings.HasPrefix(out, "<p>intro the team wrote</p>\n") || !strings.HasSuffix(out, "\n<h2>Notes</h2><p>their notes</p>") {
		t.Errorf("prose either side was not preserved:\n%s", out)
	}
	if strings.Contains(out, "old block") || strings.Count(out, "new block") != 1 {
		t.Errorf("the old block should be gone and the new one there once:\n%s", out)
	}
	if strings.Count(out, MarkerStart) != 1 || strings.Count(out, MarkerEnd) != 1 {
		t.Errorf("markers should appear once each:\n%s", out)
	}
}

func TestSpliceMigratesTheLegacyMarkers(t *testing.T) {
	body := "<p>intro</p>" + LegacyStart + "<p>old tool's block</p>" + LegacyEnd + "<p>after</p>"
	out, how, err := Splice(body, block)
	if err != nil {
		t.Fatal(err)
	}
	if how != Migrated {
		t.Errorf("how = %q, want %q", how, Migrated)
	}
	if strings.Contains(out, LegacyStart) || strings.Contains(out, LegacyEnd) || strings.Contains(out, "old tool") {
		t.Errorf("legacy markers or block survived:\n%s", out)
	}
	if out != "<p>intro</p>"+block+"<p>after</p>" {
		t.Errorf("unexpected result:\n%s", out)
	}
	// Once migrated, the next publish is an ordinary replace.
	again, how, err := Splice(out, block)
	if err != nil || how != Replaced || again != out {
		t.Errorf("second publish: how=%q err=%v\n%s", how, err, again)
	}
}

func TestSpliceAppendsWhenThereAreNoMarkers(t *testing.T) {
	out, how, err := Splice("<p>just prose</p>", block)
	if err != nil {
		t.Fatal(err)
	}
	if how != Appended {
		t.Errorf("how = %q, want %q", how, Appended)
	}
	if out != "<p>just prose</p>\n"+block {
		t.Errorf("unexpected result:\n%s", out)
	}
	// An empty body is a page with nothing on it yet.
	if out, _, err := Splice("", block); err != nil || out != block {
		t.Errorf("empty body: %q, %v", out, err)
	}
}

func TestSpliceRefusesABrokenPage(t *testing.T) {
	cases := map[string]string{
		"start without end":        "<p>x</p>" + MarkerStart + "<p>y</p>",
		"end without start":        "<p>x</p>" + MarkerEnd,
		"legacy start without end": LegacyStart + "<p>y</p>",
		"start twice":              MarkerStart + MarkerEnd + MarkerStart + MarkerEnd,
		"end before start":         MarkerEnd + "<p>y</p>" + MarkerStart,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Splice(body, block); err == nil {
				t.Errorf("no error for %q", body)
			}
		})
	}
	// A block without markers could never be found again, so it is
	// refused before the body is looked at.
	if _, _, err := Splice("<p>x</p>", "<p>no markers</p>"); err == nil {
		t.Error("a block without markers was accepted")
	}
}

func TestScaffoldHoldsTheBlockOnce(t *testing.T) {
	out := Scaffold(fixture(), block)
	if strings.Count(out, block) != 1 {
		t.Errorf("the block should appear exactly once:\n%s", out)
	}
	for _, want := range []string{"Sprint 21", "5 Jan 2026", "16 Jan 2026", "Goal:", "<h2>Notes</h2>"} {
		if !strings.Contains(out, want) {
			t.Errorf("scaffold lacks %q:\n%s", want, out)
		}
	}
	// The scaffold is itself a body Splice can update.
	if _, how, err := Splice(out, block); err != nil || how != Replaced {
		t.Errorf("a scaffolded page should splice as a replace: how=%q err=%v", how, err)
	}
}

func TestLabelsCoverEverySection(t *testing.T) {
	labels := Labels()
	for _, id := range All {
		if labels[id] == "" {
			t.Errorf("no label for %s", id)
		}
	}
	if len(labels) != len(All) {
		t.Errorf("Labels has %d entries, All has %d", len(labels), len(All))
	}
}

// Confluence rewrites a macro when it saves the page: attributes appear
// and their order changes. The marker is still the marker.
func TestSpliceFindsAMarkerConfluenceRewrote(t *testing.T) {
	saved := `<ac:structured-macro ac:name="anchor" ac:schema-version="1" ac:local-id="a1b2" ac:macro-id="9f"><ac:parameter ac:name="">argus-report-start</ac:parameter></ac:structured-macro>`
	savedEnd := `<ac:structured-macro ac:schema-version="1" ac:name="anchor" ac:macro-id="9e"><ac:parameter ac:name="">argus-report-end</ac:parameter></ac:structured-macro>`
	body := "<p>intro</p>\n" + saved + "<p>old</p>" + savedEnd + "\n<p>after</p>"
	out, how, err := Splice(body, block)
	if err != nil || how != Replaced {
		t.Fatalf("how = %q, err = %v", how, err)
	}
	if strings.Contains(out, "old") || !strings.Contains(out, "new block") || !strings.Contains(out, "<p>after</p>") || !strings.Contains(out, "<p>intro</p>") {
		t.Errorf("out = %q", out)
	}
	if _, _, err := Bounds(out); err != nil {
		t.Errorf("the spliced page should be findable again: %v", err)
	}
}

// A comment is not a marker: Confluence drops comments on save, which is
// exactly how a page once lost its markers and grew a second block.
func TestAnHTMLCommentIsNotAMarker(t *testing.T) {
	if HasMarkers("<!-- ARGUS:REPORT:START --><p>x</p><!-- ARGUS:REPORT:END -->") {
		t.Error("comments must not count as markers")
	}
}
