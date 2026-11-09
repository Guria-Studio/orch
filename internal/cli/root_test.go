package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `orch --help` tells the same story as the README and the site (#544):
// agents as a team, a page for your client, and `orch start` as the first
// command. "stakeholder" is a profile name in config, never the word used.
func TestRootHelpTellsTheStory(t *testing.T) {
	root, _ := newRootCmd("test")
	help := root.Short + "\n" + root.Long
	for _, want := range []string{"agents as a team", "client", `orch start "`} {
		if !strings.Contains(help, want) {
			t.Errorf("root help lacks %q:\n%s", want, help)
		}
	}
	if i, j := strings.Index(root.Long, "orch start"), strings.Index(root.Long, "orch init"); j >= 0 && j < i {
		t.Errorf("root help names orch init before orch start:\n%s", root.Long)
	}
	if strings.Contains(strings.ToLower(help), "stakeholder") {
		t.Errorf("root help says stakeholder:\n%s", help)
	}
}

// orch ships several skills for four agent CLIs, not "the Claude Code
// skill" (#545).
func TestInstallSkillsHelpCountsSkillsAndTargets(t *testing.T) {
	root, _ := newRootCmd("test")
	cmd, _, err := root.Find([]string{"install-skills"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd.Short, "Claude Code skill") || !strings.Contains(cmd.Short, "cursor") {
		t.Errorf("install-skills help: %q", cmd.Short)
	}
}

// No command's help calls the client a stakeholder (#544, #546). The word
// survives only as a config key or profile value, written in backticks.
func TestNoHelpSaysStakeholder(t *testing.T) {
	root, _ := newRootCmd("test")
	backticked := regexp.MustCompile("`[^`]*`")
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		text := backticked.ReplaceAllString(c.Short+"\n"+c.Long, "")
		if strings.Contains(strings.ToLower(text), "stakeholder") {
			t.Errorf("%q help says stakeholder:\n%s", c.CommandPath(), text)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

func TestDashboardHelpSaysClient(t *testing.T) {
	root, _ := newRootCmd("test")
	cmd, _, err := root.Find([]string{"dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Long, "client") {
		t.Errorf("dashboard help never names the client:\n%s", cmd.Long)
	}
	// The flag value is the config alias; prose around it says client.
	for _, line := range strings.Split(cmd.Long, "\n") {
		if strings.Contains(strings.ToLower(line), "stakeholder") && !strings.Contains(line, "`stakeholder`") {
			t.Errorf("dashboard help says stakeholder outside the profile name: %q", line)
		}
	}
	// The local operator edits tasks, config, budgets and lessons from the
	// dashboard (#545): the help must not promise it never writes.
	if strings.Contains(cmd.Long, "never writes") {
		t.Errorf("dashboard help claims it never writes:\n%s", cmd.Long)
	}
	if u := cmd.Flags().Lookup("token").Usage; strings.Contains(u, "stakeholder") {
		t.Errorf("--token help says stakeholder: %q", u)
	}
}
