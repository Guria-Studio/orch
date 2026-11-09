package atomize

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ImportFormat names a spec-driven tool whose task lists `orch atomize
// --from` reads (#526).
type ImportFormat string

const (
	// FormatSpecKit is github/spec-kit: specs/<NNN-feature>/tasks.md, with
	// `- [ ] T001 [P] [US1] ...` lines under `## Phase N: ...` headings.
	FormatSpecKit ImportFormat = "spec-kit"
	// FormatOpenSpec is Fission-AI/OpenSpec: openspec/changes/<change>/tasks.md,
	// with `- [ ] 1.1 ...` lines under `## 1. ...` headings.
	FormatOpenSpec ImportFormat = "openspec"
)

// ParseImportFormat reads --from's value: spec-kit (or speckit) and openspec,
// in any case.
func ParseImportFormat(s string) (ImportFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "spec-kit", "speckit":
		return FormatSpecKit, nil
	case "openspec":
		return FormatOpenSpec, nil
	}
	return "", fmt.Errorf("unknown --from %q: orch imports spec-kit and openspec", s)
}

// label is the tool's name as its own docs spell it.
func (f ImportFormat) label() string {
	if f == FormatOpenSpec {
		return "OpenSpec"
	}
	return "spec-kit"
}

// generatedBy is the frontmatter mark of a spec this import wrote: the only
// kind of file a re-run rewrites.
func (f ImportFormat) generatedBy() string { return "orch atomize --from " + string(f) }

// ImportSource is one spec-kit feature or one OpenSpec change: a directory
// with a tasks.md.
type ImportSource struct {
	Format    ImportFormat
	Name      string // the directory's name: 001-photo-albums, add-dark-mode
	Dir       string
	TasksPath string
}

// ImportTask is one checkbox line of a tasks.md.
type ImportTask struct {
	// Key is the id the source gives the task: T001, T006a, 1.2.
	Key string
	// Phase is spec-kit's phase number, or OpenSpec's group number.
	Phase      int
	PhaseTitle string
	// Num is the task number in its orch id: spec-kit's T012 is 12, and a
	// lettered T006a is 1006 (b is 2006, ...); OpenSpec's 2.3 is 3.
	Num      int
	Parallel bool   // spec-kit's [P]
	Story    string // spec-kit's [US1]
	Title    string
	// Text is the task as written: its line and any indented lines under it.
	Text []string
	Line int // 1-based line of the checkbox in tasks.md
	Done bool
	// After lists the keys of an explicit `(depends on T012, T013)`.
	After []string
	Files []string
	// Section counts headings: tasks under different ### subsections never
	// share a run of [P] tasks.
	Section int
}

// ImportDoc is a parsed tasks.md.
type ImportDoc struct {
	Title    string
	Tasks    []ImportTask
	Warnings []string
}

var (
	reImportCheckbox   = regexp.MustCompile(`^(\s*)[-*]\s+\[([ xX])\]\s+(.*?)\s*$`)
	reSpecKitTasksH1   = regexp.MustCompile(`^#\s+Tasks\s*:\s*(.+?)\s*$`)
	reSpecKitPhase     = regexp.MustCompile(`^##\s+Phase\s+([0-9]+(?:\.[0-9]+)*)\s*(?:[:：—–-]\s*(.*?))?\s*$`)
	reSpecKitID        = regexp.MustCompile(`^T(\d+)([a-z]?)\b`)
	reSpecKitIDLike    = regexp.MustCompile(`^T[0-9A-Z]+\b`)
	reSpecKitMarker    = regexp.MustCompile(`^\[([A-Za-z]+[0-9]*)\]\s*`)
	reOpenSpecGroup    = regexp.MustCompile(`^##\s+(?:([0-9]+)\.?\s+)?(.+?)\s*$`)
	reOpenSpecTaskNum  = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.?\s+`)
	reImportSubheading = regexp.MustCompile(`^###+\s`)
	reImportDependsOn  = regexp.MustCompile(`(?i)\b(?:depends on|after|requires|blocked by)\s+(T\d+[a-z]?(?:\s*(?:,|and|&|/)\s*T\d+[a-z]?)*)`)
	reImportDependsCl  = regexp.MustCompile(`(?i)\s*\((?:depends on|after|requires|blocked by)\s+T\d+[^)]*\)`)
	reImportDepKey     = regexp.MustCompile(`T\d+[a-z]?`)
	reImportBacktick   = regexp.MustCompile("`([^`\\s]+)`")
	reImportBarePath   = regexp.MustCompile(`(?:^|[\s(])((?:\.{0,2}/)?[\w.@-]+(?:/[\w.@-]+)+\.[A-Za-z0-9]{1,6})\b`)
	reImportPathShape  = regexp.MustCompile(`^[\w./@-]+$`)
	reImportExt        = regexp.MustCompile(`\.([A-Za-z0-9]{1,10})$`)
)

