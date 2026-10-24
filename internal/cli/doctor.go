package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/budget"
	"github.com/hectorcanaimero/orch/internal/config"
	"github.com/hectorcanaimero/orch/internal/doctor"
	"github.com/hectorcanaimero/orch/internal/graph"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/router"
	"github.com/hectorcanaimero/orch/internal/scaffold"
	"github.com/hectorcanaimero/orch/internal/tunnel"
)

// doctorProjectJSON / doctorSummaryJSON / doctorPayload mirror
// orchestrator/doctor.py::build_doctor_report's JSON shape field-for-field
// (see internal/doctor's Check for why its own json tags already match
// CheckResult.as_json()) — a caller consuming `orch doctor --json` sees
// the same {project, backend, checks, summary, exit_code} envelope either
// binary produces, for the checks Go actually runs (see newDoctorCmd's
// doc comment for the narrower set).
type doctorProjectJSON struct {
	ID   string `json:"id"`
	Root string `json:"root"`
}

type doctorSummaryJSON struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
	Skip  int `json:"skip"`
}

type doctorPayload struct {
	Project  doctorProjectJSON `json:"project"`
	Backend  string            `json:"backend"`
	Checks   []doctor.Check    `json:"checks"`
	Summary  doctorSummaryJSON `json:"summary"`
	ExitCode int               `json:"exit_code"`
	// Verdict is the readiness report's last line (#392): can `orch run`
	// go ahead, and if not, the first check in the way.
	Verdict doctor.Verdict `json:"verdict"`
}

