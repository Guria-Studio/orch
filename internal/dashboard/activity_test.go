package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hectorcanaimero/orch/internal/providers"
)

// writeLog puts a dispatch log where engine.LogPathFor writes it for the
// legacy layout newTestServer uses.
func writeLog(t *testing.T, root, taskID string, content []byte) string {
	t.Helper()
	dir := filepath.Join(root, ".orchestrator", "state", "logs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, taskID+".log")
	if err := os.WriteFile(p, content, 0o600); err != nil { // #nosec G703 -- under t.TempDir()
		t.Fatal(err)
	}
	return p
}

func activity(t *testing.T, s *Server, path string) activityPayload {
	t.Helper()
	var got activityPayload
	decode(t, get(t, s, path), &got)
	return got
}

// A running claude task, followed the way the page does: a first read from
// the tail, then reads from the offset it was handed.
func TestActivityFollowsTheLog(t *testing.T) {
	root := t.TempDir()
	s := newTestServer(t, cfg(ProfileOperator, ""), root)

	if got := activity(t, s, "/api/task/T-1/activity"); got.Exists || len(got.Steps) != 0 {
		t.Fatalf("before any dispatch = %+v, want no log and no steps", got)
	}

	capture, err := os.ReadFile(filepath.Join("..", "providers", "testdata", "claude", "2.1.301", "stream-success.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	logPath := writeLog(t, root, "T-1", capture)

	first := activity(t, s, "/api/task/T-1/activity")
	if !first.Exists || first.Offset != int64(len(capture)) {
		t.Fatalf("first read: exists=%v offset=%d, want the whole %d-byte log", first.Exists, first.Offset, len(capture))
	}
	kinds := ""
	for _, st := range first.Steps {
		kinds += st.Kind + " "
	}
	if kinds != "start tool tool text end " {
		t.Errorf("steps = %q, want start tool tool text end", kinds)
	}

	// The agent is mid-line: nothing new until the line is complete.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(`{"type":"system","subtype":"init"}` + "\n" + `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a.go"}}]}`)
	next := activity(t, s, "/api/task/T-1/activity?offset="+strconv.FormatInt(first.Offset, 10))
	if len(next.Steps) != 1 || next.Steps[0].Kind != providers.StepStart {
		t.Fatalf("partial line = %+v, want only the complete init line", next.Steps)
	}
	_, _ = f.WriteString("}\n")
	last := activity(t, s, "/api/task/T-1/activity?offset="+strconv.FormatInt(next.Offset, 10))
	if len(last.Steps) != 1 || last.Steps[0] != (providers.Step{Kind: providers.StepTool, Tool: "Edit", Detail: "a.go"}) {
		t.Errorf("completed line = %+v, want the Edit", last.Steps)
	}

	// An offset past the end: the log was replaced, start over.
	if got := activity(t, s, "/api/task/T-1/activity?offset=999999999"); got.Offset != last.Offset || len(got.Steps) != 7 {
		t.Errorf("stale offset = offset %d, %d steps; want a full re-read", got.Offset, len(got.Steps))
	}
}

func TestActivityRefusesWhatIsNotATaskLog(t *testing.T) {
	s := newTestServer(t, cfg(ProfileOperator, ""), t.TempDir())
	for _, path := range []string{
		"/api/task/a%2F..%2Fb/activity",
		"/api/task/..%2Forch.db/activity",
		"/api/task/T-1/activity?offset=-4",
		"/api/task/T-1/activity?offset=abc",
	} {
		resp := get(t, s, path)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// The transcript is not for stakeholders, token or not.
func TestActivityIsOperatorOnly(t *testing.T) {
	s := newTestServer(t, cfg(ProfileStakeholder, "test-token-stakeholder"), t.TempDir())
	req, _ := http.NewRequest(http.MethodGet, "/api/task/T-1/activity?token=test-token-stakeholder", nil)
	resp := get(t, s, req.URL.String())
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder with a valid token = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
