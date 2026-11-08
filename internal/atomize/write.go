package atomize

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Guria-Studio/orch/internal/model"
)

// timestamp formats now as YYYYMMDD-HHMMSS UTC, for backup file names.
// Ports _timestamp.
func timestamp() string {
	return time.Now().UTC().Format("20060102-150405")
}

// Apply writes tf to path through model.SaveTasksFile (atomic), backing up
// any existing file first (tasks.json.bak-<timestamp>, skipped when backup is
// false or path doesn't exist yet). Returns the backup path written, or ""
// when none was made. Ports write_tasks_json.
//
// It does not lock: a read-modify-write of tasks.json goes through
// project.MutateTasks, which holds the task editor's lock around it (#531).
func Apply(path string, tf model.TasksFile, backup bool) (string, error) {
	var backupPath string
	if backup {
		// #nosec G304 -- path is a caller-supplied project file path, the
		// normal shape of this CLI's file I/O, not untrusted input.
		existing, err := os.ReadFile(path)
		switch {
		case err == nil:
			backupPath = path + ".bak-" + timestamp()
			// #nosec G306 G703 -- tasks.json (and its backups) are plain,
			// git-tracked files meant to be read directly, not secrets;
			// backupPath is path (already validated above) plus a fixed
			// suffix, not attacker-controlled.
			if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
				return "", fmt.Errorf("atomize: write backup %s: %w", backupPath, err)
			}
		case os.IsNotExist(err):
			// Nothing to back up — first write for this project.
		default:
			return "", fmt.Errorf("atomize: read %s for backup: %w", path, err)
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("atomize: create %s: %w", dir, err)
	}
	if err := model.SaveTasksFile(path, tf); err != nil {
		return "", fmt.Errorf("atomize: %w", err)
	}
	return backupPath, nil
}

// LoadExisting reads path as a TasksFile, matching load_raw_tasks_json's
// retrocompat: a missing file loads as an empty TasksFile{} (meta/phases/
// tasks all zero-valued, which MarshalJSON already renders as {}/[]/[]) —
// this is Atomize's "project has no tasks.json yet" starting point.
func LoadExisting(path string) (model.TasksFile, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return model.TasksFile{}, nil
		}
		return model.TasksFile{}, fmt.Errorf("atomize: stat %s: %w", path, err)
	}
	return model.LoadTasksFile(path)
}