// importFileExts are the extensions that make a backticked word with no
// slash a file (`package.json`), not an identifier (`view.args`).
var importFileExts = map[string]bool{
	"go": true, "rs": true, "py": true, "ts": true, "tsx": true, "js": true, "jsx": true, "mjs": true,
	"cjs": true, "json": true, "yaml": true, "yml": true, "toml": true, "md": true, "sql": true, "sh": true,
	"java": true, "kt": true, "swift": true, "rb": true, "php": true, "cs": true, "c": true, "h": true,
	"cpp": true, "hpp": true, "css": true, "scss": true, "html": true, "vue": true, "svelte": true,
	"dart": true, "ex": true, "exs": true, "proto": true, "graphql": true, "txt": true, "lock": true,
	"mod": true, "xml": true, "gradle": true, "tf": true, "ini": true, "cfg": true, "env": true,
}

// importFiles picks the file paths a task's text names: backticked paths
// (with a slash, or a known extension) and bare paths with a slash and an
// extension. An API route (/albums), a command (`pnpm test`) or an
// identifier (`Clock`) is not a file.
func importFiles(text string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimRight(p, ".,;:")
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, m := range reImportBacktick.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if !reImportPathShape.MatchString(p) || strings.HasPrefix(p, "/") {
			continue
		}
		ext := reImportExt.FindStringSubmatch(p)
		if strings.Contains(p, "/") || (ext != nil && importFileExts[strings.ToLower(ext[1])]) {
			add(p)
		}
	}
	plain := reImportBacktick.ReplaceAllString(text, " ")
	for _, m := range reImportBarePath.FindAllStringSubmatch(plain, -1) {
		if !strings.HasPrefix(m[1], "/") {
			add(m[1])
		}
	}
	return out
}

// maxImportTitle bounds a task title (it becomes a PR title); the whole line
// stays in the description.
const maxImportTitle = 100

