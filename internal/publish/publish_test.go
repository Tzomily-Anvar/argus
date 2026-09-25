package publish_test

// The publish against the stand-in in standin_test.go. What these hold:
// nothing is written by a preview, nothing with writes off, exactly the
// previewed page with them on, once, and nothing where the page moved.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/page"
	"github.com/Tzomily-Anvar/argus/internal/publish"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

func TestPreviewCreatesWhenThereIsNoPage(t *testing.T) {
	fake := newStandIn(t)
	pub, _ := wired(t, fake, false, true)
	p, err := pub.Preview(t.Context(), 21, nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if p.Action != "create" || p.How != "created" || p.Title != "Sprint 21 Report" || p.PageID != "" || p.Version != 0 || p.ID == "" || p.Digest == "" {
		t.Errorf("preview = %+v", p)
	}
	if got := fake.searched(); strings.Join(got, "|") != "Sprint 21 Report (live)|Sprint 21 Report" {
		t.Errorf("searched %v: the live title first, then the final one", got)
	}
	if len(p.Diff) == 0 || p.Diff[0].Kind != "add" || len(p.Sections) != len(page.All) {
		t.Errorf("a first publish is all additions of every section: %+v", p)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("a preview wrote: %+v", got)
	}
	if _, err := pub.Preview(t.Context(), 21, []string{"summary", "nothing"}); err == nil {
		t.Error("an unknown section must be refused")
	}
}

func TestPreviewFindsTheLivePageAndMigratesItsMarkers(t *testing.T) {
	fake := newStandIn(t)
	fake.put(fakePage{ID: "555", Title: "Sprint 21 Report (live)", ParentID: parent, Version: 6,
		Body: "<p>scaffold</p>\n" + page.LegacyStart + "\n<p>old</p>\n" + page.LegacyEnd + "\n<p>prose</p>"})
	pub, _ := wired(t, fake, false, true)
	p, err := pub.Preview(t.Context(), 21, []string{"summary"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if p.Action != "update" || p.How != "migrated" || p.PageID != "555" || p.Version != 6 || !p.TitleChanges || p.Title != "Sprint 21 Report" {
		t.Errorf("preview = %+v", p)
	}
	if p.URL != fake.srv.URL+"/wiki/spaces/TEAM/pages/555" {
		t.Errorf("url = %q", p.URL)
	}
	var del, add int
	for _, l := range p.Diff {
		switch l.Kind {
		case "del":
			del++
		case "add":
			add++
		}
	}
	if del != 1 || add == 0 {
		t.Errorf("the diff should drop the old block and add the new: %+v", p.Diff)
	}
	if got := fake.searched(); len(got) != 1 {
		t.Errorf("the live title was found first, so one search: %v", got)
	}
}

func TestPublishCreatesThenUpdatesTheSamePage(t *testing.T) {
	fake := newStandIn(t)
	pub, st := wired(t, fake, true, true)
	p, err := pub.Preview(t.Context(), 21, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pub.Publish(t.Context(), p.ID, "not-the-digest"); !errors.Is(err, publish.ErrDigestMismatch) {
		t.Errorf("wrong digest: %v", err)
	}
	if _, err := pub.Publish(t.Context(), "no-such-preview", p.Digest); !errors.Is(err, publish.ErrExpired) {
		t.Errorf("unknown id: %v", err)
	}
	res, err := pub.Publish(t.Context(), p.ID, p.Digest)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Action != "create" || res.PageID != "9001" || res.Version != 1 || res.URL != fake.srv.URL+"/wiki/spaces/TEAM/pages/9001" {
		t.Errorf("result = %+v", res)
	}
	got := fake.sent()
	if len(got) != 1 || got[0].Method != http.MethodPost || !strings.Contains(got[0].Body, `"parentId":"200"`) || !strings.Contains(got[0].Body, `"spaceId":"100"`) {
		t.Errorf("the stand-in saw %+v", got)
	}
	if _, err := pub.Publish(t.Context(), p.ID, p.Digest); !errors.Is(err, publish.ErrExpired) {
		t.Errorf("a preview is published once: %v", err)
	}

	sp, err := st.GetSprint(t.Context(), 744)
	if err != nil || sp.ConfluencePageID != "9001" {
		t.Errorf("the page id should be stored on the sprint: %+v, %v", sp, err)
	}
	writes, _ := st.ListWrites(t.Context(), 0)
	if len(writes) != 1 {
		t.Fatalf("audit rows = %d", len(writes))
	}
	if w := writes[0]; w.Operation != "page.create" || w.Target != "9001" || w.Before != "" || w.After != "1" ||
		w.Actor != "operator@example.com" || w.ChangeSet != p.ID || w.Outcome != store.OutcomeApplied ||
		!strings.Contains(w.Note, "digest "+p.Digest) || !strings.Contains(w.Note, "title Sprint 21 Report") {
		t.Errorf("audit row = %+v", w)
	}

	// The second preview goes straight to the stored id, no title search,
	// and the update keeps the scaffold prose around the block.
	before := len(fake.searched())
	p2, err := pub.Preview(t.Context(), 21, []string{"summary", "people"})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Action != "update" || p2.PageID != "9001" || p2.Version != 1 || p2.How != "replaced" || p2.TitleChanges {
		t.Errorf("second preview = %+v", p2)
	}
	if len(fake.searched()) != before {
		t.Error("the stored id should be used without a title search")
	}
	res, err = pub.Publish(t.Context(), p2.ID, p2.Digest)
	if err != nil || res.Action != "update" || res.Version != 2 {
		t.Fatalf("second publish: %+v, %v", res, err)
	}
	pg := fake.page("9001")
	if pg.Version != 2 || !strings.Contains(pg.Body, "<h2>Notes</h2>") || strings.Count(pg.Body, page.MarkerStart) != 1 {
		t.Errorf("the page after the update: %+v", pg)
	}
	if !strings.Contains(pg.Body, "Per person") || strings.Contains(pg.Body, "Carryover") {
		t.Errorf("the page should carry exactly the sections ticked: %s", pg.Body)
	}
	writes, _ = st.ListWrites(t.Context(), 1)
	if w := writes[0]; w.Operation != "page.update" || w.Before != "1" || w.After != "2" || w.ChangeSet != p2.ID {
		t.Errorf("the update's row = %+v", w)
	}
}

func TestPublishRefusesWhenThePageMoved(t *testing.T) {
	fake := newStandIn(t)
	fake.put(fakePage{ID: "555", Title: "Sprint 21 Report", ParentID: parent, Version: 3, Body: page.MarkerStart + page.MarkerEnd})
	pub, st := wired(t, fake, true, true)
	p, err := pub.Preview(t.Context(), 21, nil)
	if err != nil || p.Version != 3 {
		t.Fatalf("preview = %+v, %v", p, err)
	}
	moved := fake.page("555")
	moved.Version = 4
	fake.put(moved)
	if _, err := pub.Publish(t.Context(), p.ID, p.Digest); !errors.Is(err, publish.ErrMoved) {
		t.Errorf("a moved page: %v", err)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("nothing should be sent to a page that moved: %+v", got)
	}
	writes, _ := st.ListWrites(t.Context(), 0)
	if len(writes) != 1 || writes[0].Outcome != store.OutcomeSkipped || writes[0].Before != "3" {
		t.Errorf("the refusal is recorded: %+v", writes)
	}

	// And when only Confluence knows it moved, its 409 is ours too.
	p, _ = pub.Preview(t.Context(), 21, nil)
	fake.mu.Lock()
	fake.conflict = true
	fake.mu.Unlock()
	if _, err := pub.Publish(t.Context(), p.ID, p.Digest); !errors.Is(err, publish.ErrMoved) {
		t.Errorf("a 409 from Confluence: %v", err)
	}
	writes, _ = st.ListWrites(t.Context(), 1)
	if writes[0].Outcome != store.OutcomeFailed || writes[0].Target != "555" {
		t.Errorf("the failure is recorded: %+v", writes)
	}
}

func TestPublishIsRefusedWhileWritesAreOff(t *testing.T) {
	fake := newStandIn(t)
	pub, st := wired(t, fake, false, true)
	p, err := pub.Preview(t.Context(), 21, nil)
	if err != nil {
		t.Fatalf("the preview still renders with writes off: %v", err)
	}
	if _, err := pub.Publish(t.Context(), p.ID, p.Digest); !errors.Is(err, publish.ErrWritesDisabled) {
		t.Errorf("got %v", err)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("wrote with the setting off: %+v", got)
	}
	if pub.Held(p.ID) == nil {
		t.Error("the preview should still be held after a refusal")
	}
	if writes, _ := st.ListWrites(t.Context(), 0); len(writes) != 0 {
		t.Errorf("a refusal logged %d rows", len(writes))
	}
	if st := pub.Status(); st.Allowed || !st.Configured || !strings.Contains(st.Reason, "ARGUS_SPRINT_ALLOW_WRITES") {
		t.Errorf("status = %+v", st)
	}
}

func TestNotOfferedWithoutASpace(t *testing.T) {
	fake := newStandIn(t)
	pub, _ := wired(t, fake, true, false)
	if pub.Configured() {
		t.Error("a space and a parent are both needed")
	}
	if _, err := pub.Preview(t.Context(), 21, nil); !errors.Is(err, publish.ErrNotConfigured) {
		t.Errorf("got %v", err)
	}
	st := pub.Status()
	if st.Configured || !strings.Contains(st.Reason, "ARGUS_CONFLUENCE_SPACE_ID") || len(st.Sections) != len(page.All) || !st.Sections[0].On {
		t.Errorf("status = %+v", st)
	}
	if got := fake.sent(); len(got) != 0 {
		t.Errorf("wrote: %+v", got)
	}
}

func TestPreviewNeedsTheReportAndAStoredPageThatExists(t *testing.T) {
	fake := newStandIn(t)
	pub, st := wired(t, fake, false, true)
	if _, err := pub.Preview(t.Context(), 22, nil); !errors.Is(err, sprint.ErrNoReport) {
		t.Errorf("a sprint nobody opened: %v", err)
	}
	sp, _ := st.GetSprint(context.Background(), 744)
	sp.ConfluencePageID = "777"
	_ = st.PutSprint(context.Background(), sp)
	if _, err := pub.Preview(t.Context(), 21, nil); !errors.Is(err, publish.ErrPageGone) {
		t.Errorf("a stored page that is gone is reported, not recreated: %v", err)
	}
	if got := fake.searched(); len(got) != 0 {
		t.Errorf("no title search when an id is stored: %v", got)
	}
}

func TestReasonsComeFromTheCapacityRows(t *testing.T) {
	fake := newStandIn(t)
	pub, st := wired(t, fake, false, true)
	if err := st.PutCapacity(t.Context(), store.Capacity{SprintJiraID: 744, AccountID: "acc-a", Note: "Two days on the incident."}); err != nil {
		t.Fatal(err)
	}
	with, err := pub.Preview(t.Context(), 21, []string{"people", "reasons"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with.Block, "Two days on the incident.") {
		t.Errorf("the note should be on the page when reasons is on:\n%s", with.Block)
	}
	without, _ := pub.Preview(t.Context(), 21, []string{"people"})
	if strings.Contains(without.Block, "Two days on the incident.") {
		t.Errorf("the note must not be on the page when reasons is off:\n%s", without.Block)
	}
}
