package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/router"
	"github.com/Guria-Studio/orch/internal/state"
)

// openTaskEditor builds the project's TaskEditor (#390, v0.22): the one path
// that changes a task's definition in tasks.json, the dashboard's too.
func openTaskEditor(cmd *cobra.Command, flags *projectFlags) (project.TaskEditor, func() error, error) {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return project.TaskEditor{}, nil, err
	}
	backend, closeDB, err := openBackend(cmd.Context(), paths, cfg)
	if err != nil {
		return project.TaskEditor{}, nil, err
	}
	if err := backend.Bootstrap(cmd.Context(), loadDAG(paths)); err != nil {
		_ = closeDB()
		return project.TaskEditor{}, nil, fmt.Errorf("bootstrap: %w", err)
	}
	return project.TaskEditor{
		TasksJSON: paths.TasksJSON(), StateDir: paths.StateDir(),
		Routes: routerKeys(paths), Backend: backend, Source: "cli",
	}, closeDB, nil
}

// routerKeys are model_router.yaml's keys, the models a task may name; nil
// (no check) when there is no router.
func routerKeys(paths config.Paths) []string {
	if rtr, err := router.Load(paths.RouterYAML()); err == nil {
		return rtr.Keys()
	}
	return nil
}

// editErr maps an edit's errors onto exit codes: 1 for a refused edit.
func editErr(id string, err error) error {
	if errors.Is(err, project.ErrInvalidEdit) || errors.Is(err, state.ErrTaskNotFound) {
		return withExitCode(1, fmt.Errorf("%s: %w", id, err))
	}
	return err
}

// taskFields are the definition flags `task add` and `task edit` share.
type taskFields struct {
	title, model, description, reason, specRef, deps, files string
	phase                                                   int
	estimate                                                float64
}

func (f *taskFields) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.title, "title", "", "Title")
	fs.StringVar(&f.model, "model", "", "Model: a model_router.yaml key, e.g. claude/claude-sonnet-4-6")
	fs.StringVar(&f.description, "description", "", "Description: the brief the agent gets")
	fs.StringVar(&f.reason, "reason", "", "Why this model (or anything worth saying)")
	fs.StringVar(&f.specRef, "spec-ref", "", "Spec ref, e.g. f1-auth.md#F1.1.T1")
	fs.StringVar(&f.deps, "deps", "", "Dependencies, comma-separated task ids")
	fs.StringVar(&f.files, "files", "", "Files the agent may write, comma-separated")
	fs.IntVar(&f.phase, "phase", 0, "Phase number")
	fs.Float64Var(&f.estimate, "estimate", 0, "Estimate in hours (it sets the dispatch timeout)")
}

// patch is what the flags given on the command line change.
func (f taskFields) patch(fs *pflag.FlagSet) project.TaskPatch {
	var p project.TaskPatch
	str := map[string]**string{"title": &p.Title, "model": &p.Model, "description": &p.Description, "reason": &p.Reason, "spec-ref": &p.SpecRef}
	val := map[string]string{"title": f.title, "model": f.model, "description": f.description, "reason": f.reason, "spec-ref": f.specRef}
	for name, dst := range str {
		if fs.Changed(name) {
			v := val[name]
			*dst = &v
		}
	}
	if fs.Changed("deps") {
		v := splitList(f.deps)
		p.Dependencies = &v
	}
	if fs.Changed("files") {
		v := splitList(f.files)
		p.Files = &v
	}
	if fs.Changed("phase") {
		p.Phase = &f.phase
	}
	if fs.Changed("estimate") {
		p.EstimateHours = &f.estimate
	}
	return p
}

func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func newTaskAddCmd(flags *projectFlags) *cobra.Command {
	var id string
	var backlog bool
	var f taskFields
	cmd := &cobra.Command{
		Use:   "add --id ID --title TITLE --model MODEL",
		Short: "Add a task to tasks.json, validated against the DAG and the router",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, closeDB, err := openTaskEditor(cmd, flags)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			t := model.Task{
				ID: id, Title: f.title, Model: f.model, Description: f.description, Reason: f.reason,
				SpecRef: f.specRef, Phase: f.phase, EstimateHours: f.estimate,
				Dependencies: splitList(f.deps), Files: splitList(f.files),
			}
			if backlog {
				t.Status = model.StatusBacklog
			}
			added, err := e.Add(cmd.Context(), t)
			if err != nil {
				return editErr(id, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "task %s added (%s, %s)\n", added.ID, added.Status, added.Model)
			return err
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Task ID, e.g. F2.1.T4")
	cmd.Flags().BoolVar(&backlog, "backlog", false, "Add it as backlog, not ready to dispatch")
	f.register(cmd.Flags())
	for _, name := range []string{"id", "title", "model"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func newTaskEditCmd(flags *projectFlags) *cobra.Command {
	var id string
	var f taskFields
	cmd := &cobra.Command{
		Use:   "edit --id ID [--title …] [--model …] [--deps …] …",
		Short: "Change a task's definition in tasks.json; only the flags given change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := f.patch(cmd.Flags())
			if p == (project.TaskPatch{}) {
				return withExitCode(1, errors.New("nothing to change: pass at least one of --title, --model, --description, --reason, --spec-ref, --deps, --files, --phase, --estimate"))
			}
			e, closeDB, err := openTaskEditor(cmd, flags)
			if err != nil {
				return err
			}
			defer func() { _ = closeDB() }()
			if _, err := e.Update(cmd.Context(), id, p); err != nil {
				return editErr(id, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "task %s updated\n", id)
			return err
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Task ID, e.g. F1.1.T3")
	f.register(cmd.Flags())
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
