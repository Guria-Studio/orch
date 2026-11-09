package project

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// ProcStart is when process pid started, as the OS reports it (#541):
// /proc/<pid>/stat's starttime (clock ticks since boot) on Linux, `ps -o
// lstart=` elsewhere. Opaque, only ever compared for equality with itself;
// "" when it cannot be read.
func ProcStart(pid int) string {
	if pid <= 0 {
		return ""
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		// The command name (field 2) is in parentheses and may hold spaces;
		// the fields after the last ')' start at field 3, so starttime, field
		// 22, is the 20th of them.
		s := string(b)
		if i := strings.LastIndexByte(s, ')'); i >= 0 {
			if f := strings.Fields(s[i+1:]); len(f) >= 20 {
				return f[19]
			}
		}
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- ps with a numeric pid.
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DispatchAlive reports whether a dispatch row's process is still the
// dispatch: its PID is alive and, when the start time was recorded, started
// when the dispatch's did.
func DispatchAlive(d state.Dispatch) bool {
	return PIDAlive(d.PID) && !PIDReused(d)
}

// PIDReused reports whether the live process under a dispatch row's PID
// started at another time than the dispatch's: the PID was reused after a
// crash. Without a recorded or readable start time it cannot tell, and says
// no — the PID-only check stands.
func PIDReused(row state.Dispatch) bool {
	if row.ProcStart == "" {
		return false
	}
	now := ProcStart(row.PID)
	return now != "" && now != row.ProcStart
}
