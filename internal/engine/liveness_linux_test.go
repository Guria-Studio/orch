//go:build linux

package engine

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestParseProcStat: a command name with spaces and parentheses does not
// shift the fields (the line is a real /proc/<pid>/stat with its comm edited).
func TestParseProcStat(t *testing.T) {
	line := []byte("4242 (node (worker) 1) S 4200 4242 4242 0 -1 4194560 1250 0 0 0 310 45 7 3 20 0 11 0 1234 1000000 5000 18446744073709551615 0 0 0 0 0 0 0 4096 0 0 0 0 17 3 0 0 0 0 0\n")
	pgid, state, ticks, ok := parseProcStat(line)
	if !ok || pgid != 4242 || state != 'S' || ticks != 310+45+7+3 {
		t.Fatalf("parseProcStat = %d %c %d %v, want 4242 S 365 true", pgid, state, ticks, ok)
	}
	if _, _, _, ok := parseProcStat([]byte("garbage")); ok {
		t.Error("parsed a line with no command name")
	}
}

func TestParseProcIO(t *testing.T) {
	r, w := parseProcIO([]byte("rchar: 170000000\nwchar: 163000000\nsyscr: 10\nread_bytes: 0\n"))
	if r != 170_000_000 || w != 163_000_000 {
		t.Fatalf("parseProcIO = %d, %d", r, w)
	}
}

// TestReadProcGroupSeesABusyChild reads a real process group: a shell
// spinning in a loop uses CPU the probe sees, and is one live process.
func TestReadProcGroupSeesABusyChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	pgid := cmd.Process.Pid
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := readProcGroup(pgid)
		if err != nil {
			t.Fatal(err)
		}
		if st.Procs != 1 || !st.HasCPU || !st.HasIO {
			t.Fatalf("readProcGroup = %+v, want one live process with CPU and I/O read", st)
		}
		if st.CPU > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no CPU seen for a spinning child in 10 s: %+v", st)
		}
		runtime.Gosched() // poll: the child is a process, not a goroutine to sync with
	}
}
