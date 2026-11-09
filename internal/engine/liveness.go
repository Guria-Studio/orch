package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EventAgentWaiting is an agent whose log is silent past watch.stall_after_s
// while its processes or its files show it working (#524): downloading data,
// running a long build. It is information, not a warning; only a silence with
// no sign of life at all is agent_stalled.
const EventAgentWaiting = "agent_waiting"

const (
	// ioMinBytes is the I/O per watch that counts as the agent's processes
	// working. An idle CLI's keep-alives and polls stay far below it.
	ioMinBytes = 1 << 20
	// walkMaxEntries caps one directory walk; past it the walk stops, at
	// the same place every time, so the totals still compare.
	walkMaxEntries = 100_000
)

// procStats is what the operating system says about one agent's process
// group: the agent's CLI and everything it started.
type procStats struct {
	// Procs is how many live processes are in the group.
	Procs int
	// CPU is the CPU time the group has used, its reaped children included.
	CPU time.Duration
	// Read and Written are bytes moved through read and write calls —
	// sockets as well as files, so a download counts. Zero with HasIO false
	// where the system does not say (macOS).
	Read, Written int64
	HasCPU, HasIO bool
}

// liveSample is the agent's signs of life at one watch.
type liveSample struct {
	at    time.Time
	proc  procStats
	files fileStats
}

// fileStats sums the regular files under the watched directories.
type fileStats struct {
	Bytes  int64
	Count  int
	Newest time.Time
}

// sampleLiveness reads e's signs of life: its process group (pgid is the
// inFlight key, the child's pid) and the files in its worktree and in
// watch.data_dirs.
func (s *Scheduler) sampleLiveness(pgid int, e *InFlight, now time.Time) liveSample {
	probe := s.procProbe
	if probe == nil {
		probe = readProcGroup
	}
	out := liveSample{at: now}
	if st, err := probe(pgid); err == nil {
		out.proc = st
	}
	for _, dir := range s.watchedDirs(e) {
		f := walkFiles(dir, s.Opts.StateDir)
		out.files.Bytes += f.Bytes
		out.files.Count += f.Count
		if f.Newest.After(out.files.Newest) {
			out.files.Newest = f.Newest
		}
	}
	return out
}

