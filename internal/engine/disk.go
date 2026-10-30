package engine

import "syscall"

// freeDiskMB is the space left, in MB, on the filesystem holding dir, as an
// unprivileged process may use it. A var so a test can fill the disk.
var freeDiskMB = func(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize) >> 20, nil // #nosec G115 -- a block size is positive
}

// lowDisk reports whether taskID waits for disk space (#497): in worktree
// mode each task gets its own checkout and build output, and a disk that
// fills mid-task fails it with nothing wrong in the code. The task waits like
// a capped budget, once logged, and goes when finished tasks free their
// worktrees. A filesystem that cannot be read dispatches.
//
// ponytail: checks the state directory's filesystem, which is the
// project's; worktrees live next to the project, so on the same disk unless
// it is a mount point of its own.
func (s *Scheduler) lowDisk(taskID string) bool {
	minMB := s.Opts.Cfg.Dispatch.MinFreeDiskMB
	if minMB <= 0 || !s.Opts.WorktreeMode {
		return false
	}
	const reason = "low-disk"
	free, err := freeDiskMB(s.Opts.StateDir)
	if err != nil || free >= uint64(minMB) {
		if s.deferReasons[taskID] == reason {
			delete(s.deferReasons, taskID)
		}
		return false
	}
	s.noteSkip(taskID, reason, "deferred: low disk space; free some (build output in old worktrees, `git worktree prune`) or lower dispatch.min_free_disk_mb",
		"free_mb", free, "min_free_disk_mb", minMB)
	return true
}
