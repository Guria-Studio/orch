package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/scaffold"
	"github.com/Guria-Studio/orch/internal/state"
)

// #376: report_findings is offered once, by the first run after which a task
// is done, and only in a project `orch init` left the marker in.
func TestReportFindingsIsOfferedOnceAfterATaskFinishes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, _, err := state.Open(ctx, filepath.Join(root, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, "p", root)
	if err := backend.Bootstrap(ctx, []model.Task{{ID: "A", Title: "a", Status: model.StatusTodo, Model: "claude/haiku"}}); err != nil {
		t.Fatal(err)
	}
	offer := func() string {
		var out bytes.Buffer
		offerReportFindings(ctx, &out, backend, root)
		return out.String()
	}

	if got := offer(); got != "" {
		t.Errorf("offered with no marker: %q", got)
	}
	marker := filepath.Join(root, ".orchestrator", scaffold.FindingsOfferName)
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := offer(); got != "" {
		t.Errorf("offered before any task finished: %q", got)
	}
	if err := backend.Transition(ctx, "A", model.StatusInProgress, state.Note{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Transition(ctx, "A", model.StatusDone, state.Note{}); err != nil {
		t.Fatal(err)
	}
	if got := offer(); !strings.Contains(got, "report_findings.enabled: true") {
		t.Errorf("not offered after a task finished: %q", got)
	}
	if got := offer(); got != "" {
		t.Errorf("offered twice: %q", got)
	}
}