// newDoctorCmd wires internal/doctor's checks (G4.5) into a subcommand —
// the package itself has no CLI surface; this is that surface.
//
// Runs the seven Check* functions internal/doctor exports: backends
// (referenced by tasks.json + model_router.yaml), routes (routing
// completeness), budget preset, orphaned worktrees, VCS readiness, MCP
// config, and SQLite health. This is the narrower G4.5 cut documented in
// internal/doctor's own package comment — Python's build_doctor_report
// also runs check_config_files and check_scripts, which already have Go
// homes elsewhere (`orch validate`'s config/router/tasks load checks,
// resolveAndValidate's tasks.json+task-start.sh check) rather than being
// duplicated here.
//
// Tolerant like `orch validate`, not strict like most project-scoped
// commands: a project missing tasks.json or config.yaml is exactly the
// case this command exists to diagnose, so a load failure becomes a
// `config.parse`/`tasks.parse`-shaped Check, not a CLI error that exits
// before any check runs.
func newDoctorCmd(flags *projectFlags) *cobra.Command {
	var asJSON, smoke, keep bool
	var smokeBackend string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Environment preflight — provider CLIs, VCS, worktrees, budget config, SQLite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if smoke {
				return runSmoke(cmd, smokeBackend, asJSON, keep)
			}
			if smokeBackend != "" || keep {
				return withExitCode(2, fmt.Errorf("--backend and --keep need --smoke"))
			}
			paths, err := config.ResolvePaths(flags.root, flags.id, flags.configPath)
			if err != nil {
				return withExitCode(2, err)
			}

			var checks []doctor.Check
			cfg := config.Defaults()
			if res, cfgErr := config.Load(paths.ConfigYAML, paths.Root); cfgErr == nil {
				cfg = res.Config
			} else {
				checks = append(checks, doctor.Check{
					Name: "config.parse", Status: doctor.StatusError,
					Detail: fmt.Sprintf("config load failed: %v", cfgErr),
				})
			}

			var tasks []model.Task
			if tf, err := model.LoadTasksFile(paths.TasksJSON()); err == nil {
				tasks = tf.Tasks
				checks = append(checks, checkTasksValid(tasks))
			} else {
				checks = append(checks, doctor.Check{
					Name: "tasks.parse", Status: doctor.StatusError,
					Detail: fmt.Sprintf("tasks.json load failed: %v", err),
				})
			}

			var rtr router.Router
			if r, routerErr := router.Load(paths.RouterYAML()); routerErr == nil {
				rtr = r
			} else {
				checks = append(checks, doctor.Check{
					Name: "router.parse", Status: doctor.StatusError,
					Detail: fmt.Sprintf("router load failed: %v", routerErr),
				})
			}

			checks = append(checks, doctor.CheckRoutes(tasks, rtr))
			checks = append(checks, doctor.CheckDefaultModels(tierDefaults(paths.TasksJSON()), func(key string) string {
				return routeBackend(rtr, key)
			}))
			// Ask only the CLIs this router uses: each is a process, bounded.
			lists, _ := scaffold.DiscoverModels(cmd.Context(), func(b model.Backend) bool {
				for _, e := range rtr {
					if e.Backend == b {
						return router.OnPath(b)
					}
				}
				return false
			})
			checks = append(checks, doctor.CheckModelsAvailable(rtr, lists))
			checks = append(checks, doctor.CheckBackends(doctor.ReferencedBackends(tasks, rtr))...)
			checks = append(checks, doctor.CheckBudgetPreset(budget.ResolvePath(paths.Root, paths.ConfigYAML, cfg.BudgetsConfig), cfg.BudgetsPreset, cfg.TypicalDispatchToken)...)
			checks = append(checks, doctor.CheckOrphanWorktrees(paths.Root))
			checks = append(checks, doctor.CheckVCSReadiness(paths.Root, doctor.VCSConfig{
				WorktreeMode: cfg.Dispatch.WorktreeMode,
				AutoPR:       cfg.VCS.AutoPR,
				Provider:     cfg.VCS.Provider,
				Host:         cfg.VCS.Host,
			})...)
			checks = append(checks, doctor.CheckMCPConfig(paths.Root))
			checks = append(checks, doctor.CheckCIStack(paths.Root))
			if cfg.VCS.AutoPR {
				checks = append(checks, doctor.CheckCIMinutes(paths.Root, repoVisibility))
			}
			checks = append(checks, doctor.CheckClaudeDoneChannel(paths.Root))
			checks = append(checks, doctor.CheckPermissions(usedBackends(tasks, rtr, paths.TasksJSON()), paths.Root, agySettingsPath(), doctor.CodexExecHelp)...)
			checks = append(checks, doctor.CheckReadiness(hydrateForDoctor(context.Background(), paths, cfg, tasks))...)
			checks = append(checks, doctor.CheckComposeIsolation(paths.Root, cfg.Dispatch.IsolateCompose))
			checks = append(checks, doctor.CheckTunnel(cfg.Tunnel.Enabled, tunnel.LookupBinary(""), tunnel.ConfigBlocker(), tunnel.LookupBinary(tunnel.TailscaleCommand)))
			checks = append(checks, doctor.CheckSQLite(context.Background(), paths.SQLitePath(cfg)))
			if o, err := readOnboarding(context.Background(), paths, cfg); err == nil {
				checks = append(checks, doctor.CheckOnboarding(o))
			} else {
				checks = append(checks, doctor.Check{Name: "onboarding.progress", Status: doctor.StatusSkip,
					Detail: fmt.Sprintf("could not read the project's progress: %v", err)})
			}

			doctor.SortByName(checks)
			doctor.SetSections(checks)
			payload := buildDoctorPayload(paths, cfg, checks)

			if asJSON {
				if err := printCompactJSON(cmd.OutOrStdout(), payload); err != nil {
					return err
				}
			} else if err := renderDoctorReport(cmd, payload); err != nil {
				return err
			}
			if payload.ExitCode != 0 {
				return withSilentExitCode(payload.ExitCode)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the full doctor report as JSON on stdout")
	cmd.Flags().BoolVar(&smoke, "smoke", false,
		"Dispatch one throwaway task to every installed AI CLI through orch run's own path, in a temporary project, and check each adapter end to end (costs real money, a few cents)")
	cmd.Flags().StringVar(&smokeBackend, "backend", "", "With --smoke: only this CLI")
	cmd.Flags().BoolVar(&keep, "keep", false, "With --smoke: leave the temporary project (its logs and database) in place and print where")
	return cmd
}

func buildDoctorPayload(paths config.Paths, cfg config.Config, checks []doctor.Check) doctorPayload {
	if checks == nil {
		checks = []doctor.Check{}
	}
	summary := doctor.Summary(checks)
	backend := cfg.State.Backend
	if backend == "" {
		backend = "sqlite"
	}
	return doctorPayload{
		Project: doctorProjectJSON{ID: paths.ID, Root: paths.Root},
		Backend: backend,
		Checks:  checks,
		Summary: doctorSummaryJSON{
			OK: summary[doctor.StatusOK], Warn: summary[doctor.StatusWarn],
			Error: summary[doctor.StatusError], Skip: summary[doctor.StatusSkip],
		},
		ExitCode: doctor.ExitCode(checks),
		Verdict:  doctor.ReadinessVerdict(checks),
	}
}

// checkTasksValid is tasks.json's own shape: schema, dependencies, cycles —
// `orch validate`'s errors, routes left to models.resolve.
func checkTasksValid(tasks []model.Task) doctor.Check {
	c := doctor.Check{Name: "tasks.valid", Status: doctor.StatusOK, Detail: fmt.Sprintf("%d task(s), DAG valid", len(tasks))}
	for _, p := range graph.Validate(tasks, nil) {
		if p.Severity == graph.SeverityError {
			c.Status, c.Detail, c.Remediation = doctor.StatusError, p.Message, "`orch validate` lists every problem."
			break
		}
	}
	return c
}

// hydrateForDoctor gives tasks the database's status. A project with no
// database yet keeps tasks.json's (what `orch run` would seed); this does not
// create one.
func hydrateForDoctor(ctx context.Context, paths config.Paths, cfg config.Config, tasks []model.Task) []model.Task {
	if _, err := os.Stat(paths.SQLitePath(cfg)); err != nil {
		return tasks
	}
	backend, closeDB, err := openBackend(ctx, paths, cfg)
	if err != nil {
		return tasks
	}
	defer func() { _ = closeDB() }()
	if hydrated, err := project.Hydrate(ctx, backend, tasks); err == nil {
		return hydrated
	}
	return tasks
}

// renderDoctorReport is the human (non-JSON) renderer — plain tabular
// output, same convention as validate's: no byte-parity requirement for
// human mode.
func renderDoctorReport(cmd *cobra.Command, payload doctorPayload) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "orch doctor · project=%s · backend=%s\n",
		payload.Project.ID, payload.Backend); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "%d ok · %d warn · %d error · %d skip\n",
		payload.Summary.OK, payload.Summary.Warn, payload.Summary.Error, payload.Summary.Skip); err != nil {
		return err
	}
	// One block per section, in the order the verdict reads them (#392).
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	for _, section := range doctor.Sections {
		header := false
		for _, c := range payload.Checks {
			if c.Section != section {
				continue
			}
			if !header {
				header = true
				if _, err := fmt.Fprintf(w, "\n%s\n", strings.ToUpper(section[:1])+section[1:]); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "  %s\t%s\t%s\n", c.Status, c.Name, c.Detail); err != nil {
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if b := payload.Verdict.Blocker; b != nil {
		_, err := fmt.Fprintf(out, "\nNot ready to `orch run`: %s — %s\n", b.Name, b.Detail)
		if err == nil && b.Remediation != "" {
			_, err = fmt.Fprintf(out, "  %s\n", b.Remediation)
		}
		return err
	}
	_, err := fmt.Fprintln(out, "\nReady to `orch run`.")
	return err
}

// tierDefaults reads tasks.json's meta.default_<tier>_model keys, which
// `orch init` stamps; nil when the file or the keys are missing.
func tierDefaults(path string) map[string]string {
	tf, err := model.LoadTasksFile(path)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, tier := range []string{"premium", "standard", "cheap"} {
		var key string
		if raw, ok := tf.Meta.Extra["default_"+tier+"_model"]; ok && json.Unmarshal(raw, &key) == nil && key != "" {
			out[tier] = key
		}
	}
	return out
}
