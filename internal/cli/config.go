package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/config"
)

// newConfigCmd is the `orch config` parent. Python's `_run_config_subcommand`
// only implements `consolidate` (H-2, folding a project-root dashboard.yaml
// into config.yaml) — `show` has no Python counterpart. `consolidate` isn't
// ported here; this parent exists so `show` sits under the same `orch
// config <verb>` shape Python already has, ready for `consolidate` to join
// it later without a breaking CLI change.
func newConfigCmd(flags *projectFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Config helpers",
	}
	cmd.AddCommand(newConfigShowCmd(flags), newConfigSetCmd(flags), newConfigUnsetCmd(flags))
	return cmd
}

// newConfigSetCmd is `orch config set KEY VALUE [KEY VALUE…]`: config.Set,
// the same function the dashboard's Settings page saves through. All the
// pairs land or none does; the value is typed by the key, and a credential is
// confirmed without being echoed.
func newConfigSetCmd(flags *projectFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE [KEY VALUE...]",
		Short: "Set config.yaml keys (dotted, as docs/CONFIG.md names them), keeping its comments",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || len(args)%2 != 0 {
				return fmt.Errorf("want KEY VALUE pairs, got %d arguments", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolveAndValidate(flags)
			if err != nil {
				return err
			}
			values := map[string]string{}
			for i := 0; i < len(args); i += 2 {
				values[args[i]] = args[i+1]
			}
			if err := config.Set(paths.ConfigYAML, paths.Root, values); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for i := 0; i < len(args); i += 2 {
				shown := args[i+1]
				if config.IsSecret(args[i]) {
					shown = "(set)"
				}
				_, _ = fmt.Fprintf(out, "%s = %s\n", args[i], shown)
			}
			return nil
		},
	}
}

// newConfigUnsetCmd is `orch config unset KEY [KEY…]`: the keys go back to
// their defaults.
func newConfigUnsetCmd(flags *projectFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: "Remove config.yaml keys so they fall back to their defaults",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolveAndValidate(flags)
			if err != nil {
				return err
			}
			if err := config.Unset(paths.ConfigYAML, paths.Root, args); err != nil {
				return err
			}
			for _, key := range args {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s unset\n", key)
			}
			return nil
		},
	}
}

// newConfigShowCmd is `orch config show` — new in Go, a thin wrapper over
// the already-existing config.Show (its own doc comment: "`orch config
// show` is a three-line command on top of this"). Prints the effective
// config — defaults, file, and any dashboard.yaml/H-2 override merged — as
// YAML, followed by the sources it came from and any ignored keys. The
// point is answering "why is orch doing that?": a dump with no sources
// sends an operator looking in the wrong file.
func newConfigShowCmd(flags *projectFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective config, merged with defaults, and where each value came from",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolveAndValidate(flags)
			if err != nil {
				return err
			}
			res, err := config.Load(paths.ConfigYAML, paths.Root)
			if err != nil {
				return fmt.Errorf("config load failed: %w", err)
			}
			return config.Show(cmd.OutOrStdout(), res)
		},
	}
	return cmd
}
