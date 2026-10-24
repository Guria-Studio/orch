package state

import "testing"

func TestIsSyntheticSpend(t *testing.T) {
	for id, want := range map[string]bool{
		SummarySpendID("run-1"):   true,
		ReviewSpendID("F0.T1", 2): true,
		"F0.T1":                   false,
		"T-verify-cache":          false,
		"-verify-1":               false,
	} {
		if got := IsSyntheticSpend(id); got != want {
			t.Errorf("IsSyntheticSpend(%q) = %v, want %v", id, got, want)
		}
	}
}
