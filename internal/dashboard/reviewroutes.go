package dashboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/state"
)

// The task page's Changes tab (#454): what the task's branch changed against
// its base, and the operator's Accept. Send back is the note endpoint with
// retry, Block the board's move: one function each, the CLI's.
//
// The diff is not on the stakeholder allow-list: a client sees it only when
// the operator adds api_task_diff to dashboard.stakeholder_routes.

func (s *Server) reviewRoutes() []route {
	return []route{
		{pattern: "GET /api/task/{id}/diff", name: "api_task_diff", handler: s.handleTaskDiff},
		{pattern: "POST /api/task/{id}/accept", name: "api_task_accept", handler: s.handleTaskAccept},
	}
}

// diffPatchLimit cuts one file's hunks: a lockfile or a generated bundle is
// not read in a browser.
const diffPatchLimit = 256 << 10

// diffTimeout bounds the git commands one diff request runs.
const diffTimeout = 30 * time.Second

// diffFile is one file of a task's change.
type diffFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
	// Patch is the file's hunks, from its first @@ line.
	Patch     string `json:"patch"`
	Truncated bool   `json:"truncated,omitempty"`
}

type taskDiff struct {
	TaskID string     `json:"task_id"`
	Base   string     `json:"base"`
	Branch string     `json:"branch"`
	Files  []diffFile `json:"files"`
}

func (s *Server) handleTaskDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rows, err := s.state.Tasks(r.Context(), state.TaskFilter{IDs: []string{id}})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload{Detail: err.Error()})
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "no task " + id})
		return
	}
	base := "main"
	if loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root); err == nil && loaded.Config.Dispatch.BaseBranch != "" {
		base = loaded.Config.Dispatch.BaseBranch
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffTimeout)
	defer cancel()
	git := func(args ...string) (string, error) { return runGit(ctx, s.paths.Root, args...) }
	// The first of each that names a commit: the remote's base is where the
	// worktree started; the branch is local unless only the push is left.
	pick := func(refs ...string) string {
		for _, ref := range refs {
			if _, err := git("rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
				return ref
			}
		}
		return ""
	}
	branch := pick("orch/"+id, "origin/orch/"+id)
	if branch == "" {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "the task's work is on no branch here (orch/" + id + ")"})
		return
	}
	baseRef := pick("origin/"+base, base)
	if baseRef == "" {
		writeJSON(w, http.StatusNotFound, errorPayload{Detail: "the base branch " + base + " is not in this repository"})
		return
	}
	out, err := git("diff", "--no-color", "--no-ext-diff", "--merge-base", baseRef, branch, "--")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorPayload{Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, taskDiff{TaskID: id, Base: baseRef, Branch: branch, Files: parseDiff(out)})
}

// runGit runs git in dir and returns its stdout.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	// #nosec G204 -- fixed git subcommands; the refs are one argument each.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotePath=false"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseDiff splits `git diff` output into files, counting each one's added
// and removed lines.
func parseDiff(out string) []diffFile {
	files := []diffFile{}
	var f *diffFile
	var patch strings.Builder
	flush := func() {
		if f == nil {
			return
		}
		f.Patch = patch.String()
		if len(f.Patch) > diffPatchLimit {
			f.Patch, f.Truncated = f.Patch[:diffPatchLimit], true
		}
		files = append(files, *f)
		patch.Reset()
	}
	inHunk := false
	for _, line := range strings.SplitAfter(out, "\n") {
		trimmed := strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			f, inHunk = &diffFile{}, false
			// a/<path> b/<path>; the +++ line below says it better, when there is one.
			if i := strings.LastIndex(trimmed, " b/"); i >= 0 {
				f.Path = trimmed[i+3:]
			}
		case f == nil:
		case inHunk || strings.HasPrefix(line, "@@"):
			inHunk = true
			patch.WriteString(line)
			if strings.HasPrefix(line, "+") {
				f.Additions++
			} else if strings.HasPrefix(line, "-") {
				f.Deletions++
			}
		case strings.HasPrefix(line, "+++ b/"):
			f.Path = strings.TrimPrefix(trimmed, "+++ b/")
		case strings.HasPrefix(line, "rename from "):
			f.OldPath = strings.TrimPrefix(trimmed, "rename from ")
		case strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		}
	}
	flush()
	return files
}

func (s *Server) handleTaskAccept(w http.ResponseWriter, r *http.Request) {
	if !s.operatorWrite(w, r, s.steer != nil, &struct{}{}) {
		return
	}
	steer := *s.steer
	steer.Source = "dashboard"
	msg, err := steer.Accept(r.Context(), r.PathValue("id"))
	if errors.Is(err, project.ErrNoPR) {
		writeJSON(w, http.StatusConflict, errorPayload{Detail: err.Error()})
		return
	}
	if err != nil {
		s.steerFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"task_id": r.PathValue("id"), "message": msg})
}
