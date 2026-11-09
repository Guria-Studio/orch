package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/findings"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/vcs"
)

// newFindingsFiler is what sends a finding; tests swap it.
var newFindingsFiler = func() findings.Filer { return vcs.NewGitHubProvider() }

// newFindingsCmd is `orch findings` (#536): the reports this project's agents
// queued about orch, previewed exactly as they would be filed, and sent or
// dismissed by the operator. The dashboard's Findings page does the same.
func newFindingsCmd(flags *projectFlags) *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "findings [--all] [--json]",
		Short: "Review the bugs your agents found in orch, and send or dismiss each",
		Long: "Agents report problems with orch itself through orch_report_finding when " +
			"report_findings.enabled is on. Nothing is public until you send it: this lists " +
			"the queued reports with the exact title and body `orch findings send` would file " +
			"on " + findings.Repo + " with your gh login, secrets already redacted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			backend, closeDB, err := openFindings(cmd, flags)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			rows, err := backend.QueuedFindings(cmd.Context())
			if err != nil {
				return err
			}
			if !all {
				rows = pendingFindings(rows)
			}
			if asJSON {
				return writeFindingsJSON(cmd.OutOrStdout(), rows)
			}
			return writeFindings(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Also list the findings already sent or dismissed")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print JSON")

	var sendAll bool
	send := &cobra.Command{
		Use:   "send ID... | --all",
		Short: "File the queued findings as issues on " + findings.Repo + " with your gh login",
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachFinding(cmd, flags, args, sendAll, func(b project.Backend, id int64) (string, error) {
				url, err := findings.Send(cmd.Context(), b, newFindingsFiler(), id)
				return "sent " + url, err
			})
		},
	}
	send.Flags().BoolVar(&sendAll, "all", false, "Send every queued finding")

	dismiss := &cobra.Command{
		Use:   "dismiss ID...",
		Short: "Drop queued findings without sending them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachFinding(cmd, flags, args, false, func(b project.Backend, id int64) (string, error) {
				return "dismissed", findings.Dismiss(cmd.Context(), b, id)
			})
		},
	}
	cmd.AddCommand(send, dismiss)
	return cmd
}

func openFindings(cmd *cobra.Command, flags *projectFlags) (project.Backend, func() error, error) {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return nil, nil, err
	}
	return openBackend(cmd.Context(), paths, cfg)
}

// eachFinding runs act on each id named, or on every queued one with all,
// printing one line per finding; the first failure is the exit code.
func eachFinding(cmd *cobra.Command, flags *projectFlags, args []string, all bool,
	act func(project.Backend, int64) (string, error)) error {
	if all == (len(args) > 0) {
		return withExitCode(2, fmt.Errorf("name the findings by id, or pass --all"))
	}
	backend, closeDB, err := openFindings(cmd, flags)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	var ids []int64
	if all {
		rows, err := backend.QueuedFindings(cmd.Context())
		if err != nil {
			return err
		}
		for _, f := range pendingFindings(rows) {
			ids = append(ids, f.ID)
		}
	}
	for _, a := range args {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return withExitCode(2, fmt.Errorf("%q is not a finding id", a))
		}
		ids = append(ids, id)
	}
	var first error
	for _, id := range ids {
		msg, err := act(backend, id)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "#%d: %v\n", id, err)
			if first == nil {
				first = withExitCode(1, err)
			}
			continue
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "#%d: %s\n", id, msg)
	}
	return first
}

func pendingFindings(rows []project.QueuedFinding) []project.QueuedFinding {
	out := []project.QueuedFinding{}
	for _, f := range rows {
		if f.Status == project.FindingQueued {
			out = append(out, f)
		}
	}
	return out
}

func writeFindings(w io.Writer, rows []project.QueuedFinding) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No findings waiting. Your agents queue them here when orch gets in their way.")
		return err
	}
	for _, f := range rows {
		head := fmt.Sprintf("#%d · %s · queued %s", f.ID, f.Status, f.QueuedAt.Local().Format("2006-01-02 15:04"))
		if f.TaskID != "" {
			head += " by " + f.TaskID
		}
		if f.URL != "" {
			head += " · " + f.URL
		}
		if _, err := fmt.Fprintf(w, "%s\nTitle: %s\n\n%s\n────\n", head, f.Title, f.Body); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Send: orch findings send ID (or --all) · drop: orch findings dismiss ID\n")
	return err
}

func writeFindingsJSON(w io.Writer, rows []project.QueuedFinding) error {
	type row struct {
		ID       int64  `json:"id"`
		Status   string `json:"status"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		TaskID   string `json:"task_id"`
		QueuedAt string `json:"queued_at"`
		URL      string `json:"url"`
	}
	out := make([]row, 0, len(rows))
	for _, f := range rows {
		out = append(out, row{f.ID, f.Status, f.Title, f.Body, f.TaskID, f.QueuedAt.UTC().Format("2006-01-02T15:04:05Z"), f.URL})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
