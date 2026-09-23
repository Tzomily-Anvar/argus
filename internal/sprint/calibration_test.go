package sprint_test

import (
	"encoding/json"
	"testing"

	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
)

// The estimate field id, like the points one, differs on every Jira site.
const estimateField = "customfield_10020"

// sized builds an issue carrying whichever of the two figures is given.
//
// A nil means the field is absent rather than zero, which is the
// distinction the whole comparison rests on: Jira uses null for "nobody
// filled this in", and a ticket with no estimate is not a ticket
// estimated at nothing.
func sized(t *testing.T, key string, points, estimate *float64) jira.Issue {
	t.Helper()
	fields := map[string]any{
		"summary":        "Something",
		"issuetype":      map[string]any{"name": "Task"},
		"status":         map[string]any{"name": "Done"},
		"assignee":       map[string]any{"accountId": "acc-a", "displayName": "Person A"},
		"created":        "2026-01-01T09:00:00.000+0000",
		"resolutiondate": "2026-01-10T09:00:00.000+0000",
	}
	if points != nil {
		fields[pointsField] = *points
	}
	if estimate != nil {
		fields[estimateField] = *estimate
	}
	b, err := json.Marshal(map[string]any{"key": key, "fields": fields})
	if err != nil {
		t.Fatalf("encoding the test issue: %v", err)
	}
	var is jira.Issue
	if err := json.Unmarshal(b, &is); err != nil {
		t.Fatalf("decoding the test issue: %v", err)
	}
	return is
}

func num(f float64) *float64 { return &f }

// calibrationInputs is the standard sprint with both fields configured.
func calibrationInputs(t *testing.T, issues ...jira.Issue) sprint.Inputs {
	t.Helper()
	in := inputs(t)
	in.EstimateField = estimateField
	in.Issues = issues
	// A named ticket is only useful if it can be opened, so the tests
	// build the links the panel shows.
	in.BaseURL = "https://jira.example"
	return in
}

// The plain case: tickets carrying both figures are added up and compared,
// and the direction of the gap is kept.
func TestCalibrationComparesBothFields(t *testing.T) {
	in := calibrationInputs(t,
		sized(t, "ABC-1", num(5), num(3)),
		sized(t, "ABC-2", num(2), num(2)),
		sized(t, "ABC-3", num(1), num(3)),
	)
	c := sprint.Build(in).Calibration

	if !c.Configured {
		t.Fatal("both fields are set, so the comparison is available")
	}
	if c.Compared != 3 || c.Finished != 3 {
		t.Errorf("coverage: compared %d of %d, want 3 of 3", c.Compared, c.Finished)
	}
	if c.Estimated != 8 || c.Actual != 8 {
		t.Errorf("totals: estimated %.1f, actual %.1f, want 8 and 8", c.Estimated, c.Actual)
	}
	// The totals agreeing is not the tickets agreeing, which is exactly
	// why the split is reported beside them.
	if c.Matched != 1 || c.Over != 1 || c.Under != 1 {
		t.Errorf("split: matched %d, over %d, under %d, want 1/1/1", c.Matched, c.Over, c.Under)
	}
	if c.Difference != 0 || !c.HasVariance || c.Variance != 0 {
		t.Errorf("difference %.1f, variance %.3f, want both zero", c.Difference, c.Variance)
	}
}

// Direction is kept rather than collapsed into a magnitude: work costing
// more than expected and work costing less are different situations.
func TestCalibrationKeepsDirection(t *testing.T) {
	over := sprint.Build(calibrationInputs(t,
		sized(t, "ABC-1", num(13), num(10)),
	)).Calibration
	if over.Difference != 3 || over.Variance != 0.3 {
		t.Errorf("over: difference %.1f, variance %.3f, want 3 and 0.3", over.Difference, over.Variance)
	}

	under := sprint.Build(calibrationInputs(t,
		sized(t, "ABC-1", num(8), num(10)),
	)).Calibration
	if under.Difference != -2 || under.Variance != -0.2 {
		t.Errorf("under: difference %.1f, variance %.3f, want -2 and -0.2", under.Difference, under.Variance)
	}
}

