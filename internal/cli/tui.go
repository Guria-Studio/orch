package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/dashboard"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/providers"
	"github.com/hectorcanaimero/orch/internal/state"
)

// `orch tui` is `orch status` that keeps watching: the task table, the phase
// progress, the spend per provider and the selected task's log as steps,
// redrawn every tuiPoll in the terminal it was started in. It only reads —
// the database through the same readers `status` uses, the log through the
// dashboard's own activity reader — so it runs beside `orch run` and changes
// nothing. Raw ANSI over golang.org/x/term rather than a TUI framework: one
// screen, three keys, nothing a framework would earn its dependency for.

const tuiPoll = 1500 * time.Millisecond

// tuiFilters is the order `f` cycles the status filter in; "" shows every task.
var tuiFilters = []model.Status{"", model.StatusInProgress, model.StatusTodo, model.StatusBlocked, model.StatusDone, model.StatusBacklog}

// tuiRow is one task as the table shows it. Elapsed is zero for a task that
// has never started.
type tuiRow struct {
	ID      string
	Status  model.Status
	Backend string
	Model   string
	Phase   int
	Elapsed time.Duration
}

// tuiUsage is one provider's spend over the project's whole history.
type tuiUsage struct {
	Backend   string
	CostUSD   float64
	TokensIn  int
	TokensOut int
}

// tuiView is everything one frame shows. renderTUI turns it into the screen,
// with no I/O, so the layout is tested without a terminal.
type tuiView struct {
	Project    string
	RunStatus  string // the latest run's status; empty if the project never ran
	Rows       []tuiRow
	Usage      []tuiUsage
	Filter     model.Status
	SelectedID string
	Steps      []providers.Step
	Err        string // the last refresh's error, shown instead of quitting
	Width      int
	Height     int
}

// visibleRows is the rows the filter keeps, and the index of the selected one
// among them. Selection is held by id, not index, so a refresh that reorders
// or drops rows keeps the cursor on the same task; a task that left the
// filter puts it back on the first row.
func visibleRows(rows []tuiRow, filter model.Status, selectedID string) ([]tuiRow, int) {
	var out []tuiRow
	sel := 0
	for _, r := range rows {
		if filter != "" && r.Status != filter {
			continue
		}
		if r.ID == selectedID {
			sel = len(out)
		}
		out = append(out, r)
	}
	return out, sel
}

// moveSelection is the id one row up (delta -1) or down (+1) from the
// selected one, stopping at either end.
func moveSelection(rows []tuiRow, filter model.Status, selectedID string, delta int) string {
	vis, sel := visibleRows(rows, filter, selectedID)
	if len(vis) == 0 {
		return selectedID
	}
	sel = min(max(sel+delta, 0), len(vis)-1)
	return vis[sel].ID
}

// nextFilter is the filter after f in tuiFilters, wrapping around.
func nextFilter(f model.Status) model.Status {
	for i, s := range tuiFilters {
		if s == f {
			return tuiFilters[(i+1)%len(tuiFilters)]
		}
	}
	return ""
}

