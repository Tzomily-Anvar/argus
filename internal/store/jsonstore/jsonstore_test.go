package jsonstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/jsonstore"
	"github.com/Tzomily-Anvar/argus/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := jsonstore.New(t.TempDir())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// writes.json on every existing install was written before change_set
// and outcome existed, so neither key is in the file. Those records have
// to read as belonging to no change set with no recorded outcome, and a
// record appended afterwards has to carry both keys through the file and
// back - because the file is the only copy.
func TestWriteContextRoundTripsThroughFile(t *testing.T) {
	dir := t.TempDir()
	old := `[{"id":1,"at":"2026-01-16T14:00:00Z","operation":"assign","target":"ABC-124",` +
		`"after":"account-a","actor":"argus"}]`
	if err := os.WriteFile(filepath.Join(dir, "writes.json"), []byte(old), 0o640); err != nil {
		t.Fatalf("seeding writes.json: %v", err)
	}

	s, err := jsonstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.AppendWrite(ctx(), store.WriteRecord{
		Operation: "points.set", Target: "ABC-123", After: "3", Actor: "account-a",
		ChangeSet: "cs-1", Outcome: store.OutcomeApplied,
	}); err != nil {
		t.Fatalf("AppendWrite: %v", err)
	}

	all, err := s.ListWrites(ctx(), 0)
	if err != nil {
		t.Fatalf("ListWrites: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	if all[0].ChangeSet != "cs-1" || all[0].Outcome != store.OutcomeApplied {
		t.Errorf("the new record lost its context through the file: %+v", all[0])
	}
	if all[1].ChangeSet != "" || all[1].Outcome != "" {
		t.Errorf("a record from before the fields existed should read as empty, got %+v", all[1])
	}

	// What is on disk matters as much as what reads back: the new record
	// must carry both keys, and the old one must not have gained them,
	// since an omitted key is how "never recorded" is spelled here.
	b, err := os.ReadFile(filepath.Join(dir, "writes.json"))
	if err != nil {
		t.Fatalf("reading writes.json back: %v", err)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("parsing writes.json: %v", err)
	}
	if string(raw[0]["change_set"]) != `"cs-1"` || string(raw[0]["outcome"]) != `"applied"` {
		t.Errorf("new record on disk lacks its context: %s", raw[0])
	}
	for _, key := range []string{"change_set", "outcome"} {
		if _, present := raw[1][key]; present {
			t.Errorf("the old record should not have gained %q on rewrite:\n%s", key, b)
		}
	}
	if !strings.Contains(string(b), `"change_set": "cs-1"`) {
		t.Errorf("change_set is not spelled as the registry records it:\n%s", b)
	}
}
