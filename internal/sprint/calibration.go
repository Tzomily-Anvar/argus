package sprint

import (
	"math"
	"sort"
)

// Calibration compares what work was expected to cost against what it
// turned out to cost.
//
// Two numbers are recorded on a ticket: an estimate agreed at refinement,
// and the actual points filled in when the work finishes. The distance
// between them says something about how well refinement is reading the
// work - which is a retrospective conversation, not a score. There is no
// right answer somebody failed to hit, so nothing here is named accuracy
// and nothing here is coloured as a failure.
//
// The estimate field has a second job in this report: it stands in for a
// missing actual on finished work, flagged as estimate_used. That is a
// data gap rather than a comparison, and a ticket sized that way is left
// out of everything below. Comparing the estimate against a copy of
// itself would score a perfect match out of a number nobody recorded,
// and the more of them a sprint had the better calibrated it would look.
type Calibration struct {
	// Configured is false when this site cannot make the comparison at
	// all - no estimate field, or one field doing both jobs. A team with
	// only one number to record has nothing to compare and should be told
	// that rather than shown an empty chart.
	Configured bool `json:"configured"`

	// Estimated and Actual are totalled over the comparable tickets only,
	// so they are two readings of the same subset rather than two totals
	// of different things.
	//
	// Both are the ticket's whole size, not the share this sprint was
	// credited with. The question is what refinement thought of a piece
	// of work against what it cost, and that is answered once, by the
	// sprint that finished it.
	Estimated float64 `json:"estimated"`
	Actual    float64 `json:"actual"`

	// Difference is Actual less Estimated: positive means the work cost
	// more than refinement expected, negative that it cost less. The two
	// are different situations and are never folded into one magnitude -
	// a team consistently over and a team consistently under need
	// different conversations, and an absolute figure hides which.
	Difference float64 `json:"difference"`

	// Variance is Difference as a share of Estimated, so 0.3 is work
	// costing a third more than expected. Absent when nothing estimable
	// was compared: dividing by a zero total would report an infinite
	// miss on no evidence.
	Variance    float64 `json:"variance"`
	HasVariance bool    `json:"has_variance"`

	// Compared is how many finished tickets carried both figures, and
	// Finished how many finished at all. The pair is the coverage, and it
	// is reported next to every total because the totals speak for the
	// subset and not for the sprint.
	Compared int `json:"compared"`
	Finished int `json:"finished"`

	// How the comparable tickets split. Most land on Matched, so the
	// variance above is the work of a handful of tickets rather than a
	// general drift - which is why the ones below are worth naming.
	Matched int `json:"matched"`
	Over    int `json:"over"`
	Under   int `json:"under"`

	// Thin marks a comparison over too few tickets to read as anything
	// but a hint. It is never a reason to hide the figure, only to say
	// how much weight it carries.
	Thin bool `json:"thin"`

	// Diverged is the handful that moved furthest, in both directions.
	// A refinement conversation starts from a specific piece of work, and
	// an average gives nobody anything to talk about.
	Diverged []Divergence `json:"diverged,omitempty"`
}

// Divergence is one ticket whose actual and estimate disagreed.
type Divergence struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	URL     string `json:"url"`

	Estimated float64 `json:"estimated"`
	Actual    float64 `json:"actual"`

	// Difference is Actual less Estimated, keeping the sign so the two
	// directions stay apart in the list as well as in the totals.
	Difference float64 `json:"difference"`
}

// thinComparison is the point below which a sprint's variance is a
// reading on a few tickets rather than on refinement.
//
// Sprint-sized samples are small to begin with. At seven comparable
// tickets a single one three points out moves the variance by roughly a
// fifth, so a figure drawn from that many is worth showing and is not
// worth drawing a conclusion from. Ten is where one ticket stops being
// able to do that on its own.
const thinComparison = 10

// divergedPerDirection is how many tickets are named each way. Three is
// enough to show whether the variance came from one outlier or from a
// pattern, and few enough that the list is read rather than scrolled.
const divergedPerDirection = 3

