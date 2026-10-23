package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/scaffold"
	"github.com/hectorcanaimero/orch/internal/templates"
)

// newInitCmd ports `orch init` (orchestrator/init_cmd.py's `run_init_cli`).
//
// Two modes, and which one runs is decided the way Python decides it: passing
// any scaffolder argument — a path, `--template`, `--force`, `--sdd`,
// `--project-name` — means batch. Passing none means the operator typed
// `orch init` and wants to be asked.
//
// That rule is worth keeping rather than replacing with a `--yes` flag: the
// arguments themselves are the statement of intent. Someone who has typed out
// `--template python-api` has already answered the wizard's main question, and
// being asked it again is the tool not listening.
//
// Exit codes match Python's: 0 success or a declined confirm gate, 1 a
// conflict or a write failure.
func newInitCmd(flags *projectFlags) *cobra.Command {
	var (
		template    string
		projectName string
		force       bool
		sdd         bool
		noFindings  bool
		advanced    bool
	)

	cmd := &cobra.Command{
		Use:   "init [PATH]",
		Short: "Scaffold a new orch project",
		Long: "Scaffold a new orch project.\n\n" +
			"With no arguments, asks three questions (project id, template, confirm) and shows " +
			"everything it is about to write before writing any of it; --advanced asks the rest.\n" +
			"The project goes in the current directory, with the default models of the AI CLIs on PATH.\n" +
			"With a path or any of the flags below, scaffolds directly.\n\n" +
			"Templates: " + strings.Join(templates.Names(), ", "),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := scaffold.Options{
				Template: template,
				Name:     projectName,
				Force:    force,
				SDD:      sdd,

				NoReportFindings: noFindings,
			}
			if len(args) == 1 {
				opts.Root = args[0]
			}

			// Python's `_is_scaffolder_flag_provided`: a path or any of the
			// flags means batch.
			batch := opts.Root != "" || template != "" || projectName != "" || force || sdd || noFindings
			// Each installed CLI's current models, before the wizard shows a
			// tier default: a packaged route its CLI no longer lists is not
			// one (#397). Bounded per CLI; a failure keeps the packaged routes.
			var failed []model.Backend
			opts.Models, failed = scaffold.DiscoverModels(cmd.Context(), router.OnPath)
			var term *scaffold.WizardIO
			if !batch {
				var confirmed bool
				var err error
				term = scaffold.NewWizardIO(cmd.InOrStdin(), cmd.OutOrStdout())
				opts, confirmed, err = scaffold.Wizard(term, opts, advanced)
				if err != nil {
					return err
				}
				if !confirmed {
					// Declining is the gate working, not a failure. Python
					// returns 0 here too, and the wizard has already printed
					// "Aborted. Nothing written."
					return nil
				}
			}
			if opts.Root == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("resolving the current directory: %w", err)
				}
				opts.Root = cwd
			}
			if opts.BaseBranch == "" {
				opts.BaseBranch = repoBranch(opts.Root)
			}
			if opts.TierDefaults == nil {
				// Batch decides what the wizard would have: the models of the
				// CLIs that are installed (#376).
				tiers, err := scaffold.InstalledTierDefaults(router.OnPath, opts.Models)
				if err != nil {
					return err
				}
				opts.TierDefaults = tiers
			}

			res, err := scaffold.Run(opts)
			if err != nil {
				var conflict *scaffold.ConflictError
				if errors.As(err, &conflict) {
					// The conflict message is a list of paths and the flag
					// that overrides it — already the whole story, so it goes
					// out without a second sentence wrapped around it.
					return err
				}
				var notFound *templates.NotFoundError
				if errors.As(err, &notFound) {
					return err
				}
				return fmt.Errorf("scaffolding %s: %w", opts.Root, err)
			}

			recordInitTemplate(res.Root, opts.Template)
			printInitResult(cmd, res, opts, term != nil, scaffold.DiscoverySummary(router.OnPath, opts.Models, failed))
			// The wizard asks; batch names the command in its next steps. A
			// failure here is reported, but the project is already written.
			if term != nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
				if err := offerClaudePermissions(term.Confirm, cmd.OutOrStdout(), res.Root, false); err != nil {
					return fmt.Errorf("the project is initialized, but its claude settings were not: %w", err)
				}
				if err := offerAgyPermissionsIfInstalled(term.Confirm, cmd.OutOrStdout()); err != nil {
					return fmt.Errorf("the project is initialized, but agy's settings were not: %w", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&template, "template", "",
		"Project template ("+strings.Join(templates.Names(), ", ")+"); omit for a blank project")
	cmd.Flags().StringVar(&projectName, "project-name", "",
		"Name used in generated files; default = the directory name")
	cmd.Flags().BoolVar(&force, "force", false,
		"Overwrite an existing project's files")
	cmd.Flags().BoolVar(&sdd, "sdd", false,
		"Also scaffold the openspec/ layout")
	cmd.Flags().BoolVar(&noFindings, "no-report-findings", false,
		"Write report_findings.enabled: false (it is on by default: agents may file public issues about orch with your gh login)")
	cmd.Flags().BoolVar(&advanced, "advanced", false,
		"Also ask the project root, state backend, budget preset, spec root, tier models and openspec layout")
	return cmd
}

