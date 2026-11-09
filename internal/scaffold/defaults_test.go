package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"gopkg.in/yaml.v3"
)

// scaffoldOptIns are the keys where the config `orch init` writes says
// something other than config.Defaults(), each on purpose. Defaults() is what
// a config WITHOUT the key gets, so an existing project keeps behaving the
// same on an upgrade; the scaffold is what a NEW project starts with. A new
// difference goes here with its reason, or one side is fixed.
var scaffoldOptIns = map[string]string{
	// Python's packaged config.yaml and its loader's _apply_defaults disagreed
	// on these and Go kept both: the loader's values are pinned by the frozen
	// goldens (TestEffectiveConfigMatchesThePythonGoldens), the file's by
	// testdata/python-frozen/config.yaml. docs/CONFIG.md documents the loader's.
	"concurrency.global_max":          "Python's packaged file wrote 8; the loader's default is 6",
	"concurrency.per_file":            "Python's packaged file wrote 1; the loader's default is 0 (off)",
	"concurrency.per_provider.gemini": "the packaged file caps every backend; the loader caps only the first three",
	"concurrency.per_provider.agy":    "the packaged file caps every backend; the loader caps only the first three",
	"concurrency.per_provider.muse":   "the packaged file caps every backend; the loader caps only the first three",
	"strict_files_phases":             "Python's packaged file enforced files[] in phase 10; the loader enforces none",
	"dispatch.worktree_mode":          "on in a new project; a config without it runs agents in place",
	"vcs.auto_pr":                     "on in a new project; a config without it opens no PR",
	// Go-only opt-ins: on in a new project, off in a config without the key,
	// so an upgrade never turns a feature on behind the operator's back.
	"dispatch.min_free_disk_mb":   "#497: 2048 in a new project; absent = 0, never holds a dispatch",
	"retry.conflict.max_attempts": "#438: 1 in a new project; absent = off",
	"report_findings.enabled":     "#399: on in a new project; absent = off",
	"verify.enabled":              "#398: on in a new project; absent = off",
	"knowledge.enabled":           "#451: on in a new project; absent = off",
	"knowledge.max_in_prompt":     "#451: written as 5; absent loads as 0, which Knowledge.Prompt reads as 5",
	"knowledge.auto_capture":      "#451: written as true; absent loads as null, which Knowledge.Captures reads as on",
}

// leftOutOfScaffold are the keys Defaults() gives a value the scaffold does
// not write. The scaffold is the file an operator reads first, so it carries
// the knobs a new project is likely to turn; the rest keep their default and
// are documented in docs/CONFIG.md.
var leftOutOfScaffold = map[string]string{ // #nosec G101 -- config key names and why, no credential
	"budget.per_dispatch_usd":        "commented out: claude gets --max-budget-usd only when the key is written",
	"retry.max_attempts":             "advanced: the per-class retry rules",
	"dashboard.profile":              "commented out: operator unless the client view is wanted",
	"dashboard.summary_language":     "the older name of dashboard.language, which the scaffold writes",
	"notifications.budget_alerts":    "on, and silent without a webhook",
	"notifications.budget_alert_pct": "advanced",
	"publish.interval_s":             "orch publish has its own flags",
	"publish.to":                     "orch publish has its own flags",
	"publish.dir":                    "orch publish has its own flags",
	"publish.git_branch":             "orch publish has its own flags",
	"sync.issues_label":              "orch sync issues has --label",
	"portal.documents":               "the planning pipeline's documents; advanced",
	"verify.max_attempts":            "advanced",
	"verify.timeout_s":               "advanced",
	"verify.app.ready_timeout_s":     "advanced: the app block itself is commented out",
	"update.check_clis":              "advanced",
	"guard.secrets":                  "safe default (block); advanced",
	"guard.debug":                    "safe default (warn); advanced",
	"guard.protected":                "safe default (block); advanced",
	"watch.enabled":                  "advanced",
	"watch.stall_after_s":            "advanced",
	"radar":                          "advanced",
}

// The defaults live twice: config.Defaults(), which a config without a key
// gets, and defaults/config.yaml, which `orch init` writes. Every key the
// scaffold writes must load to what Defaults() says, and every default the
// scaffold leaves out must be left out on purpose — a difference nobody
// listed above is drift (#550).
func TestScaffoldConfigMatchesDefaults(t *testing.T) {
	body, err := packagedDefault("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	scaffold, err := config.Load(path, dir)
	if err != nil {
		t.Fatalf("the scaffold does not load: %v", err)
	}
	for _, w := range scaffold.Warnings {
		t.Errorf("loading the scaffold warns about itself: %s", w)
	}
	empty, err := config.Load(filepath.Join(dir, "absent.yaml"), dir)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	written := map[string]any{}
	flattenYAML("", raw, written)
	got, want := effective(t, scaffold.Config), effective(t, empty.Config)

	for _, key := range sortedKeys(written) {
		_, optIn := scaffoldOptIns[key]
		same := reflect.DeepEqual(got[key], want[key])
		switch {
		case !same && !optIn:
			t.Errorf("%s: the scaffold writes %v, config.Defaults() says %v; fix one side or list it in scaffoldOptIns",
				key, got[key], want[key])
		case same && optIn:
			t.Errorf("scaffoldOptIns lists %s but the scaffold and Defaults() agree on %v; remove the entry", key, got[key])
		}
	}
	for _, key := range sortedKeys(want) {
		if _, ok := written[key]; ok || isZero(want[key]) {
			continue
		}
		if _, ok := leftOutOfScaffold[key]; !ok {
			t.Errorf("%s: config.Defaults() says %v and the scaffold does not write it; add it to the scaffold "+
				"or to leftOutOfScaffold with the reason", key, want[key])
		}
	}
	for key := range scaffoldOptIns {
		if _, ok := written[key]; !ok {
			t.Errorf("scaffoldOptIns lists %s, which the scaffold does not write", key)
		}
	}
	for key := range leftOutOfScaffold {
		if _, ok := written[key]; ok {
			t.Errorf("leftOutOfScaffold lists %s, which the scaffold writes", key)
		} else if _, ok := want[key]; !ok || isZero(want[key]) {
			t.Errorf("leftOutOfScaffold lists %s, which has no default to leave out", key)
		}
	}
}

// effective is a loaded config as dotted key -> value, the way the YAML
// encoder writes it.
func effective(t *testing.T, c config.Config) map[string]any {
	t.Helper()
	b, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	flattenYAML("", m, out)
	return out
}

func flattenYAML(prefix string, m map[string]any, into map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			flattenYAML(key, child, into)
			continue
		}
		into[key] = v
	}
}

func isZero(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map {
		return rv.Len() == 0
	}
	return fmt.Sprint(v) == fmt.Sprint(reflect.Zero(rv.Type()).Interface())
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
