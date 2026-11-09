package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
)

// A deliverable that came from a GitHub issue links it (#525): the client
// opened the issue, so the page points back at it. One without an issue has
// no key at all, so every page published before keeps its shape.
func TestDeliverableLinksItsSourceIssue(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	in := fixtureInput(now)
	in.Phases = nil
	in.Tasks = []model.Task{
		{ID: "F0.12.T1", Phase: 0, Title: "Exportar CSV", Status: model.StatusDone,
			SourceIssue: "https://github.com/acme/shop/issues/12"},
		{ID: "F0.13.T1", Phase: 0, Title: "Otra cosa", Status: model.StatusTodo},
	}

	snap := Build(in)

	var got []Deliverable
	for _, p := range snap.Milestones[0].Packages {
		got = append(got, p.Deliverables...)
	}
	if len(got) != 2 {
		t.Fatalf("deliverables = %+v", got)
	}
	if got[0].IssueURL != "https://github.com/acme/shop/issues/12" || got[1].IssueURL != "" {
		t.Errorf("issue URLs = %q, %q", got[0].IssueURL, got[1].IssueURL)
	}
	raw, _ := json.Marshal(got[1])
	if strings.Contains(string(raw), "issue_url") {
		t.Errorf("deliverable without an issue = %s, want no issue_url key", raw)
	}
}