func importTitle(s string) string {
	s = strings.TrimSpace(reImportDependsCl.ReplaceAllString(s, ""))
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= maxImportTitle {
		return s
	}
	r := []rune(s)[:maxImportTitle]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > maxImportTitle/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

// lineScanner walks a tasks.md the way both formats need: it skips fenced
// code and HTML comments (spec-kit's template keeps sample tasks in both)
// and gathers the indented lines under a checkbox into that task.
type lineScanner struct {
	inFence, inComment bool
}

// skip reports whether line is inside a fence or comment (or opens/closes one).
func (s *lineScanner) skip(line string) bool {
	trim := strings.TrimSpace(line)
	if s.inComment {
		if strings.Contains(trim, "-->") {
			s.inComment = false
		}
		return true
	}
	if strings.HasPrefix(trim, "```") {
		s.inFence = !s.inFence
		return true
	}
	if s.inFence {
		return true
	}
	if i := strings.Index(trim, "<!--"); i >= 0 && !strings.Contains(trim[i:], "-->") {
		s.inComment = true
		return true
	}
	return false
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }

// importParser is what the two formats share: checkboxes, continuation
// lines, duplicate keys.
type importParser struct {
	doc     ImportDoc
	seen    map[string]int
	current *ImportTask
	indent  int
}

func (p *importParser) flush() {
	if p.current == nil {
		return
	}
	full := strings.Join(p.current.Text, "\n")
	if p.current.Files == nil {
		p.current.Files = importFiles(full)
	}
	p.doc.Tasks = append(p.doc.Tasks, *p.current)
	p.current = nil
}

// continuation adds line to the open task when it is indented under it.
func (p *importParser) continuation(line string) bool {
	if p.current == nil || strings.TrimSpace(line) == "" || indentOf(line) <= p.indent {
		return false
	}
	p.current.Text = append(p.current.Text, strings.TrimRight(line, " \t"))
	return true
}

func (p *importParser) start(task ImportTask, indent int, lineno int) {
	p.flush()
	if prev, dup := p.seen[task.Key]; dup {
		p.doc.Warnings = append(p.doc.Warnings, fmt.Sprintf(
			"line %d: task %s is already on line %d — skipped", lineno, task.Key, prev))
		return
	}
	p.seen[task.Key] = lineno
	p.current = &task
	p.indent = indent
}

// ParseSpecKitTasks reads a spec-kit tasks.md (templates/tasks-template.md):
// `## Phase N: Title` headings and `- [ ] T001 [P] [US1] Description` lines,
// `[x]`/`[X]` for done. Phases numbered 3.1, 3.2 (older spec-kit) are
// renumbered 1, 2, ... in order.
func ParseSpecKitTasks(text string) ImportDoc {
	p := importParser{seen: map[string]int{}}
	var scan lineScanner
	phaseTitle := ""
	var phaseLabels []string
	section := 0

	for i, line := range splitLines(text) {
		lineno := i + 1
		if scan.skip(line) {
			p.flush()
			continue
		}
		if p.continuation(line) {
			continue
		}
		if m := reSpecKitTasksH1.FindStringSubmatch(line); m != nil && p.doc.Title == "" {
			p.doc.Title = m[1]
			continue
		}
		if m := reSpecKitPhase.FindStringSubmatch(line); m != nil {
			p.flush()
			phaseTitle = m[2]
			phaseLabels = append(phaseLabels, m[1])
			section++
			continue
		}
		if strings.HasPrefix(line, "#") {
			p.flush()
			section++
			continue
		}
		m := reImportCheckbox.FindStringSubmatch(line)
		if m == nil {
			p.flush()
			continue
		}
		rest := m[3]
		id := reSpecKitID.FindStringSubmatch(rest)
		if id == nil {
			p.flush()
			what := rest
			if l := reSpecKitIDLike.FindString(rest); l != "" {
				what = l
			}
			p.doc.Warnings = append(p.doc.Warnings, fmt.Sprintf(
				"line %d: checkbox without a task id (%s) — skipped", lineno, oneLine(what)))
			continue
		}
		n, _ := strconv.Atoi(id[1])
		if id[2] != "" {
			n += 1000 * int(id[2][0]-'a'+1)
		}
		task := ImportTask{
			Key: "T" + id[1] + id[2], Num: n, PhaseTitle: phaseTitle,
			Line: lineno, Done: m[2] != " ", Section: section,
			Text: []string{strings.TrimSpace(line)},
		}
		rest = strings.TrimSpace(rest[len(id[0]):])
		for {
			mk := reSpecKitMarker.FindStringSubmatch(rest)
			if mk == nil {
				break
			}
			if strings.EqualFold(mk[1], "P") {
				task.Parallel = true
			} else {
				task.Story = mk[1]
			}
			rest = rest[len(mk[0]):]
		}
		task.Title = importTitle(rest)
		p.start(task, len(m[1]), lineno)
	}
	p.flush()

	// Phase numbers: as written when every one is a whole number, else in
	// order of appearance.
	ordinal := map[string]int{}
	whole := true
	for _, l := range phaseLabels {
		if _, ok := ordinal[l]; !ok {
			ordinal[l] = len(ordinal) + 1
		}
		if strings.Contains(l, ".") {
			whole = false
		}
	}
	phaseOf := specKitPhaseOfLine(text)
	for i := range p.doc.Tasks {
		label := phaseOf[p.doc.Tasks[i].Line]
		switch {
		case label == "":
			p.doc.Tasks[i].Phase = 1
		case whole:
			p.doc.Tasks[i].Phase, _ = strconv.Atoi(label)
		default:
			p.doc.Tasks[i].Phase = ordinal[label]
		}
	}
	addAfter(&p.doc)
	return p.doc
}

// specKitPhaseOfLine maps every line number to the phase label in force there.
func specKitPhaseOfLine(text string) map[int]string {
	out := map[int]string{}
	var scan lineScanner
	label := ""
	for i, line := range splitLines(text) {
		if !scan.skip(line) {
			if m := reSpecKitPhase.FindStringSubmatch(line); m != nil {
				label = m[1]
			}
		}
		out[i+1] = label
	}
	return out
}

// addAfter reads each task's explicit `depends on` clause.
func addAfter(doc *ImportDoc) {
	keys := map[string]bool{}
	for _, t := range doc.Tasks {
		keys[t.Key] = true
	}
	for i := range doc.Tasks {
		t := &doc.Tasks[i]
		for _, m := range reImportDependsOn.FindAllStringSubmatch(strings.Join(t.Text, " "), -1) {
			for _, k := range reImportDepKey.FindAllString(m[1], -1) {
				if !keys[k] {
					doc.Warnings = append(doc.Warnings, fmt.Sprintf(
						"line %d: %s depends on %s, which is not in tasks.md — ignored", t.Line, t.Key, k))
					continue
				}
				if k != t.Key && !containsStr(t.After, k) {
					t.After = append(t.After, k)
				}
			}
		}
	}
}

// ParseOpenSpecTasks reads an OpenSpec change's tasks.md: `## 1. Group`
// headings and `- [ ] 1.1 Description` lines, `[x]` for done.
func ParseOpenSpecTasks(text string) ImportDoc {
	p := importParser{seen: map[string]int{}}
	var scan lineScanner
	group, groupTitle, groups, section := 0, "", 0, 0
	inGroup := map[int]int{}

	for i, line := range splitLines(text) {
		lineno := i + 1
		if scan.skip(line) {
			p.flush()
			continue
		}
		if p.continuation(line) {
			continue
		}
		if strings.HasPrefix(line, "## ") || line == "##" {
			p.flush()
			groups++
			section++
			group, groupTitle = groups, ""
			if m := reOpenSpecGroup.FindStringSubmatch(line); m != nil {
				if m[1] != "" {
					group, _ = strconv.Atoi(m[1])
				}
				groupTitle = m[2]
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			p.flush()
			section++
			continue
		}
		m := reImportCheckbox.FindStringSubmatch(line)
		if m == nil {
			p.flush()
			continue
		}
		if group == 0 {
			group, groups = 1, 1
		}
		rest := m[3]
		inGroup[group]++
		task := ImportTask{
			Phase: group, PhaseTitle: groupTitle, Num: inGroup[group],
			Line: lineno, Done: m[2] != " ", Section: section,
			Text: []string{strings.TrimSpace(line)},
		}
		task.Key = fmt.Sprintf("%d.%d", group, task.Num)
		if n := reOpenSpecTaskNum.FindStringSubmatch(rest); n != nil {
			task.Phase, _ = strconv.Atoi(n[1])
			task.Num, _ = strconv.Atoi(n[2])
			task.Key = n[1] + "." + n[2]
			rest = rest[len(n[0]):]
		}
		task.Title = importTitle(rest)
		p.start(task, len(m[1]), lineno)
	}
	p.flush()
	return p.doc
}

// ImportDeps works out what each open task waits on, by key, the way
// spec-kit's /implement runs a tasks.md: phase by phase, and within a phase
// in order, except that a run of [P] tasks under one heading goes together.
// An explicit `depends on` replaces the order for its task. A task with
// nothing to wait on inside its phase waits on the previous phase's last
// tasks (those nothing in that phase waits on). Done tasks are not imported
// and nothing waits on them. OpenSpec has no [P]: a change's tasks run in
// the order written.
func ImportDeps(doc ImportDoc) map[string][]string {
	deps := map[string][]string{}
	var phases []int
	byPhase := map[int][]ImportTask{}
	open := map[string]bool{}
	for _, t := range doc.Tasks {
		if t.Done {
			continue
		}
		open[t.Key] = true
		if _, ok := byPhase[t.Phase]; !ok {
			phases = append(phases, t.Phase)
		}
		byPhase[t.Phase] = append(byPhase[t.Phase], t)
	}

	var prevSinks []string
	for _, ph := range phases {
		tasks := byPhase[ph]
		inPhase := map[string]bool{}
		for _, t := range tasks {
			inPhase[t.Key] = true
		}
		// Stages: a run of [P] tasks in one section, or one task.
		var stages [][]string
		stageOf := map[string]int{}
		for i, t := range tasks {
			if i > 0 && t.Parallel && tasks[i-1].Parallel && tasks[i-1].Section == t.Section {
				stages[len(stages)-1] = append(stages[len(stages)-1], t.Key)
			} else {
				stages = append(stages, []string{t.Key})
			}
			stageOf[t.Key] = len(stages) - 1
		}
		waitedOn := map[string]bool{}
		for _, t := range tasks {
			var base []string
			if len(t.After) > 0 {
				for _, k := range t.After {
					if open[k] {
						base = append(base, k)
					}
				}
			} else if s := stageOf[t.Key]; s > 0 {
				base = append(base, stages[s-1]...)
			}
			local := false
			for _, k := range base {
				if inPhase[k] {
					local = true
					waitedOn[k] = true
				}
			}
			if !local {
				for _, k := range prevSinks {
					if !containsStr(base, k) {
						base = append(base, k)
					}
				}
			}
			deps[t.Key] = base
		}
		prevSinks = nil
		for _, t := range tasks {
			if !waitedOn[t.Key] {
				prevSinks = append(prevSinks, t.Key)
			}
		}
	}
	return deps
}

// FindImportSources lists the features (spec-kit) or changes (OpenSpec)
// under dir that have a tasks.md, sorted by name, and the directories that
// have none yet. dir may be one feature or change, the directory holding
// them (spec-kit's specs/, OpenSpec's openspec/changes/), or the repository
// root. OpenSpec's archive/ holds finished changes and is not read.
func FindImportSources(format ImportFormat, dir string) ([]ImportSource, []string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dir)
	}
	if isFile(filepath.Join(abs, "tasks.md")) {
		return []ImportSource{newImportSource(format, abs)}, nil, nil
	}
	parent := abs
	var candidates []string
	if format == FormatSpecKit {
		candidates = []string{filepath.Join(abs, "specs")}
	} else {
		candidates = []string{filepath.Join(abs, "openspec", "changes"), filepath.Join(abs, "changes")}
	}
	for _, c := range candidates {
		if isDir(c) {
			parent = c
			break
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", parent, err)
	}
	var srcs []ImportSource
	var missing []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || (format == FormatOpenSpec && e.Name() == "archive") {
			continue
		}
		d := filepath.Join(parent, e.Name())
		if isFile(filepath.Join(d, "tasks.md")) {
			srcs = append(srcs, newImportSource(format, d))
		} else {
			missing = append(missing, d)
		}
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].Name < srcs[j].Name })
	sort.Strings(missing)
	return srcs, missing, nil
}

