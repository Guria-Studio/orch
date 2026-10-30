package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Guria-Studio/orch/internal/doctor"
	"github.com/Guria-Studio/orch/internal/engine"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/providers"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/scaffold"
	"github.com/Guria-Studio/orch/internal/state"
)

// smokeCapUSD is each smoke dispatch's spend cap. claude enforces it
// (--max-budget-usd); the other CLIs take no cap, and the task is a few
// tool calls.
const smokeCapUSD = 0.25

// smokeEffort is the effort a smoke route asks of the CLIs that take one.
const smokeEffort = "low"

// effortBackends are the CLIs the smoke hands an effort to (codex, muse
// --reasoning-effort). agy takes --effort too, but its packaged models carry
// theirs in the name (gemini-3.8-flash-medium) and agy refuses both at once.
var effortBackends = []model.Backend{model.BackendCodex, model.BackendMuse}

// smokeNonce is the token a smoke task's prompt carries; a variable so the
// tests can know it.
var smokeNonce = func() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "smoke-" + hex.EncodeToString(b)
}

type smokeRow struct {
	Backend  string         `json:"backend"`
	Model    string         `json:"model"`
	Effort   string         `json:"effort,omitempty"`
	SpendUSD float64        `json:"spend_usd"`
	Checks   []doctor.Check `json:"checks"`
}

type smokePayload struct {
	Backends []smokeRow `json:"backends"`
	SpendUSD float64    `json:"spend_usd"`
	ExitCode int        `json:"exit_code"`
}

// runSmoke is `orch doctor --smoke` (#437): one throwaway task per installed
// CLI, dispatched by runProject — `orch run`'s own code path, with its prompt
// builder, MCP config, permissions and worktrees — in a temporary project
// that is removed afterwards. Nothing is pushed: the project has no remote.
func runSmoke(cmd *cobra.Command, only string, asJSON, keep bool) error {
	ctx := cmd.Context()
	_, fake := engine.FakeProviderDir()
	backends, err := smokeBackends(only, fake)
	if err != nil {
		return withExitCode(2, err)
	}
	var lists scaffold.ModelLists
	if !fake {
		lists, _ = scaffold.DiscoverModels(ctx, func(b model.Backend) bool { return slices.Contains(backends, b) })
	}
	routes, err := smokeRoutes(backends, lists)
	if err != nil {
		return withExitCode(2, err)
	}

	// Under the user's cache, not /tmp: codex's sandbox may write /tmp, and a
	// project there would hide #387 (the agent cannot reach the database).
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	base = filepath.Join(base, "orch")
	if err := os.MkdirAll(base, 0o750); err != nil {
		return withExitCode(2, err)
	}
	parent, err := os.MkdirTemp(base, "smoke-")
	if err != nil {
		return withExitCode(2, err)
	}
	if keep {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "--keep: the smoke project stays in %s\n", parent)
	} else {
		defer func() { _ = os.RemoveAll(parent) }()
	}
	root := filepath.Join(parent, "project")
	nonce := smokeNonce()
	if err := smokeProject(root, routes, nonce); err != nil {
		return withExitCode(2, fmt.Errorf("prepare the smoke project: %w", err))
	}

	flags := &projectFlags{root: root}
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return withExitCode(2, err)
	}
	backend, closeBackend, err := openBackend(ctx, paths, cfg)
	if err != nil {
		return withExitCode(2, err)
	}
	defer func() { _ = closeBackend() }()

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "orch doctor --smoke: dispatching one task to each of %v (cap $%.2f each where the CLI takes one)\n", backends, smokeCapUSD)
	// The run's own verdict is in the rows: a blocked smoke task is a failed check.
	_ = runProject(ctx, cmd.InOrStdin(), cmd.ErrOrStderr(), flags, runOptions{mode: string(engine.ModeAuto), backend: backend})

	payload := smokePayload{}
	prices := pricing.Load(root)
	spend, err := backend.AllSpend(ctx, time.Time{})
	if err != nil {
		return withExitCode(2, fmt.Errorf("read the smoke's spend: %w", err))
	}
	for _, b := range backends {
		row, err := smokeEvidence(ctx, backend, paths.StateDir(), root, b, routes[b], nonce, spend, prices)
		if err != nil {
			return withExitCode(2, err)
		}
		payload.SpendUSD += row.SpendUSD
		for _, c := range row.Checks {
			if c.Status == doctor.StatusError {
				payload.ExitCode = 1
			}
		}
		payload.Backends = append(payload.Backends, row)
	}

	if asJSON {
		err = printCompactJSON(cmd.OutOrStdout(), payload)
	} else {
		err = renderSmoke(cmd.OutOrStdout(), payload)
	}
	if err != nil {
		return err
	}
	if payload.ExitCode != 0 {
		return withSilentExitCode(payload.ExitCode)
	}
	return nil
}

