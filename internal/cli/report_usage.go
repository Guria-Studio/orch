package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/pyfmt"
	"github.com/Guria-Studio/orch/internal/usage"
)

// usageQuotaNote is what every usage view says about the numbers (#417).
const usageQuotaNote = "These are the tokens each dispatch reported, as orch recorded them. They are not " +
	"your provider plan's quota (a ChatGPT or Claude subscription's 5-hour window), which orch cannot see; " +
	"orch's budget window is its own guardrail on these numbers."

// newReportUsageCmd is `orch report usage` (#417): where the tokens went.
func newReportUsageCmd(flags *projectFlags) *cobra.Command {
	var (
		asJSON bool
		top    int
	)
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Split token use by kind, model, task and attempt, retries apart",
		Long: "Print the project's token use from orch's spend records: fresh input, cached\n" +
			"input (context re-read from the prompt cache), cache writes, output and the\n" +
			"reasoning part of it — in total, per model and per task, each task attempt by\n" +
			"attempt, with what retries used. --json prints all of it; the text shows the\n" +
			"--top heaviest tasks. " + usageQuotaNote,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			paths, cfg, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}
			backend, closeDB, err := openBackend(ctx, paths, cfg)
			if err != nil {
				return err
			}
			defer func() {
				if cerr := closeDB(); cerr != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[warn] closing the database: %v\n", cerr)
				}
			}()
			rows, err := backend.AllSpend(ctx, time.Time{})
			if err != nil {
				return err
			}
			u := usage.Build(rows)
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), u)
			}
			return writeUsage(cmd.OutOrStdout(), u, top)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the whole breakdown as JSON")
	cmd.Flags().IntVar(&top, "top", 10, "Tasks to list in the text, heaviest first")
	return cmd
}

func tokensLine(t usage.Tokens) string {
	n := func(v int) string { return pyfmt.Commas(int64(v)) }
	line := fmt.Sprintf("%s tokens: input %s · cached input %s · cache write %s · output %s (reasoning %s) · %d dispatch(es)",
		n(t.Total()), n(t.Input), n(t.CachedInput), n(t.CacheWrite), n(t.Output), n(t.Reasoning), t.Dispatches)
	if t.CostUSD > 0 {
		line += fmt.Sprintf(" · $%.2f reported", t.CostUSD)
	}
	if t.Estimated {
		line += " · some counts estimated"
	}
	return line
}

func writeUsage(w io.Writer, u usage.Usage, top int) error {
	var b strings.Builder
	if u.Total.Dispatches == 0 {
		b.WriteString("No dispatch has been recorded yet.\n")
	} else {
		fmt.Fprintf(&b, "Total: %s\n", tokensLine(u.Total))
		if u.RetryTokens > 0 {
			fmt.Fprintf(&b, "Retries: %s tokens (%.0f%% of the total)\n",
				pyfmt.Commas(int64(u.RetryTokens)), 100*float64(u.RetryTokens)/float64(max(u.Total.Total(), 1)))
		}
		b.WriteString("\nBy model\n")
		for _, m := range u.ByModel {
			fmt.Fprintf(&b, "  %s %s — %s\n", m.Backend, m.Model, tokensLine(m.Tokens))
		}
		fmt.Fprintf(&b, "\nBy task (heaviest %d of %d)\n", min(top, len(u.ByTask)), len(u.ByTask))
		for _, t := range u.ByTask[:min(top, len(u.ByTask))] {
			fmt.Fprintf(&b, "  %s — %s\n", t.TaskID, tokensLine(t.Tokens))
			if len(t.Attempts) > 1 {
				for _, a := range t.Attempts {
					label := fmt.Sprintf("attempt %d", a.Attempt)
					if a.Attempt == 0 {
						label = "before attempts were recorded"
					}
					fmt.Fprintf(&b, "      %s: %s\n", label, tokensLine(a.Tokens))
				}
			}
		}
	}
	b.WriteString("\n" + usageQuotaNote + "\n")
	_, err := io.WriteString(w, b.String())
	return err
}
