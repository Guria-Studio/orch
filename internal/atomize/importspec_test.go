package atomize

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The fixtures under testdata/import are modelled on the real formats:
// spec-kit's templates/tasks-template.md and two real features written with
// it (specs/010-pager in pamburus/hl, specs/003-focus in tiko-run/tiko), and
// OpenSpec's README example plus openspec/changes/*/tasks.md in
// Fission-AI/OpenSpec. The PR that added them cites the commits.

func readFixture(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata", "import"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func keysOf(tasks []ImportTask) []string {
	out := []string{}
	for _, task := range tasks {
		out = append(out, task.Key)
	}
	return out
}

func findImportTask(t *testing.T, doc ImportDoc, key string) ImportTask {
	t.Helper()
	for _, task := range doc.Tasks {
		if task.Key == key {
			return task
		}
	}
	t.Fatalf("no task %s in %v", key, keysOf(doc.Tasks))
	return ImportTask{}
}

func TestParseImportFormat(t *testing.T) {
	for in, want := range map[string]ImportFormat{
		"spec-kit": FormatSpecKit, "speckit": FormatSpecKit, "Spec-Kit": FormatSpecKit,
		"openspec": FormatOpenSpec, "OpenSpec": FormatOpenSpec,
	} {
		got, err := ParseImportFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseImportFormat(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParseImportFormat("kiro")
	if err == nil || !strings.Contains(err.Error(), "spec-kit") || !strings.Contains(err.Error(), "openspec") {
		t.Errorf("an unknown format should name the two it knows, got %v", err)
	}
}

// Every checkbox line with a task id is read, in order: its phase, the [P]
// and [USn] markers, a `(depends on ...)` clause, the paths it names and
// whether it is checked. Lines inside an HTML comment or a fence (the
// template's own samples) and a TXXX placeholder are not tasks.
func TestParseSpecKitTasks(t *testing.T) {
	doc := ParseSpecKitTasks(readFixture(t, "spec-kit", "specs", "001-photo-albums", "tasks.md"))

	if doc.Title != "Photo albums" {
		t.Errorf("Title = %q", doc.Title)
	}
	want := []string{"T001", "T002", "T003", "T004", "T005", "T006", "T006a",
		"T010", "T011", "T012", "T013", "T014", "T015", "T016", "T020"}
	if got := keysOf(doc.Tasks); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v\nwant   %v", got, want)
	}
	if len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "TXXX") {
		t.Errorf("warnings = %q, want one naming the TXXX placeholder", doc.Warnings)
	}

	t001 := findImportTask(t, doc, "T001")
	if !t001.Done || t001.Phase != 1 || t001.PhaseTitle != "Setup (Shared Infrastructure)" {
		t.Errorf("T001 = %+v", t001)
	}
	if !findImportTask(t, doc, "T016").Done {
		t.Error("T016 is checked with [X]: it is done")
	}
	t003 := findImportTask(t, doc, "T003")
	if !t003.Parallel || t003.Done || t003.Files[0] != ".oxlintrc.json" {
		t.Errorf("T003 = %+v", t003)
	}
	t006a := findImportTask(t, doc, "T006a")
	if t006a.Num != 1006 || t006a.Phase != 2 {
		t.Errorf("T006a: Num = %d, Phase = %d; want 1006, 2", t006a.Num, t006a.Phase)
	}
	if !reflect.DeepEqual(t006a.Files, []string{"src/lib/deps.ts"}) {
		t.Errorf("T006a files = %q", t006a.Files)
	}
	t014 := findImportTask(t, doc, "T014")
	if t014.Story != "US1" || t014.Phase != 3 || t014.Parallel {
		t.Errorf("T014 = %+v", t014)
	}
	if !reflect.DeepEqual(t014.After, []string{"T012", "T013"}) {
		t.Errorf("T014 After = %q", t014.After)
	}
	if t014.Title != "Implement AlbumService in src/services/album_service.py" {
		t.Errorf("T014 Title = %q (markers and the depends clause belong out of it)", t014.Title)
	}
	if !reflect.DeepEqual(t014.Files, []string{"src/services/album_service.py"}) {
		t.Errorf("T014 files = %q", t014.Files)
	}
	t010 := findImportTask(t, doc, "T010")
	if !reflect.DeepEqual(t010.Files, []string{"tests/contract/test_albums.py"}) {
		t.Errorf("T010 files = %q (an URL path like /albums is not a file)", t010.Files)
	}
	if t010.Section == findImportTask(t, doc, "T012").Section {
		t.Error("the Tests and Implementation subsections are different sections")
	}
	t015 := findImportTask(t, doc, "T015")
	if len(t015.Text) != 2 || !strings.Contains(t015.Text[1], "1-80 characters") {
		t.Errorf("T015 keeps its continuation line: %q", t015.Text)
	}
	if t015.Line != 70 {
		t.Errorf("T015 Line = %d, want 70", t015.Line)
	}
}

