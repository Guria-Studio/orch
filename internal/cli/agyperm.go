package cli

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/scaffold"
)

// agySettingsPath is doctor.AgySettingsPath; a variable so tests never touch
// the real one.
var agySettingsPath = doctor.AgySettingsPath

// newAgyPermissionsCmd is `orch agy-permissions`: add the agy allow rules
// orch's agents need to report a task, after showing them and asking (#383).
func newAgyPermissionsCmd() *cobra.Command {
	var dryRun, yes, all bool
	cmd := &cobra.Command{
		Use:   "agy-permissions",
		Short: "Allow agy agents scripts/task-finish.sh and task-block.sh",
		Long: "agy run by orch cannot ask before running a command, so it refuses\n" +
			"scripts/task-finish.sh and task-block.sh, and its agents cannot report a\n" +
			"task done or blocked. Show the rules agy's settings lack, then add them to\n" +
			"permissions.allow after asking. agy reads permissions only from its own\n" +
			"settings file in your home directory, so they apply to every agy session.\n" +
			"It only adds: no rule you wrote is removed or reordered.\n\n" +
			"agy also ends the agent's turn at the first other command it refuses (an ls,\n" +
			"a test run). --all-commands adds command(*), which lets agy run any command\n" +
			"without asking, in every agy session: what a task that runs its tests needs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in := bufio.NewReader(cmd.InOrStdin())
			ask := func(q string) (bool, error) { return askYesNo(in, cmd.OutOrStdout(), q, true) }
			if yes {
				ask = nil
			}
			return offerAgyPermissions(ask, cmd.OutOrStdout(), dryRun, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all-commands", false, "Also allow every command (command(*)), in every agy session")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the rules that would be added; write nothing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Add them without asking")
	return cmd
}

// offerAgyPermissions shows the missing rules and, unless dryRun, adds them
// once ask says yes (nil ask: without asking).
func offerAgyPermissions(ask func(question string) (bool, error), out io.Writer, dryRun, all bool) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }
	path := agySettingsPath()
	if path == "" {
		return fmt.Errorf("cannot find your home directory, where agy keeps its settings")
	}
	rules := doctor.AgyToolRules
	if all {
		rules = append([]string{doctor.AgyAllCommandsRule}, rules...)
	}
	missing := doctor.MissingAgyRules(path, rules)
	if len(missing) == 0 {
		say("agy settings already allow orch's scripts.")
		return nil
	}
	if all {
		say("agy will run any command its agents choose without asking — in every agy session, not only orch's.")
	}
	say("agy agents cannot report a task done or blocked until agy allows these scripts.")
	say("These rules would be added to permissions.allow in %s", path)
	say("(agy's own settings, outside the project: they apply to every agy session):")
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
			say("Nothing written. `%s` asks again; or add them by hand.", doctor.AgyPermissionsCommand)
			return nil
		}
	}
	added, err := scaffold.AllowAgyTools(path, rules)
	if err != nil {
		return err
	}
	say("Added %s to %s.", strings.Join(added, ", "), path)
	if !all && len(doctor.MissingAgyRules(path, []string{doctor.AgyAllCommandsRule})) > 0 {
		say("agy still ends the agent's turn at the first other command it refuses; "+
			"`%s --all-commands` allows them all.", doctor.AgyPermissionsCommand)
	}
	return nil
}

// offerAgyPermissionsIfInstalled is what init and start do: ask, only when
// agy is on PATH. Without a way to ask it names the command instead, because
// the file is the user's, not the project's.
func offerAgyPermissionsIfInstalled(ask func(question string) (bool, error), out io.Writer) error {
	if _, err := exec.LookPath("agy"); err != nil {
		return nil
	}
	if path := agySettingsPath(); path == "" || len(doctor.MissingAgyRules(path, doctor.AgyToolRules)) == 0 {
		return nil
	}
	_, _ = fmt.Fprintln(out)
	if ask == nil {
		_, _ = fmt.Fprintf(out, "agy is installed: run `%s` so its agents can report a task done.\n", doctor.AgyPermissionsCommand)
		return nil
	}
	return offerAgyPermissions(ask, out, false, false)
}
