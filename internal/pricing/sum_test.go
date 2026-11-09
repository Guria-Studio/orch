package pricing

import "testing"

// Sum is the dashboard's and `orch status`'s shared rule (#578): billed
// dollars stay billed, unbilled tokens are priced apart, and a row with
// neither is unknown rather than free.
func TestSumSplitsBilledFromEstimated(t *testing.T) {
	table := Load("")
	gpt5 := Tokens{In: 100_000, Out: 10_000} // $0.35 at gpt-5's row

	cases := []struct {
		name                string
		rows                func(*Sum)
		reported, estimated float64
		source              string
	}{
		{"nothing ran", func(*Sum) {}, 0, 0, SourceNone},
		{"billed", func(s *Sum) { s.Add(table, 0.42, "claude-sonnet-4-6", Tokens{In: 1000}) }, 0.42, 0, SourceReported},
		{"subscription", func(s *Sum) { s.Add(table, 0, "gpt-5", gpt5) }, 0, 0.35, SourceEstimated},
		{"billed and unbilled", func(s *Sum) {
			s.Add(table, 0.5, "claude-sonnet-4-6", Tokens{In: 1000})
			s.Add(table, 0, "gpt-5", gpt5)
		}, 0.5, 0.35, SourceReported},
		{"neither dollars nor tokens", func(s *Sum) { s.Add(table, 0, "gpt-5", Tokens{}) }, 0, 0, SourceNoData},
		{"no model prices as default", func(s *Sum) { s.Add(table, 0, "", gpt5) }, 0, table.EstimateCost("default", gpt5), SourceEstimated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s Sum
			tc.rows(&s)
			if s.Reported != tc.reported || s.Estimated != tc.estimated || s.Source() != tc.source {
				t.Errorf("got reported=%v estimated=%v source=%q, want %v %v %q",
					s.Reported, s.Estimated, s.Source(), tc.reported, tc.estimated, tc.source)
			}
			if s.Total() != s.Reported+s.Estimated {
				t.Errorf("Total() = %v, want %v", s.Total(), s.Reported+s.Estimated)
			}
		})
	}
}