// A ticket carrying only one of the two figures cannot be compared, and
// the coverage has to say so rather than the totals quietly speaking for
// the whole sprint.
func TestCalibrationCoverageExcludesOneSidedTickets(t *testing.T) {
	in := calibrationInputs(t,
		sized(t, "ABC-1", num(5), num(5)),
		sized(t, "ABC-2", num(8), nil),  // actual, never refined
		sized(t, "ABC-3", nil, num(13)), // estimate, no actual recorded
		sized(t, "ABC-4", nil, nil),     // neither
	)
	c := sprint.Build(in).Calibration

	if c.Compared != 1 {
		t.Errorf("compared %d, want only the ticket carrying both", c.Compared)
	}
	if c.Finished != 4 {
		t.Errorf("finished %d, want 4 - coverage is out of everything that finished", c.Finished)
	}
	if c.Estimated != 5 || c.Actual != 5 {
		t.Errorf("totals: estimated %.1f, actual %.1f, want 5 and 5 from the one comparable ticket",
			c.Estimated, c.Actual)
	}
}

// The fallback is a data gap, not a comparison.
//
// A finished ticket with no actual is sized from its estimate so the
// sprint is not credited with zero for work that demonstrably happened.
// Comparing that figure against the estimate it was copied from would
// score a perfect match out of a number nobody recorded - and the more of
// them a sprint had, the better calibrated it would look.
func TestCalibrationExcludesTheEstimateFallback(t *testing.T) {
	in := calibrationInputs(t,
		sized(t, "ABC-1", nil, num(8)),
		sized(t, "ABC-2", num(5), num(3)),
	)
	r := sprint.Build(in)
	c := r.Calibration

	if flagCount(r, sprint.FlagEstimateFallback) != 1 {
		t.Fatal("the ticket with no actual should still raise the fallback flag")
	}
	if c.Compared != 1 {
		t.Errorf("compared %d, want 1: the fallback ticket has one number, not two", c.Compared)
	}
	if c.Matched != 0 {
		t.Errorf("matched %d, want 0 - the fallback must not invent an exact match", c.Matched)
	}
	if c.Estimated != 3 || c.Actual != 5 {
		t.Errorf("totals: estimated %.1f, actual %.1f, want 3 and 5", c.Estimated, c.Actual)
	}
}

// Nothing comparable is a state of its own, and the totals must not read
// as a sprint that estimated perfectly.
func TestCalibrationWithNothingComparable(t *testing.T) {
	c := sprint.Build(calibrationInputs(t,
		sized(t, "ABC-1", num(5), nil),
		sized(t, "ABC-2", num(3), nil),
	)).Calibration

	if !c.Configured {
		t.Error("the fields are configured; it is the tickets that carry nothing")
	}
	if c.Compared != 0 || c.Finished != 2 {
		t.Errorf("coverage: compared %d of %d, want 0 of 2", c.Compared, c.Finished)
	}
	if c.HasVariance {
		t.Error("a variance over no tickets is not a variance of zero, it is no reading at all")
	}
	if c.Thin {
		t.Error("thin is about too few tickets to trust, not about none at all")
	}
	if len(c.Diverged) != 0 {
		t.Errorf("diverged %d, want none", len(c.Diverged))
	}
}

// A site with no estimate field, or with one field doing both jobs, has
// nothing to compare and has to be told so.
func TestCalibrationWithoutAnEstimateField(t *testing.T) {
	t.Run("no estimate field", func(t *testing.T) {
		in := calibrationInputs(t, sized(t, "ABC-1", num(5), num(3)))
		in.EstimateField = ""
		if c := sprint.Build(in).Calibration; c.Configured || c.Compared != 0 {
			t.Errorf("configured %v, compared %d, want false and 0", c.Configured, c.Compared)
		}
	})

	// One field doing both jobs would have every ticket agreeing with
	// itself, which is a perfect score on no evidence.
	t.Run("one field for both", func(t *testing.T) {
		in := calibrationInputs(t, sized(t, "ABC-1", num(5), num(3)))
		in.EstimateField = pointsField
		c := sprint.Build(in).Calibration
		if c.Configured || c.Compared != 0 || c.Matched != 0 {
			t.Errorf("configured %v, compared %d, matched %d, want false, 0, 0",
				c.Configured, c.Compared, c.Matched)
		}
	})
}