// watchedDirs is the attempt's working directory plus watch.data_dirs,
// relative ones against the project root.
func (s *Scheduler) watchedDirs(e *InFlight) []string {
	var dirs []string
	if e.Dispatch.Cwd != "" {
		dirs = append(dirs, e.Dispatch.Cwd)
	}
	for _, d := range s.Opts.Cfg.Watch.DataDirs {
		if d == "" {
			continue
		}
		if !filepath.IsAbs(d) {
			d = filepath.Join(s.Opts.Cwd, d)
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// walkFiles sums the regular files under dir. It skips .git and orch's own
// state (the watch's events land in orch.db, which must not read as the
// agent's work), and what it cannot read.
func walkFiles(dir, stateDir string) fileStats {
	var out fileStats
	n := 0
	errStop := errors.New("stop")
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if n++; n > walkMaxEntries {
			return errStop
		}
		if d.IsDir() {
			if path != dir && (d.Name() == ".git" || d.Name() == ".orchestrator" || (stateDir != "" && path == stateDir)) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out.Bytes += info.Size()
		out.Count++
		if info.ModTime().After(out.Newest) {
			out.Newest = info.ModTime()
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		// The callback returns nothing else; a walk that failed some other
		// way says nothing about growth.
		return fileStats{}
	}
	return out
}

// active reports whether cur shows work since prev: files that grew, shrank
// or changed, CPU above minCPUPct of one core, or I/O above ioMinBytes.
func active(prev, cur liveSample, minCPUPct float64) bool {
	if cur.files.Bytes != prev.files.Bytes || cur.files.Count != prev.files.Count || cur.files.Newest.After(prev.files.Newest) {
		return true
	}
	if prev.proc.HasCPU && cur.proc.HasCPU {
		cpu, elapsed := cur.proc.CPU-prev.proc.CPU, cur.at.Sub(prev.at)
		if cpu > 0 && float64(cpu) >= float64(elapsed)*max(minCPUPct, 0)/100 {
			return true
		}
	}
	if prev.proc.HasIO && cur.proc.HasIO && (cur.proc.Read-prev.proc.Read)+(cur.proc.Written-prev.proc.Written) >= ioMinBytes {
		return true
	}
	return false
}

// waitEvidence is what the agent did between base and cur, in the words
// the dashboard and the client page show: the activity and a figure.
type waitEvidence struct {
	// Activity is download | write | read | compute | edit.
	Activity   string
	FilesBytes int64
	ReadBytes  int64
	CPU        time.Duration
	Window     time.Duration
}

func evidence(base, cur liveSample) waitEvidence {
	ev := waitEvidence{
		FilesBytes: cur.files.Bytes - base.files.Bytes,
		Window:     cur.at.Sub(base.at),
	}
	if base.proc.HasIO && cur.proc.HasIO {
		ev.ReadBytes = cur.proc.Read - base.proc.Read
	}
	if base.proc.HasCPU && cur.proc.HasCPU {
		ev.CPU = cur.proc.CPU - base.proc.CPU
	}
	switch {
	case ev.FilesBytes > 0 && ev.ReadBytes >= ioMinBytes:
		ev.Activity = "download"
	case ev.FilesBytes > 0:
		ev.Activity = "write"
	case ev.ReadBytes >= ioMinBytes:
		ev.Activity = "read"
	case ev.CPU > 0 && cur.files.Bytes == base.files.Bytes && !cur.files.Newest.After(base.files.Newest):
		ev.Activity = "compute"
	default:
		ev.Activity = "edit"
	}
	return ev
}

// String is the evidence as one line: "downloading data: +163 MB in 1h0m0s".
func (ev waitEvidence) String() string {
	in := ev.Window.Round(time.Second)
	switch ev.Activity {
	case "download":
		return fmt.Sprintf("downloading data: +%s in %s", humanBytes(ev.FilesBytes), in)
	case "write":
		return fmt.Sprintf("writing files: +%s in %s", humanBytes(ev.FilesBytes), in)
	case "read":
		return fmt.Sprintf("receiving data: %s in %s", humanBytes(ev.ReadBytes), in)
	case "compute":
		return fmt.Sprintf("computing: %s of CPU in %s", ev.CPU.Round(time.Second), in)
	default:
		return fmt.Sprintf("changing files in %s", in)
	}
}

// extra is the evidence as an event's extra fields.
func (ev waitEvidence) extra() map[string]any {
	return map[string]any{
		"activity":    ev.Activity,
		"files_bytes": ev.FilesBytes,
		"read_bytes":  ev.ReadBytes,
		"cpu_s":       int(ev.CPU.Seconds()),
		"window_s":    int(ev.Window.Seconds()),
	}
}

// parsePSGroup sums `ps -A -o pgid=,time=` over the lines of group pgid.
// time is [[dd-]hh:]mm:ss with optional hundredths, as BSD and procps print
// it.
func parsePSGroup(out []byte, pgid int) procStats {
	st := procStats{HasCPU: true}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if g, err := strconv.Atoi(f[0]); err != nil || g != pgid {
			continue
		}
		d, ok := parsePSTime(f[1])
		if !ok {
			continue
		}
		st.Procs++
		st.CPU += d
	}
	return st
}

// parsePSTime reads ps's cumulative CPU time: "1-02:03:04", "02:03:04",
// "3:04.56".
func parsePSTime(s string) (time.Duration, bool) {
	var days int64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, false
	}
	total := time.Duration(secs * float64(time.Second))
	for i, unit := range []time.Duration{time.Minute, time.Hour} {
		idx := len(parts) - 2 - i
		if idx < 0 {
			break
		}
		n, err := strconv.ParseInt(parts[idx], 10, 64)
		if err != nil {
			return 0, false
		}
		total += time.Duration(n) * unit
	}
	return total + time.Duration(days)*24*time.Hour, true
}

// humanBytes is n in decimal units, one decimal under 10: "163 MB", "1.5 GB".
func humanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	if f < 10 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0") + " " + units[i]
	}
	return fmt.Sprintf("%.0f %s", f, units[i])
}