// OpenSpec: `## N. Group` headings and `- [ ] N.M` items; nested bullets
// stay with their task.
func TestParseOpenSpecTasks(t *testing.T) {
	doc := ParseOpenSpecTasks(readFixture(t, "openspec", "openspec", "changes", "add-dark-mode", "tasks.md"))
	want := []string{"1.1", "1.2", "1.3", "2.1", "2.2", "2.3", "3.1"}
	if got := keysOf(doc.Tasks); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	t11 := findImportTask(t, doc, "1.1")
	if !t11.Done || t11.Phase != 1 || t11.Num != 1 || t11.PhaseTitle != "Theme infrastructure" {
		t.Errorf("1.1 = %+v", t11)
	}
	t22 := findImportTask(t, doc, "2.2")
	if t22.Phase != 2 || t22.Num != 2 || len(t22.Text) != 2 {
		t.Errorf("2.2 = %+v", t22)
	}
	if got := findImportTask(t, doc, "2.1").Files; !reflect.DeepEqual(got, []string{"src/components/ThemeToggle.tsx"}) {
		t.Errorf("2.1 files = %q", got)
	}
	if got := findImportTask(t, doc, "3.1").Files; len(got) != 0 {
		t.Errorf("3.1 files = %q (a command in backticks is not a file)", got)
	}
}

// Dependencies follow how spec-kit's /implement runs a tasks.md: phase by
// phase; within a phase in order, a run of [P] tasks together; a subsection
// heading ends a run; an explicit `depends on` wins. Done tasks are not
// imported and nothing waits on them.
func TestImportDepsSpecKit(t *testing.T) {
	doc := ParseSpecKitTasks(readFixture(t, "spec-kit", "specs", "001-photo-albums", "tasks.md"))
	deps := ImportDeps(doc)
	want := map[string][]string{
		"T002":  nil,
		"T003":  {"T002"},
		"T004":  {"T003"},
		"T005":  {"T004"},
		"T006":  {"T004"},
		"T006a": {"T005", "T006"},
		"T010":  {"T006a"},
		"T011":  {"T006a"},
		"T012":  {"T010", "T011"},
		"T013":  {"T010", "T011"},
		"T014":  {"T012", "T013"},
		"T015":  {"T014"},
		"T020":  {"T015"},
	}
	if !reflect.DeepEqual(deps, want) {
		t.Errorf("deps =\n%v\nwant\n%v", deps, want)
	}
}

func TestImportDepsOpenSpecIsInOrder(t *testing.T) {
	doc := ParseOpenSpecTasks(readFixture(t, "openspec", "openspec", "changes", "add-dark-mode", "tasks.md"))
	want := map[string][]string{
		"1.2": nil, "1.3": {"1.2"}, "2.1": {"1.3"}, "2.2": {"2.1"}, "2.3": {"2.2"}, "3.1": {"2.3"},
	}
	if got := ImportDeps(doc); !reflect.DeepEqual(got, want) {
		t.Errorf("deps = %v, want %v", got, want)
	}
}

