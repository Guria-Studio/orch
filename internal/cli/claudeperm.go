package cli

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/scaffold"
)

// newClaudePermissionsCmd is `orch claude-permissions`: add the claude allow
// rules orch's own tools need to the project's .claude/settings.json, after
// showing them and asking (#361).
func newClaudePermissionsCmd(flags *projectFlags) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "claude-permissions",
		Short: "Allow claude agents orch's own tools in .claude/settings.json",
		Long: "Show the claude permission rules orch's agents need — orch's MCP tools and\n" +
			"scripts/task-finish.sh / task-block.sh — that the project's settings do not\n" +
			"allow yet, then add them to .claude/settings.json after asking. It only adds:\n" +
			"no rule you wrote is removed or reordered.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, _, err := loadProjectConfig(flags)
			if err != nil {
				return err
			}
			in := bufio.NewReader(cmd.InOrStdin())
			ask := func(q string) (bool, error) { return askYesNo(in, cmd.OutOrStdout(), q, true) }
			if yes {
				ask = nil
			}
			return offerClaudePermissions(ask, cmd.OutOrStdout(), paths.Root, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the rules that would be added; write nothing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Add them without asking")
	return cmd
}

// offerClaudePermissions shows the missing rules and, unless dryRun, adds
// them once ask says yes (nil ask: without asking). `orch init`'s wizard
// asks the same question through it.
func offerClaudePermissions(ask func(question string) (bool, error), out io.Writer, root string, dryRun bool) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }
	missing := doctor.MissingOrchRules(root)
	if len(missing) == 0 {
		say("claude settings already allow orch's tools.")
		return nil
	}
	path := filepath.Join(root, ".claude", "settings.json")
	say("Claude agents are refused orch's own tools until the project allows them.")
	say("These rules would be added to permissions.allow in %s:", path)
	for _, rule := range missing {
		say("  %s", rule)
	}
	if dryRun {
		return nil
	}
	if ask != nil {
		ok, err := ask("Add them?")
		if err != nil {
			return err
		}
		if !ok {
			say("Nothing written. `%s` asks again; or add them by hand.", doctor.ClaudePermissionsCommand)
			return nil
		}
	}
	added, err := scaffold.AllowOrchTools(root)
	if err != nil {
		return err
	}
	say("Added %s to %s.", strings.Join(added, ", "), path)
	return nil
}