func newImportSource(format ImportFormat, dir string) ImportSource {
	return ImportSource{Format: format, Name: filepath.Base(dir), Dir: dir, TasksPath: filepath.Join(dir, "tasks.md")}
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// ReadFirstFiles are the documents beside a tasks.md an agent reads before
// its task, in the order it should: spec-kit's spec and plan and their
// companions, OpenSpec's proposal, design and spec deltas.
func ReadFirstFiles(format ImportFormat) []string {
	if format == FormatOpenSpec {
		return []string{"proposal.md", "design.md", "specs/"}
	}
	return []string{"spec.md", "plan.md", "data-model.md", "research.md", "contracts/", "quickstart.md"}
}

// ParseImportTasks parses a tasks.md in format.
func ParseImportTasks(format ImportFormat, text string) ImportDoc {
	if format == FormatOpenSpec {
		return ParseOpenSpecTasks(text)
	}
	return ParseSpecKitTasks(text)
}

// ImportSpecFileName is the orch spec an import writes for src, in the spec
// root: speckit-<feature>.md or openspec-<change>.md.
func ImportSpecFileName(src ImportSource) string {
	prefix := "speckit-"
	if src.Format == FormatOpenSpec {
		prefix = "openspec-"
	}
	return prefix + src.Name + ".md"
}

var reFeatureNumber = regexp.MustCompile(`^([0-9]+)-`)

// PreferredPackage is the package number src asks for: a spec-kit feature's
// own number (001-photo-albums is 1). 0 when it has none — an OpenSpec
// change, or a timestamp-named feature too long to be a package.
func PreferredPackage(src ImportSource) int {
	if src.Format != FormatSpecKit {
		return 0
	}
	m := reFeatureNumber.FindStringSubmatch(src.Name)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 || n >= 100000 {
		return 0
	}
	return n
}

// ImportedPackage reads back the package an earlier import of format gave
// the spec in text; false when text is not such an import.
func ImportedPackage(text string, format ImportFormat) (int, bool) {
	fm, _ := extractFrontmatter(text)
	if !fm.Present || fm.GeneratedBy != format.generatedBy() {
		return 0, false
	}
	n, err := strconv.Atoi(fm.Package)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// IsImportSpec reports whether text is a spec an import of format wrote,
// the only kind of file a re-run overwrites.
func IsImportSpec(text string, format ImportFormat) bool {
	fm, _ := extractFrontmatter(text)
	return fm.Present && fm.GeneratedBy == format.generatedBy()
}

// AllocateImportPackages gives every source a package number, the middle
// part of its tasks' ids (F<phase>.<package>.T<n>), so that two features'
// T001 never meet: the one an earlier import gave it (owned, by name), else
// the one it asks for (PreferredPackage) when no other task uses it, else
// one past the highest in use. used holds the packages other specs and
// tasks.json already use.
func AllocateImportPackages(srcs []ImportSource, owned map[string]int, used map[int]bool) map[string]int {
	taken := map[int]bool{}
	for k, v := range used {
		taken[k] = v
	}
	for _, s := range srcs {
		if p, ok := owned[s.Name]; ok {
			taken[p] = true
		}
	}
	next := func() int {
		maxP := 0
		for p, ok := range taken {
			if ok && p > maxP {
				maxP = p
			}
		}
		return maxP + 1
	}
	out := map[string]int{}
	for _, s := range srcs {
		if p, ok := owned[s.Name]; ok {
			out[s.Name] = p
			continue
		}
		p := PreferredPackage(s)
		if p == 0 || taken[p] {
			p = next()
		}
		taken[p] = true
		out[s.Name] = p
	}
	return out
}

// ImportSpecInput is what ImportSpec writes a spec from.
type ImportSpecInput struct {
	Source    ImportSource
	Doc       ImportDoc
	Package   int
	ProjectID string
	Model     string
	// SourceDir is the feature or change directory as the agent will find it
	// (relative to the project root, slash-separated).
	SourceDir string
	// ReadFirst are the documents beside tasks.md that exist, same form.
	ReadFirst []string
}

// ImportSpec is the orch spec `orch atomize --from` writes for one spec-kit
// feature or OpenSpec change: one task per open checkbox, id
// F<phase>.<package>.T<n>, with ImportDeps' dependencies, the files the line
// names, and a description that says where the task comes from — file, line
// and key — and quotes it. The quote (every line behind "> ") keeps the
// source's markdown away from the spec parser, as IssueSpec does with an
// issue body. Deterministic: the same tasks.md gives the same bytes, so a
// re-run sees nothing to change.
func ImportSpec(in ImportSpecInput) string {
	f := in.Source.Format
	tasksRel := strings.TrimSuffix(in.SourceDir, "/") + "/tasks.md"
	title := in.Doc.Title
	if title == "" {
		title = in.Source.Name
	}

	var b strings.Builder
	b.WriteString("---\ntype: spec\n")
	if in.ProjectID != "" {
		fmt.Fprintf(&b, "project_id: %s\n", in.ProjectID)
	}
	fmt.Fprintf(&b, "package: \"%d\"\n", in.Package)
	fmt.Fprintf(&b, "generated_by: %s\n", f.generatedBy())
	fmt.Fprintf(&b, "source: %s\n---\n\n", tasksRel)
	fmt.Fprintf(&b, "Generated by `%s` from `%s` (%s). A re-run rewrites this file:\n", f.generatedBy(), tasksRel, f.label())
	b.WriteString("edit tasks.md, not this, and import again. Checked tasks are not imported.\n")

	deps := ImportDeps(in.Doc)
	idOf := map[string]string{}
	for _, t := range in.Doc.Tasks {
		idOf[t.Key] = fmt.Sprintf("F%d.%d.T%d", t.Phase, in.Package, t.Num)
	}

	var phases []int
	byPhase := map[int][]ImportTask{}
	for _, t := range in.Doc.Tasks {
		if t.Done {
			continue
		}
		if _, ok := byPhase[t.Phase]; !ok {
			phases = append(phases, t.Phase)
		}
		byPhase[t.Phase] = append(byPhase[t.Phase], t)
	}
	unit := "Phase"
	if f == FormatOpenSpec {
		unit = "Group"
	}
	for _, ph := range phases {
		tasks := byPhase[ph]
		phTitle := tasks[0].PhaseTitle
		if phTitle == "" {
			phTitle = fmt.Sprintf("%s %d", unit, ph)
		}
		fmt.Fprintf(&b, "\n# F%d — %s\n\n", ph, oneLine(phTitle))
		fmt.Fprintf(&b, "## F%d.%d — %s\n", ph, in.Package, oneLine(title))
		for _, t := range tasks {
			fmt.Fprintf(&b, "\n### %s — %s\n\n", idOf[t.Key], t.Title)
			fmt.Fprintf(&b, "From %s `%s`, line %d, task %s", f.label(), tasksRel, t.Line, t.Key)
			if t.PhaseTitle != "" {
				fmt.Fprintf(&b, " (%s %d: %s)", strings.ToLower(unit), t.Phase, oneLine(t.PhaseTitle))
			}
			if t.Story != "" {
				fmt.Fprintf(&b, ", user story %s", t.Story)
			}
			b.WriteString(".\n")
			if len(in.ReadFirst) > 0 {
				quoted := make([]string, len(in.ReadFirst))
				for i, r := range in.ReadFirst {
					quoted[i] = "`" + r + "`"
				}
				fmt.Fprintf(&b, "Read first: %s.\n", strings.Join(quoted, ", "))
			}
			b.WriteString("\n")
			for _, line := range t.Text {
				b.WriteString("> " + line + "\n")
			}
			fmt.Fprintf(&b, "\n- **Model**: %s\n", in.Model)
			if d := deps[t.Key]; len(d) > 0 {
				ids := make([]string, len(d))
				for i, k := range d {
					ids[i] = idOf[k]
				}
				fmt.Fprintf(&b, "- **Dependencies**: %s\n", strings.Join(ids, ", "))
			}
			if len(t.Files) > 0 {
				quoted := make([]string, len(t.Files))
				for i, p := range t.Files {
					quoted[i] = "`" + p + "`"
				}
				fmt.Fprintf(&b, "- **Files**: %s\n", strings.Join(quoted, ", "))
			}
		}
	}
	return b.String()
}
