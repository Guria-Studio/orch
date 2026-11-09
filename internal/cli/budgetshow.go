package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/budget"
	"github.com/Guria-Studio/orch/internal/pyfmt"
)

// budgetScopeNote is what `orch budget show` says about the numbers (#577).
const budgetScopeNote = "The token window counts this project's own dispatches, weighted (cache reads x0.10, " +
	"cache writes x1.25). It is not your plan's quota, which other projects and your interactive use share; " +
	"the presets are a starting point to calibrate. With `source: provider` a provider is gated on the plan's " +
	"own windows as its CLI last reported them (claude, codex), and on the token window until a report is fresh."

// newBudgetShowCmd prints each budgeted provider's window and what decides
// it. Exit 2 for a project or budgets.yaml that does not load.
func newBudgetShowCmd(flags *projectFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show each provider's budget window, its source, and the plan quota it last reported",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return withExitCode(2, err)
			}
			path := budget.ResolvePath(paths.Root, paths.ConfigYAML, cfg.BudgetsConfig)
			if path == "" {
				return withExitCode(2, errors.New("budgets_config is empty in config.yaml: the guardrail is off"))
			}
			bcfg, err := budget.LoadConfig(path, cfg.BudgetsPreset)
			if err != nil {
				return withExitCode(2, err)
			}
			if bcfg == nil {
				return withExitCode(2, fmt.Errorf("no budgets.yaml at %s: the guardrail is off", path))
			}
			bcfg.UnreportedDispatchTokens = cfg.TypicalDispatchToken
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			snap, err := budget.NewGate(backend, bcfg).Snapshot(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), snap)
			}
			return writeBudgetShow(cmd.OutOrStdout(), cfg.BudgetsPreset, path, bcfg, snap)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the per-provider snapshot as JSON")
	return cmd
}

// writeBudgetShow renders the snapshot, one provider per line, sorted.
func writeBudgetShow(w io.Writer, preset, path string, cfg *budget.Config, snap map[string]budget.ProviderSnapshot) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Preset %s (%s)\n\n", preset, path)
	for _, name := range cfg.Names() {
		s := snap[name]
		tokens := fmt.Sprintf("%s of %s weighted tokens in %sh",
			pyfmt.Commas(int64(s.TokensUsed)), pyfmt.Commas(int64(s.TokenBudget)), trimFloat(s.WindowHours))
		pauses := fmt.Sprintf("pauses at %s%%", trimFloat(s.ThresholdPct))
		switch {
		case s.Source == budget.SourceProvider && len(s.ProviderWindows) > 0:
			parts := make([]string, 0, len(s.ProviderWindows))
			for _, pw := range s.ProviderWindows {
				parts = append(parts, fmt.Sprintf("%s %s%% used, resets %s", pw.Window, trimFloat(pw.UsedPct), pw.ResetsAt))
			}
			fmt.Fprintf(&b, "%s — provider quota (account): %s; %s%s\n", name, strings.Join(parts, " · "), pauses, deferring(s))
			fmt.Fprintf(&b, "    this project's token window, for reference: %s\n", tokens)
		case s.Source == budget.SourceProvider:
			fmt.Fprintf(&b, "%s — provider quota (account): no fresh report yet, so the token window decides: %s (%.0f%%), %s%s\n",
				name, tokens, s.UsagePct, pauses, deferring(s))
		default:
			fmt.Fprintf(&b, "%s — token window (this project): %s (%.0f%%), %s%s\n", name, tokens, s.UsagePct, pauses, deferring(s))
		}
	}
	b.WriteString("\n" + budgetScopeNote + "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// deferring is the tail of a capped provider's line.
func deferring(s budget.ProviderSnapshot) string {
	if !s.Capped {
		return ""
	}
	if s.ResetAt == nil {
		return " — deferring"
	}
	return " — deferring until " + *s.ResetAt
}
