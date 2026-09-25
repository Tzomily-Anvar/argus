package publish_test

import (
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/page"
)

// The markers are spelled twice: in the page package, which renders the
// block between them, and in the Jira client, whose gate insists every
// page body carries them and cannot import page without a cycle. This
// is the test that keeps the two spellings one.
func TestGateAndPageAgreeOnTheMarkers(t *testing.T) {
	if jira.PageMarkerStart != page.MarkerStart {
		t.Errorf("start marker: gate %q, page %q", jira.PageMarkerStart, page.MarkerStart)
	}
	if jira.PageMarkerEnd != page.MarkerEnd {
		t.Errorf("end marker: gate %q, page %q", jira.PageMarkerEnd, page.MarkerEnd)
	}
}

// The settings catalogue spells the default section list because it
// cannot import page either. It has to be page.All, in order.
func TestSectionsDefaultIsEverySection(t *testing.T) {
	s, ok := config.Core().Find("ARGUS_CONFLUENCE_SECTIONS")
	if !ok {
		t.Fatal("ARGUS_CONFLUENCE_SECTIONS is not in the catalogue")
	}
	if want := strings.Join(page.All, ","); s.Default != want {
		t.Errorf("default = %q, want %q", s.Default, want)
	}
}
