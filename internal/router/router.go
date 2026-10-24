// Package router resolves a task's `model` string to the CLI and model that
// will actually run it.
//
// Ported from `orchestrator/router.py`. The contract it enforces (FR-D-6,
// AS-06) is the last thing standing between a typo in tasks.json and a
// subprocess spawned against the wrong provider: every `task.model` MUST be a
// key in model_router.yaml, checked before anything is dispatched.
//
// Two design points carried over deliberately:
//
//   - The router absorbs drift; tasks.json is never edited to match it. A
//     model the router does not know is fixed by adding a route, not by
//     rewriting the task. `AddMissing` exists for exactly that.
//   - `is_premium` is recomputed from `tier` rather than trusted from the
//     file. The YAML flag is a denormalised cache and config authors forget
//     it; the tier is the truth.
package router

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/pyfmt"
	"gopkg.in/yaml.v3"
)

// Router maps a tasks.json `model` string to its route.
type Router map[string]model.RouteEntry

// FormatError is a model_router.yaml that does not describe routes: a bad
// backend, an unknown tier, a missing cli_model, a dangling escalation.
//
// Separate from a missing route because the fix is different — this is a file
// to correct, not an entry to add.
type FormatError struct {
	Path string
	Msg  string
}

func (e *FormatError) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Msg) }

// UnroutedError lists every task whose model has no route.
//
// Every offender, not the first: an operator fixing them one error message at
// a time would run orch once per typo. The list is sorted by task id so the
// output is stable enough to diff.
type UnroutedError struct {
	Offenders []Offender
}

// Offender is one task pointing at a model the router does not know.
type Offender struct {
	TaskID string
	Model  string
}

func (e *UnroutedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "model_router.yaml is missing entries for %d task(s):\n", len(e.Offenders))
	for _, o := range e.Offenders {
		fmt.Fprintf(&b, "  - %s: %s\n", o.TaskID, pyfmt.Quote(o.Model))
	}
	b.WriteString("Run `orch router add-missing` to auto-add them " +
		"(backend/cli_model inferred, tier defaults to standard), " +
		"or edit model_router.yaml by hand.")
	return b.String()
}

var validBackends = map[model.Backend]bool{
	model.BackendClaude: true, model.BackendCodex: true,
	model.BackendOpencode: true, model.BackendGemini: true,
	model.BackendAgy: true, model.BackendMuse: true,
}

var validTiers = map[model.Tier]bool{
	model.TierPremium: true, model.TierStandard: true, model.TierCheap: true,
}

var validEfforts = map[string]bool{"low": true, "medium": true, "high": true}

// validServiceTiers are the values codex knows for service_tier (codex
// 0.154). Which ones a model takes is codex's call: one it does not
// advertise is dropped, and the run says so (codexTierNotAdvertised).
var validServiceTiers = map[string]bool{"default": true, "priority": true, "fast": true, "flex": true}

// Load reads model_router.yaml and validates every row.
//
// A missing file is an error here, unlike config.yaml: a project with tasks
// and no router cannot dispatch anything, and saying so at load time beats
// failing per-task later.
func Load(path string) (Router, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the router path is the caller's project layout
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(path, raw)
}

