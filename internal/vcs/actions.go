package vcs

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ghIn runs gh in dir — gh reads which repository it is about from dir's git
// remote (see runIn). A variable so tests can fake GitHub.
var ghIn = func(dir string, args ...string) (string, error) {
	if err := checkBinary("gh"); err != nil {
		return "", err
	}
	return runIn(dir, "gh", nil, args...)
}

// RepoVisibility is the GitHub repository of dir's remote: "public",
// "private" or "internal".
func RepoVisibility(dir string) (string, error) {
	out, err := ghIn(dir, "repo", "view", "--json", "visibility", "-q", ".visibility")
	if err != nil {
		return "", fmt.Errorf("asking GitHub whether the repository is private: %w", err)
	}
	return strings.ToLower(strings.TrimSpace(out)), nil
}

// ActionsUsage is what a repository's GitHub Actions runs took since a date.
type ActionsUsage struct {
	Since time.Time `json:"since"`
	Runs  int       `json:"runs"`
	// Minutes is billable minutes: each job's duration rounded up to the
	// minute, as GitHub bills Linux runners. macOS and Windows runners cost
	// more per minute; orch's workflow uses neither.
	Minutes    int            `json:"minutes"`
	ByWorkflow map[string]int `json:"by_workflow"`
	// PullRequests counts the pull-request branches that ran;
	// MinutesPerPR is their average, what one more task's PR costs.
	PullRequests int     `json:"pull_requests"`
	MinutesPerPR float64 `json:"minutes_per_pr"`
}

// ActionsMinutes adds up the jobs of every workflow run since since. One API
// call per run: a month of an orch project is tens of runs, not thousands.
func ActionsMinutes(dir string, since time.Time) (ActionsUsage, error) {
	u := ActionsUsage{Since: since, ByWorkflow: map[string]int{}}
	out, err := ghIn(dir, "api", "--paginate",
		"repos/{owner}/{repo}/actions/runs?per_page=100&created=>="+since.Format("2006-01-02"),
		"-q", `.workflow_runs[] | [.id, .name, .event, .head_branch] | @tsv`)
	if err != nil {
		return u, fmt.Errorf("listing the workflow runs: %w", err)
	}
	perBranch := map[string]int{}
	for _, run := range parseTSV(out, 4) {
		jobs, err := ghIn(dir, "api", "--paginate",
			"repos/{owner}/{repo}/actions/runs/"+run[0]+"/jobs?per_page=100",
			"-q", `.jobs[] | [.started_at, .completed_at] | @tsv`)
		if err != nil {
			return u, fmt.Errorf("reading the jobs of run %s: %w", run[0], err)
		}
		m := billableMinutes(jobs)
		u.Runs++
		u.Minutes += m
		u.ByWorkflow[run[1]] += m
		if run[2] == "pull_request" {
			perBranch[run[3]] += m
		}
	}
	u.PullRequests = len(perBranch)
	if u.PullRequests > 0 {
		total := 0
		for _, m := range perBranch {
			total += m
		}
		u.MinutesPerPR = math.Round(float64(total)/float64(u.PullRequests)*10) / 10
	}
	return u, nil
}

// billableMinutes adds up "started<TAB>completed" job lines, each rounded up
// to a whole minute. A job that never started or has not finished bills
// nothing yet.
func billableMinutes(tsv string) int {
	total := 0
	for _, f := range parseTSV(tsv, 2) {
		start, err1 := time.Parse(time.RFC3339, f[0])
		end, err2 := time.Parse(time.RFC3339, f[1])
		if err1 != nil || err2 != nil || !end.After(start) {
			continue
		}
		total += int(math.Ceil(end.Sub(start).Minutes()))
	}
	return total
}

func parseTSV(out string, n int) [][]string {
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Split(line, "\t"); len(f) == n && f[0] != "" {
			rows = append(rows, f)
		}
	}
	return rows
}

// FreeMinutesNote is what GitHub's plans include for private repositories;
// public ones are not billed.
const FreeMinutesNote = "GitHub Free includes 2,000 Actions minutes a month for private repositories, Pro and Team 3,000"

// SortedWorkflows lists ByWorkflow's names, most minutes first.
func (u ActionsUsage) SortedWorkflows() []string {
	names := make([]string, 0, len(u.ByWorkflow))
	for n := range u.ByWorkflow {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if u.ByWorkflow[names[i]] != u.ByWorkflow[names[j]] {
			return u.ByWorkflow[names[i]] > u.ByWorkflow[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