// smokeBackends is --backend, or every CLI on PATH. Under the fake hook a
// named backend need not be installed.
func smokeBackends(only string, fake bool) ([]model.Backend, error) {
	if only != "" {
		b := model.Backend(only)
		if _, err := providers.Get(b); err != nil {
			return nil, fmt.Errorf("--backend %s: %w", only, err)
		}
		if !fake && !router.OnPath(b) {
			return nil, fmt.Errorf("--backend %s: %s is not on PATH", only, only)
		}
		return []model.Backend{b}, nil
	}
	var out []model.Backend
	for _, b := range router.BackendPreference {
		if router.OnPath(b) {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no AI CLI is on PATH (%v)", router.BackendPreference)
	}
	return out, nil
}

// smokeRoute is the route key and entry a backend's smoke task runs on.
type smokeRoute struct {
	Key   string
	Entry model.RouteEntry
}

// smokeRoutes picks each backend's cheapest packaged route its CLI still
// lists, and asks the CLIs that take an effort for the lowest.
func smokeRoutes(backends []model.Backend, lists scaffold.ModelLists) (map[model.Backend]smokeRoute, error) {
	packaged, err := scaffold.PackagedRouter()
	if err != nil {
		return nil, err
	}
	packaged, _ = lists.Available(packaged)
	out := map[model.Backend]smokeRoute{}
	for _, b := range backends {
		for _, tier := range []model.Tier{model.TierCheap, model.TierStandard, model.TierPremium} {
			key := packaged.ForTier(tier, func(x model.Backend) bool { return x == b })
			if key == "" {
				continue
			}
			e := packaged[key]
			e.FallbackCLIModel, e.EscalationModel = nil, nil
			if slices.Contains(effortBackends, b) {
				effort := smokeEffort
				e.Effort = &effort
			}
			out[b] = smokeRoute{Key: key, Entry: e}
			break
		}
		if _, ok := out[b]; !ok {
			return nil, fmt.Errorf("no packaged route runs %s on a model it lists", b)
		}
	}
	return out, nil
}

func smokeTaskID(b model.Backend) string { return "smoke-" + string(b) }

// smokeProject scaffolds the throwaway project: `orch init`'s files, a git
// repository with one commit (task worktrees branch from it), one task per
// backend, and a config that keeps the smoke cheap and local.
func smokeProject(root string, routes map[model.Backend]smokeRoute, nonce string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	if err := smokeGit(root, "init", "-q", "-b", "main"); err != nil {
		return err
	}
	if _, err := scaffold.Run(scaffold.Options{Root: root, Name: "orch-smoke", BaseBranch: "main", NoSampleTask: true, NoReportFindings: true}); err != nil {
		return err
	}
	config := fmt.Sprintf(`concurrency:
  global_max: 6
  per_provider: {claude: 1, codex: 1, opencode: 1, gemini: 1, agy: 1, muse: 1}
budget:
  per_dispatch_usd: %g
retry:
  max_attempts: 1
dispatch:
  worktree_mode: true
  base_branch: main
  isolate_compose: false
vcs:
  auto_pr: false
verify:
  enabled: false
report_findings:
  enabled: false
`, smokeCapUSD)
	if err := os.WriteFile(filepath.Join(root, ".orchestrator", "config.yaml"), []byte(config), 0o600); err != nil {
		return err
	}
	rtr := map[string]model.RouteEntry{}
	tf, err := model.LoadTasksFile(filepath.Join(root, "tasks.json"))
	if err != nil {
		return err
	}
	tf.Tasks = nil
	for b, r := range routes {
		rtr[r.Key] = r.Entry
		id := smokeTaskID(b)
		tf.Tasks = append(tf.Tasks, model.Task{
			ID: id, Title: "orch doctor --smoke: " + string(b), Model: r.Key, Status: model.StatusTodo,
			Dependencies: []string{}, Files: []string{}, EstimateHours: 0.1,
			Description: fmt.Sprintf("This is orch's smoke test of the %s adapter; change no file. "+
				"1. If you have orch's MCP tools, call orch_task_note with task_id %q and note %q. "+
				"2. Report %s done as the protocol below says, with the note %q. "+
				"3. End your reply with %s.", b, id, nonce, id, nonce, nonce),
		})
	}
	slices.SortFunc(tf.Tasks, func(a, b model.Task) int { return strings.Compare(a.ID, b.ID) })
	if err := model.SaveTasksFile(filepath.Join(root, "tasks.json"), tf); err != nil {
		return err
	}
	body, err := yaml.Marshal(rtr)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, ".orchestrator", "model_router.yaml"), body, 0o600); err != nil {
		return err
	}
	// The project's own claude rules, as `orch init` grants them (#400); the
	// other CLIs keep theirs in the user's settings, which the smoke reads as
	// they are.
	if _, err := scaffold.AllowOrchTools(root); err != nil {
		return err
	}
	if err := smokeGit(root, "add", "-A"); err != nil {
		return err
	}
	return smokeGit(root, "-c", "user.name=orch", "-c", "user.email=orch@localhost", "commit", "-q", "-m", "orch doctor --smoke")
}

