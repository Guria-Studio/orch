package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scriptedProbe hands the watch one procStats per call, the last one again
// once they run out: an agent's process group as the system would report it.
func scriptedProbe(stats func(call int) procStats) func(int) (procStats, error) {
	call := 0
	return func(int) (procStats, error) {
		call++
		return stats(call), nil
	}
}

// watchSilently runs ticks watches 30 s apart over a log that never grows
// after the first, and returns the events recorded.
func watchSilently(t *testing.T, f *reapFixture, log string, ticks int) []string {
	t.Helper()
	write(t, log, toolLine("a", "python fetch.py"))
	ctx, now := context.Background(), time.Now()
	for i := range ticks {
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}
	return f.backend.eventTypes("C-1")
}

// TestWatchReadsTheAgentsProcesses: with the log silent and the files still,
// the process group decides. CPU above watch.min_cpu_pct or a megabyte of
// I/O per watch is work; an idle CLI's trickle is not (#524).
func TestWatchReadsTheAgentsProcesses(t *testing.T) {
	const ticks = 41 // 20 silent minutes
	cases := []struct {
		name   string
		stats  func(call int) procStats
		want   string // the events, joined
		reason string // in agent_waiting's reason
	}{
		{
			name: "busy CPU is computing",
			// 3 s of CPU per 30 s watch: 10% of a core.
			stats: func(c int) procStats {
				return procStats{Procs: 2, CPU: time.Duration(c) * 3 * time.Second, HasCPU: true}
			},
			want: "agent_waiting,agent_waiting", reason: "computing: ",
		},
		{
			name: "an idle CLI's CPU is not work",
			// 0.1 s per watch: under the 2% default.
			stats: func(c int) procStats {
				return procStats{Procs: 1, CPU: time.Duration(c) * 100 * time.Millisecond, HasCPU: true}
			},
			want: "agent_stalled",
		},
		{
			name: "data arriving is receiving",
			stats: func(c int) procStats {
				return procStats{Procs: 1, Read: int64(c) * 5 << 20, HasCPU: true, HasIO: true}
			},
			want: "agent_waiting,agent_waiting", reason: "receiving data: ",
		},
		{
			name: "a keep-alive is not",
			stats: func(c int) procStats {
				return procStats{Procs: 1, Read: int64(c) * 4 << 10, Written: int64(c) * 4 << 10, HasCPU: true, HasIO: true}
			},
			want: "agent_stalled",
		},
		{
			// Busy for the first 5 silent minutes: waiting at 10, stalled at
			// 15, once nothing has moved for stall_after_s.
			name: "working, then nothing for 10 minutes",
			stats: func(c int) procStats {
				return procStats{Procs: 1, CPU: time.Duration(min(c, 10)) * 3 * time.Second, HasCPU: true}
			},
			want: "agent_waiting,agent_stalled", reason: "computing: ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, log := watchFixture(t)
			f.s.procProbe = scriptedProbe(tc.stats)
			got := strings.Join(watchSilently(t, f, log, ticks), ",")
			if got != tc.want {
				t.Fatalf("events = %s, want %s", got, tc.want)
			}
			if tc.reason == "" {
				return
			}
			if reason, _ := f.backend.eventExtra("C-1", EventAgentWaiting)["reason"].(string); !strings.HasPrefix(reason, tc.reason) {
				t.Errorf("reason = %q, want it to start %q", reason, tc.reason)
			}
		})
	}
}

// TestWatchLivenessOffIsTheLogAlone: watch.liveness: false is #487's watch,
// growing files or not.
func TestWatchLivenessOffIsTheLogAlone(t *testing.T) {
	f, log := watchFixture(t)
	f.s.Opts.Cfg.Watch.Liveness = false
	f.s.procProbe = func(int) (procStats, error) {
		t.Fatal("the process group was read with watch.liveness off")
		return procStats{}, nil
	}
	data := filepath.Join(attemptOf(f).Dispatch.Cwd, "data.csv")
	write(t, log, toolLine("a", "python fetch.py"))
	ctx, now := context.Background(), time.Now()
	for i := range 21 {
		write(t, data, strings.Repeat("x", i+1))
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}
	if got := f.backend.eventTypes("C-1"); strings.Join(got, ",") != EventAgentStalled {
		t.Fatalf("events = %v, want one agent_stalled", got)
	}
	reason, _ := f.backend.eventExtra("C-1", EventAgentStalled)["reason"].(string)
	if reason != "its log has been silent for 10m0s" {
		t.Errorf("reason = %q, want #487's wording unchanged", reason)
	}
}

