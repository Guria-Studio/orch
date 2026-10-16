package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CheckClaudeDoneChannel warns when the project's own claude settings deny a
// channel the dispatch prompt names for reporting a task done (#328).
//
// Either denial alone is enough to hurt: an agent with orch's MCP tools that
// cannot see orch_set_status is left with orch_block, and one without them
// falls back to scripts/task-finish.sh. Claude hides a denied MCP tool rather
// than refusing the call, which is how #336 read "the tool does not exist".
// orch never removes a rule; this only says which one to remove.
func CheckClaudeDoneChannel(root string) Check {
	const name = "claude.done_channel"
	var denied []string
	for _, f := range claudePermissions(root) {
		for _, rule := range f.Deny {
			if deniesDoneChannel(rule) {
				denied = append(denied, ".claude/"+f.File+": "+rule)
			}
		}
	}
	if len(denied) == 0 {
		return Check{Name: name, Status: StatusOK, Detail: "no claude settings deny task-finish.sh or orch_set_status"}
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail: "claude settings deny a done channel the dispatch prompt names — " + strings.Join(denied, "; "),
		Remediation: "Remove the rule from permissions.deny (allow `Bash(scripts/task-finish.sh:*)` and `mcp__orch__orch_set_status`). " +
			"Without them an agent can only exit and let orch record the done, and its note never reaches the tasks that depend on it.",
	}
}

// OrchToolRules are the claude allow rules the dispatch prompt's channels
// need: orch's MCP tools and the two scripts that report a task done or
// blocked. Without them an agent is refused every one of them (#361).
var OrchToolRules = []string{"mcp__orch", "Bash(scripts/task-finish.sh:*)", "Bash(scripts/task-block.sh:*)"}

// ClaudePermissionsCommand adds OrchToolRules to the project's settings.
const ClaudePermissionsCommand = "orch claude-permissions"

// MissingOrchRules is the part of OrchToolRules no project settings file
// allows, in OrchToolRules' order.
func MissingOrchRules(root string) []string {
	allowed := map[string]bool{}
	for _, f := range claudePermissions(root) {
		for _, rule := range f.Allow {
			allowed[rule] = true
		}
	}
	var missing []string
	for _, rule := range OrchToolRules {
		broad := allowed["mcp__orch__*"]
		if strings.HasPrefix(rule, "Bash(") {
			broad = allowed["Bash"] || allowed["Bash(*)"]
		}
		if !allowed[rule] && !broad {
			missing = append(missing, rule)
		}
	}
	return missing
}

// CheckClaudePermissions warns when a project whose tasks route to claude has
// not allowed orch's own tools: its agents are refused them, and claude hides
// a refused MCP tool rather than saying so.
func CheckClaudePermissions(root string, usesClaude bool) Check {
	const name = "claude.permissions"
	if !usesClaude {
		return Check{Name: name, Status: StatusSkip, Detail: "no task routes to claude"}
	}
	missing := MissingOrchRules(root)
	if len(missing) == 0 {
		return Check{Name: name, Status: StatusOK, Detail: "claude settings allow orch's tools"}
	}
	return Check{
		Name: name, Status: StatusWarn,
		Detail:      "claude agents will be refused orch's tools — not in .claude/settings.json: " + strings.Join(missing, ", "),
		Remediation: "Run `" + ClaudePermissionsCommand + "` (it shows the rules and asks before writing), or add them to permissions.allow by hand.",
	}
}

type claudePermissionFile struct {
	File        string
	Allow, Deny []string
}

// claudePermissions reads the permission rules of the project's two claude
// settings files; a missing or unreadable file contributes nothing.
func claudePermissions(root string) []claudePermissionFile {
	var out []claudePermissionFile
	for _, file := range []string{"settings.json", "settings.local.json"} {
		raw, err := os.ReadFile(filepath.Join(root, ".claude", file)) // #nosec G304 -- the project's own settings file
		if err != nil {
			continue
		}
		var doc struct {
			Permissions struct {
				Allow []string `json:"allow"`
				Deny  []string `json:"deny"`
			} `json:"permissions"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		out = append(out, claudePermissionFile{File: file, Allow: doc.Permissions.Allow, Deny: doc.Permissions.Deny})
	}
	return out
}

// deniesDoneChannel reports whether a claude deny rule covers
// scripts/task-finish.sh or mcp__orch__orch_set_status.
func deniesDoneChannel(rule string) bool {
	switch rule {
	case "Bash", "Bash(*)", "mcp__orch", "mcp__orch__*", "mcp__orch__orch_set_status":
		return true
	}
	return strings.HasPrefix(rule, "Bash(") && strings.Contains(rule, "task-finish.sh")
}
