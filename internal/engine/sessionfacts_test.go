package engine

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/providers"
)

// codexFixtureHome is a CODEX_HOME whose sessions/ holds trimmed real
// rollouts, keyed by the thread ids of the exec logs beside them.
func codexFixtureHome(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "providers", "testdata", "codex", "0.154.0"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func codexDispatch(env ...string) Dispatch {
	return Dispatch{
		Req: providers.Request{TaskID: "B-020", Route: model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-6-astra"}},
		Env: env,
	}
}

// After a codex run the engine reads the session rollout its thread id
// names and keeps the plan's windows on the result, where outcomeEvent
// turns them into extra.quota (#577).
func TestReadSessionFactsTakesCodexQuotaFromTheRollout(t *testing.T) {
	d := codexDispatch("HOME=/nonexistent", "CODEX_HOME="+codexFixtureHome(t))
	res := providers.Result{SessionID: "01a16611-5f35-7b43-a5a8-0c137c0da92a"}
	if err := readSessionFacts(d, &res); err != nil {
		t.Fatalf("readSessionFacts: %v", err)
	}
	want := []providers.QuotaWindow{
		{Window: "five_hour", Utilization: 0.20, ResetsAt: 1792722104},
		{Window: "seven_day", Utilization: 0.03, ResetsAt: 1793308904},
	}
	if !reflect.DeepEqual(res.Quota, want) {
		t.Errorf("Quota = %+v, want %+v", res.Quota, want)
	}
	ev := outcomeEvent("run-1", d, Outcome{Result: res}, "2026-10-23T00:03:02Z", 1)
	if got, ok := ev.Extra["quota"].([]providers.QuotaWindow); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("outcome extra.quota = %#v", ev.Extra["quota"])
	}
}

// Nothing to read is not a failure: no thread id, no rollout for it, no
// sessions directory, or a backend that keeps none. The result is left as
// the parser made it.
func TestReadSessionFactsMissingRolloutIsNotAFailure(t *testing.T) {
	home := codexFixtureHome(t)
	cases := []struct {
		name string
		d    Dispatch
		id   string
	}{
		{"no thread id", codexDispatch("CODEX_HOME=" + home), ""},
		{"no rollout for the thread", codexDispatch("CODEX_HOME=" + home), "01a00000-0000-7000-8000-000000000000"},
		{"no sessions dir", codexDispatch("CODEX_HOME=" + t.TempDir()), "01a16611-5f35-7b43-a5a8-0c137c0da92a"},
		{"not codex", Dispatch{Req: providers.Request{Route: model.RouteEntry{Backend: model.BackendClaude}}, Env: []string{"CODEX_HOME=" + home}}, "01a16611-5f35-7b43-a5a8-0c137c0da92a"},
	}
	for _, c := range cases {
		res := providers.Result{SessionID: c.id}
		if err := readSessionFacts(c.d, &res); err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
		}
		if res.Quota != nil {
			t.Errorf("%s: Quota = %+v, want nil", c.name, res.Quota)
		}
	}
}

// The dispatch's own environment decides where codex kept the session; the
// last assignment of a variable wins, as it does for the child.
func TestDispatchEnvLookup(t *testing.T) {
	get := dispatchGetenv([]string{"A=1", "B=2", "A=3", "C"})
	if get("A") != "3" || get("B") != "2" || get("C") != "" {
		t.Errorf("A=%q B=%q C=%q", get("A"), get("B"), get("C"))
	}
}

// The rollout's turn_context names the model codex resolved and the effort
// it ran at (#580); a rollout that cannot be found leaves both unknown rather
// than echoing the route.
func TestReadSessionFactsTakesTheEffectiveModel(t *testing.T) {
	d := codexDispatch("CODEX_HOME=" + codexFixtureHome(t))
	res := providers.Result{SessionID: "01a16611-5f35-7b43-a5a8-0c137c0da92a"}
	if err := readSessionFacts(d, &res); err != nil {
		t.Fatal(err)
	}
	if res.EffectiveModel != "gpt-6-astra" || res.EffectiveEffort != "high" {
		t.Errorf("effective = %q / %q, want gpt-6-astra / high", res.EffectiveModel, res.EffectiveEffort)
	}
	missing := providers.Result{SessionID: "01a00000-0000-7000-8000-000000000000"}
	if err := readSessionFacts(d, &missing); err != nil || missing.EffectiveModel != "" || missing.EffectiveEffort != "" {
		t.Errorf("no rollout: %q / %q, %v; want unknown", missing.EffectiveModel, missing.EffectiveEffort, err)
	}
}

