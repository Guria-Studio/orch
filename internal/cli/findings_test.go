package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/findings"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

type recordingFiler struct{ titles []string }

func (r *recordingFiler) CreateIssue(_, title, _ string, _ []string) (string, error) {
	r.titles = append(r.titles, title)
	return "https://github.com/Guria-Studio/orch/issues/77", nil
}

// TestFindingsAreReviewedBeforeTheyLeave (#536): an agent's report waits in
// the queue; `orch findings` shows exactly what would be filed, the end of a
// run says how many wait, and only `send` files one — `dismiss` drops it.
func TestFindingsAreReviewedBeforeTheyLeave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	if out, err := runOrch(t, "", "init", dir, "--no-permissions"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	paths, cfg, err := loadProjectConfig(&projectFlags{root: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeDB() }()
	for _, title := range []string{"explain lists backlog as ready", "doctor misses a stale route"} {
		if _, err := backend.QueueFinding(ctx, state.QueuedFinding{Title: title, Body: "## Summary\n" + title, QueuedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	var note bytes.Buffer
	noteQueuedFindings(ctx, &note, project.New(paths, cfg, backend))
	if !strings.Contains(note.String(), "2 bug(s)") || !strings.Contains(note.String(), "orch findings") {
		t.Errorf("end-of-run note = %q", note.String())
	}

	out, err := runOrch(t, "", "findings", "--project-root", dir)
	if err != nil || !strings.Contains(out, "Title: explain lists backlog as ready") || !strings.Contains(out, "## Summary\nexplain") {
		t.Fatalf("findings: %v\n%s", err, out)
	}

	filer := &recordingFiler{}
	prev := newFindingsFiler
	newFindingsFiler = func() findings.Filer { return filer }
	t.Cleanup(func() { newFindingsFiler = prev })

	if out, err := runOrch(t, "", "findings", "send", "1", "--project-root", dir); err != nil || !strings.Contains(out, "#1: sent https://") {
		t.Fatalf("send: %v\n%s", err, out)
	}
	if out, err := runOrch(t, "", "findings", "dismiss", "2", "--project-root", dir); err != nil || !strings.Contains(out, "#2: dismissed") {
		t.Fatalf("dismiss: %v\n%s", err, out)
	}
	if len(filer.titles) != 1 || filer.titles[0] != "explain lists backlog as ready" {
		t.Errorf("filed %v, want only #1", filer.titles)
	}
	if out, _ := runOrch(t, "", "findings", "--project-root", dir); !strings.Contains(out, "No findings waiting") {
		t.Errorf("queue after send and dismiss:\n%s", out)
	}
	note.Reset()
	noteQueuedFindings(ctx, &note, project.New(paths, cfg, backend))
	if note.Len() != 0 {
		t.Errorf("a run with nothing queued still says %q", note.String())
	}
	if _, err := runOrch(t, "", "findings", "send", "--project-root", dir); err == nil {
		t.Error("send with neither ids nor --all was accepted")
	}
}
