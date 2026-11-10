package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/project"
)

// newRadarCmd is `orch radar`, the conflict radar's (#482) own commands.
func newRadarCmd(flags *projectFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "radar",
		Short: "How well the conflict radar predicted the collisions",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newRadarReportCmd(flags))
	return cmd
}

// radarReportPayload is `orch radar report --json`.
type radarReportPayload struct {
	Since string `json:"since"`
	Radar string `json:"radar"`
	project.RadarReport
}

// newRadarReportCmd is `orch radar report` (#522): how many collisions the
// radar's warnings predicted and how many slipped through.
func newRadarReportCmd(flags *projectFlags) *cobra.Command {
	var (
		since  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Collisions the conflict radar predicted and the ones that slipped through",
		Long: "Reads the event log. Warnings are the radar's files_overlap and branch_conflict\n" +
			"events, each about a pair of tasks. Collisions are PRs that conflicted with their\n" +
			"base (sent to the agent or blocked), local lands that conflicted, clean base\n" +
			"merges that broke the local verify, and semantic collisions that stopped a merge.\n" +
			"A collision is predicted when an earlier warning names its task and either the\n" +
			"task it collided with or a file in common. Recall: predicted collisions over all\n" +
			"collisions. Precision: warned pairs that collided over warned pairs. Read-only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, err := parseSince(since, time.Now())
			if err != nil {
				return withExitCode(2, err)
			}
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return withExitCode(2, err)
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return withExitCode(1, err)
			}
			defer func() {
				if cerr := closeDB(); cerr != nil {
					cmd.PrintErrf("[warn] closing the database: %v\n", cerr)
				}
			}()
			events, err := backend.AllEvents(ctx, 0)
			if err != nil {
				return withExitCode(1, fmt.Errorf("reading events: %w", err))
			}
			rep := project.RadarEffectiveness(events, from)
			radar := cfg.Radar
			if radar == "" {
				radar = "off"
			}
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), radarReportPayload{Since: since, Radar: radar, RadarReport: rep})
			}
			return writeRadarReport(cmd.OutOrStdout(), since, radar, rep)
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "Window: Nd (days), a Go duration such as 72h, or all")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the report as JSON")
	return cmd
}

// ratio is n of d as a percentage, or "—" with nothing counted.
func ratio(r *float64) string {
	if r == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *r*100)
}

// byKindLine is "kind n, kind n", in kind order.
func byKindLine(m map[string]int) string {
	kinds := make([]string, 0, len(m))
	for k := range m {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func writeRadarReport(w io.Writer, since, radar string, r project.RadarReport) error {
	var b strings.Builder
	window := "all of history"
	if r.From != "" {
		window = "since " + r.From + " (" + since + ")"
	}
	fmt.Fprintf(&b, "Conflict radar (radar: %s), %s\n\n", radar, window)
	p := r.Predictions
	fmt.Fprintf(&b, "Warnings:   %d (files_overlap %d, branch_conflict %d) about %d pair(s) of tasks; %d pair(s) collided later\n",
		p.FilesOverlap+p.BranchConflict, p.FilesOverlap, p.BranchConflict, p.Pairs, p.PairsCollided)
	c := r.Collisions
	fmt.Fprintf(&b, "Collisions: %d", c.Total)
	if c.Total > 0 {
		fmt.Fprintf(&b, " (%s)", byKindLine(c.ByKind))
	}
	fmt.Fprintf(&b, "; %d predicted, %d escaped\n", c.Predicted, c.Escaped)
	fmt.Fprintf(&b, "Recall:     %s of the collisions had a warning first\n", ratio(r.Recall))
	fmt.Fprintf(&b, "Precision:  %s of the warned pairs collided\n", ratio(r.Precision))
	if radar == "off" && p.Pairs == 0 {
		b.WriteString("\nThe radar is off in config.yaml (radar: warn or serialize turns it on), so nothing could be predicted.\n")
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	if len(r.Events) == 0 {
		return nil
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "AT\tTASK\tKIND\tFILES\tPREDICTED BY"); err != nil {
		return err
	}
	for _, e := range r.Events {
		files := strings.Join(e.Files, ", ")
		if files == "" {
			files = "—"
		}
		by := "escaped"
		if e.Predicted {
			by = strings.Join(e.PredictedBy, "; ") + " (" + e.Match + ")"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.At, e.TaskID, e.Kind, files, by); err != nil {
			return err
		}
	}
	return tw.Flush()
}
