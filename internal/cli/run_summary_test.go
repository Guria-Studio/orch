package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/demo"
	"github.com/hectorcanaimero/orch/internal/notify"
	"github.com/hectorcanaimero/orch/internal/state"
)

// When a run's loop ends, orch run prints its summary (#456); a run that did
// not finish has none to print.
func TestRunPrintsItsSummary(t *testing.T) {
	ctx := context.Background()
	paths, err := demo.Build(ctx, filepath.Join(t.TempDir(), "demo"), time.Now())
	if err != nil {
		t.Fatalf("demo.Build: %v", err)
	}
	loaded, err := config.Load(paths.ConfigYAML, paths.Root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	db, _, err := state.Open(ctx, paths.SQLitePath(loaded.Config))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := state.NewSQLite(db, paths.ID, paths.Root)
	cfg := loaded.Config
	cfg.Dashboard.SummaryLanguage = "en"

	var out bytes.Buffer
	reportRunSummary(ctx, &out, backend, paths, cfg, &notify.Notifier{}, demo.HistoryRunID)
	if !strings.Contains(out.String(), "Run "+demo.HistoryRunID+" finished: 16 done") {
		t.Errorf("finished run printed:\n%s", out.String())
	}
	out.Reset()
	reportRunSummary(ctx, &out, backend, paths, cfg, &notify.Notifier{}, demo.RunID)
	if out.Len() != 0 {
		t.Errorf("an unfinished run printed a summary:\n%s", out.String())
	}
}
