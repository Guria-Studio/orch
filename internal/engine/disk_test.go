package engine

import (
	"context"
	"testing"
)

// TestLowDiskHoldsANewDispatch pins #497: in worktree mode a ready task waits
// while the disk is below dispatch.min_free_disk_mb, says why once, and goes
// once space is back.
func TestLowDiskHoldsANewDispatch(t *testing.T) {
	f, _, _, _ := prFixture(t)
	f.s.Opts.Cfg.Dispatch.MinFreeDiskMB = 2048
	free := uint64(650)
	old := freeDiskMB
	freeDiskMB = func(string) (uint64, error) { return free, nil }
	t.Cleanup(func() { freeDiskMB = old })

	for i := range 2 {
		if started, err := f.s.Refill(context.Background()); err != nil || started != 0 {
			t.Fatalf("Refill %d at %d MB free: started %d, %v; want 0", i, free, started, err)
		}
	}
	if got := f.s.DeferReasons()["C-1"]; got != "low-disk" {
		t.Errorf("defer reason = %q, want low-disk", got)
	}

	free = 4096
	if started, err := f.s.Refill(context.Background()); err != nil || started != 1 {
		t.Errorf("with space back: started %d, %v; want 1", started, err)
	}
}
