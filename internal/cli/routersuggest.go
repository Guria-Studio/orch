package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/estimate"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/pricing"
	"github.com/Guria-Studio/orch/internal/router"
)

// routerSuggestPayload is `orch router suggest --json`.
type routerSuggestPayload struct {
	Since string `json:"since"`
	// From is the first instant counted, RFC 3339; "" for all of history.
	From      string                `json:"from"`
	MinSample int                   `json:"min_sample"`
	Kinds     []estimate.KindRoutes `json:"kinds"`
}

// newRouterSuggestCmd is `orch router suggest` (#457): which route works for
// which kind of task, from this project's own history, and the router
// changes that follow from it. Dry run unless --apply.
func newRouterSuggestCmd(flags *projectFlags) *cobra.Command {
	var (
		since              string
		asJSON, apply, yes bool
	)
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Suggest model_router.yaml changes from this project's dispatch history",
		Long: "For each route key tasks use and each route its attempts ran on: first-try\n" +
			"success, mean attempts per task, mean cost and duration per attempt, with sample\n" +
			"sizes. Suggests a cheaper route where first-try success is within 5 points at\n" +
			"under 90% of the cost, or a stronger one where the current route averages 1.5\n" +
			"attempts or more. Nothing is suggested for a route with fewer than 5 tasks.\n" +
			"--apply writes the suggestions to model_router.yaml after showing the diff.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, err := parseSince(since, time.Now())
			if err != nil {
				return withExitCode(2, err)
			}
			if asJSON && apply {
				return withExitCode(2, errors.New("--json and --apply cannot be combined"))
			}
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return withExitCode(2, err)
			}
			rtr, err := router.Load(paths.RouterYAML())
			if err != nil {
				return withExitCode(2, fmt.Errorf("load %s: %w", paths.RouterYAML(), err))
			}
			tf, err := model.LoadTasksFile(paths.TasksJSON())
			if err != nil {
				return withExitCode(2, fmt.Errorf("load %s: %w", paths.TasksJSON(), err))
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return withExitCode(1, err)
			}
			defer func() { _ = closeDB() }()
			spend, err := backend.AllSpend(ctx, from)
			if err != nil {
				return withExitCode(1, fmt.Errorf("reading past spend: %w", err))
			}
			events, err := backend.AllEvents(ctx, 0)
			if err != nil {
				return withExitCode(1, fmt.Errorf("reading events: %w", err))
			}
			kinds := estimate.SuggestRoutes(estimate.SuggestInput{Tasks: tf.Tasks, Routes: rtr,
				Spend: spend, Events: events, Prices: pricing.Load(paths.Root)})

			out := cmd.OutOrStdout()
			if asJSON {
				p := routerSuggestPayload{Since: since, MinSample: estimate.MinSample, Kinds: kinds}
				if !from.IsZero() {
					p.From = from.UTC().Format(time.RFC3339)
				}
				return printCompactJSON(out, p)
			}
			printRouteSuggestions(out, since, len(spend), kinds)

			var todo []*estimate.Suggestion
			for _, k := range kinds {
				if k.Suggestion != nil {
					todo = append(todo, k.Suggestion)
				}
			}
			if len(todo) == 0 {
				_, err := fmt.Fprintln(out, "\nNo changes to suggest.")
				return err
			}
			if !apply {
				_, err := fmt.Fprintln(out, "\nDry run: `orch router suggest --apply` writes these to model_router.yaml.")
				return err
			}
			if _, err := fmt.Fprintf(out, "\n%s:\n", paths.RouterYAML()); err != nil {
				return err
			}
			for _, s := range todo {
				old := rtr[s.Key]
				if _, err := fmt.Fprintf(out, "  %s:\n-   backend: %s\n-   cli_model: %s\n+   backend: %s\n+   cli_model: %s\n",
					s.Key, old.Backend, old.CLIModel, s.ToBackend, s.ToCLIModel); err != nil {
					return err
				}
			}
			if !yes {
				if _, err := fmt.Fprintf(out, "\nApply %d change(s)? [y/N] ", len(todo)); err != nil {
					return err
				}
				reply, err := readLine(cmd.InOrStdin())
				if err != nil {
					return err
				}
				if r := strings.ToLower(strings.TrimSpace(reply)); r != "y" && r != "yes" {
					if _, err := fmt.Fprintln(out, "Aborted. No changes written."); err != nil {
						return err
					}
					return withSilentExitCode(1)
				}
			}
			for _, s := range todo {
				if err := router.SetRoute(paths.RouterYAML(), s.Key, s.ToBackend, s.ToCLIModel); err != nil {
					return withExitCode(2, err)
				}
			}
			_, err = fmt.Fprintf(out, "Updated %d route(s). Review each one's tier: it drives the budget and the semi gate.\n", len(todo))
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "History window: Nd (days), a Go duration such as 72h, or all")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the table as JSON")
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the suggested changes to model_router.yaml (asks first)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "With --apply, skip the confirmation prompt")
	return cmd
}

// parseSince is --since as the first instant counted: the zero Time for
// "all", else now minus N days (`30d`) or a Go duration (`72h`).
func parseSince(raw string, now time.Time) (time.Time, error) {
	if raw == "all" {
		return time.Time{}, nil
	}
	var d time.Duration
	if n, err := strconv.Atoi(strings.TrimSuffix(raw, "d")); err == nil && strings.HasSuffix(raw, "d") {
		d = time.Duration(n) * 24 * time.Hour
	} else if d, err = time.ParseDuration(raw); err != nil {
		d = 0
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("--since must be Nd, a duration such as 72h, or all (got %q)", raw)
	}
	return now.Add(-d), nil
}

func printRouteSuggestions(out io.Writer, since string, attempts int, kinds []estimate.KindRoutes) {
	_, _ = fmt.Fprintf(out, "Route history, %s: %d attempt(s) across %d route key(s). Minimum sample: %d tasks.\n",
		since, attempts, len(kinds), estimate.MinSample)
	for _, k := range kinds {
		current := k.Current
		if current == "" {
			current = "no route"
		}
		_, _ = fmt.Fprintf(out, "\n%s  (now %s)\n", k.Kind, current)
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "  route\ttasks\tfirst-try\tattempts/task\tattempts\tcost/attempt\ttime/attempt")
		for _, r := range k.Routes {
			mark := " "
			if r.Current {
				mark = "*"
			}
			cost := "—" // no row had a cost to resolve
			if r.MeanCostUSD > 0 {
				cost = fmt.Sprintf("$%.4f", r.MeanCostUSD)
			}
			_, _ = fmt.Fprintf(w, "%s %s\t%d\t%.0f%%\t%.2f\t%d\t%s\t%s\n", mark, r.Route, r.Tasks,
				r.FirstTry*100, r.MeanAttempts, r.Attempts, cost, estimate.Duration(r.MeanDurationS))
		}
		_ = w.Flush()
		if s := k.Suggestion; s != nil {
			_, _ = fmt.Fprintf(out, "  → suggest %s: %s\n", s.To, s.Reason)
		}
	}
}
