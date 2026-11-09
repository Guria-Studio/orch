//go:build !linux

package engine

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// readProcGroup asks ps for the CPU time of the processes in group pgid,
// where there is no /proc (macOS, the BSDs). ps does not report I/O, so
// HasIO is false and a silent download counts only through the files it
// writes and the CPU it uses.
func readProcGroup(pgid int) (procStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pgid=,time=").Output() // #nosec G204 -- fixed argv
	if err != nil {
		return procStats{}, fmt.Errorf("ps: %w", err)
	}
	return parsePSGroup(out, pgid), nil
}
