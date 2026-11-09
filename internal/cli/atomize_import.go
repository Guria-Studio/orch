package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Guria-Studio/orch/internal/atomize"
	"github.com/Guria-Studio/orch/internal/model"
	"github.com/Guria-Studio/orch/internal/project"
	"github.com/Guria-Studio/orch/internal/router"
)

// atomizeImportOpts is what `orch atomize --from <format> [DIR]` was asked.
type atomizeImportOpts struct {
	format    atomize.ImportFormat
	dir       string // "" = the project root
	model     string
	specsRoot string
	tasksPath string
	apply     bool
	noBackup  bool
	list      bool
}

// specImport is one feature or change and the orch spec it gets.
type specImport struct {
	src     atomize.ImportSource
	path    string // the generated spec, absolute
	text    string
	pkg     int
	open    int
	done    int
	status  string // new | update | unchanged | empty (nothing open, nothing written)
	warning []string
}

var reTaskPkg = regexp.MustCompile(`^F\d+\.(\d+)\.T\d+$`)

func pkgOfID(id string) (int, bool) {
	m := reTaskPkg.FindStringSubmatch(id)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// specRefFile is a specRef's file part, as the spec root names it.
func specRefFile(ref string) string {
	if i := strings.Index(ref, "#"); i >= 0 {
		return ref[:i]
	}
	return ref
}

// runAtomizeImport is `orch atomize --from spec-kit|openspec [DIR]` (#526):
// each spec-kit feature (specs/<feature>/tasks.md) or OpenSpec change
// (openspec/changes/<change>/tasks.md) under DIR gets one orch spec in the
// spec root, speckit-<feature>.md or openspec-<change>.md, and the specs
// become tasks through the same parse, merge and diff as any other `orch
// atomize`. Read-only unless --apply, which writes the specs and then
// tasks.json.
//
// The generated spec is derived: a re-run rewrites it from tasks.md (only a
// file it wrote itself, marked in its frontmatter) and is a no-op when
// tasks.md has not changed. Ids are F<phase>.<package>.T<n>, the package
// fixed per feature on its first import and read back from the spec after,
// so a re-run keeps every id.
func runAtomizeImport(cmd *cobra.Command, flags *projectFlags, o atomizeImportOpts) error {
	paths, cfg, err := loadProjectConfig(flags)
	if err != nil {
		return err
	}
	rtr, err := router.Load(paths.RouterYAML())
	if err != nil {
		return withExitCode(2, fmt.Errorf("checking --model against %s: %w", paths.RouterYAML(), err))
	}
	if err := checkSyncModel(o.model, rtr.Keys()); err != nil {
		return withExitCode(2, err)
	}
	dir := o.dir
	if dir == "" {
		dir = paths.Root
	}
	srcs, missing, err := atomize.FindImportSources(o.format, dir)
	if err != nil {
		return withExitCode(2, err)
	}

	specFiles, err := atomize.WalkSpecFiles(o.specsRoot)
	if err != nil {
		return fmt.Errorf("walk specs under %s: %w", o.specsRoot, err)
	}
	existing, err := atomize.LoadExisting(o.tasksPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", o.tasksPath, err)
	}

	plan, err := planSpecImport(srcs, specFiles, existing.Tasks, paths.Root, o, paths.ID)
	if err != nil {
		return err
	}
	if err := printSpecImportPlan(cmd, plan, missing, o, paths.Root); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	overrides := map[string]string{}
	var toWrite []specImport
	for _, p := range plan {
		if p.status == "new" || p.status == "update" {
			toWrite = append(toWrite, p)
			overrides[p.path] = p.text
		}
	}
	if o.apply && len(toWrite) > 0 {
		if err := os.MkdirAll(o.specsRoot, 0o750); err != nil {
			return fmt.Errorf("creating %s: %w", o.specsRoot, err)
		}
		for _, w := range toWrite {
			if err := os.WriteFile(w.path, []byte(w.text), 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", w.path, err)
			}
		}
		if _, err := fmt.Fprintf(out, "Wrote %d spec(s)\n", len(toWrite)); err != nil {
			return err
		}
		overrides = nil
		if specFiles, err = atomize.WalkSpecFiles(o.specsRoot); err != nil {
			return fmt.Errorf("walk specs under %s: %w", o.specsRoot, err)
		}
	}
	// Dry run: the specs are parsed from memory, so the diff below is the
	// one --apply would produce.
	files := append([]string(nil), specFiles...)
	for p := range overrides {
		if !containsString(files, p) {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	parse, err := atomize.ParseFilesWith(files, overrides, o.specsRoot, paths.ID)
	if err != nil {
		return fmt.Errorf("parse specs under %s: %w", o.specsRoot, err)
	}
	if len(parse.FilesScanned) == 0 {
		return nil
	}
	if o.apply {
		for _, w := range specGitWarnings(paths.Root, cfg.Dispatch.BaseBranch, parse) {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), w); err != nil {
				return err
			}
		}
	}
	if o.list {
		return renderAtomizeList(cmd, parse)
	}

	_, diff := atomize.MergeTasks(existing, parse.Tasks)
	if _, err := fmt.Fprint(out, "\n"+atomize.RenderDiff(diff, parse)); err != nil {
		return err
	}
	if !o.apply {
		if len(diff.NewTasks) > 0 {
			_, err := fmt.Fprintln(out, "New tasks are written in backlog, which orch run does not dispatch: "+
				"after --apply, promote the ones to run with orch task set --id <ID> --status todo.")
			return err
		}
		return nil
	}
	if len(diff.NewTasks) == 0 && len(diff.Updated) == 0 {
		_, err := fmt.Fprintln(out, "--apply pasado pero no hay cambios que aplicar.")
		return err
	}
	var written atomize.MergeDiff
	backupPath, err := project.MutateTasks(paths.StateDir(), o.tasksPath, !o.noBackup, func(tf *model.TasksFile) error {
		*tf, written = atomize.MergeTasks(*tf, parse.Tasks)
		return nil
	})
	if err != nil {
		return fmt.Errorf("write %s: %w", o.tasksPath, err)
	}
	if _, err := fmt.Fprintf(out, "✓ Escrito: %s\n", o.tasksPath); err != nil {
		return err
	}
	if backupPath != "" {
		if _, err := fmt.Fprintf(out, "  backup: %s\n", backupPath); err != nil {
			return err
		}
	}
	return noteNewInBacklog(out, written.NewTasks)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// planSpecImport works out, per source, the spec it gets and whether that
// is new, changed or already there. It refuses to go on when a file at a
// generated spec's path was not written by this import.
func planSpecImport(srcs []atomize.ImportSource, specFiles []string, existing []model.Task,
	root string, o atomizeImportOpts, projectID string) ([]specImport, error) {
	genPath := map[string]string{} // source name -> spec path
	genFile := map[string]string{} // spec file name -> source name
	current := map[string]string{} // source name -> spec text on disk
	owned := map[string]int{}
	for _, s := range srcs {
		name := atomize.ImportSpecFileName(s)
		p := filepath.Join(o.specsRoot, name)
		genPath[s.Name], genFile[name] = p, s.Name
		data, err := os.ReadFile(p) // #nosec G304 -- a path under the project's own spec root
		if err == nil {
			text := string(data)
			if !atomize.IsImportSpec(text, o.format) {
				return nil, fmt.Errorf("%s was not written by orch atomize --from %s: move it, or the import would overwrite it",
					relTo(root, p), o.format)
			}
			current[s.Name] = text
			if pkg, ok := atomize.ImportedPackage(text, o.format); ok {
				owned[s.Name] = pkg
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
	}
	// The spec was deleted but its tasks are still in tasks.json: keep
	// their package, so the ids stay the same.
	for _, t := range existing {
		if name, ok := genFile[specRefFile(t.SpecRef)]; ok {
			if _, has := owned[name]; !has {
				if pkg, ok := pkgOfID(t.ID); ok {
					owned[name] = pkg
				}
			}
		}
	}

	// Packages other specs and tasks use, so a feature's ids never meet theirs.
	used := map[int]bool{}
	for _, t := range existing {
		if _, ours := genFile[specRefFile(t.SpecRef)]; ours {
			continue
		}
		if pkg, ok := pkgOfID(t.ID); ok {
			used[pkg] = true
		}
	}
	var others []string
	for _, f := range specFiles {
		if _, ours := genFile[filepath.Base(f)]; !ours || filepath.Dir(f) != filepath.Clean(o.specsRoot) {
			others = append(others, f)
		}
	}
	otherParse, err := atomize.ParseFiles(others, o.specsRoot, projectID)
	if err != nil {
		return nil, fmt.Errorf("parse specs under %s: %w", o.specsRoot, err)
	}
	for _, t := range otherParse.Tasks {
		used[t.Package] = true
	}
	pkgs := atomize.AllocateImportPackages(srcs, owned, used)

	var plan []specImport
	for _, s := range srcs {
		data, err := os.ReadFile(s.TasksPath) // #nosec G304 -- the tasks.md the operator pointed --from at
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", s.TasksPath, err)
		}
		doc := atomize.ParseImportTasks(o.format, string(data))
		var readFirst []string
		for _, f := range atomize.ReadFirstFiles(o.format) {
			info, err := os.Stat(filepath.Join(s.Dir, strings.TrimSuffix(f, "/")))
			if err != nil || info.IsDir() != strings.HasSuffix(f, "/") {
				continue
			}
			rel := relTo(root, filepath.Join(s.Dir, f))
			if strings.HasSuffix(f, "/") {
				rel += "/"
			}
			readFirst = append(readFirst, rel)
		}
		text := atomize.ImportSpec(atomize.ImportSpecInput{
			Source: s, Doc: doc, Package: pkgs[s.Name], ProjectID: projectID, Model: o.model,
			SourceDir: relTo(root, s.Dir), ReadFirst: readFirst,
		})
		p := specImport{src: s, path: genPath[s.Name], text: text, pkg: pkgs[s.Name]}
		for _, t := range doc.Tasks {
			if t.Done {
				p.done++
			} else {
				p.open++
			}
		}
		for _, w := range doc.Warnings {
			p.warning = append(p.warning, relTo(root, s.TasksPath)+": "+w)
		}
		cur, onDisk := current[s.Name]
		switch {
		case onDisk && cur == text:
			p.status = "unchanged"
		case onDisk:
			p.status = "update"
		case p.open == 0:
			p.status = "empty"
		default:
			p.status = "new"
		}
		plan = append(plan, p)
	}
	return plan, nil
}

// printSpecImportPlan is the import's half of the report; the atomize diff
// follows it.
func printSpecImportPlan(cmd *cobra.Command, plan []specImport, missing []string, o atomizeImportOpts, root string) error {
	var b strings.Builder
	label := map[atomize.ImportFormat]string{atomize.FormatSpecKit: "spec-kit", atomize.FormatOpenSpec: "OpenSpec"}[o.format]
	unit := "feature"
	if o.format == atomize.FormatOpenSpec {
		unit = "change"
	}
	changed := 0
	for _, p := range plan {
		if p.status == "new" || p.status == "update" {
			changed++
		}
	}
	switch {
	case len(plan) == 0:
		fmt.Fprintf(&b, "No %s %s with a tasks.md under %s.\n", label, unit, relTo(root, absOr(o.dir, root)))
	case changed == 0:
		fmt.Fprintf(&b, "Nothing to write: every %s %s is already imported as it is now.\n", label, unit)
	case o.apply:
		fmt.Fprintf(&b, "Writing %d spec(s) from %s:\n", changed, label)
	default:
		fmt.Fprintf(&b, "Would write %d spec(s) from %s:\n", changed, label)
	}
	for _, p := range plan {
		fmt.Fprintf(&b, "  %s  %s  %s: %d open, %d done, package %d\n",
			relTo(root, p.path), p.status, p.src.Name, p.open, p.done, p.pkg)
	}
	if len(missing) > 0 {
		rel := make([]string, len(missing))
		for i, m := range missing {
			rel[i] = relTo(root, m)
		}
		fmt.Fprintf(&b, "No tasks.md yet (nothing to import): %s\n", strings.Join(rel, ", "))
	}
	for _, p := range plan {
		for _, w := range p.warning {
			fmt.Fprintf(&b, "warning: %s\n", w)
		}
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), b.String())
	return err
}

func absOr(dir, root string) string {
	if dir == "" {
		return root
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

var (
	// errFromWithFile: --file names one spec on disk, --from writes new ones.
	errFromWithFile = errors.New("--from and --file name two different inputs: pass one")
	// errFromWithIssues: two importers, one run.
	errFromWithIssues = errors.New("--from and --from-issues are two different imports: run one, then the other")
)