// renderTUI lays out one frame: Height lines, each cut to Width, joined by
// "\n". The header takes four lines, the table about half of what is left and
// the selected task's log the rest.
func renderTUI(v tuiView) string {
	vis, sel := visibleRows(v.Rows, v.Filter, v.SelectedID)
	filter := "all"
	if v.Filter != "" {
		filter = string(v.Filter)
	}
	run := "never run"
	if v.RunStatus != "" {
		run = "run " + v.RunStatus
	}
	lines := []string{
		fmt.Sprintf("orch tui · %s · %s · filter: %s · ↑/↓ select  f filter  q quit", v.Project, run, filter),
		phaseLine(v.Rows),
		usageLine(v.Usage),
	}
	if v.Err != "" {
		lines = append(lines, "refresh failed: "+v.Err)
	} else {
		lines = append(lines, "")
	}

	body := max(v.Height-len(lines), 4)
	tableRows := max(body/2-1, 1) // minus the column header
	lines = append(lines, fmt.Sprintf("%-14s %-12s %-28s %-5s %s", "ID", "STATUS", "BACKEND/MODEL", "PHASE", "ELAPSED"))
	// Scroll just enough to keep the selected row on screen.
	first := min(max(sel-tableRows+1, 0), max(len(vis)-tableRows, 0))
	for i := first; i < first+tableRows; i++ {
		if i >= len(vis) {
			lines = append(lines, "")
			continue
		}
		r := vis[i]
		line := fmt.Sprintf("%-14s %-12s %-28s %-5d %s", r.ID, r.Status, r.Backend+"/"+r.Model, r.Phase, fmtElapsed(r.Elapsed))
		if i == sel {
			// Reverse video on the cut line, so the escape codes never count
			// toward the width.
			line = "\x1b[7m" + cut(line, v.Width) + "\x1b[0m"
		}
		lines = append(lines, line)
	}
	if len(vis) == 0 {
		lines[len(lines)-tableRows] = "no tasks match the filter"
	}

	logLines := max(v.Height-len(lines)-1, 0)
	selected := ""
	if len(vis) > 0 {
		selected = vis[sel].ID
	}
	lines = append(lines, "── log "+selected+" "+strings.Repeat("─", max(v.Width-9-len(selected), 0)))
	steps := stepLines(v.Steps)
	if len(steps) == 0 && selected != "" {
		steps = []string{"no activity yet"}
	}
	if len(steps) > logLines {
		steps = steps[len(steps)-logLines:]
	}
	lines = append(lines, steps...)
	for len(lines) < v.Height {
		lines = append(lines, "")
	}

	for i, l := range lines[:v.Height] {
		if !strings.HasPrefix(l, "\x1b[") {
			lines[i] = cut(l, v.Width)
		}
	}
	return strings.Join(lines[:v.Height], "\n")
}

// phaseLine is the DAG's progress: done over total for each phase, in order.
func phaseLine(rows []tuiRow) string {
	done, total := map[int]int{}, map[int]int{}
	for _, r := range rows {
		total[r.Phase]++
		if r.Status == model.StatusDone {
			done[r.Phase]++
		}
	}
	phases := make([]int, 0, len(total))
	for p := range total {
		phases = append(phases, p)
	}
	sort.Ints(phases)
	parts := make([]string, 0, len(phases)+1)
	all := 0
	for _, p := range phases {
		parts = append(parts, fmt.Sprintf("P%d %d/%d", p, done[p], total[p]))
		all += done[p]
	}
	parts = append(parts, fmt.Sprintf("done %d/%d", all, len(rows)))
	return strings.Join(parts, "  ")
}

func usageLine(usage []tuiUsage) string {
	if len(usage) == 0 {
		return "spend: none yet"
	}
	parts := make([]string, 0, len(usage))
	for _, u := range usage {
		parts = append(parts, fmt.Sprintf("%s $%.2f (%s in / %s out)", u.Backend, u.CostUSD, fmtTokens(u.TokensIn), fmtTokens(u.TokensOut)))
	}
	return "spend: " + strings.Join(parts, " · ")
}

// stepLines renders the steps of the log's last attempt, one line each: a log
// that several attempts appended to would otherwise show them run together.
func stepLines(steps []providers.Step) []string {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Kind == providers.StepStart {
			steps = steps[i:]
			break
		}
	}
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		detail := strings.Join(strings.Fields(s.Detail), " ")
		switch s.Kind {
		case providers.StepStart:
			out = append(out, "▶ attempt started")
		case providers.StepTool:
			out = append(out, "• "+s.Tool+" "+detail)
		case providers.StepError:
			out = append(out, "✗ "+detail)
		case providers.StepEnd:
			out = append(out, "■ finished "+detail)
		default:
			out = append(out, "  "+detail)
		}
	}
	return out
}

