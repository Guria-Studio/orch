package providers

import (
	"reflect"
	"testing"
)

// codexRollout reads one of the trimmed real rollouts under
// testdata/codex/0.154.0/sessions, named by the thread id its exec log
// carries.
func codexRollout(t *testing.T, day, name string) []byte {
	t.Helper()
	return readFixture(t, "codex", "0.154.0", "sessions", day, name)
}

const (
	reasoningRollout = "rollout-2026-10-22T21-02-56-01a16611-5f35-7b43-a5a8-0c137c0da92a.jsonl"
	tierRollout      = "rollout-2026-10-22T20-58-49-01a1660d-9856-78b2-b0b9-9ed7e79874e7.jsonl"
	successRollout   = "rollout-2026-09-12T18-07-19-01a096cd-6b96-79c2-a96c-4e05ebc6cb89.jsonl"
)

// codex exec --json names its session in thread.started and nowhere else;
// it is the key to the rollout file (#577, #580).
func TestCodexParseKeepsTheThreadID(t *testing.T) {
	cases := []struct{ file, want string }{
		{"success.jsonl", "01a096cd-6b96-79c2-a96c-4e05ebc6cb89"},
		{"reasoning.jsonl", "01a16611-5f35-7b43-a5a8-0c137c0da92a"},
		{"tier-not-advertised.jsonl", "01a1660d-9856-78b2-b0b9-9ed7e79874e7"},
		// Failed runs have a session too: the quota they burned is real.
		{"auth-error.jsonl", "01a096ab-2ec3-7571-9b47-086ce5dcdaff"},
	}
	for _, c := range cases {
		got := CodexProvider{}.Parse(0, readFixture(t, "codex", "0.154.0", c.file)).SessionID
		if got != c.want {
			t.Errorf("%s: SessionID = %q, want %q", c.file, got, c.want)
		}
	}
	if got := (CodexProvider{}).Parse(1, nil).SessionID; got != "" {
		t.Errorf("empty log: SessionID = %q, want empty", got)
	}
}

// The rollout's last token_count carries the plan's real windows: primary is
// the 5-hour one and secondary the weekly, both as used_percent (0-100).
// They map to the names claude's windows already use, so the dashboard's
// Quota page labels them the same way.
func TestCodexRolloutReadsTheRateLimits(t *testing.T) {
	got := ParseCodexRollout(codexRollout(t, "2026/10/22", reasoningRollout)).Quota
	want := []QuotaWindow{
		{Window: "five_hour", Utilization: 0.20, ResetsAt: 1792722104},
		{Window: "seven_day", Utilization: 0.03, ResetsAt: 1793308904},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Quota = %+v, want %+v", got, want)
	}
	// A different capture, different numbers: the reader is not echoing a
	// constant.
	got = ParseCodexRollout(codexRollout(t, "2026/10/22", tierRollout)).Quota
	want = []QuotaWindow{
		{Window: "five_hour", Utilization: 0.18, ResetsAt: 1792722104},
		{Window: "seven_day", Utilization: 0.03, ResetsAt: 1793308904},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tier rollout Quota = %+v, want %+v", got, want)
	}
}

// The newest token_count wins; windows codex leaves null are skipped; a
// window length orch has no name for keeps its minutes; and a rollout with
// no rate_limits at all reports nil, not zero-filled windows.
func TestCodexRolloutQuotaShapes(t *testing.T) {
	two := []byte(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":5.0,"window_minutes":300,"resets_at":100}}}}
{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":7.5,"window_minutes":300,"resets_at":200},"secondary":null}}}
`)
	if got, want := ParseCodexRollout(two).Quota, []QuotaWindow{{Window: "five_hour", Utilization: 0.075, ResetsAt: 200}}; !reflect.DeepEqual(got, want) {
		t.Errorf("newest wins: %+v, want %+v", got, want)
	}
	odd := []byte(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":50,"window_minutes":60,"resets_at":1}}}}`)
	if got, want := ParseCodexRollout(odd).Quota, []QuotaWindow{{Window: "60m", Utilization: 0.5, ResetsAt: 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("unnamed window: %+v, want %+v", got, want)
	}
	none := []byte(`{"type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":null}}
not json at all
`)
	if got := ParseCodexRollout(none).Quota; got != nil {
		t.Errorf("no rate_limits: Quota = %+v, want nil", got)
	}
}

// A thread id is spliced into a glob; anything but a plain id is refused so
// a hostile log line cannot widen the search.
func TestCodexRolloutPatternRefusesOddIDs(t *testing.T) {
	if got := CodexRolloutPattern("/s", "01a096cd-6b96-79c2-a96c-4e05ebc6cb89"); got != "/s/*/*/*/rollout-*-01a096cd-6b96-79c2-a96c-4e05ebc6cb89.jsonl" {
		t.Errorf("pattern = %q", got)
	}
	for _, id := range []string{"", "*", "../x", "a/b", "a?b", "a[b]"} {
		if got := CodexRolloutPattern("/s", id); got != "" {
			t.Errorf("id %q: pattern = %q, want empty", id, got)
		}
	}
}

// CODEX_HOME moves codex's whole state directory; without it, ~/.codex.
func TestCodexSessionsDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := CodexSessionsDir(env(map[string]string{"CODEX_HOME": "/c", "HOME": "/h"})); got != "/c/sessions" {
		t.Errorf("CODEX_HOME: %q", got)
	}
	if got := CodexSessionsDir(env(map[string]string{"HOME": "/h"})); got != "/h/.codex/sessions" {
		t.Errorf("HOME: %q", got)
	}
	if got := CodexSessionsDir(env(map[string]string{})); got != "" {
		t.Errorf("neither: %q, want empty", got)
	}
}
