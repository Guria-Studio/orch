//go:build linux

package engine

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, the unit of /proc/<pid>/stat's CPU times. It is 100
// on every Linux orch builds for; reading sysconf would need cgo.
const clockTicks = 100

// readProcGroup sums /proc over the processes whose group is pgid: the
// agent's CLI, its shells and whatever they run. A process another user owns
// has no readable io file and counts for CPU only.
func readProcGroup(pgid int) (procStats, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return procStats{}, fmt.Errorf("read /proc: %w", err)
	}
	st := procStats{HasCPU: true, HasIO: true}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "stat")) // #nosec G304 -- a /proc path
		if err != nil {
			continue // exited between the listing and the read
		}
		group, state, ticks, ok := parseProcStat(raw)
		if !ok || group != pgid {
			continue
		}
		if state != 'Z' {
			st.Procs++
		}
		st.CPU += time.Duration(ticks) * time.Second / clockTicks
		if io, err := os.ReadFile(filepath.Join(dir, "io")); err == nil { // #nosec G304 -- a /proc path
			r, w := parseProcIO(io)
			st.Read += r
			st.Written += w
		}
	}
	return st, nil
}

// parseProcStat reads the process group, state and CPU ticks (utime, stime
// and the reaped children's cutime and cstime) from a /proc/<pid>/stat line.
// The command name may hold spaces and parentheses, so fields count from the
// last ')'.
func parseProcStat(raw []byte) (pgid int, state byte, ticks int64, ok bool) {
	i := bytes.LastIndexByte(raw, ')')
	if i < 0 {
		return 0, 0, 0, false
	}
	f := strings.Fields(string(raw[i+1:]))
	// f[0] is field 3 (state); pgrp is field 5, utime..cstime 14..17.
	if len(f) < 15 {
		return 0, 0, 0, false
	}
	pgid, err := strconv.Atoi(f[2])
	if err != nil {
		return 0, 0, 0, false
	}
	for _, s := range f[11:15] {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, 0, 0, false
		}
		ticks += n
	}
	return pgid, f[0][0], ticks, true
}

// parseProcIO is rchar and wchar from /proc/<pid>/io.
func parseProcIO(raw []byte) (read, written int64) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "rchar":
			read = n
		case "wchar":
			written = n
		}
	}
	return read, written
}
