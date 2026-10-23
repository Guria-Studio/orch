package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newProviderCmd is `orch provider`: turn a backend off and on for this
// project. `orch run` leaves the tasks routed to a disabled backend waiting,
// as it does for a capped budget; the dashboard's Providers and quota switch
// runs the same function (project.Steer.SetProvider).
func newProviderCmd(flags *projectFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Turn a provider off and on: orch run leaves its tasks waiting while it is off",
	}
	set := func(disabled bool) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			steer, closeDB, err := openSteer(cmd, flags)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			reason, _ := cmd.Flags().GetString("reason")
			if err := steer.SetProvider(cmd.Context(), args[0], disabled, reason); err != nil {
				return withExitCode(2, err)
			}
			word := "on"
			if disabled {
				word = "off"
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", args[0], word)
			return err
		}
	}
	disable := &cobra.Command{
		Use:   "disable BACKEND [--reason TEXT]",
		Short: "Stop dispatching to a provider; its tasks wait until it is enabled again",
		Args:  cobra.ExactArgs(1),
		RunE:  set(true),
	}
	disable.Flags().String("reason", "", "Why, shown with the provider in orch provider list and the dashboard")
	enable := &cobra.Command{
		Use:   "enable BACKEND",
		Short: "Dispatch to a provider again",
		Args:  cobra.ExactArgs(1),
		RunE:  set(false),
	}

	var asJSON bool
	list := &cobra.Command{
		Use:   "list [--json]",
		Short: "List the providers turned off or back on, with the reason given",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			steer, closeDB, err := openSteer(cmd, flags)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			states, err := steer.Backend.ProviderStates(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				type row struct {
					Backend   string `json:"backend"`
					Disabled  bool   `json:"disabled"`
					Reason    string `json:"reason"`
					UpdatedAt string `json:"updated_at"`
				}
				rows := make([]row, 0, len(states))
				for _, p := range states {
					rows = append(rows, row{p.Backend, p.Disabled, p.Reason, p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			off := 0
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, p := range states {
				word := "on"
				if p.Disabled {
					word = "off"
					off++
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Backend, word, p.Reason)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if off == 0 {
				_, err = fmt.Fprintln(out, "every provider is on")
			}
			return err
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	cmd.AddCommand(disable, enable, list)
	return cmd
}