// TestWatchResumesAWaitingAgentWhenItWrites: a waiting agent that writes to
// its log again gets agent_resumed, so the dashboard stops showing the wait.
func TestWatchResumesAWaitingAgentWhenItWrites(t *testing.T) {
	f, log := watchFixture(t)
	f.s.procProbe = scriptedProbe(func(c int) procStats {
		return procStats{Procs: 1, CPU: time.Duration(c) * 3 * time.Second, HasCPU: true}
	})
	write(t, log, toolLine("a", "make bench"))
	ctx, now := context.Background(), time.Now()
	for i := range 21 {
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}
	write(t, log, toolLine("a", "make bench")+toolLine("b", "make report"))
	f.s.WatchAgents(ctx, now.Add(21*watchInterval))
	if got := strings.Join(f.backend.eventTypes("C-1"), ","); got != "agent_waiting,agent_resumed" {
		t.Fatalf("events = %s, want agent_waiting,agent_resumed", got)
	}
}

// TestWatchDataDirsCount: growth in a configured data dir, outside the
// worktree, is the agent's work too; relative entries are the project root's.
func TestWatchDataDirsCount(t *testing.T) {
	f, log := watchFixture(t)
	f.s.Opts.Cwd = t.TempDir()
	f.s.Opts.Cfg.Watch.DataDirs = []string{"cache"}
	data := filepath.Join(f.s.Opts.Cwd, "cache", "candles.csv")
	write(t, log, toolLine("a", "python fetch.py"))
	ctx, now := context.Background(), time.Now()
	for i := range 21 {
		write(t, data, strings.Repeat("x", 1000*(i+1)))
		f.s.WatchAgents(ctx, now.Add(time.Duration(i)*watchInterval))
	}
	if got := strings.Join(f.backend.eventTypes("C-1"), ","); got != EventAgentWaiting {
		t.Fatalf("events = %s, want agent_waiting", got)
	}
}

// TestWalkFilesSkipsGitAndOrchState: commits and orch's own database are not
// the agent's work — the watch's events land in orch.db.
func TestWalkFilesSkipsGitAndOrchState(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "src", "a.go"), "12345")
	write(t, filepath.Join(dir, ".git", "objects", "x"), "ignored")
	write(t, filepath.Join(dir, ".orchestrator", "state", "orch.db"), "ignored")
	write(t, filepath.Join(dir, "state", "logs", "a.log"), "ignored")
	got := walkFiles(dir, filepath.Join(dir, "state"))
	if got.Bytes != 5 || got.Count != 1 {
		t.Fatalf("walk = %d bytes in %d files, want 5 in 1", got.Bytes, got.Count)
	}
}

func TestWaitEvidenceReads(t *testing.T) {
	at := time.Now()
	base := liveSample{at: at, proc: procStats{HasCPU: true, HasIO: true}}
	cases := []struct {
		cur  liveSample
		want string
	}{
		{liveSample{at: at.Add(time.Hour), files: fileStats{Bytes: 163_000_000}, proc: procStats{Read: 170_000_000, HasCPU: true, HasIO: true}},
			"downloading data: +163 MB in 1h0m0s"},
		{liveSample{at: at.Add(5 * time.Minute), files: fileStats{Bytes: 40_000_000}, proc: procStats{HasCPU: true, HasIO: true}},
			"writing files: +40 MB in 5m0s"},
		{liveSample{at: at.Add(5 * time.Minute), proc: procStats{Read: 2_100_000_000, HasCPU: true, HasIO: true}},
			"receiving data: 2.1 GB in 5m0s"},
		{liveSample{at: at.Add(5 * time.Minute), proc: procStats{CPU: 250 * time.Second, HasCPU: true, HasIO: true}},
			"computing: 4m10s of CPU in 5m0s"},
		{liveSample{at: at.Add(5 * time.Minute), files: fileStats{Newest: at.Add(time.Minute)}, proc: procStats{HasCPU: true, HasIO: true}},
			"changing files in 5m0s"},
	}
	for _, tc := range cases {
		if got := evidence(base, tc.cur).String(); got != tc.want {
			t.Errorf("evidence = %q, want %q", got, tc.want)
		}
	}
}

func TestParsePSTime(t *testing.T) {
	cases := map[string]time.Duration{
		"0:00.00":    0,
		"3:04.56":    3*time.Minute + 4560*time.Millisecond,
		"02:03:04":   2*time.Hour + 3*time.Minute + 4*time.Second,
		"1-02:03:04": 26*time.Hour + 3*time.Minute + 4*time.Second,
	}
	for in, want := range cases {
		if got, ok := parsePSTime(in); !ok || got != want {
			t.Errorf("parsePSTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "12", "a:b", "x-01:02"} {
		if _, ok := parsePSTime(bad); ok {
			t.Errorf("parsePSTime(%q) accepted", bad)
		}
	}
}

// TestParsePSGroup: `ps -A -o pgid=,time=` lines, padded as ps pads them;
// only the agent's group counts.
func TestParsePSGroup(t *testing.T) {
	out := []byte("    1   2:41.07\n  812   0:01.50\n  812   1:00.25\n  9000   0:09.00\n")
	got := parsePSGroup(out, 812)
	if got.Procs != 2 || got.CPU != 61750*time.Millisecond || !got.HasCPU || got.HasIO {
		t.Fatalf("parsePSGroup = %+v, want 2 procs, 61.75 s of CPU, no I/O", got)
	}
}
