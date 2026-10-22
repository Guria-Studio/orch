package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/engine"
	"github.com/hectorcanaimero/orch/internal/scaffold"
	"github.com/hectorcanaimero/orch/internal/state"
	"github.com/hectorcanaimero/orch/internal/telemetry"
)

// collector answers every milestone with status, and keeps the events it saw.
type collector struct {
	status int
	events []string
}

func (c *collector) RoundTrip(req *http.Request) (*http.Response, error) {
	var m telemetry.Milestone
	body, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(body, &m)
	c.events = append(c.events, m.Event+"/"+m.Backend+m.Template)
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

func onboardingProject(t *testing.T) (config.Paths, config.Config) {
	t.Helper()
	res, err := scaffold.Run(scaffold.Options{Root: t.TempDir(), Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	recordInitTemplate(res.Root, "")
	paths, err := config.ResolvePaths(res.Root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(paths.ConfigYAML, paths.Root)
	if err != nil {
		t.Fatal(err)
	}
	return paths, loaded.Config
}

func appendEvent(t *testing.T, paths config.Paths, cfg config.Config, eventType, backend string) {
	t.Helper()
	ctx := context.Background()
	b, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeDB() }()
	if err := b.(*state.SQLite).AppendEvent(ctx, "run-1", state.Event{EventType: eventType, TaskID: "T-" + eventType + "-" + backend, Backend: backend}); err != nil {
		t.Fatal(err)
	}
}

// Each milestone goes out once, when the project reaches it; one the
// collector refused goes out on a later command (#378).
func TestOnboardingMilestonesGoOutOnceEach(t *testing.T) {
	ctx := context.Background()
	paths, cfg := onboardingProject(t)
	c := &collector{status: http.StatusOK}
	r := telemetry.Reporter{Enabled: true, Endpoint: "http://example.invalid/collect", Transport: c}

	reportOnboarding(ctx, paths, cfg, r)
	reportOnboarding(ctx, paths, cfg, r)
	if want := []string{"project_init/blank"}; strings.Join(c.events, ",") != strings.Join(want, ",") {
		t.Fatalf("after init: %v, want %v", c.events, want)
	}

	appendEvent(t, paths, cfg, engine.EventDispatch, "codex")
	c.status = http.StatusServiceUnavailable
	reportOnboarding(ctx, paths, cfg, r) // refused: not marked
	c.status = http.StatusOK
	appendEvent(t, paths, cfg, engine.EventSuccess, "codex")
	reportOnboarding(ctx, paths, cfg, r)
	reportOnboarding(ctx, paths, cfg, r)

	want := "project_init/blank,first_dispatch/codex,first_dispatch/codex,first_task_done/"
	if got := strings.Join(c.events, ","); got != want {
		t.Errorf("events = %s, want %s", got, want)
	}
}

func TestOnboardingSendsNothingWithTelemetryOff(t *testing.T) {
	paths, cfg := onboardingProject(t)
	appendEvent(t, paths, cfg, engine.EventDispatch, "claude")
	c := &collector{status: http.StatusOK}
	reportOnboarding(context.Background(), paths, cfg, telemetry.Reporter{Enabled: false, Endpoint: "http://example.invalid", Transport: c})
	if len(c.events) != 0 {
		t.Errorf("sent %v with telemetry off", c.events)
	}
	if m := readOnboardingMarker(paths); len(m.Sent) != 0 {
		t.Errorf("marked %v as sent with telemetry off", m.Sent)
	}
}

// doctor reads the same progress, telemetry or not.
func TestReadOnboardingFollowsTheDatabase(t *testing.T) {
	ctx := context.Background()
	paths, cfg := onboardingProject(t)
	o, err := readOnboarding(ctx, paths, cfg)
	if err != nil || o.Template != "blank" || o.Dispatched || o.FinishedTask {
		t.Fatalf("fresh project: %+v, %v", o, err)
	}
	appendEvent(t, paths, cfg, engine.EventDispatch, "claude")
	appendEvent(t, paths, cfg, engine.EventDispatch, "codex")
	o, err = readOnboarding(ctx, paths, cfg)
	if err != nil || !o.Dispatched || o.DispatchBackend != "claude" || o.FinishedTask {
		t.Fatalf("after dispatch: %+v, %v", o, err)
	}
}

// init names the root explicitly; later commands usually run from inside the
// project. Both must find the same marker whichever state layout the
// database ends up in.
func TestOnboardingMarkerIsFoundFromInsideTheProject(t *testing.T) {
	res, err := scaffold.Run(scaffold.Options{Root: t.TempDir(), Name: "p", Template: "python-api"})
	if err != nil {
		t.Fatal(err)
	}
	recordInitTemplate(res.Root, "python-api")
	t.Chdir(res.Root)
	paths, err := config.ResolvePaths("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := readOnboardingMarker(paths).Template; got != "python-api" {
		t.Errorf("template from inside the project = %q, want python-api", got)
	}
}
