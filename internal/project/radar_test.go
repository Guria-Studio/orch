package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

func rev(typ, task, ts string, extra map[string]any) state.Event {
	return state.Event{EventType: typ, TaskID: task, TS: ts, Extra: extra}
}

func files(f ...string) []any {
	out := make([]any, len(f))
	for i, s := range f {
		out[i] = s
	}
	return out
}

// radarFixture is the spec's acceptance case (#522 §6.0): two warnings and
// three collisions, one of which the first warning predicted.
func radarFixture() []state.Event {
	return []state.Event{
		rev("files_overlap", "A", "2026-11-01T10:00:00Z", map[string]any{"with": "B", "files": files("src/a.go"), "mode": "warn"}),
		rev("branch_conflict", "C", "2026-11-01T11:00:00Z", map[string]any{"with": "D", "files": files("web/x.ts")}),
		rev("ci_conflict_retry", "A", "2026-11-01T12:00:00Z", map[string]any{"agent": true, "attempt": 1.0, "files": files("src/a.go")}),
		rev("ci_conflict_retry", "E", "2026-11-01T13:00:00Z", map[string]any{"agent": true, "attempt": 1.0, "files": files("lib/z.go")}),
		rev("ci_blocked", "F", "2026-11-01T14:00:00Z", map[string]any{
			"reason": "the PR conflicts with its base branch, so CI will not run (conflicts in docs/y.md)"}),
	}
}

func TestRadarEffectivenessSpecFixture(t *testing.T) {
	rep := RadarEffectiveness(radarFixture(), time.Time{})
	if rep.Recall == nil || *rep.Recall != 1.0/3 {
		t.Errorf("recall = %v, want 1/3", rep.Recall)
	}
	if rep.Precision == nil || *rep.Precision != 0.5 {
		t.Errorf("precision = %v, want 1/2", rep.Precision)
	}
	wantP := RadarPredictions{FilesOverlap: 1, BranchConflict: 1, Pairs: 2, PairsCollided: 1}
	if rep.Predictions != wantP {
		t.Errorf("predictions = %+v, want %+v", rep.Predictions, wantP)
	}
	wantC := RadarCollisions{Total: 3, Predicted: 1, Escaped: 2,
		ByKind: map[string]int{RadarPRConflict: 2, RadarPRConflictBlocked: 1}}
	if !reflect.DeepEqual(rep.Collisions, wantC) {
		t.Errorf("collisions = %+v, want %+v", rep.Collisions, wantC)
	}
	if len(rep.Events) != 3 {
		t.Fatalf("events = %+v, want 3", rep.Events)
	}
	if e := rep.Events[0]; !e.Predicted || e.Match != "files" || !reflect.DeepEqual(e.PredictedBy, []string{"A ~ B"}) {
		t.Errorf("A's collision = %+v, want predicted by A ~ B on a file", e)
	}
	if e := rep.Events[2]; e.Predicted || !reflect.DeepEqual(e.Files, []string{"docs/y.md"}) {
		t.Errorf("F's block = %+v, want an escape on docs/y.md", e)
	}
	if !rep.Pairs[0].Collided || rep.Pairs[1].Collided {
		t.Errorf("pairs = %+v, want A~B collided and C~D not", rep.Pairs)
	}
}

// TestRadarEffectivenessMatching: one case per rule of what counts as a
// prediction and as a collision.
func TestRadarEffectivenessMatching(t *testing.T) {
	warn := func(task, other string, f ...string) state.Event {
		return rev("branch_conflict", task, "2026-11-01T10:00:00Z", map[string]any{"with": other, "files": files(f...)})
	}
	at := "2026-11-01T12:00:00Z"
	tests := []struct {
		name      string
		events    []state.Event
		total     int
		predicted bool
		match     string
	}{
		{"the other side of the pair collides", []state.Event{warn("A", "B", "x.go"),
			rev("ci_conflict_retry", "B", at, map[string]any{"agent": true, "files": files("x.go")})}, 1, true, "files"},
		{"caused_by names the warned task, no file in common", []state.Event{warn("A", "B", "x.go"),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("y.go"), "caused_by": files("B")})}, 1, true, "with"},
		{"caused_by names another task and no file in common", []state.Event{warn("A", "B", "x.go"),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("y.go"), "caused_by": files("C")})}, 1, false, ""},
		{"a warned directory covers the file", []state.Event{
			rev("files_overlap", "A", "2026-11-01T10:00:00Z", map[string]any{"with": "B", "files": files("web/")}),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("web/src/App.tsx")})}, 1, true, "files"},
		{"a warned glob covers the file", []state.Event{
			rev("files_overlap", "A", "2026-11-01T10:00:00Z", map[string]any{"with": "B", "files": files("src/**/*.go")}),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("src/x/y.go")})}, 1, true, "files"},
		{"a warning after the collision predicts nothing", []state.Event{
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("x.go")}),
			rev("branch_conflict", "A", "2026-11-01T13:00:00Z", map[string]any{"with": "B", "files": files("x.go")})}, 1, false, ""},
		{"a warning on other tasks predicts nothing", []state.Event{warn("C", "D", "x.go"),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "files": files("x.go")})}, 1, false, ""},
		{"a clean merge orch pushed is no collision", []state.Event{
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": false, "reason": "merged main cleanly and pushed"})}, 0, false, ""},
		{"a verify broken by the base counts any warning on the task", []state.Event{warn("A", "B", "x.go"),
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "verify_failed": true})}, 1, true, "task"},
		{"an old conflict block without files counts any warning on the task", []state.Event{warn("A", "B", "x.go"),
			rev("ci_blocked", "A", at, map[string]any{"reason": "the PR conflicts with its base branch, so CI will not run"})}, 1, true, "task"},
		{"a CI block for another reason is no collision", []state.Event{
			rev("ci_blocked", "A", at, map[string]any{"reason": "CI failed after 2 attempts"})}, 0, false, ""},
		{"a local land conflict that blocked", []state.Event{warn("A", "B", "db/schema.sql"),
			rev("fail", "A", at, map[string]any{"reason": "the work conflicts with main in db/schema.sql, go.mod (kept on orch/A)"})}, 1, true, "files"},
		{"a local land conflict sent to the agent", []state.Event{
			rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "local": true, "files": files("x.go")})}, 1, false, ""},
		{"a blocked semantic collision with the warned branch", []state.Event{warn("A", "B", "x.go"),
			rev("semantic_collision", "A", at, map[string]any{"blocked": true, "with": "B", "file": "db/005_a.sql"})}, 1, true, "with"},
		{"a semantic warning is no collision", []state.Event{
			rev("semantic_collision", "A", at, map[string]any{"blocked": false, "with": "base", "file": "db/005_a.sql"})}, 0, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := RadarEffectiveness(tt.events, time.Time{})
			if rep.Collisions.Total != tt.total {
				t.Fatalf("collisions = %d, want %d (%+v)", rep.Collisions.Total, tt.total, rep.Events)
			}
			if tt.total == 0 {
				return
			}
			if e := rep.Events[0]; e.Predicted != tt.predicted || e.Match != tt.match {
				t.Errorf("predicted = %v by %q, want %v by %q (%+v)", e.Predicted, e.Match, tt.predicted, tt.match, e)
			}
		})
	}
}

