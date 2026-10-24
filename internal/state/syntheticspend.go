package state

import (
	"fmt"
	"strconv"
	"strings"
)

// A dispatch outside the task loop (engine.OneShot) writes its spend row
// under a synthetic task id: its cost counts in every total, but it is not a
// task, nor a task's attempt. Both writers take their id from here, and
// IsSyntheticSpend is the one rule that tells such a row from a task's.
const (
	summarySpendPrefix = "summary-" // `orch report summary --ai` (#461)
	reviewSpendInfix   = "-verify-" // the local verify's review (#470)
)

// SummarySpendID names the spend of the AI summary of run runID.
func SummarySpendID(runID string) string { return summarySpendPrefix + runID }

// ReviewSpendID names the spend of the local verify's review of a task's
// attempt. Under ORCH_FAKE_PROVIDER it is also the reviewer's canned
// response name.
func ReviewSpendID(taskID string, attempt int) string {
	return fmt.Sprintf("%s%s%d", taskID, reviewSpendInfix, attempt)
}

// IsSyntheticSpend reports whether a spend row's task id is one of the above.
func IsSyntheticSpend(taskID string) bool {
	if strings.HasPrefix(taskID, summarySpendPrefix) {
		return true
	}
	i := strings.LastIndex(taskID, reviewSpendInfix)
	if i <= 0 {
		return false
	}
	_, err := strconv.Atoi(taskID[i+len(reviewSpendInfix):])
	return err == nil
}
