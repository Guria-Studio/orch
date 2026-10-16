package ci

import (
	"bytes"
	"os"
	"path/filepath"
)

// PlaceholderStep names the one step of the workflow `orch init` writes for
// a repository whose stack it cannot tell yet (#348).
const PlaceholderStep = "Placeholder - no stack detected"

// WorkflowPath is the workflow orch writes and reads.
const WorkflowPath = ".github/workflows/orch-ci.yml"

// PlaceholderOutgrown reports whether root's workflow is still init's
// placeholder although the repository now has something to check — the
// tasks built the code since. `orch ci setup --force` proposes the real one.
func PlaceholderOutgrown(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, WorkflowPath)) // #nosec G304 -- the workflow orch manages
	if err != nil || !bytes.Contains(raw, []byte(PlaceholderStep)) {
		return false
	}
	st, err := Detect(root)
	return err == nil && st.HasChecks()
}
