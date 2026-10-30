---
name: orch-discover
description: Use before planning on a repository that already has code (step 0 of orch-plan, or alone when you inherit a codebase). Reads the code, configs and CI, and writes docs/arch/000-discovery.md with evidence-backed facts, gaps, boot-time side effects and questions for the client, then proposes orch config and rules the operator confirms. Never writes code, specs or tasks.json.
---

# orch-discover — what the code really is, with evidence

Step 0 of **DISCOVER → PRD → ARCH → SPEC → TASKS**, for a repository that
already has code. Every later stage reads the code again for its own feature;
this one reads it once, writes down what is true, and leaves facts the PRD,
the architecture, the tasks and the operator can check.

**The rule: every claim cites the file it comes from** (`path` or
`path:line`). A fact with no file is a guess. Put it under *Unknowns* as a
question instead. Nothing is invented, and a README is not evidence about the
code: it is evidence about what the README says.

## When to run, and when to reuse

- The repository has source files and `docs/arch/000-discovery.md` does not
  exist: run.
- It exists: read its `commit:`, then `git log --oneline <commit>..HEAD -- <the
  paths the new work touches>`. Nothing there, or nothing that changes a
  section: reuse it and say so. Otherwise update the sections those commits
  touch, and the `commit:`.
- An empty repository (a new project): there is nothing to discover. Skip to
  `orch-prd`.

## How to read

Breadth first, then depth where the risk is:

1. The layout, the dependency manifests and lock files, the entrypoints
   (`main`, `Dockerfile`, `docker-compose*.yml`, `Procfile`, `package.json`
   scripts, Makefiles).
2. CI and hooks: `.github/workflows/`, `.gitlab-ci.yml`, `.husky/`,
   `.pre-commit-config.yaml`. They say what is really checked, which is not
   always what the README says.
3. The wiring: DI containers, module registries, path aliases
   (`tsconfig.json` `paths`, webpack/vite aliases), the ORM's data source.
4. Tracked files that should not be: `git ls-files` for `.env*`,
   `.vscode/launch.json`, `*.pem`, `*.key`, per-customer config overlays,
   dumps. **Never copy a secret's value into the document.** Name the file
   and the kind of value (a database password, an OAuth secret).
5. What happens when the app starts: the ORM's options (TypeORM
   `migrationsRun`, `synchronize`; a start script running `prisma migrate
   deploy` or `manage.py migrate`), schedulers and queues registered at boot,
   calls to real providers on startup.

Read; do not run the app, its migrations or its tests to find out. Running
them has the very side effects section 9 is about.

## Where it goes

`docs/arch/000-discovery.md`. **Never under `specs/`**: `orch atomize` walks
the spec root and warns about any non-spec file it meets.

```yaml
---
type: discovery
project_id: <the active project's id>
generated_by: orch-discover
generated_at: <YYYY-MM-DD>
commit: <short sha it was written from>
---
```

## Sections

```markdown
# <Project> — discovery

## 1. Components and deployables
Each thing that runs on its own: entrypoint, how it is built, how it is
started. — `Dockerfile`, `src/main.ts:12`

## 2. Stack, confirmed
| Piece | Version | Source |
| --- | --- | --- |
| Node | 20.11 | `.nvmrc` |

## 3. Observed architecture
Layers and who calls whom, as the imports show it; the DI/wiring hubs;
module aliases and where they point.

## 4. Integrations
External providers (payments, email, storage, auth…) and the file each
adapter lives in.

## 5. Conventions, confirmed
Tests (framework, where they live, how they are named), error handling, lint
and import rules. Only what the code follows, with an example file each.

## 6. Quality gates
What CI and the hooks actually run, command by command, with the file.

## 7. Docs vs implementation
Where the README or the docs disagree with the code: what they say, what the
code does, both files.

## 8. Gaps
Tracked secrets (file and kind of value, never the value), dead aliases,
missing configs, scripts that point at files that do not exist.

## 9. Boot-time side effects
What starting the app does besides serving: migrations it runs, jobs it
schedules, providers it calls. Each with its file. orch's local verify
starts the app for E2E (`verify.app`), so each of these runs on every
verify against whatever the task's `.env` points to.

## 10. Unknowns for the client
The questions only the client can answer, numbered, in one batch.

## 11. Proposals for orch
(see below)
```

Write a section as "none found" rather than leave it out: an empty Gaps
section that says so is a fact, and a missing one is a question.

## Proposals — applied only after the operator says yes

End with what the discovery suggests for orch, each line with the section
and file it comes from. Show them; **change nothing until the operator
confirms**, and apply only the ones they pick:

- **`gate.critical_paths`**: migrations, auth, billing and permission paths
  found in sections 3 and 4. In `orch run --mode semi`, a task touching
  them is offered to the operator first.
- **`guard.protected_paths`**: the tracked secret files from section 8. A
  task that changes one is blocked before its push.
- **Rules** (`kind: rule` in `.orch/knowledge/`): the layer conventions of
  sections 3 and 5 that a task must not break, each with its `area`. They go
  to every task touching that area and to the verify review.
- **`verify.commands`**: the checks of section 6, so the local verify runs
  what CI runs.
- **`verify.app`**: if section 9 found side effects, say what the task's
  `.env` must point to before turning it on, or advise leaving it off.

To apply, once confirmed:

```bash
orch config set gate.critical_paths "src/db/migrations/**, src/auth/**"
orch config set guard.protected_paths ".vscode/launch.json"
orch config set verify.commands "pnpm lint, pnpm test"
orch knowledge add --kind rule --title "Controllers call services, never repositories" --area "src/controllers/**" --body "A controller calls a service; only a service uses a repository."
```

## Then

Report the path, the counts per section (gaps, side effects, unknowns), and
which proposals the operator applied. When run from `orch-plan`, continue with
`orch-prd`. The unknowns go to the client before the PRD settles anything
they could change.
