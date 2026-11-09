# Running spec-kit and OpenSpec plans with orch

[spec-kit](https://github.com/github/spec-kit) and
[OpenSpec](https://github.com/Fission-AI/OpenSpec) decide **what** to build:
a spec, a plan, and a `tasks.md` checklist. orch runs that checklist: each
open task goes to an agent in its own worktree, tasks that can run together
do, and each one ends in a pull request.

```sh
orch atomize --from spec-kit --model claude/sonnet          # preview
orch atomize --from spec-kit --model claude/sonnet --apply  # write specs + tasks.json
orch task set --id F1.1.T2 --status todo                    # promote what should run
orch run
```

`--from openspec` works the same way. The full flag reference is the
`atomize` row of [`CLI.md`](CLI.md).

## What is read

| | spec-kit | OpenSpec |
|---|---|---|
| Where | `specs/<feature>/tasks.md` (e.g. `specs/001-photo-albums/`) | `openspec/changes/<change>/tasks.md`; `archive/` is skipped |
| Groups | `## Phase N: Title` (older `3.1`, `3.2` are renumbered 1, 2, …) | `## N. Title` |
| Task | `- [ ] T012 [P] [US1] Create Album model in src/models/album.py` | `- [ ] 2.1 Create ThemeToggle component` |
| Done | `- [x]` / `- [X]`: not imported | same |
| Read first | `spec.md`, `plan.md`, `data-model.md`, `research.md`, `contracts/`, `quickstart.md` | `proposal.md`, `design.md`, `specs/` |

`DIR` (default: the project root) can be the repository, the directory that
holds the features or changes, or a single one:
`orch atomize --from spec-kit specs/001-photo-albums --model M`.

## What it writes

One orch spec per feature or change in the spec root:
`specs/speckit-001-photo-albums.md`, `specs/openspec-add-dark-mode.md`. Each
open checkbox becomes one task:

- **Id** `F<phase>.<package>.T<n>`. spec-kit's `T012` in phase 3 of feature
  `001` is `F3.1.T12`; a lettered `T006a` is `T1006`. OpenSpec's `2.3` is
  `F2.<package>.T3`. The package is the feature's number unless another spec
  already uses it; an OpenSpec change gets the next free one. It is recorded
  in the generated spec, so ids never move between runs.
- **Dependencies**, the way spec-kit's `/implement` runs a `tasks.md`: phase
  by phase; within a phase in order; consecutive `[P]` tasks under the same
  heading run together; `(depends on T012, T013)` replaces the order for its
  task. A task with nothing to wait on inside its phase waits on the previous
  phase's last tasks. OpenSpec has no parallel marker, so a change's tasks run
  in order — different changes run in parallel.
- **Files**: paths the line names (backticked, or with a slash and an
  extension). orch uses them to point out edits outside the task's scope.
- **Description**: the source file, line and task key, the documents to read
  first, and the line itself, quoted.

## Re-running

The generated spec is derived from `tasks.md`; edit `tasks.md`, not the
spec, and import again. A run whose `tasks.md` has not changed writes
nothing (`unchanged`). A task checked off since the last import leaves the
spec; orch keeps it in `tasks.json` with its status (the diff lists it as
an orphan). orch never ticks the boxes in `tasks.md` — parallel agents
editing the same checklist would conflict — so tick them yourself if
spec-kit or OpenSpec should see the work as done.

A file at the generated path that the import did not write (no
`generated_by: orch atomize --from …` in its frontmatter) is never
overwritten; the command stops and names it.

## Example

The fixtures in `internal/atomize/testdata/import/` are small projects in
each format; `internal/cli/testdata/script/atomize-from-spec.txtar` runs the
whole import on them.