// calibrate compares estimates against actuals over the work a sprint
// finished. It is given the concluded rows only: carryover has no actual
// yet, and work that finished before this sprint opened was already
// compared by the sprint that finished it.
func calibrate(finished []Row, in Inputs) Calibration {
	c := Calibration{
		// One field doing both jobs would compare a number against
		// itself, which is the same false perfect score the fallback
		// exclusion below exists to avoid.
		Configured: in.EstimateField != "" && in.EstimateField != in.PointsField,
		Finished:   len(finished),
	}
	if !c.Configured {
		return c
	}

	var diverged []Divergence
	for _, row := range finished {
		// UsedEstimate is the fallback: the actual was never filled in
		// and the estimate was counted in its place. There is one number
		// here, not two, so there is nothing to compare.
		if row.UsedEstimate || !row.HasPoints || !row.HasEstimate {
			continue
		}
		c.Compared++
		c.Estimated += row.Estimate
		c.Actual += row.Points

		d := round2(row.Points - row.Estimate)
		switch {
		case d > 0:
			c.Over++
		case d < 0:
			c.Under++
		default:
			c.Matched++
			continue
		}
		diverged = append(diverged, Divergence{
			Key: row.Key, Summary: row.Summary, URL: row.URL,
			Estimated: row.Estimate, Actual: row.Points, Difference: d,
		})
	}

	c.Estimated = round2(c.Estimated)
	c.Actual = round2(c.Actual)
	c.Difference = round2(c.Actual - c.Estimated)
	if c.Estimated > 0 {
		// Four places, so a percentage keeps two. Rounding the ratio to
		// two would round the percentage to whole units of ten.
		c.Variance = math.Round((c.Difference/c.Estimated)*10000) / 10000
		c.HasVariance = true
	}
	c.Thin = c.Compared > 0 && c.Compared < thinComparison
	c.Diverged = worstDivergences(diverged)
	return c
}

// worstDivergences picks the few tickets that moved furthest each way.
//
// Ranked by points rather than by ratio. A one-point ticket that cost
// three is the larger miss in proportion, but the tickets a retrospective
// can do something about are the ones that moved the sprint, and both
// figures are shown either way so the ratio is never hidden.
func worstDivergences(all []Divergence) []Divergence {
	if len(all) == 0 {
		return nil
	}
	// Ties broken on the key, so the same sprint always names the same
	// tickets rather than a different one each time Jira changes order.
	sort.Slice(all, func(i, j int) bool {
		a, b := math.Abs(all[i].Difference), math.Abs(all[j].Difference)
		if a != b {
			return a > b
		}
		return all[i].Key < all[j].Key
	})

	var over, under []Divergence
	for _, d := range all {
		if d.Difference > 0 && len(over) < divergedPerDirection {
			over = append(over, d)
		}
		if d.Difference < 0 && len(under) < divergedPerDirection {
			under = append(under, d)
		}
	}

	out := append(over, under...)
	// Biggest overrun first down to biggest underrun last, so the list
	// reads as one scale rather than as two piles.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Difference > out[j].Difference })
	return out
}

// CalibrationSprint is one sprint's place in the run of them.
//
// It carries the same figures as a report's own calibration, minus the
// tickets: naming the divergences of six sprints at once would be a page
// of tickets nobody asked about, and the sprint report already names its
// own.
type CalibrationSprint struct {
	Number int    `json:"number"`
	Name   string `json:"name"`

	Calibration Calibration `json:"calibration"`
}

// CalibrationTrend is the run of sprints, oldest first.
//
// The trend is where the value is: one sprint's variance is a number, and
// five in a row is the only thing that shows whether refinement is
// settling. It is assembled from built reports rather than from stored
// aggregates, because which sprint a ticket counts in is decided by the
// dating rules in Build - a figure frozen at the time of the sweep would
// keep reporting the answer an older rule gave.
type CalibrationTrend struct {
	Sprints []CalibrationSprint `json:"sprints"`

	// Building says some of the run has not been swept yet, so the answer
	// is partial and worth asking for again. The same contract the report
	// itself has: show what there is rather than block on several Jira
	// sweeps at once.
	Building bool `json:"building"`
}
