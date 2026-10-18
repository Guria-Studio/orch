package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/engine"
	"github.com/hectorcanaimero/orch/internal/state"
	"github.com/hectorcanaimero/orch/internal/telemetry"
)

// onboardingMarker is <root>/.orchestrator/state/onboarding.json (#378): the
// template `orch init` scaffolded, and which onboarding milestones telemetry
// has delivered, so each goes out once per project. It sits at the state
// root, not in StateDir: init writes it before any database decides between
// the legacy and the namespaced layout, and every later command, however it
// spells the root, has to find the same file.
type onboardingMarker struct {
	Template string   `json:"template,omitempty"`
	Sent     []string `json:"sent,omitempty"`
}

func onboardingMarkerPath(paths config.Paths) string {
	return filepath.Join(paths.Root, ".orchestrator", "state", "onboarding.json")
}

func readOnboardingMarker(paths config.Paths) onboardingMarker {
	var m onboardingMarker
	if b, err := os.ReadFile(onboardingMarkerPath(paths)); err == nil { // #nosec G304 -- the project's own state dir
		_ = json.Unmarshal(b, &m) // a damaged marker reads as empty: at worst a milestone is sent twice
	}
	return m
}

func writeOnboardingMarker(paths config.Paths, m onboardingMarker) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(onboardingMarkerPath(paths)), 0o750); err != nil {
		return err
	}
	return os.WriteFile(onboardingMarkerPath(paths), append(b, '\n'), 0o600)
}

// recordInitTemplate notes what `orch init` scaffolded at root. Best-effort:
// the project is already written, and a missing marker only means the
// template is unknown.
func recordInitTemplate(root, template string) {
	if template == "" {
		template = "blank"
	}
	paths, err := config.ResolvePaths(root, "", "")
	if err != nil {
		return
	}
	m := readOnboardingMarker(paths)
	m.Template = template
	_ = writeOnboardingMarker(paths, m)
}

// readOnboarding reads a project's progress: its marker, and the database's
// first dispatch and first success events. A database that does not exist
// yet is not created here — it just means nothing has run.
func readOnboarding(ctx context.Context, paths config.Paths, cfg config.Config) (doctor.Onboarding, error) {
	o := doctor.Onboarding{Template: readOnboardingMarker(paths).Template}
	dbPath := paths.SQLitePath(cfg)
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		return o, err
	}
	defer func() { _ = closeDB() }()
	db, ok := backend.(*state.SQLite)
	if !ok {
		return o, nil
	}
	if o.DispatchBackend, o.Dispatched, err = db.FirstEventBackend(ctx, engine.EventDispatch); err != nil {
		return o, err
	}
	_, o.FinishedTask, err = db.FirstEventBackend(ctx, engine.EventSuccess)
	return o, err
}

// reportOnboarding sends each milestone the project has reached and
// telemetry has not delivered yet, and marks the ones the collector took.
func reportOnboarding(ctx context.Context, paths config.Paths, cfg config.Config, r telemetry.Reporter) {
	m := readOnboardingMarker(paths)
	if len(m.Sent) == 3 {
		return
	}
	o, err := readOnboarding(ctx, paths, cfg)
	if err != nil {
		return
	}
	template := o.Template
	if template == "" {
		template = "unknown"
	}
	reached := []struct {
		event, backend, template string
		done                     bool
	}{
		{telemetry.MilestoneProjectInit, "", template, true},
		{telemetry.MilestoneFirstDispatch, o.DispatchBackend, "", o.Dispatched},
		{telemetry.MilestoneFirstTaskDone, "", "", o.FinishedTask},
	}
	changed := false
	for _, s := range reached {
		if !s.done || slices.Contains(m.Sent, s.event) {
			continue
		}
		if !r.ReportMilestone(s.event, s.backend, s.template) {
			break // the collector is down or off: the rest waits for the next command
		}
		m.Sent = append(m.Sent, s.event)
		changed = true
	}
	if changed {
		_ = writeOnboardingMarker(paths, m)
	}
}