// What differs between the route and what ran, field by field; a side that
// is unknown or unset never makes a difference.
func TestRouteMismatch(t *testing.T) {
	route := model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-6-luna", Effort: strp("high"), ServiceTier: strp("flex")}
	cases := []struct {
		name string
		res  providers.Result
		want []string
	}{
		{"nothing known", providers.Result{}, nil},
		{"all as asked", providers.Result{EffectiveModel: "gpt-6-luna", EffectiveEffort: "high", EffectiveServiceTier: "flex"}, nil},
		{"model aliased", providers.Result{EffectiveModel: "gpt-6-astra"}, []string{"model"}},
		{"tier dropped", providers.Result{EffectiveModel: "gpt-6-luna", EffectiveServiceTier: "default"}, []string{"service_tier"}},
		{"everything", providers.Result{EffectiveModel: "x", EffectiveEffort: "low", EffectiveServiceTier: "default"}, []string{"model", "effort", "service_tier"}},
	}
	for _, c := range cases {
		if got := routeMismatch(route, c.res); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A route that set no effort or tier cannot be contradicted on them.
	bare := model.RouteEntry{Backend: model.BackendCodex, CLIModel: "gpt-6-astra"}
	if got := routeMismatch(bare, providers.Result{EffectiveModel: "gpt-6-astra", EffectiveEffort: "xhigh"}); got != nil {
		t.Errorf("bare route: %v, want none", got)
	}
}

// RecordFinish keeps declared and effective side by side on the attempt and
// the outcome event, and writes one model_mismatch event when they differ —
// the tier codex dropped, here (#580).
func TestRecordFinishRecordsTheEffectiveRoute(t *testing.T) {
	ctx := context.Background()
	b := openBackend(t)
	d := codexDispatch()
	d.Req.Route.Effort, d.Req.Route.ServiceTier = strp("high"), strp("flex")
	start := time.Date(2026, 10, 23, 0, 2, 56, 0, time.UTC)
	out := Outcome{StartedAt: start, Duration: 5 * time.Second, Result: providers.Result{
		Success: true, EffectiveModel: "gpt-6-astra", EffectiveEffort: "high", EffectiveServiceTier: "default"}}
	if err := RecordFinish(ctx, b, "run-1", d, out, 1); err != nil {
		t.Fatal(err)
	}
	as, err := b.Attempts(ctx)
	if err != nil || len(as) != 1 {
		t.Fatalf("attempts = %+v, %v", as, err)
	}
	a := as[0]
	if a.Model != "gpt-6-astra" || a.Effort != "high" || a.ServiceTier != "flex" ||
		a.EffectiveModel != "gpt-6-astra" || a.EffectiveEffort != "high" || a.EffectiveServiceTier != "default" {
		t.Errorf("attempt = %+v", a)
	}
	evs, err := b.Events(ctx, "B-020", 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.EventType)
	}
	if !reflect.DeepEqual(types, []string{EventSuccess, EventModelMismatch}) {
		t.Fatalf("events = %v, want success then one model_mismatch", types)
	}
	if got := evs[0].Extra["effective_model"]; got != "gpt-6-astra" {
		t.Errorf("success extra.effective_model = %v", got)
	}
	mm := evs[1].Extra
	if mm["fields"] == nil || mm["detail"] != "service tier flex asked, default ran" || mm["attempt"] != float64(1) {
		t.Errorf("model_mismatch extra = %v", mm)
	}

	// The same run on a route that asked for no tier: nothing to flag.
	b2 := openBackend(t)
	if err := RecordFinish(ctx, b2, "run-1", codexDispatch(), out, 1); err != nil {
		t.Fatal(err)
	}
	if evs, _ := b2.Events(ctx, "B-020", 0); len(evs) != 1 {
		t.Errorf("events = %+v, want only the outcome", evs)
	}
}