// Parse is Load for a router already in memory — the packaged default, say;
// path only names it in errors.
func Parse(path string, raw []byte) (Router, error) {
	// Decoding into a node first keeps the file's key ORDER available, which
	// matters for error messages: reporting the first bad row as the file
	// reads is more useful than reporting whichever one a map happened to
	// yield.
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, &FormatError{Path: path, Msg: fmt.Sprintf("invalid YAML: %v", err)}
	}
	// `yaml.Unmarshal` reads the FIRST document and silently ignores whatever
	// follows it. On this file that is a data-loss bug rather than a nicety:
	// the stub `orch init` wrote before #141 ended with an explicit `{}`, and
	// a block mapping appended after a complete document is a second document
	// — so a router with routes in it loaded as empty, with no error, and the
	// tasks using those models failed as unrouted. PyYAML raises on the same
	// file, which is how the Python side of this was caught.
	if err := rejectTrailingDocuments(raw); err != nil {
		return nil, &FormatError{Path: path, Msg: err.Error()}
	}
	if len(doc.Content) == 0 {
		return Router{}, nil // an empty file is an empty router, not an error
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, &FormatError{Path: path,
			Msg: fmt.Sprintf("expected top-level mapping, got %s", nodeKindName(top.Kind))}
	}

	out := Router{}
	for i := 0; i+1 < len(top.Content); i += 2 {
		key := top.Content[i].Value
		row := top.Content[i+1]
		if row.Kind != yaml.MappingNode {
			return nil, &FormatError{Path: path,
				Msg: fmt.Sprintf("entry %s is not a mapping", pyfmt.Quote(key))}
		}
		var entry model.RouteEntry
		if err := row.Decode(&entry); err != nil {
			return nil, &FormatError{Path: path,
				Msg: fmt.Sprintf("entry %s: %v", pyfmt.Quote(key), err)}
		}
		if err := validateEntry(path, key, entry); err != nil {
			return nil, err
		}
		// The YAML flag is a cache; the tier is the truth.
		entry.IsPremium = entry.Tier == model.TierPremium
		out[key] = entry
	}

	// FR-D-8: an escalation_model names another route KEY in this same file.
	// Checked after the whole file is read, because it may point forwards.
	// Fail-fast here so no dispatch ever picks up a dangling escalation.
	for _, key := range out.Keys() {
		entry := out[key]
		if entry.EscalationModel == nil {
			continue
		}
		if _, ok := out[*entry.EscalationModel]; !ok {
			return nil, &FormatError{Path: path, Msg: fmt.Sprintf(
				"entry %s has escalation_model %s that is not a route in this "+
					"file (add it, or remove the escalation_model field)",
				pyfmt.Quote(key), pyfmt.Quote(*entry.EscalationModel))}
		}
	}
	// #439: each failover entry is another route key, checked the same way.
	for _, key := range out.Keys() {
		for _, to := range out[key].Failover {
			if to == key {
				return nil, &FormatError{Path: path, Msg: fmt.Sprintf(
					"entry %s fails over to itself", pyfmt.Quote(key))}
			}
			if _, ok := out[to]; !ok {
				return nil, &FormatError{Path: path, Msg: fmt.Sprintf(
					"entry %s has failover %s that is not a route in this file "+
						"(add it, or remove it from failover)", pyfmt.Quote(key), pyfmt.Quote(to))}
			}
		}
	}
	return out, nil
}

func validateEntry(path, key string, e model.RouteEntry) error {
	if !validBackends[e.Backend] {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s has invalid backend %s (expected one of %s)",
			pyfmt.Quote(key), pyfmt.Quote(string(e.Backend)), sortedQuoted(backendNames()))}
	}
	if !validTiers[e.Tier] {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s has invalid tier %s (expected one of %s)",
			pyfmt.Quote(key), pyfmt.Quote(string(e.Tier)), sortedQuoted(tierNames()))}
	}
	if e.CLIModel == "" {
		return &FormatError{Path: path,
			Msg: fmt.Sprintf("entry %s missing non-empty cli_model", pyfmt.Quote(key))}
	}
	if e.Effort != nil && !validEfforts[*e.Effort] {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s field `effort` must be one of 'low' | 'medium' | 'high', got %s",
			pyfmt.Quote(key), pyfmt.Quote(*e.Effort))}
	}
	if e.ServiceTier != nil {
		if e.Backend != model.BackendCodex {
			return &FormatError{Path: path, Msg: fmt.Sprintf(
				"entry %s field `service_tier` applies to codex routes only, not %s",
				pyfmt.Quote(key), pyfmt.Quote(string(e.Backend)))}
		}
		if !validServiceTiers[*e.ServiceTier] {
			return &FormatError{Path: path, Msg: fmt.Sprintf(
				"entry %s field `service_tier` must be one of 'default' | 'priority' | 'fast' | 'flex', got %s",
				pyfmt.Quote(key), pyfmt.Quote(*e.ServiceTier))}
		}
	}
	return nil
}