// printInitResult is the banner, ported from `_print_next_steps`.
//
// The router line comes first among the notes because it is the one that
// changed: `orch init` now routes a template's own models, and saying so is
// what stops an operator from wondering whether they still need to.
//
// asks says the wizard will offer the claude rules right after, so the step
// that names the command is left out.
func printInitResult(cmd *cobra.Command, res scaffold.Result, opts scaffold.Options, asks bool, models []string) {
	out := cmd.OutOrStdout()
	// A failed write to stdout is not something this command can act on —
	// the project is already scaffolded and the banner is the last thing it
	// does. The exit code, not this text, is what a script reads.
	say := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}

	say("")
	say("✓ orch project initialized at %s", res.Root)
	if len(res.RoutesAdded) > 0 {
		say("✓ model_router.yaml: added %d route(s) for the template's tasks — %s",
			len(res.RoutesAdded), strings.Join(res.RoutesAdded, ", "))
		say("  (tier defaults to `standard`; review it, it drives the budget gate)")
	}
	for _, key := range res.RoutesUninferable {
		say("! model_router.yaml: cannot infer a backend for %q (no `backend/` prefix) — add it by hand", key)
	}
	for _, warning := range res.Warnings {
		say("! %s", warning)
	}
	for _, line := range models {
		say("· models: %s", line)
	}

	say("")
	in := ""
	if wd, err := os.Getwd(); err != nil || wd != res.Root {
		in = " --project-root " + res.Root
	}
	if problem := baseProblem(res.Root, opts.BaseBranch); problem != "" {
		say("First: %s", problem)
		say("")
	}
	switch {
	case opts.Template != "":
		say("Next: run the tasks it came with (`orch tasks%s` lists them):", in)
		say("    orch run%s", in)
	case res.SampleTask != "":
		say("Next: watch an agent do the sample task (%s), then plan your own:", res.SampleTask)
		say("    orch run%s", in)
	default:
		say("Next: install an AI CLI (claude is the one orch knows best) and check it with `orch doctor`.")
	}
	say("")
	say("Plan what you want to build:")
	for _, line := range planSteps() {
		say("    %s", line)
	}
	if !asks {
		say("")
		say("Before the first run, let claude agents use orch's tools (it shows the rules and asks):")
		say("    orch claude-permissions%s", in)
	}
	say("")
	say("Config: %s/.orchestrator/config.yaml", res.Root)
	say("  One file. `budgets.yaml` and `dashboard.yaml` are optional overrides")
	say("  that deep-merge on top if you drop them in the project root.")
	if !opts.NoReportFindings {
		say("")
		say(reportFindingsNotice)
	}
}

// reportFindingsNotice is what init and start say about report_findings,
// which a new project has on (#399).
const reportFindingsNotice = "report_findings is on: agents may file public issues about orch with your gh login.\n" +
	"  Turn it off with report_findings.enabled: false in .orchestrator/config.yaml."

// planSteps is how init and an empty `orch run` say to plan the work:
// `orch start` (#377) installs the planning skills and has claude plan it.
func planSteps() []string {
	return []string{`orch start "<what you want to build>"`}
}

// repoBranch is the branch HEAD names in dir's repository — known even before
// the first commit — or "" outside a repository.
func repoBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output() // #nosec G204 -- fixed git arguments; dir is the project root
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// baseProblem says why task worktrees cannot branch from base in root — not a
// repository, or no commit on the base yet, the state `git init` leaves —
// with the command that fixes it; "" when they can. #376: a fresh repository
// blocked its first task on git's "invalid reference: main".
func baseProblem(root, base string) string {
	if base == "" {
		base = "main"
	}
	if exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Run() != nil { // #nosec G204 -- fixed git arguments
		return `each task works on its own git branch, and this is not a git repository yet: git init && git add -A && git commit -m "orch init"`
	}
	for _, ref := range []string{base, "origin/" + base} {
		if exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil { // #nosec G204 -- fixed git arguments; base comes from the project's config
			return ""
		}
	}
	return fmt.Sprintf(`each task branches from %s, which has no commit yet: git add -A && git commit -m "orch init"`, base)
}