func TestFindImportSources(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("testdata", "import", "spec-kit"))
	srcs, missing, err := FindImportSources(FormatSpecKit, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || srcs[0].Name != "001-photo-albums" ||
		srcs[0].TasksPath != filepath.Join(root, "specs", "001-photo-albums", "tasks.md") {
		t.Errorf("spec-kit sources = %+v", srcs)
	}
	if !reflect.DeepEqual(missing, []string{filepath.Join(root, "specs", "002-share-links")}) {
		t.Errorf("missing = %q, want the feature without tasks.md", missing)
	}
	// The specs directory itself, and one feature directory, work too.
	for _, dir := range []string{filepath.Join(root, "specs"), filepath.Join(root, "specs", "001-photo-albums")} {
		got, _, err := FindImportSources(FormatSpecKit, dir)
		if err != nil || len(got) != 1 || got[0].Name != "001-photo-albums" {
			t.Errorf("FindImportSources(%s) = %+v, %v", dir, got, err)
		}
	}

	osRoot, _ := filepath.Abs(filepath.Join("testdata", "import", "openspec"))
	srcs, _, err = FindImportSources(FormatOpenSpec, osRoot)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range srcs {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"add-dark-mode", "add-export"}) {
		t.Errorf("openspec changes = %v (archived changes are done)", names)
	}

	if _, _, err := FindImportSources(FormatSpecKit, filepath.Join(root, "nope")); err == nil {
		t.Error("a directory that does not exist is an error")
	}
}

func TestImportSpecFileNameAndPreferredPackage(t *testing.T) {
	sk := ImportSource{Format: FormatSpecKit, Name: "001-photo-albums"}
	if got := ImportSpecFileName(sk); got != "speckit-001-photo-albums.md" {
		t.Errorf("file name = %q", got)
	}
	if got := PreferredPackage(sk); got != 1 {
		t.Errorf("preferred package = %d, want the feature number", got)
	}
	os := ImportSource{Format: FormatOpenSpec, Name: "add-dark-mode"}
	if got := ImportSpecFileName(os); got != "openspec-add-dark-mode.md" {
		t.Errorf("file name = %q", got)
	}
	if got := PreferredPackage(os); got != 0 {
		t.Errorf("an OpenSpec change has no number: %d", got)
	}
}

func photoAlbumsSpec(t *testing.T, model string) (string, string) {
	t.Helper()
	root, _ := filepath.Abs(filepath.Join("testdata", "import", "spec-kit"))
	srcs, _, err := FindImportSources(FormatSpecKit, root)
	if err != nil || len(srcs) != 1 {
		t.Fatal(srcs, err)
	}
	doc := ParseSpecKitTasks(readFixture(t, "spec-kit", "specs", "001-photo-albums", "tasks.md"))
	text := ImportSpec(ImportSpecInput{
		Source: srcs[0], Doc: doc, Package: 1, ProjectID: "albums", Model: model,
		SourceDir: "specs/001-photo-albums", ReadFirst: []string{"specs/001-photo-albums/spec.md", "specs/001-photo-albums/plan.md"},
	})
	return text, ImportSpecFileName(srcs[0])
}

