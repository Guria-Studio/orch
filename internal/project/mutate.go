package project

import (
	"path/filepath"

	"github.com/Guria-Studio/orch/internal/atomize"
	"github.com/Guria-Studio/orch/internal/model"
)

// MutateTasks is the one path for a wholesale rewrite of tasks.json —
// `orch atomize --apply`, `orch sync issues`, `orch start`, the welcome page.
// It holds the lock TaskEditor holds, reads the file, hands it to change and
// writes the result atomically, with a tasks.json.bak-<ts> first when backup
// is set. They used to read and write outside that lock, so one landing
// during a dashboard edit lost the other's write (#531). It returns the
// backup path, or "" when none was made.
func MutateTasks(stateDir, path string, backup bool, change func(*model.TasksFile) error) (string, error) {
	unlock, err := lockFile(filepath.Join(stateDir, taskEditLockFile))
	if err != nil {
		return "", err
	}
	defer unlock()
	tf, err := atomize.LoadExisting(path)
	if err != nil {
		return "", err
	}
	if err := change(&tf); err != nil {
		return "", err
	}
	return atomize.Apply(path, tf, backup)
}
