package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/project"
)

// newMetricsCmd is `orch metrics` (#538): how good the agents' work is, from
// every recorded attempt. The dashboard's /api/agent-metrics serves the same.
func newMetricsCmd(flags *projectFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "metrics [--json]",
		Short: "First-attempt rate, retries and cost per done task, verify send-backs, review findings and CI after verify, by route and tier",
		Long: "Reads the attempts table, one row per finished attempt (recorded since orch v0.31). " +
			"Task figures count a task under the route and tier of its first attempt; " +
			"attempt figures count each attempt where it ran.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := openProject(cmd.Context(), flags)
			if err != nil {
				return err
			}
			defer func() { _ = p.Close() }()
			attempts, err := p.Backend.Attempts(cmd.Context())
			if err != nil {
				return err
			}
			tasks, err := project.Runtime(cmd.Context(), p.Backend)
			if err != nil {
				return err
			}
			m := project.ComputeMetrics(attempts, tasks)
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(m)
			}
			return writeMetrics(cmd.OutOrStdout(), m)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	return cmd
}

func writeMetrics(w io.Writer, m project.Metrics) error {
	if m.Total.Attempts == 0 {
		_, err := fmt.Fprintln(w, "No attempts recorded yet: orch keeps one row per finished attempt from v0.31 on.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "GROUP\tATTEMPTS\tDONE\tFIRST-TRY\tRETRIES/DONE\tCOST/DONE\tSEND-BACK\tFINDINGS FIXED/IGNORED\tCI FAILED AFTER VERIFY\tRAN OTHER MODEL")
	row := func(name string, r project.MetricsRow) {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d/%d\t%s\t%.2f\t$%.2f\t%s\t%d/%d\t%s\t%d\n", name, r.Attempts, r.DoneTasks, r.Tasks,
			pct(r.FirstAttemptRate, r.DoneTasks), r.RetriesPerDone, r.CostPerDone,
			pct(r.SendBackRate, r.Verified), r.FindingsFixed, r.FindingsIgnored, pct(r.CIFailRate, r.CIAfterVerify), r.ModelMismatches)
	}
	row("total", m.Total)
	for _, r := range m.ByRoute {
		row("route "+r.Key, r)
	}
	for _, r := range m.ByTier {
		key := r.Key
		if key == "" {
			key = "(none)"
		}
		row("tier "+key, r)
	}
	return tw.Flush()
}

// pct is rate as a percentage, or "-" when nothing was counted.
func pct(rate float64, of int) string {
	if of == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%% of %d", rate*100, of)
}
