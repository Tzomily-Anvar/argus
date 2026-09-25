package store_test

// The persisted shapes, written down.
//
// Everything in this file is a checked-in statement of what Argus has
// already written to disk and into Postgres in every existing install.
// Changing a struct without changing the expectation here fails the
// build, and the failure explains which kind of change it was and what to
// do instead. See storetest.CheckJSONFields for that reasoning in full.
//
// The expectations are spelled out by hand rather than generated from the
// structs, which would only ever agree with itself. A person has to
// retype the field name, which is the point: it is the moment to ask
// whether every install's existing data still reads.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/store/storetest"
)

func TestPersistedJSONShape(t *testing.T) {
	// Every type the store writes. TestEveryPersistedTypeIsRegistered
	// below refuses to let a new one be added without an entry here.
	t.Run("Person", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.Person", store.Person{}, map[string]string{
			"account_id": "string",
			"name":       "string",
			"baseline":   "float64",
			"active":     "bool",
			"updated_at": "time.Time",
		})
	})

	t.Run("Sprint", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.Sprint", store.Sprint{}, map[string]string{
			"jira_id":    "int64",
			"label":      "string",
			"number":     "int",
			"starts_at":  "*time.Time",
			"ends_at":    "*time.Time",
			"state":      "string",
			"updated_at": "time.Time",
			// Added for publishing. Additive: a sprint written before it
			// reads as never published, which is the truthful reading.
			"confluence_page_id": "string",
		})
	})

	t.Run("Capacity", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.Capacity", store.Capacity{}, map[string]string{
			"sprint_jira_id":     "int64",
			"account_id":         "string",
			"planned_days_off":   "float64",
			"unplanned_days_off": "float64",
			"reviewed":           "bool",
			"note":               "string",
			"updated_at":         "time.Time",
		})
	})

	t.Run("SprintStats", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.SprintStats", store.SprintStats{}, map[string]string{
			"sprint_jira_id":     "int64",
			"baseline_total":     "float64",
			"capacity_total":     "float64",
			"planned_days_off":   "float64",
			"unplanned_days_off": "float64",
			"delivered_total":    "float64",
			"promised":           "int",
			"injected":           "int",
			"completed":          "int",
			"by_epic_class":      "map[string]float64",
			"stories_done":       "int",
			"story_points_done":  "float64",
			"recorded_at":        "time.Time",
		})
	})

	t.Run("WriteRecord", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.WriteRecord", store.WriteRecord{}, map[string]string{
			"id":        "int64",
			"at":        "time.Time",
			"operation": "string",
			"target":    "string",
			"before":    "string",
			"after":     "string",
			"actor":     "string",
			"note":      "string",
			// Added for the sprint write-back. Both are additive: a record
			// written before them reads as belonging to no change set,
			// with no recorded outcome, which is the truthful reading.
			"change_set": "string",
			"outcome":    "string",
		})
	})

	// The close-out draft: per-sprint working state, written by the panel
	// and read back by it after a reload or a restart. Its rows are the
	// sprint package's ChangeRequest, spelled again in the store so the
	// store does not import the package above it; TestDraftRequestMirrors
	// ChangeRequest in internal/sprint holds the two to each other.
	t.Run("Draft", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.Draft", store.Draft{}, map[string]string{
			"sprint_jira_id": "int64",
			"requests":       "[]store.DraftRequest",
			"updated_at":     "time.Time",
		})
	})

	t.Run("DraftRequest", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.DraftRequest", store.DraftRequest{}, map[string]string{
			"key":        "string",
			"op":         "string",
			"points":     "*float64",
			"assignee":   "string",
			"person":     "string",
			"hours":      "float64",
			"started":    "time.Time",
			"note":       "string",
			"worklog_id": "string",
		})
	})

	// PruneResult is never written to disk, but it is returned over the
	// HTTP API, so a rename here breaks the frontend instead of the data.
	// Cheap to hold still, so it is held still.
	t.Run("PruneResult", func(t *testing.T) {
		storetest.CheckJSONFields(t, "store.PruneResult", store.PruneResult{}, map[string]string{
			"capacity_rows": "int",
			"write_rows":    "int",
			// Added with the close-out draft. Additive: an older answer
			// simply lacks the key.
			"drafts": "int",
		})
	})
}

// A type added to store.go and not listed above would be data nobody is
// protecting, and the omission would be invisible. So the source is
// parsed for exported structs carrying JSON tags and each one is required
// to appear in the registry test.
func TestEveryPersistedTypeIsRegistered(t *testing.T) {
	covered := map[string]bool{
		"Person": true, "Sprint": true, "Capacity": true,
		"SprintStats": true, "WriteRecord": true, "PruneResult": true,
		"Draft": true, "DraftRequest": true,
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "store.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing store.go: %v", err)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || !spec.Name.IsExported() {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		tagged := false
		for _, field := range st.Fields.List {
			if field.Tag != nil && strings.Contains(field.Tag.Value, "json:") {
				tagged = true
			}
		}
		if tagged && !covered[spec.Name.Name] {
			t.Errorf("store.%s carries JSON tags but has no entry in "+
				"TestPersistedJSONShape.\n\n"+
				"That means its field names can be renamed or retyped without "+
				"anything noticing, and any data already written under the old "+
				"names is unrecoverable. Add its expected shape there, and a row "+
				"to the golden fixture if it is written to disk.", spec.Name.Name)
		}
		return true
	})
}
