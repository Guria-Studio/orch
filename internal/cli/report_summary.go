package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newReportSummaryCmd is `orch report summary` (#456): the receipt in plain
// words — what was built, what stopped and why, and the next steps with
// their commands — the same text the Now page shows under the receipt and
// `orch run` posts to the webhooks when a run ends.
func newReportSummaryCmd(flags *projectFlags) *cobra.Command {
	var (
		runID    string
		asJSON   bool
		language string
	)
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Say what a run built, what stopped and why, and what to do next",
		Long: "Print a run's summary in plain words — by default the latest one that\n" +
			"finished: what was built, why each blocked task stopped (its failure class,\n" +
			"the local verify, CI or its agent's note), and the next one to three steps\n" +
			"with the command for each. Built from the receipt, so it costs nothing.\n" +
			"Written in dashboard.language unless --language.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkLanguage(language); err != nil {
				return err
			}
			rec, _, _, cfg, done, err := loadRunReceipt(cmd, flags, runID)
			if err != nil {
				return err
			}
			defer done()
			if language == "" {
				language = cfg.Dashboard.SummaryLanguage
			}
			s := rec.Summary(language, cfg.GitHub.AutoMerge)
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), s)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), s.Text()); err != nil {
				return fmt.Errorf("write summary: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "Run id (default: the latest finished run)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the summary as JSON")
	cmd.Flags().StringVar(&language, "language", "", "Override dashboard.language (en|es|pt)")
	return cmd
}
