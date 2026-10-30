package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/budget"
	"github.com/Guria-Studio/orch/internal/config"
)

// newBudgetCmd is the `orch budget` parent: the rolling-window guardrail's
// limits, which the dashboard's Quota page edits through the same function.
func newBudgetCmd(flags *projectFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Change the rolling-window limits in budgets.yaml",
	}
	cmd.AddCommand(newBudgetSetCmd(flags))
	return cmd
}

// newBudgetSetCmd edits one provider's window in the active preset of the
// project's budgets.yaml. Only the flags given change. Exit 2 for a project
// or file that does not load, 1 for limits refused.
func newBudgetSetCmd(flags *projectFlags) *cobra.Command {
	var tokens int
	var window, threshold float64
	cmd := &cobra.Command{
		Use:   "set <provider>",
		Short: "Set a provider's token budget, window or threshold in the active preset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolveAndValidate(flags)
			if err != nil {
				return withExitCode(2, err)
			}
			res, err := config.Load(paths.ConfigYAML, paths.Root)
			if err != nil {
				return withExitCode(2, fmt.Errorf("config load failed: %w", err))
			}
			path := budget.ResolvePath(paths.Root, paths.ConfigYAML, res.Config.BudgetsConfig)
			if path == "" {
				return withExitCode(2, errors.New("budgets_config is empty in config.yaml: the guardrail is off, so there are no limits to set"))
			}
			var l budget.Limits
			if cmd.Flags().Changed("tokens") {
				l.TokenBudget = &tokens
			}
			if cmd.Flags().Changed("window-hours") {
				l.WindowHours = &window
			}
			if cmd.Flags().Changed("threshold") {
				l.ThresholdPct = &threshold
			}
			pb, err := budget.SetLimits(path, res.Config.BudgetsPreset, args[0], l)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d tokens per %sh window, pauses at %s%% (%s, preset %s)\n",
				args[0], pb.TokenBudget, trimFloat(pb.WindowHours), trimFloat(pb.ThresholdPct), path, res.Config.BudgetsPreset)
			return err
		},
	}
	cmd.Flags().IntVar(&tokens, "tokens", 0, "token_budget: tokens the window allows")
	cmd.Flags().Float64Var(&window, "window-hours", 0, "window_hours: the provider's rolling window")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "threshold_pct: the share of the budget at which dispatching pauses (0-100]")
	return cmd
}

func trimFloat(f float64) string { return fmt.Sprintf("%g", f) }