// Too few tickets to draw a conclusion from is said out loud rather than
// presented with the confidence of a full sprint.
func TestCalibrationMarksAThinComparison(t *testing.T) {
	few := calibrationInputs(t,
		sized(t, "ABC-1", num(5), num(3)),
		sized(t, "ABC-2", num(2), num(2)),
	)
	if c := sprint.Build(few).Calibration; !c.Thin {
		t.Errorf("two comparable tickets should be marked thin, compared %d", c.Compared)
	}

	var many []jira.Issue
	for i := 0; i < 12; i++ {
		many = append(many, sized(t, "ABC-"+string(rune('A'+i)), num(3), num(3)))
	}
	if c := sprint.Build(calibrationInputs(t, many...)).Calibration; c.Thin {
		t.Errorf("twelve comparable tickets should not be marked thin, compared %d", c.Compared)
	}
}

// The tickets that moved furthest, both directions, because a refinement
// conversation starts from specific work rather than from an average.
func TestCalibrationNamesTheWorstDivergences(t *testing.T) {
	in := calibrationInputs(t,
		sized(t, "ABC-1", num(13), num(5)), // +8, the worst overrun
		sized(t, "ABC-2", num(3), num(3)),  // matched, never named
		sized(t, "ABC-3", num(1), num(8)),  // -7, the worst underrun
		sized(t, "ABC-4", num(6), num(5)),  // +1
		sized(t, "ABC-5", num(4), num(5)),  // -1
		sized(t, "ABC-6", num(9), num(5)),  // +4
		sized(t, "ABC-7", num(10), num(5)), // +5
	)
	c := sprint.Build(in).Calibration

	// Three each way at most, and the matched ticket is not one of them.
	if len(c.Diverged) != 5 {
		t.Fatalf("diverged %d, want 5: three overruns and the two underruns", len(c.Diverged))
	}
	want := []string{"ABC-1", "ABC-7", "ABC-6", "ABC-5", "ABC-3"}
	for i, key := range want {
		if c.Diverged[i].Key != key {
			t.Errorf("diverged[%d] is %s, want %s - biggest overrun first, biggest underrun last",
				i, c.Diverged[i].Key, key)
		}
	}
	// ABC-4 is the smallest overrun and is squeezed out by the three
	// above it, which is the point of showing a handful.
	for _, d := range c.Diverged {
		if d.Key == "ABC-4" || d.Key == "ABC-2" {
			t.Errorf("%s should not be named", d.Key)
		}
	}
	if d := c.Diverged[0]; d.Estimated != 5 || d.Actual != 13 || d.Difference != 8 {
		t.Errorf("worst overrun reads %.1f -> %.1f (%.1f), want 5 -> 13 (8)",
			d.Estimated, d.Actual, d.Difference)
	}
	if got, want := c.Diverged[0].URL, "https://jira.example/browse/ABC-1"; got != want {
		t.Errorf("link is %q, want %q - the next step is opening the ticket", got, want)
	}
}

// Carryover has no actual yet and is not compared. The sprint that
// finishes it is the one that gets to learn from it.
func TestCalibrationCountsOnlyWorkFinishedHere(t *testing.T) {
	open := sized(t, "ABC-9", num(8), num(2))
	open.Fields.Status.Name = "In Progress"
	open.Fields.Resolved = jira.Time{}

	in := calibrationInputs(t, sized(t, "ABC-1", num(5), num(5)), open)
	c := sprint.Build(in).Calibration

	if c.Compared != 1 || c.Finished != 1 {
		t.Errorf("coverage: compared %d of %d, want 1 of 1 - the open ticket is neither",
			c.Compared, c.Finished)
	}
	if c.Difference != 0 {
		t.Errorf("difference %.1f, want 0: the carried ticket must not be counted", c.Difference)
	}
}
