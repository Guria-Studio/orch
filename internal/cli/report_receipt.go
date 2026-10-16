package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pricing"
	"github.com/hectorcanaimero/orch/internal/receipt"
)

// newReportReceiptCmd is `orch report receipt`: one run's summary, the same
// one the dashboard's Now page shows, as Markdown to paste or JSON to script.
func newReportReceiptCmd(flags *projectFlags) *cobra.Command {
	var (
		runID       string
		asJSON      bool
		asMarkdown  bool
		asSVG       bool
		noBuiltWith bool
	)
	cmd := &cobra.Command{
		Use:   "receipt",
		Short: "Summarise a run: tasks done and blocked, time, spend per provider, PRs",
		Long: "Print the receipt of a run — by default the latest one that finished — as\n" +
			"Markdown (the default) or JSON. Spend is labelled reported, estimated\n" +
			"(pricing.yaml) or no data, per provider. The Markdown ends with a\n" +
			"\"Built with orch\" line unless --no-built-with. --svg prints the same run as\n" +
			"a 1200x630 image card (the Open Graph size) to post.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (asJSON && asMarkdown) || (asSVG && (asJSON || asMarkdown)) {
				return withExitCode(2, errors.New("--json, --markdown and --svg are mutually exclusive"))
			}
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return err
			}
			// A failed close loses nothing on a read, so it warns on stderr
			// rather than changing an exit code the receipt already earned.
			defer func() {
				if cerr := closeDB(); cerr != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[warn] closing the database: %v\n", cerr)
				}
			}()

			// Titles and the project name dress the receipt; the run is in the
			// database. A tasks.json that does not load is said, not fatal.
			tf, err := model.LoadTasksFile(paths.TasksJSON())
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[warn] reading tasks.json: %v — tasks are named by id\n", err)
			}
			rec, err := receipt.Load(ctx, backend, runID, receipt.Titles(tf.Tasks), pricing.Load(paths.Root))
			if err != nil {
				return err
			}
			if rec == nil {
				if runID != "" {
					return withExitCode(1, fmt.Errorf("no events for run %q", runID))
				}
				return withExitCode(1, errors.New("no run has finished yet — `orch run` writes a receipt when its loop ends"))
			}
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), rec)
			}
			if asSVG {
				project := tf.Meta.Project
				if project == "" {
					project = paths.ID
				}
				if _, err := fmt.Fprint(cmd.OutOrStdout(), rec.SVG(project, !noBuiltWith)); err != nil {
					return fmt.Errorf("write receipt: %w", err)
				}
				return nil
			}
			md := rec.Markdown()
			if !noBuiltWith {
				md += "\n" + receipt.BuiltWith + "\n"
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), md); err != nil {
				return fmt.Errorf("write receipt: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "Run id (default: the latest finished run)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the receipt as JSON")
	cmd.Flags().BoolVar(&asMarkdown, "markdown", false, "Print the receipt as Markdown (the default)")
	cmd.Flags().BoolVar(&asSVG, "svg", false, "Print the receipt as a 1200x630 SVG card to post")
	cmd.Flags().BoolVar(&noBuiltWith, "no-built-with", false, "Leave out the \"Built with orch\" line")
	return cmd
}