// cut trims s to width runes.
// ponytail: counts runes, not terminal cells; a wide (CJK, emoji) character
// overflows by one cell. Measure with a wcwidth table if that shows up.
func cut(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	return string([]rune(s)[:max(width, 0)])
}

func fmtElapsed(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	d = d.Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// loadTUIView reads one frame's data. It skips the Bootstrap `status` runs
// first: that writes the task definitions, and a view polling every second
// beside `orch run` has no business writing anything.
func loadTUIView(ctx context.Context, paths config.Paths, backend state.Backend, now time.Time) (tuiView, error) {
	v := tuiView{Project: paths.ID}
	spend, err := backend.AllSpend(ctx, time.Time{})
	if err != nil {
		return v, fmt.Errorf("read spend: %w", err)
	}
	costByTask := map[string]float64{}
	byBackend := map[string]*tuiUsage{}
	for _, s := range spend {
		costByTask[s.TaskID] += s.CostUSD
		u := byBackend[s.Backend]
		if u == nil {
			u = &tuiUsage{Backend: s.Backend}
			byBackend[s.Backend] = u
		}
		u.CostUSD += s.CostUSD
		u.TokensIn += s.TokensIn
		u.TokensOut += s.TokensOut
	}
	for _, u := range byBackend {
		v.Usage = append(v.Usage, *u)
	}
	sort.Slice(v.Usage, func(i, j int) bool { return v.Usage[i].Backend < v.Usage[j].Backend })

	// buildStatusRows hydrates through project.Hydrate: statuses come from the
	// database, not tasks.json.
	rows, err := buildStatusRows(ctx, backend, paths.ID, loadDAG(paths), loadRouterTolerant(paths), costByTask)
	if err != nil {
		return v, fmt.Errorf("read task rows: %w", err)
	}
	runtime, err := backend.Tasks(ctx, state.TaskFilter{})
	if err != nil {
		return v, fmt.Errorf("read task timings: %w", err)
	}
	byID := make(map[string]state.TaskRuntime, len(runtime))
	for _, r := range runtime {
		byID[r.ID] = r
	}
	for _, r := range rows {
		v.Rows = append(v.Rows, tuiRow{
			ID: r.ID, Status: r.Status, Backend: r.Backend, Model: r.CLIModel, Phase: r.Phase,
			Elapsed: elapsedOf(byID[r.ID], r.Status, now),
		})
	}

	run, err := backend.LatestRun(ctx)
	switch {
	case err == nil:
		v.RunStatus = run.Status
	case !errors.Is(err, state.ErrNoRuns):
		return v, fmt.Errorf("read latest run: %w", err)
	}
	return v, nil
}

// elapsedOf is how long a task has been at it: from its first start to now
// while it runs, to when it finished once done, to its last change otherwise.
func elapsedOf(rt state.TaskRuntime, status model.Status, now time.Time) time.Duration {
	start, ok := state.ParseTS(rt.StartedAt)
	if !ok {
		return 0
	}
	end := now
	if status != model.StatusInProgress {
		at := rt.FinishedAt
		if at == "" {
			at = rt.UpdatedAt
		}
		if end, ok = state.ParseTS(at); !ok {
			return 0
		}
	}
	return end.Sub(start)
}

func newTUICmd(flags *projectFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Live terminal view of the run: tasks, phases, spend, the selected task's log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			inFd, outFd := int(os.Stdin.Fd()), int(os.Stdout.Fd()) // #nosec G115 -- a file descriptor fits in an int
			if !term.IsTerminal(inFd) || !term.IsTerminal(outFd) {
				return errors.New("orch tui needs an interactive terminal; for a one-off snapshot use `orch status` (or `orch status --json`)")
			}
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}
			backend, closeDB, err := openBackend(cmd.Context(), paths, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			old, err := term.MakeRaw(inFd)
			if err != nil {
				return fmt.Errorf("switch the terminal to raw mode: %w", err)
			}
			// Alternate screen and hidden cursor on the way in; both undone,
			// and the terminal's mode restored, however the loop ends.
			out := cmd.OutOrStdout()
			_, _ = io.WriteString(out, "\x1b[?1049h\x1b[?25l")
			defer func() {
				_, _ = io.WriteString(out, "\x1b[?25h\x1b[?1049l")
				if err := term.Restore(inFd, old); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "orch tui: could not restore the terminal (%v); run `reset`\n", err)
				}
			}()
			return tuiLoop(ctx, out, os.Stdin, outFd, paths, backend)
		},
	}
}

