package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// claudeSettingsFiles are the project-level settings claude loads from its
// working directory, lowest precedence first.
var claudeSettingsFiles = []string{"settings.json", "settings.local.json"}

// projectClaudeSettings returns the project's claude settings for
// `--settings`, merged into one JSON document.
//
// A repo that gitignores .claude/ (settings.local.json is ignored by default)
// checks out worktrees without them, so an agent there ran with no
// allow-list: under acceptEdits every Bash, web and MCP call was refused and
// the run still succeeded (#231). Such a file is passed whole.
//
// A file the dispatch directory has is passed for its `permissions` only
// (#361). Claude ignores a project file's allow rules in a directory nobody
// accepted its trust dialog in, and every worktree is a new directory beside
// the project — so the rules the operator wrote counted nowhere an agent
// worked. `--settings` is honoured untrusted. The rest of that file claude
// loads itself; passing it again would run its hooks twice. Empty when there
// are no settings.
func projectClaudeSettings(root, workdir string) string {
	if root == "" {
		return ""
	}
	var merged map[string]any
	for _, name := range claudeSettingsFiles {
		path, permsOnly := filepath.Join(root, ".claude", name), false
		if own := filepath.Join(workdir, ".claude", name); fileExists(own) {
			path, permsOnly = own, true
		}
		raw, err := os.ReadFile(path) // #nosec G304 -- the project's own settings file
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue // claude would refuse it too; not orch's to fix
		}
		if permsOnly {
			perms, ok := doc["permissions"]
			if !ok {
				continue
			}
			doc = map[string]any{"permissions": perms}
		}
		merged = mergeSettings(merged, doc)
	}
	if merged == nil {
		return ""
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return ""
	}
	return string(out)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mergeSettings layers src over dst the way claude layers settings files:
// objects merge key by key, lists (permission rules) accumulate, and any
// other value from the later file wins.
func mergeSettings(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			dv, _ := dst[k].(map[string]any)
			dst[k] = mergeSettings(dv, sv)
		case []any:
			dv, _ := dst[k].([]any)
			// Start from a non-nil slice: append(nil, empty...) is nil, which
			// marshals to null, and claude drops a settings document whose
			// "deny": null fails its schema — allow rules and all.
			dst[k] = append(append([]any{}, dv...), sv...)
		default:
			dst[k] = v
		}
	}
	return dst
}
