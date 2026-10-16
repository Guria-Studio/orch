package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/project"
	"github.com/hectorcanaimero/orch/internal/vcs"
)

// actionsMinutes is vcs.ActionsMinutes; a variable so tests do not ask
// GitHub.
var actionsMinutes = vcs.ActionsMinutes

// newCIUsageCmd is `orch ci usage`: what this month's GitHub Actions runs
// billed, and what the tasks still to do would add at that rate (#363).
func newCIUsageCmd(flags *projectFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "This month's GitHub Actions minutes, and what the remaining tasks would add",
		Long: "Add up the billable minutes of this month's workflow runs (each job rounded up\n" +
			"to a minute, the way GitHub bills Linux runners), per workflow and per pull\n" +
			"request, and estimate what the tasks not done yet would add — orch opens one\n" +
			"pull request per task. Public repositories are not billed; it says so.\n" +
			"Needs gh, logged in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := ciRoot(flags)
			if err != nil {
				return err
			}
			visibility, err := repoVisibility(root)
			if err != nil {
				return withExitCode(1, err)
			}
			now := time.Now().UTC()
			u, err := actionsMinutes(root, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
			if err != nil {
				return withExitCode(1, err)
			}
			remaining := remainingTasks(cmd, flags)
			out := ciUsage{ActionsUsage: u, Visibility: visibility, Billed: visibility != "public", RemainingTasks: remaining}
			if remaining >= 0 && u.PullRequests > 0 {
				out.EstimatedMinutes = int(u.MinutesPerPR*float64(remaining) + 0.5)
			}
			if asJSON {
				return printCompactJSON(cmd.OutOrStdout(), out)
			}
			return printCIUsage(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the usage as JSON")
	return cmd
}

type ciUsage struct {
	vcs.ActionsUsage
	Visibility string `json:"visibility"`
	Billed     bool   `json:"billed"`
	// RemainingTasks is -1 when the directory is not an orch project.
	RemainingTasks   int `json:"remaining_tasks"`
	EstimatedMinutes int `json:"estimated_minutes"`
}

// remainingTasks counts the tasks not done and not in the backlog, with their
// status from the database; -1 when there is no orch project to read.
func remainingTasks(cmd *cobra.Command, flags *projectFlags) int {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return -1
	}
	backend, closeDB, err := openBackend(cmd.Context(), paths, cfg)
	if err != nil {
		return -1
	}
	defer func() { _ = closeDB() }()
	tasks, err := project.Load(cmd.Context(), backend, paths.TasksJSON())
	if err != nil {
		return -1
	}
	n := 0
	for _, t := range tasks {
		if t.Status != model.StatusDone && t.Status != model.StatusBacklog {
			n++
		}
	}
	return n
}

func printCIUsage(w io.Writer, u ciUsage) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }
	say("GitHub Actions since %s: %d minutes over %d runs.", u.Since.Format("Jan 2"), u.Minutes, u.Runs)
	for _, name := range u.SortedWorkflows() {
		say("  %-24s %d min", name, u.ByWorkflow[name])
	}
	if u.PullRequests > 0 {
		say("Pull requests: %d, %.1f minutes each on average.", u.PullRequests, u.MinutesPerPR)
	}
	if u.RemainingTasks > 0 && u.PullRequests > 0 {
		say("The %d tasks still to do: about %d more minutes (one PR each, retries not counted).", u.RemainingTasks, u.EstimatedMinutes)
	}
	if !u.Billed {
		say("This repository is public: GitHub does not bill its Actions minutes.")
		return nil
	}
	say("This repository is %s: its minutes count against the account's plan. %s.", u.Visibility, vcs.FreeMinutesNote)
	return nil
}