// tuiLoop redraws on every key and every tuiPoll until `q`, Ctrl+C or a
// signal. A failed refresh keeps the last frame and says why; it does not end
// the view, since `orch run` holding the database for a moment is normal.
func tuiLoop(ctx context.Context, out io.Writer, in io.Reader, outFd int, paths config.Paths, backend state.Backend) error {
	keys := make(chan byte)
	// ponytail: this reader stays blocked on stdin after the view closes;
	// the process exits right after, so it is never collected. A cancelable
	// read (poll on the fd) is the fix if tui ever runs inside a longer life.
	go func() {
		buf := make([]byte, 16)
		for {
			n, err := in.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			for _, b := range buf[:n] {
				select {
				case keys <- b:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	ticker := time.NewTicker(tuiPoll)
	defer ticker.Stop()
	var (
		v tuiView
		k tuiKeys
	)
	refresh := func() {
		next, err := loadTUIView(ctx, paths, backend, time.Now())
		if err != nil {
			v.Err = err.Error()
		} else {
			v = next
		}
	}
	refresh()
	for {
		vis, sel := visibleRows(v.Rows, k.filter, k.selID)
		v.Steps = nil
		if len(vis) > 0 {
			k.selID = vis[sel].ID
			v.Steps = logSteps(paths.StateDir(), k.selID)
		}
		v.Filter, v.SelectedID = k.filter, k.selID
		v.Width, v.Height, _ = term.GetSize(outFd)
		if v.Width <= 0 || v.Height <= 0 {
			v.Width, v.Height = 80, 24
		}
		frame := strings.ReplaceAll(renderTUI(v), "\n", "\x1b[K\r\n")
		if _, err := io.WriteString(out, "\x1b[H"+frame+"\x1b[K\x1b[J"); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			refresh()
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			var quit bool
			if k, quit = k.press(v.Rows, b); quit {
				return nil
			}
		}
	}
}

// tuiKeys is what the keyboard controls: the filter, the selected task, and
// how much of an arrow key's escape sequence (ESC [ A/B) has arrived, since
// raw mode hands it over byte by byte.
type tuiKeys struct {
	filter model.Status
	selID  string
	esc    int
}

// press applies one key byte, and says whether it quits the view.
func (k tuiKeys) press(rows []tuiRow, b byte) (tuiKeys, bool) {
	move := 0
	switch {
	case k.esc == 0 && b == 0x1b, k.esc == 1 && b == '[':
		k.esc++
		return k, false
	case k.esc == 2 && b == 'A', b == 'k':
		move = -1
	case k.esc == 2 && b == 'B', b == 'j':
		move = +1
	case b == 'q', b == 0x03: // Ctrl+C arrives as a byte in raw mode
		return k, true
	case b == 'f':
		k.filter = nextFilter(k.filter)
	}
	k.esc = 0
	if move != 0 {
		k.selID = moveSelection(rows, k.filter, k.selID, move)
	}
	return k, false
}

// logSteps is the task's log as steps, or one error step saying why it could
// not be read: an empty pane would look like an agent that did nothing.
func logSteps(stateDir, taskID string) []providers.Step {
	act, err := dashboard.ReadActivity(filepath.Join(stateDir, "logs", taskID+".log"), -1)
	if err != nil {
		return []providers.Step{{Kind: providers.StepError, Detail: "reading the log: " + err.Error()}}
	}
	return act.Steps
}
