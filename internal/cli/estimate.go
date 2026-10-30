package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/estimate"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/state"
)

// newEstimateCmd is `orch estimate`: what the next `orch run` would dispatch,
// cost and take, from this project's past attempts (#357).
func newEstimateCmd(flags *projectFlags) *cobra.Command {
	var (
		asJSON   bool
		only     string
		maxTasks int
	)
	cmd := &cobra.Command{
		Use:   "estimate",
		Short: "Tasks, cost and time the next `orch run` would take, before it starts",
		Long: "Estimate the next run: the tasks it would dispatch (todo tasks whose\n" +
			"dependencies are done or get done during the run), their cost per backend and\n" +
			"the wall time with concurrency.global_max slots. Each task costs the median of\n" +
			"this project's past attempts on its model (else on any model, else a stated\n" +
			"default), times the attempts a task has taken here on average.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return withExitCode(1, err)
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return withExitCode(1, err)
			}
			defer func() { _ = closeDB() }()
			tasks, err := project.Load(ctx, backend, paths.TasksJSON())
			if err != nil {
				return withExitCode(1, err)
			}
			e, err := projectEstimate(ctx, backend, paths, cfg, tasks, only, maxTasks)
			if err != nil {
				return withExitCode(1, err)
			}
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), e)
			}
			printEstimate(cmd.OutOrStdout(), e)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the estimate as JSON")
	cmd.Flags().StringVar(&only, "only", "", "As `orch run --only`: only tasks whose id matches this glob")
	cmd.Flags().IntVar(&maxTasks, "max-tasks", 0, "As `orch run --max-tasks`: at most this many tasks; 0 means no limit")
	return cmd
}

// projectEstimate gathers what estimate.Compute reads for a project whose
// tasks already carry their runtime status.
func projectEstimate(ctx context.Context, backend state.Backend, paths config.Paths, cfg config.Config,
	tasks []model.Task, only string, maxTasks int) (estimate.Estimate, error) {
	spend, err := backend.AllSpend(ctx, time.Time{})
	if err != nil {
		return estimate.Estimate{}, fmt.Errorf("reading past spend: %w", err)
	}
	return estimate.Compute(estimate.Input{
		Tasks: tasks, Routes: loadRouterTolerant(paths), Spend: spend,
		Prices: pricing.Load(paths.Root), Slots: cfg.Concurrency.GlobalMax,
		Only: only, MaxTasks: maxTasks, TypicalTokens: cfg.TypicalDispatchToken,
	}), nil
}

func printEstimate(w io.Writer, e estimate.Estimate) {
	for _, l := range e.Lines() {
		if l != "" {
			_, _ = fmt.Fprintln(w, l)
		}
	}
}
