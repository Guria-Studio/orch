package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/Guria-Studio/orch/internal/ci"
	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/project"
)

// The operator's CI page: how the project's pipeline is set up, the orch half
// (config.yaml: PRs, retries, merges) and the repo half (its GitHub Actions
// workflows). The live half, each task's PR and CI state, rides on /api/tasks.
// Operator only: workflow files and repository settings are not a client's.

func (s *Server) ciRoutes() []route {
	return []route{
		{pattern: "GET /api/ci", name: "api_ci", handler: s.handleCI},
	}
}

type ciPayload struct {
	Config    ciConfig      `json:"config"`
	Workflows []ci.Workflow `json:"workflows"`
	// Warnings name settings that cannot work together, in words.
	Warnings []string `json:"warnings"`
	// MergeQueue is the merge queue (#522): its settings and its entries.
	MergeQueue ciMergeQueue `json:"merge_queue"`
}

type ciMergeQueue struct {
	Enabled  bool   `json:"enabled"`
	Reverify string `json:"reverify"`
	// Entries are the queued PRs in the order the queue merges them
	// (position 1 is next), then the ten that left it last, newest first.
	Entries []ciQueueEntry `json:"entries"`
}

type ciQueueEntry struct {
	TaskID     string `json:"task_id"`
	PRURL      string `json:"pr_url"`
	State      string `json:"state"`
	Position   int    `json:"position,omitempty"`
	Requeues   int    `json:"requeues"`
	Reason     string `json:"reason,omitempty"`
	EnqueuedAt string `json:"enqueued_at"`
	UpdatedAt  string `json:"updated_at"`
}

// queueLeftShown is how many entries that left the queue the CI page lists.
const queueLeftShown = 10

type ciConfig struct {
	WorktreeMode  bool   `json:"worktree_mode"`
	BaseBranch    string `json:"base_branch"`
	Provider      string `json:"provider"`
	Host          string `json:"host"`
	AutoPR        bool   `json:"auto_pr"`
	MaxRetries    int    `json:"ci_max_retries"`
	PollIntervalS int    `json:"ci_poll_interval_s"`
	AutoMerge     bool   `json:"auto_merge"`
	TestCommand   string `json:"test_command"`
	MergeQueue    bool   `json:"merge_queue"`
}

func (s *Server) handleCI(w http.ResponseWriter, r *http.Request) {
	payload, err := s.ciView(r.Context())
	if err != nil {
		s.failRead(w, "ci", err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) ciView(ctx context.Context) (ciPayload, error) {
	loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root)
	if err != nil {
		return ciPayload{}, fmt.Errorf("reading %s: %w", s.paths.ConfigYAML, err)
	}
	c := loaded.Config
	out := ciPayload{
		Config: ciConfig{
			WorktreeMode:  c.Dispatch.WorktreeMode,
			BaseBranch:    c.Dispatch.BaseBranch,
			Provider:      c.VCS.Provider,
			Host:          c.VCS.Host,
			AutoPR:        c.VCS.AutoPR,
			MaxRetries:    c.VCS.CIMaxRetries,
			PollIntervalS: c.VCS.CIPollIntervalS,
			AutoMerge:     c.GitHub.AutoMerge,
			TestCommand:   c.GitHub.TestCommand,
			MergeQueue:    c.MergeQueue.Enabled,
		},
		Workflows: []ci.Workflow{},
		Warnings:  []string{},
		MergeQueue: ciMergeQueue{Enabled: c.MergeQueue.Enabled, Reverify: c.MergeQueue.Reverify,
			Entries: []ciQueueEntry{}},
	}
	entries, err := s.queueEntries(ctx)
	if err != nil {
		return ciPayload{}, err
	}
	out.MergeQueue.Entries = entries

	workflows, err := ci.ReadWorkflows(filepath.Join(s.paths.Root, ".github", "workflows"))
	if err != nil {
		return ciPayload{}, fmt.Errorf("reading the project's CI workflows: %w", err)
	}
	out.Workflows = workflows
	out.Warnings = ciWarnings(out.Config, workflows)
	return out, nil
}

// ciWarnings are the combinations that silently do nothing, or silently do
// something else, said the way the fix reads.
func ciWarnings(c ciConfig, workflows []ci.Workflow) []string {
	out := []string{}
	if c.Provider == config.VCSProviderLocal {
		// No PR, no CI on one: finished tasks merge into the base here.
		return out
	}
	if c.AutoPR && !c.WorktreeMode {
		out = append(out, "vcs.auto_pr is on but dispatch.worktree_mode is off: orch run refuses to start, because there is no branch to open a PR from.")
	}
	onPRs := false
	for _, w := range workflows {
		onPRs = onPRs || w.OnPRs
	}
	if c.AutoPR && !onPRs {
		out = append(out, "No workflow runs on pull_request: orch waits for checks that never come, then finishes each PR without CI (and without merging it).")
	}
	if c.MergeQueue && !c.AutoMerge {
		out = append(out, "merge_queue.enabled is on but github.auto_merge is off: orch merges nothing, so nothing is queued.")
	}
	if c.AutoMerge && !c.AutoPR {
		out = append(out, "github.auto_merge is on but vcs.auto_pr is off: there is never a PR to merge.")
	}
	return out
}

// queueEntries is the merge queue as the CI page lists it: what waits, in
// merge order, then what left it last. Empty when the state reader keeps no
// queue.
func (s *Server) queueEntries(ctx context.Context) ([]ciQueueEntry, error) {
	out := []ciQueueEntry{}
	qr, ok := s.state.(project.QueueReader)
	if !ok {
		return out, nil
	}
	rows, err := qr.QueueEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the merge queue: %w", err)
	}
	var queued, left []project.QueueEntry
	for _, e := range rows {
		if e.State == project.QueueQueued {
			queued = append(queued, e)
		} else {
			left = append(left, e)
		}
	}
	if tf, err := project.Definitions(s.paths.TasksJSON()); err == nil {
		project.SortQueue(queued, tf.Tasks)
	} else {
		project.SortQueue(queued, nil)
	}
	sortByUpdatedDesc(left)
	if len(left) > queueLeftShown {
		left = left[:queueLeftShown]
	}
	row := func(e project.QueueEntry, pos int) ciQueueEntry {
		return ciQueueEntry{TaskID: e.TaskID, PRURL: e.PRURL, State: e.State, Position: pos, Requeues: e.Requeues,
			Reason: e.Reason, EnqueuedAt: e.EnqueuedAt.UTC().Format(time.RFC3339), UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339)}
	}
	for i, e := range queued {
		out = append(out, row(e, i+1))
	}
	for _, e := range left {
		out = append(out, row(e, 0))
	}
	return out, nil
}

func sortByUpdatedDesc(entries []project.QueueEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].UpdatedAt.After(entries[j].UpdatedAt) })
}
