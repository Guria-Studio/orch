package pricing

// Where a summed cost's dollars come from. The dashboard's budget summary
// (`cost_source`) and `orch status` (`cost_source`) both use these words.
const (
	// SourceNone: no spend row at all — nothing ran.
	SourceNone = "none"
	// SourceReported: at least one row carries a cost the CLI billed.
	SourceReported = "reported"
	// SourceEstimated: nothing was billed, but the rows' tokens were priced
	// from the table.
	SourceEstimated = "estimated"
	// SourceNoData: rows exist with neither a billed cost nor tokens to
	// price — unknown, not free.
	SourceNoData = "no_data"
)

// Sum adds spend rows up under ResolveCost's rule, keeping the two kinds of
// dollar apart: Reported is what the CLIs billed, Estimated what the table
// priced for the rows that billed nothing. A surface that shows one number
// shows Total and says when Estimated is part of it; one that sits next to a
// real bill (a budget) shows Reported and Estimated side by side.
type Sum struct {
	Rows      int
	Reported  float64
	Estimated float64
}

// Add counts one spend row: its recorded cost when above zero, else its
// tokens priced from t (ResolveCost; a row with no model is priced as
// `default`, and one with no tokens adds nothing).
func (s *Sum) Add(t Table, recordedCost float64, model string, tokens Tokens) {
	s.Rows++
	if recordedCost > 0 {
		s.Reported += recordedCost
		return
	}
	s.Estimated += t.ResolveCost(recordedCost, model, tokens)
}

// Total is billed plus estimated: what the dashboard's Usage page shows.
func (s Sum) Total() float64 { return s.Reported + s.Estimated }

// Source says what the dollars are; see the Source* constants. A sum with
// both kinds is "reported": part of it is a bill, and Estimated says how much
// is not.
func (s Sum) Source() string {
	switch {
	case s.Rows == 0:
		return SourceNone
	case s.Reported > 0:
		return SourceReported
	case s.Estimated > 0:
		return SourceEstimated
	default:
		return SourceNoData
	}
}