func smokeGit(root string, args ...string) error {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput() // #nosec G204 -- fixed git arguments; root is the smoke's own temp project
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return nil
}

// smokeEvidence reads one backend's smoke task back out of the database and
// its log, and checks it.
func smokeEvidence(ctx context.Context, db state.Backend, stateDir, root string, b model.Backend,
	r smokeRoute, nonce string, spend []state.Spend, prices pricing.Table) (smokeRow, error) {
	id := smokeTaskID(b)
	e := doctor.SmokeEvidence{
		Nonce: nonce, Route: r.Entry,
		TakesEffort: slices.Contains(effortBackends, b),
		MCPExpected: b == model.BackendClaude,
	}
	rt, err := db.Task(ctx, id)
	if err != nil {
		return smokeRow{}, fmt.Errorf("read the smoke task %s: %w", id, err)
	}
	e.Status, e.Comments = rt.Status, rt.Comments
	events, err := db.Events(ctx, id, 0)
	if err != nil {
		return smokeRow{}, fmt.Errorf("read the events of %s: %w", id, err)
	}
	exitCode := 0
	for _, ev := range events {
		switch ev.EventType {
		case engine.EventDispatch:
			e.Dispatch = ev.Extra
		case engine.EventFail, engine.EventTimeout:
			if code, ok := ev.Extra["exit_code"].(float64); ok {
				exitCode = int(code)
			}
		}
	}
	p, _ := providers.Get(b)
	if e.Dispatch != nil {
		// The argv the CLI got, rebuilt from what the dispatch recorded.
		recorded := r.Entry
		recorded.CLIModel, _ = e.Dispatch["cli_model"].(string)
		recorded.Effort = nil
		if effort, ok := e.Dispatch["effort"].(string); ok {
			recorded.Effort = &effort
		}
		e.Argv = p.Argv(providers.Request{TaskID: id, Route: recorded, Cwd: root})
		e.Text = p.Parse(exitCode, []byte(engine.ReadLog(engine.LogPathFor(stateDir, id)))).Text
	}
	for _, s := range spend {
		if s.TaskID != id {
			continue
		}
		e.SpendRows++
		e.Usage.TokensIn += s.TokensIn
		e.Usage.TokensOut += s.TokensOut
		e.Usage.CacheReadTokens += s.CacheReadTokens
		e.Usage.CacheCreationTokens += s.CacheCreationTokens
		e.Usage.Estimated = e.Usage.Estimated || s.Estimated
		e.CostUSD += prices.ResolveCost(s.CostUSD, s.Model, s.TokensIn, s.TokensOut)
	}
	row := smokeRow{Backend: string(b), Model: r.Entry.CLIModel, SpendUSD: e.CostUSD, Checks: doctor.SmokeChecks(e)}
	if r.Entry.Effort != nil {
		row.Effort = *r.Entry.Effort
	}
	return row, nil
}

func renderSmoke(out io.Writer, p smokePayload) error {
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	for _, r := range p.Backends {
		effort := ""
		if r.Effort != "" {
			effort = " · effort " + r.Effort
		}
		if _, err := fmt.Fprintf(w, "\n%s · %s%s · $%.4f\n", r.Backend, r.Model, effort, r.SpendUSD); err != nil {
			return err
		}
		for _, c := range r.Checks {
			if _, err := fmt.Fprintf(w, "  %s\t%s\t%s\n", c.Status, c.Name, c.Detail); err != nil {
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	verdict := "every backend passed"
	if p.ExitCode != 0 {
		verdict = "a check failed"
	}
	_, err := fmt.Fprintf(out, "\nspent $%.4f · %s\n", p.SpendUSD, verdict)
	return err
}