func TestRadarEffectivenessKinds(t *testing.T) {
	at := "2026-11-01T12:00:00Z"
	rep := RadarEffectiveness([]state.Event{
		rev("ci_conflict_retry", "A", at, map[string]any{"agent": true, "local": true, "files": files("x.go")}),
		rev("ci_conflict_retry", "B", at, map[string]any{"agent": true, "verify_failed": true}),
		rev("fail", "C", at, map[string]any{"reason": "the work conflicts with main in a.go, b.go (kept on orch/C)"}),
		rev("semantic_collision", "D", at, map[string]any{"blocked": true, "with": "base", "file": "db/005_d.sql"}),
		rev("fail", "E", at, map[string]any{"reason": "the agent exited non-zero"}),
	}, time.Time{})
	want := map[string]int{RadarLandConflict: 2, RadarBaseBroke: 1, RadarSemantic: 1}
	if !reflect.DeepEqual(rep.Collisions.ByKind, want) {
		t.Errorf("by kind = %v, want %v", rep.Collisions.ByKind, want)
	}
	if got := rep.Events[2].Files; !reflect.DeepEqual(got, []string{"a.go", "b.go"}) {
		t.Errorf("land conflict files = %v, want a.go, b.go", got)
	}
	if got := rep.Events[3].With; len(got) != 0 {
		t.Errorf("a collision with the base names tasks %v, want none", got)
	}
}

// TestRadarEffectivenessWindow: --since drops what came before, and an empty
// window has no rates rather than 0%.
func TestRadarEffectivenessWindow(t *testing.T) {
	from := time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC)
	rep := RadarEffectiveness(radarFixture(), from)
	if rep.From != "2026-11-01T11:30:00Z" {
		t.Errorf("from = %q", rep.From)
	}
	if rep.Predictions.Pairs != 0 || rep.Precision != nil {
		t.Errorf("predictions = %+v precision = %v, want none in the window", rep.Predictions, rep.Precision)
	}
	if rep.Collisions.Total != 3 || rep.Collisions.Predicted != 0 || rep.Recall == nil || *rep.Recall != 0 {
		t.Errorf("collisions = %+v recall = %v, want 3 escapes", rep.Collisions, rep.Recall)
	}
	empty := RadarEffectiveness(nil, time.Time{})
	if empty.Recall != nil || empty.Precision != nil || empty.Events == nil || empty.Pairs == nil {
		t.Errorf("empty report = %+v, want null rates and empty lists", empty)
	}
}

// TestRadarEffectivenessOnRealEvents runs the report over the events orch's
// dogfood project recorded (testdata/radar/README.md): every collision there
// had a warning first, and 4 of the 10 warned pairs collided.
func TestRadarEffectivenessOnRealEvents(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "radar", "dogfood-events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		EventType string         `json:"event_type"`
		TaskID    string         `json:"task_id"`
		TS        string         `json:"ts"`
		Extra     map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	events := make([]state.Event, len(rows))
	for i, r := range rows {
		events[i] = rev(r.EventType, r.TaskID, r.TS, r.Extra)
	}
	rep := RadarEffectiveness(events, time.Time{})
	wantP := RadarPredictions{FilesOverlap: 11, BranchConflict: 6, Pairs: 10, PairsCollided: 4}
	if rep.Predictions != wantP {
		t.Errorf("predictions = %+v, want %+v", rep.Predictions, wantP)
	}
	wantC := RadarCollisions{Total: 3, Predicted: 3, Escaped: 0,
		ByKind: map[string]int{RadarPRConflict: 2, RadarPRConflictBlocked: 1}}
	if !reflect.DeepEqual(rep.Collisions, wantC) {
		t.Errorf("collisions = %+v, want %+v (CI failures are not collisions)", rep.Collisions, wantC)
	}
	if got := rep.Events[1].PredictedBy; !reflect.DeepEqual(got, []string{"F12.2.T1 ~ F13.1.T1", "F12.2.T1 ~ F13.1.T2"}) {
		t.Errorf("F12.2.T1's conflict predicted by %v, want both F13.1 tasks", got)
	}
}