// The generated spec goes through the normal parser: one task per open
// checkbox, ids F<phase>.<package>.T<n>, the dependencies above, the files,
// the model, and a description that points back at the source line and
// quotes it, so nothing in it is read as a spec field.
func TestImportSpecParsesThroughTheNormalParser(t *testing.T) {
	text, name := photoAlbumsSpec(t, "claude-sonnet-4-6")
	specs := t.TempDir()
	res := ParseText(text, filepath.Join(specs, name), specs, "albums")
	if len(res.Warnings) != 0 {
		t.Errorf("warnings: %q", res.Warnings)
	}
	var ids []string
	byID := map[string]ParsedTask{}
	for _, task := range res.Tasks {
		ids = append(ids, task.ID)
		byID[task.ID] = task
	}
	wantIDs := []string{"F1.1.T2", "F1.1.T3", "F2.1.T4", "F2.1.T5", "F2.1.T6", "F2.1.T1006",
		"F3.1.T10", "F3.1.T11", "F3.1.T12", "F3.1.T13", "F3.1.T14", "F3.1.T15", "F4.1.T20"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("ids = %v\nwant  %v", ids, wantIDs)
	}
	t14 := byID["F3.1.T14"]
	if !reflect.DeepEqual(t14.Dependencies, []string{"F3.1.T12", "F3.1.T13"}) {
		t.Errorf("deps = %q", t14.Dependencies)
	}
	if !reflect.DeepEqual(byID["F2.1.T1006"].Dependencies, []string{"F2.1.T5", "F2.1.T6"}) {
		t.Errorf("T006a deps = %q", byID["F2.1.T1006"].Dependencies)
	}
	if len(byID["F1.1.T2"].Dependencies) != 0 {
		t.Errorf("the first open task waits on nothing: %q", byID["F1.1.T2"].Dependencies)
	}
	if t14.Model != "claude-sonnet-4-6" || t14.Title != "Implement AlbumService in src/services/album_service.py" {
		t.Errorf("T14 = %+v", t14)
	}
	if !reflect.DeepEqual(t14.Files, []string{"src/services/album_service.py"}) {
		t.Errorf("files = %q", t14.Files)
	}
	if t14.SpecRef != name+"#F3.1.T14" {
		t.Errorf("specRef = %q", t14.SpecRef)
	}
	for _, want := range []string{
		"specs/001-photo-albums/tasks.md",
		"line 69",
		"T014",
		"US1",
		"specs/001-photo-albums/spec.md",
		"> - [ ] T014 [US1] Implement AlbumService in src/services/album_service.py (depends on T012, T013)",
	} {
		if !strings.Contains(t14.Description, want) {
			t.Errorf("description lacks %q:\n%s", want, t14.Description)
		}
	}
	if !strings.Contains(byID["F3.1.T15"].Description, ">   with validation of the album name") {
		t.Errorf("T15's continuation line belongs in its description:\n%s", byID["F3.1.T15"].Description)
	}
}

// Same input, same bytes: a re-run finds the spec unchanged. The package
// it was given is in its frontmatter, where the next run reads it back.
func TestImportSpecIsDeterministicAndRecordsItsPackage(t *testing.T) {
	a, _ := photoAlbumsSpec(t, "m")
	b, _ := photoAlbumsSpec(t, "m")
	if a != b {
		t.Error("two runs on the same tasks.md wrote different specs")
	}
	pkg, ok := ImportedPackage(a, FormatSpecKit)
	if !ok || pkg != 1 {
		t.Errorf("ImportedPackage = %d, %v", pkg, ok)
	}
	if _, ok := ImportedPackage(a, FormatOpenSpec); ok {
		t.Error("a spec-kit import is not an OpenSpec one")
	}
	if _, ok := ImportedPackage("---\ntype: spec\npackage: \"1\"\n---\n# F1 — x\n", FormatSpecKit); ok {
		t.Error("a hand-written spec is not an import")
	}
}

func TestAllocateImportPackages(t *testing.T) {
	srcs := []ImportSource{
		{Format: FormatSpecKit, Name: "001-a"},
		{Format: FormatSpecKit, Name: "002-b"},
		{Format: FormatSpecKit, Name: "003-c"},
		{Format: FormatOpenSpec, Name: "add-x"},
	}
	// 002 is already imported as package 9; package 1 belongs to a hand-written spec.
	owned := map[string]int{"002-b": 9}
	used := map[int]bool{1: true, 9: true}
	got := AllocateImportPackages(srcs, owned, used)
	want := map[string]int{"001-a": 10, "002-b": 9, "003-c": 3, "add-x": 11}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %v, want %v", got, want)
	}
}
