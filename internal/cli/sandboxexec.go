package cli

import (
	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/sandbox"
)

// sandboxExec is sandbox.Exec, swapped by the test that checks what the
// command hands it.
var sandboxExec = sandbox.Exec

// newSandboxExecCmd is the launcher dispatch.sandbox wraps an agent in
// (#481): `orch sandbox-exec --rw <dir> … --ro <dir> … -- <cli> <args>`
// confines itself with Landlock and execs the CLI in its place. Hidden: the
// engine spawns it, nobody types it, and its flags are internal.
func newSandboxExecCmd() *cobra.Command {
	var p sandbox.Policy
	cmd := &cobra.Command{
		Use:    sandbox.Command + " [--rw path]... [--ro path]... -- <command> [args]",
		Short:  "Run a command confined by Landlock (used by dispatch.sandbox)",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		// Only returns when nothing was exec'd.
		RunE: func(_ *cobra.Command, args []string) error {
			return withExitCode(126, sandboxExec(p, args))
		},
	}
	cmd.Flags().StringArrayVar(&p.RW, "rw", nil, "path the command may read and write")
	cmd.Flags().StringArrayVar(&p.RO, "ro", nil, "path the command may read and execute")
	return cmd
}