// Keys returns the route keys sorted, so every listing and error message is
// in the same order twice running.
func (r Router) Keys() []string {
	out := make([]string, 0, len(r))
	for k := range r {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate fails fast when any task references a model the router lacks.
//
// Returns an *UnroutedError naming every offender. This is AS-06, and it runs
// before a single subprocess is spawned.
func (r Router) Validate(tasks []model.Task) error {
	var offenders []Offender
	for _, t := range tasks {
		if _, ok := r[t.Model]; !ok {
			offenders = append(offenders, Offender{TaskID: t.ID, Model: t.Model})
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Slice(offenders, func(i, j int) bool { return offenders[i].TaskID < offenders[j].TaskID })
	return &UnroutedError{Offenders: offenders}
}

// MissingModels returns the distinct models tasks reference and the router
// lacks, sorted.
//
// Distinct, not per-task: an UnroutedError prints a line per task, but twenty
// tasks usually share two missing models, and the thing being added is a
// model.
func (r Router) MissingModels(tasks []model.Task) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tasks {
		if _, ok := r[t.Model]; ok || seen[t.Model] {
			continue
		}
		seen[t.Model] = true
		out = append(out, t.Model)
	}
	sort.Strings(out)
	return out
}

// ErrCannotInfer means a route key does not follow the `<backend>/<model>`
// convention, so nothing can be guessed from it.
var ErrCannotInfer = errors.New("cannot infer a route")

// InferEntry guesses a route from a `<backend>/<cli_model>` key.
//
// `backend` and `cli_model` fall out of the key, the latter mapped to an id the
// CLI accepts where orch knows the backend's ids (`claude/sonnet-5` →
// `claude-sonnet-5`). A model it cannot map is kept as written; callers show
// CLIModelWarning for it. `tier` does not: it is a
// business decision about cost and capability that no model name reveals, so
// it defaults to standard — non-premium, and therefore not tripping the semi
// gate. Whatever writes these must tell the operator to review the tiers.
//
// Returns ErrCannotInfer for a bare model name or an unknown backend prefix
// (`openai/...`). Those need a person, not a guess: writing a wrong route is
// worse than writing none, because it dispatches somewhere.
func InferEntry(key string, defaultTier model.Tier) (model.RouteEntry, error) {
	if defaultTier == "" {
		defaultTier = model.TierStandard
	}
	backend, cliModel, found := strings.Cut(key, "/")
	if !found || cliModel == "" || !validBackends[model.Backend(backend)] {
		return model.RouteEntry{}, fmt.Errorf("%s: %w", pyfmt.Quote(key), ErrCannotInfer)
	}
	return model.RouteEntry{
		Backend:   model.Backend(backend),
		CLIModel:  knownCLIModel(model.Backend(backend), cliModel),
		Tier:      defaultTier,
		IsPremium: defaultTier == model.TierPremium,
	}, nil
}

// What the claude CLI takes as `--model`: an alias it resolves to the current
// model of a family, or a full `claude-…` id. #266 found `claude/sonnet-5`
// inferring `cli_model: sonnet-5`, which the CLI rejects with a 404, and every
// task using it died at dispatch while validate and doctor were green; #348
// found the same with `opus-5-5`, because the fix then was a list of known
// ids and `claude-opus-5-5` was newer than the list.
//
// So claude is checked by shape, not by list: a model newer than this binary
// must still dispatch, and the mistake that actually happens — the family
// written without its `claude-` prefix — is a shape. Whether a well-formed id
// names a model that exists is the CLI's to say; preflight stays offline.
//
// Only claude is checked. The repo holds no reliable rule for codex, opencode,
// gemini or agy: their CLIs take provider-prefixed or gateway-specific ids that
// change release to release, so checking them would only raise false warnings.
// TestEveryPackagedClaudeModelIsKnown (here) and TestEveryTemplateModelIsInferable
// (internal/templates) fail if the packaged router or a template routes to a
// value this would warn about.
var claudeAliases = []string{"opus", "sonnet", "haiku", "fable", "opusplan", "default"}

// claudeAliasesShown are the aliases a warning suggests: the four families.
const claudeAliasesShown = "opus, sonnet, haiku, fable"

var (
	// reClaudeID is a full claude model id: `claude-` and lowercase
	// dash/dot-separated parts (`claude-opus-5-5`, `claude-haiku-4-5-20251001`).
	reClaudeID = regexp.MustCompile(`^claude-[a-z0-9]+(?:[-.][a-z0-9]+)*$`)
	// reClaudeFamily is a family with a version and no prefix (`opus-5-5`,
	// `sonnet-4-6`): the id people write, which the CLI does not take.
	reClaudeFamily = regexp.MustCompile(`^(?:opus|sonnet|haiku|fable)-[0-9][a-z0-9]*(?:[-.][a-z0-9]+)*$`)
)

// validCLIModel reports whether the backend's CLI takes name as written. A
// backend orch has no rule for always does.
func validCLIModel(backend model.Backend, name string) bool {
	if backend != model.BackendClaude {
		return true
	}
	return slices.Contains(claudeAliases, name) || reClaudeID.MatchString(name)
}

// knownCLIModel maps the model half of a route key to an id the backend's CLI
// accepts: an alias or a full id is kept, and a claude family written without
// its `claude-` prefix (`sonnet-5`, `opus-5-5`) gains it. Anything else comes
// back unchanged, for CLIModelWarning to flag.
func knownCLIModel(backend model.Backend, name string) string {
	if backend == model.BackendClaude && reClaudeFamily.MatchString(name) {
		return "claude-" + name
	}
	return name
}

// CLIModelWarning describes a route whose cli_model its backend's CLI will
// not take, or returns "" when it will or the backend is not checked.
//
// A warning and not an error: the rule is orch's knowledge, not the CLI's. No
// network call either — preflight stays offline.
func CLIModelWarning(key string, e model.RouteEntry) string {
	if validCLIModel(e.Backend, e.CLIModel) {
		return ""
	}
	fix := "a full claude-… id (e.g. 'claude-sonnet-4-6') or an alias (" + claudeAliasesShown + ")"
	if mapped := knownCLIModel(e.Backend, e.CLIModel); mapped != e.CLIModel {
		fix = pyfmt.Quote(mapped)
	}
	return fmt.Sprintf("route %s has cli_model %s, which is not a known %s model id "+
		"and will likely fail at dispatch — set it to %s in model_router.yaml",
		pyfmt.Quote(key), pyfmt.Quote(e.CLIModel), e.Backend, fix)
}

// CLIModelWarnings returns CLIModelWarning for every route the tasks use,
// once per route, sorted by key. Routes no task uses are not reported.
func (r Router) CLIModelWarnings(tasks []model.Task) []string {
	used := Router{}
	for _, t := range tasks {
		if e, ok := r[t.Model]; ok {
			used[t.Model] = e
		}
	}
	var out []string
	for _, key := range used.Keys() {
		if w := CLIModelWarning(key, used[key]); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// AddMissing appends inferred routes for every model in `keys` the file does
// not already have, and returns the keys it added plus the ones it could not
// infer.
//
// It APPENDS raw YAML rather than re-dumping the parsed mapping. A round trip
// through a YAML encoder would drop the header comments that explain the file
// and reorder the hand-grouped entries — the file is read by people, and
// rewriting it is a worse outcome than the duplication of formatting rows by
// hand here.
//
// Idempotent: a key already present is skipped, so running it twice adds
// nothing the second time.
func AddMissing(path string, keys []string, defaultTier model.Tier) (added, skipped []string, err error) {
	existing, err := Load(path) // also proves the current file is well-formed
	if err != nil {
		return nil, nil, err
	}

	toAdd := map[string]model.RouteEntry{}
	for _, k := range keys {
		if _, have := existing[k]; have {
			continue
		}
		entry, ierr := InferEntry(k, defaultTier)
		if ierr != nil {
			skipped = append(skipped, k)
			continue
		}
		toAdd[k] = entry
	}
	sort.Strings(skipped)
	if len(toAdd) == 0 {
		return nil, skipped, nil
	}

	for k := range toAdd {
		added = append(added, k)
	}
	sort.Strings(added)

	body, err := os.ReadFile(path) // #nosec G304 -- same path Load just validated
	if err != nil {
		return nil, skipped, fmt.Errorf("read %s: %w", path, err)
	}

	var b strings.Builder
	header := dropEmptyFlowMapping(string(body))
	b.WriteString(header)
	if len(header) > 0 && !strings.HasSuffix(header, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# ---- auto-added by `orch router add-missing` (review tiers) ----\n")
	for i, k := range added {
		if i > 0 {
			b.WriteString("\n")
		}
		e := toAdd[k]
		fmt.Fprintf(&b, "%s:\n  backend: %s\n  cli_model: %s\n  tier: %s\n",
			k, e.Backend, e.CLIModel, e.Tier)
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return nil, skipped, fmt.Errorf("write %s: %w", path, err)
	}
	return added, skipped, nil
}

// rejectTrailingDocuments reports content after the first YAML document.
//
// `yaml.Unmarshal` stops at the first document. A decoder reads them one at a
// time, so asking for a second is how we find out there is one — and on this
// file a second document means routes nobody will ever read.
func rejectTrailingDocuments(raw []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var first any
	if err := dec.Decode(&first); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty file
		}
		return fmt.Errorf("invalid YAML: %v", err)
	}
	var second any
	err := dec.Decode(&second)
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		// The usual shape: a block mapping written after a `{}` line, which
		// the parser reports as a document that never started.
		return fmt.Errorf("content after the first YAML document (%v) — "+
			"a router written below a `{}` line is ignored, so those routes "+
			"would never load. Delete the `{}` and leave the entries at the "+
			"top level", err)
	default:
		return errors.New("more than one YAML document — a router file holds " +
			"exactly one mapping, and only the first document would be read")
	}
}

// dropEmptyFlowMapping removes a lone `{}` line from a router file's header.
//
// `orch init` wrote `{}` as the empty router until #141, because
// `load_router` needed a mapping. Appending a block mapping after it produces
// a file neither PyYAML nor Load will read. Removing the line keeps every
// comment above it — which is the part worth preserving, since the header
// explains what the file is — and leaves a document the appended entries can
// join.
//
// Only an EMPTY flow mapping is dropped, and only when it is alone on its
// line. A file with real routes in flow style is left exactly as it is: this
// repairs a stub, it does not reformat anyone's file.
func dropEmptyFlowMapping(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "{}" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// Fallback is one route that can substitute a different CLI model when the
// installed CLI has drifted from what the route names.
type Fallback struct {
	Key      string
	CLIModel string
	Fallback string
}

// Fallbacks lists the routes carrying a fallback_cli_model, sorted by key.
//
// The caller announces these at startup. Note what this is NOT: FR-D-7
// originally specified one WARN per substitution at startup, and Sprint C
// removed that — the announcement is informational, because the actual swap
// happens later in the reap loop when a dispatch fails with a version-drift
// error, and a WARN per route on every start spammed the console. Python
// prints one INFO summary line, or one INFO per route under -v. Reintroducing
// the WARN here would undo a deliberate decision, so this returns the data and
// leaves the level to the caller.
func (r Router) Fallbacks() []Fallback {
	var out []Fallback
	for _, key := range r.Keys() {
		e := r[key]
		if e.FallbackCLIModel == nil || *e.FallbackCLIModel == "" {
			continue
		}
		out = append(out, Fallback{Key: key, CLIModel: e.CLIModel, Fallback: *e.FallbackCLIModel})
	}
	return out
}

// ---- small helpers ---------------------------------------------------------

func backendNames() []string {
	out := make([]string, 0, len(validBackends))
	for b := range validBackends {
		out = append(out, string(b))
	}
	return out
}

func tierNames() []string {
	out := make([]string, 0, len(validTiers))
	for t := range validTiers {
		out = append(out, string(t))
	}
	return out
}

// sortedQuoted renders a set the way Python prints `sorted(a_set)`, so the
// error messages match.
func sortedQuoted(vs []string) string {
	sort.Strings(vs)
	quoted := make([]string, len(vs))
	for i, v := range vs {
		quoted[i] = pyfmt.Quote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "list"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.MappingNode:
		return "dict"
	default:
		return "unknown"
	}
}

// SetRoute points an existing route at another backend and CLI model: what
// `orch router suggest --apply` writes (#457).
//
// Like AddMissing it edits the text rather than re-dumping the mapping, so
// the comments, the order and every other field survive: only the two values
// change, each on its own line, a trailing comment kept. A flow-style entry
// (`key: {backend: …}`) is refused rather than reformatted. The result is
// parsed again before it is written, so a value that would not read back
// leaves the file as it was.
func SetRoute(path, key string, backend model.Backend, cliModel string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- the router path is the caller's project layout
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := Parse(path, raw); err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 {
		return &FormatError{Path: path, Msg: fmt.Sprintf("no route %s", pyfmt.Quote(key))}
	}
	top := doc.Content[0]
	var row *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == key {
			row = top.Content[i+1]
		}
	}
	if row == nil {
		return &FormatError{Path: path, Msg: fmt.Sprintf("no route %s", pyfmt.Quote(key))}
	}
	if row.Style&yaml.FlowStyle != 0 {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s is written in flow style ({...}); edit it by hand", pyfmt.Quote(key))}
	}
	lines := strings.Split(string(raw), "\n")
	want := map[string]string{"backend": string(backend), "cli_model": cliModel}
	for i := 0; i+1 < len(row.Content); i += 2 {
		k, v := row.Content[i], row.Content[i+1]
		value, ok := want[k.Value]
		if !ok {
			continue
		}
		if v.Kind != yaml.ScalarNode || v.Line != k.Line || v.Line > len(lines) {
			return &FormatError{Path: path, Msg: fmt.Sprintf(
				"entry %s field `%s` is not a one-line value; edit it by hand", pyfmt.Quote(key), k.Value)}
		}
		line := lines[v.Line-1]
		head, rest := line[:v.Column-1], line[v.Column-1:]
		end := len(rest)
		if c := v.LineComment + k.LineComment; c != "" {
			if at := strings.LastIndex(rest, c); at >= 0 {
				end = len(strings.TrimRight(rest[:at], " \t"))
			}
		}
		lines[v.Line-1] = head + value + rest[end:]
	}
	out := []byte(strings.Join(lines, "\n"))
	got, err := Parse(path, out)
	if err != nil {
		return err
	}
	if e := got[key]; e.Backend != backend || e.CLIModel != cliModel {
		return &FormatError{Path: path, Msg: fmt.Sprintf(
			"entry %s did not read back as %s/%s; edit it by hand", pyfmt.Quote(key), backend, cliModel)}
	}
	return os.WriteFile(path, out, 0o600) // #nosec G703 -- the router path Load just read
}
