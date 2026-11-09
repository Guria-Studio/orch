# orch — Step-by-step user manual (Go CLI)

> Also available in: [Español](MANUAL.es.md) · [Português](MANUAL.pt.md)
>
> This manual documents the **Go** `orch` binary — the single-binary
> rewrite. Every command below is real output from `orch --help` on a
> built binary; the full flag reference lives in [`CLI.md`](CLI.md), which
> is the source of truth this manual is kept in sync with. Looking for the
> old Python `orch`? See [Python legacy](#python-legacy) at the bottom.

**orch runs AI agents as a team and shows your client a live page, not a
status thread.** It is for freelancers and agencies building with AI: it splits
the work into a graph of tasks, runs each one on the coding agent CLI you route
it to, in parallel, and keeps a page your client can open to see where the work
stands. No client? The same page is your own status board.

```bash
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
cd my-project                        # a git repository
orch start "what you want to build"
```

`orch start` sets the project up with the AI CLIs it finds, lets claude plan the
idea with you, estimates the plan and runs the first tasks with the dashboard
open. The rest of this manual is that same path step by step, and what comes
after it: `orch run` for the remaining tasks, `orch dashboard` to watch and
review the work, and `orch publish` for the client page.

**Total setup time**: ~2 minutes (one `curl | sh`, no Python, no venv).
**Per-feature time**: a few minutes of spec-writing + unattended execution.

---

## Contents

1. [Install](#1-install)
2. [Create a new project](#2-create-a-new-project)
3. [Write specs and atomize them into tasks.json](#3-write-specs-and-atomize-them-into-tasksjson)
4. [Check the plan before running](#4-check-the-plan-before-running)
5. [Run — dispatch tasks to AI agents](#5-run--dispatch-tasks-to-ai-agents)
6. [Open the dashboard](#6-open-the-dashboard)
7. [Inspect a run: status, tasks, events, logs](#7-inspect-a-run-status-tasks-events-logs)
8. [Fix things by hand: task set, task-status, reset](#8-fix-things-by-hand-task-set-task-status-reset)
9. [The model router](#9-the-model-router)
10. [Inspecting config](#10-inspecting-config)
11. [MCP — letting an agent drive orch directly](#11-mcp--letting-an-agent-drive-orch-directly)
12. [Installing orch's skills](#12-installing-orchs-skills)
13. [When something fails](#13-when-something-fails)
14. [Updating orch](#14-updating-orch)
15. [Features by layer](#15-features-by-layer)
16. [Python legacy](#python-legacy)

---

## 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
```

Downloads the right `linux`/`darwin` × `amd64`/`arm64` tarball from the
latest GitHub Release, verifies its checksum, and installs to
`~/.local/bin` (override with `INSTALL_DIR=...`). No Go toolchain, no
Python, no dependencies to resolve — `orch` is one static binary.

Or from source, CLI only (`go install github.com/Guria-Studio/orch/cmd/orch@latest`, Go ≥ 1.25): it builds without the web bundles, so `orch dashboard` and `orch publish` need the script or a release tarball instead.

Or grab a tarball directly from
[the Releases page](https://github.com/Guria-Studio/orch/releases) and
put `orch` on your `PATH` yourself. See [`RELEASING.md`](RELEASING.md) for
the full tag scheme and how releases are built.

Verify:

```bash
orch --version
orch --help
```

`orch --help` lists the commands by layer: *Start a project*, *Run tasks*,
*Follow progress*, *Deliver results*, *Integrations*, *Advanced*.

### AI CLIs you need

At least **one** of these on your `PATH`, authenticated with a
subscription or API key. orch dispatches to all six, and one project can
mix them — each task goes to the CLI its model routes to
([§9](#9-the-model-router)):

- **`claude`** — Claude Code CLI (Anthropic)
- **`codex`** — Codex CLI (OpenAI)
- **`gemini`** — Gemini CLI (Google)
- **`opencode`** — OpenCode (any provider it is configured for)
- **`agy`** — Antigravity CLI (Google; several model families behind one CLI)
- **`muse`** — Muse Code (Meta). orch runs it with approvals and its sandbox
  off (`--approval-mode never --disable-sandbox`), so the agent can report
  its task done

You only need the CLIs your tasks actually route to.

Run `orch doctor` any time to check what's actually reachable — see
[§13](#13-when-something-fails).

---

## 2. Create a new project

**The short way**: in a git repository, `orch start "what you want to build"` does
this whole section and §3 for you — it scaffolds with the CLIs on your PATH,
installs the planning skills, lets claude plan the idea with you (`/orch-plan`),
checks and estimates the plan, and offers to run the first two tasks with the
dashboard open. The rest of this section is the same path step by step.

```bash
orch init ~/work/my-app --template python-api
```

```
$ orch init --help
Scaffold a new orch project.

With no arguments, asks three questions (project id, template, confirm) and shows everything it is about to write before writing any of it; --advanced asks the rest.
The project goes in the current directory, with the default models of the AI CLIs on PATH.
With a path or any of the flags below, scaffolds directly.

Templates: chatbot-whatsapp, data-pipeline, expo-mobile, nextjs-saas, python-api

Usage:
  orch init [PATH] [flags]

Flags:
      --advanced              Also ask the project root, state backend, budget preset, spec root, tier models and openspec layout
      --force                 Overwrite an existing project's files
  -h, --help                  help for init
      --no-permissions        Do not allow orch's agent rules in the settings of the CLIs the project uses; orch claude-permissions / orch agy-permissions add them later
      --no-report-findings    Write report_findings.enabled: false (it is on by default: agents queue the bugs they find in orch, and you review each before it is sent)
      --project-name string   Name used in generated files; default = the directory name
      --sdd                   Also scaffold the openspec/ layout
      --template string       Project template (chatbot-whatsapp, data-pipeline, expo-mobile, nextjs-saas, python-api); omit for a blank project
```

Run it with **no arguments**, in your project's directory, and it asks three
questions — project id (default: the directory's name), template (default:
blank) and the confirm gate — so Enter through all of them gives a project
that runs. It scaffolds into the **current directory**, picks the default
models from the AI CLIs it finds on `PATH` (claude first, then codex, gemini,
opencode, agy, muse) and writes your repository's own branch as
`dispatch.base_branch`. `orch init --advanced` also asks the root, state
backend, budget preset, spec root, tier models and openspec layout.

```bash
orch init
```

That writes `.orchestrator/config.yaml`, `.orchestrator/model_router.yaml`,
`tasks.json`, and (with `--sdd`) an `openspec/` layout. It also writes a
`.mcp.json` pointing at `orch mcp` — see [§11](#11-mcp--letting-an-agent-drive-orch-directly)
— which is left alone on re-`init`, `--force` included, since it may
already list your project's other MCP servers.

An agent reports its task through `scripts/task-finish.sh` /
`task-block.sh` and orch's MCP tools, and a CLI run headless refuses what it
has no rule for. So `orch init` and `orch start` **grant orch's basic
permissions by default** to each CLI the project routes to that is on your
`PATH` — never to one it does not use, and only those scoped rules, never a
skip-permissions flag: claude's in the project's `.claude/settings.json`, agy's
in `~/.gemini/antigravity-cli/settings.json` (outside the project, so the path
and every rule added are printed). The wizard asks and Enter accepts; batch
`init` and `start --yes` just write them. `--no-permissions` writes nothing and
names `orch claude-permissions` / `orch agy-permissions`, which add them later.
codex, opencode and muse need nothing written; `orch doctor` reports each CLI
(gemini as unknown). gemini refuses a folder it was not told to trust, and every
task runs in a fresh worktree, so orch sets `GEMINI_CLI_TRUST_WORKSPACE=true` on
each gemini dispatch; set it to `false` in your environment to keep the gate.

A new project has `report_findings.enabled: true`: the wizard and `orch start`
ask once, yes by default. Agents that hit a bug in orch itself **queue** a
report; nothing is filed on their own. You read each one, title and body as
they would be posted on Guria-Studio/orch under your `gh` login, with
`orch findings` or the dashboard's System › Findings page, and send it
(`orch findings send ID`) or dismiss it. `orch run` and Now say how many wait.
`--no-report-findings` turns it off. See [`DOGFOODING.md`](DOGFOODING.md).

A blank project starts with **one sample task**, `F0.T1` ("Add a Getting
started section to README.md"), on the cheapest model of an installed CLI, so
the first `orch run` shows an agent at work. Each task branches from a
commit, so commit what init wrote first — init says so when the repository
has none:

```bash
git add -A && git commit -m "orch init"
orch run
```

With no git remote, the work stays on each task's local `orch/<task-id>`
branch: no push and no pull request.

A templated project (`--template`) ships with real, routed tasks in
`tasks.json` — `orch tasks` works immediately, no atomizing required to
try the rest of this manual.

---

## 3. Write specs and atomize them into tasks.json

Specs are markdown files under `<project-root>/docs` (or wherever
`spec_root` in `config.yaml` points) in the format described in
[`SPEC-FORMAT.md`](SPEC-FORMAT.md). `orch atomize` parses them and merges
new tasks into `tasks.json` — **read-only unless you pass `--apply`**:

```bash
$ orch atomize --help
Parse markdown specs and merge them into tasks.json (read-only unless --apply)

Usage:
  orch atomize [flags]

Flags:
      --apply               Escribí tasks.json (con backup). Sin este flag es read-only.
      --file string         Sólo un archivo markdown (bypass del walk de --specs-dir)
  -h, --help                help for atomize
      --list                Modo listar: sólo imprime lo parseado, sin merge/diff
      --no-backup           No crear backup .bak-<ts> al escribir (default: sí crea)
      --specs-dir string    Directorio de specs .md (default: <project-root>/<spec_root>, "specs")
      --tasks-json string   Path a tasks.json (default: <project-root>/tasks.json)
```

(The flag help text is in Spanish in the binary today — that's the real
output, not a typo in this manual.)

```bash
# Preview: parses every spec under the specs dir, shows a diff, writes nothing
orch atomize

# Just one file
orch atomize --file specs/f1-auth.md

# Apply — writes tasks.json (with a tasks.json.bak-<ts> backup unless --no-backup)
orch atomize --file specs/f1-auth.md --apply
```

**Guarantees**: idempotent (re-running with the same spec doesn't touch
existing task IDs, only adds new ones); a task naming a model that isn't
in `model_router.yaml` is caught by `orch validate` / at dispatch, not
silently accepted; fenced code blocks in a spec are skipped by the parser
(so an example spec inside `specs/README.md` doesn't get imported as real
tasks).

**New tasks land in `backlog`**, and `orch run` never dispatches a backlog
task. `--apply` ends by saying so, with the command that promotes them —
`orch task set --id F1.1.T1 --status todo` for one, a `for id in …; do …;
done` loop over the same command for several. Promote the ones you want run.
If you forget, `orch run` (and `orch estimate`) say how many tasks sit in
backlog and print the same command instead of only "nothing is ready".

---

## 4. Check the plan before running

The Go binary's `run` command has **no `--dry-run` flag** — that's a
known, documented gap versus Python (see `run`'s row in
[`CLI.md`](CLI.md)). Preview what a run would do with the read-only
commands instead:

```bash
# Everything wrong with the DAG or routing, before you spend a token
orch validate

# What's ready to dispatch right now
orch tasks --status todo

# The dependency graph, as Graphviz DOT
orch graph | dot -Tpng -o plan.png

# What the next run would dispatch, and what it would cost and take
orch estimate
```

`orch validate` runs the same checks Python's preflight did that have a
Go home today: config/router/tasks load, schema, dependency cycles, and
unresolved model routes. Exit code `0` clean, `2` if it found at least one
error.

```bash
$ orch validate --help
Static validation of tasks.json + routing (schema, deps, cycles, routes)

Usage:
  orch validate [flags]

Flags:
      --files   Also check that parent dirs of each task.files[] entry exist + are writable (not implemented yet)
  -h, --help    help for validate
      --json    Emit the full validation report as JSON on stdout
```

Also run `orch doctor` here (see [§13](#13-when-something-fails)) — it
covers the environment side `validate` doesn't: provider CLIs actually on
`PATH`, VCS readiness, orphaned worktrees, budget preset sanity, SQLite
health.

---

## 5. Run — dispatch tasks to AI agents

```bash
$ orch run --help
Walk the DAG, dispatching ready tasks to their CLI agents

Usage:
  orch run [flags]

Flags:
  -h, --help            help for run
      --max-tasks int   Start at most this many tasks (their retries do not count); 0 means no limit
      --mode string     auto dispatches everything ready; semi asks before each critical task (default "auto")
      --no-push         Skip pushing task branches — for a project with no remote
      --only string     Only dispatch tasks whose id matches this glob (dependencies still resolve across the whole DAG)
      --task-locks      Take a per-task lock, so several orch instances can share one project
      --worktree-mode   Give each task its own git worktree and branch (also settable as dispatch.worktree_mode)
```

```bash
# Dispatch everything ready, no prompts
orch run

# Ask before each task marked critical
orch run --mode semi

# Isolate each task in its own git worktree/branch and open a PR per task
# on success (needs dispatch.worktree_mode / vcs.auto_pr — see CONFIG.md)
orch run --worktree-mode

# No remote to push to yet
orch run --no-push
```

**Ctrl-C** drains in-flight work before exiting, with exit code **130**. A
**second** Ctrl-C SIGKILLs every child process group immediately.

**Worktree mode**: each task gets its own git worktree and branch, in
`<project>.worktrees/<task-id>/` next to the project (see `CONFIG.md`); on
success `orch` commits, pushes, and — if `vcs.auto_pr` is on — opens a PR
into `dispatch.base_branch`, then leaves the task `in-progress` for the CI
poller. A green CI check marks it `done` (and merges it too, if
`github.auto_merge` is on — a merge refused because the base just moved is
retried for a couple of minutes, and a PR behind its base is updated first,
so dependents start from a base that has the code; a merge that never goes
through is reported with gh's own error in the `pr_auto_merge_failed`
event and the task's note); a red one gets one retry with the failing logs
attached, then `blocked`. A red that GitHub caused by never starting the
job (Actions billing, the spending limit) is blocked at once with that
reason, no retry spent. A PR that conflicts with its base is brought up to
date in the task's kept work: orch merges the base in and pushes when it
merges cleanly, with no agent; otherwise the agent gets the conflicting files
and the base commits behind them to resolve, `retry.conflict` times (1 in a
new project; a config without the key keeps the old block) before the task is
blocked naming the files. A run that opens a PR exits before polling that
PR's CI — a run finishing green is noticed on the *next* `orch run`, same
as Python.

**Project knowledge** (`knowledge.enabled`, on in a new project): what one task
learns reaches the tasks after it. Lessons are short markdown files under
`.orch/knowledge/`, committed and reviewed like any other file. An agent
records one with the MCP tool `orch_learn` (it lands in its PR); orch drafts
one when the local verify rejected an attempt and the next one passed, or when
the agent resolved a conflicting PR; you add your own with `orch knowledge
add`. Each dispatch prompt carries the few most relevant under `Project
knowledge` — those whose `area` covers the task's files first. `orch knowledge
list` / `show` / `rm` manage them and `orch knowledge stats` compares
first-try success before and since the first lesson. See
[`CONFIG.md`](CONFIG.md#knowledge--new-451).

**Local verify with the task's app** (`verify.app`): orch starts the app in
the task's worktree, with the task's `.env`, before running the E2E tests,
once per verify of every task. An app that migrates its database on boot, starts
scheduled jobs or calls real providers does that each time. So point it at a
database and services only that task uses. `dispatch.isolate_compose` isolates
only the containers inside the task's compose project, not a database the
`.env` points to outside it. See
[`CONFIG.md`](CONFIG.md#verify--new).

**Concurrency, budgets, retries** are all config-driven, not flags — see
[`CONFIG.md`](CONFIG.md).

### Starting without GitHub

A repository with no remote yet — or a machine without `gh` — does not need
pull requests to get work onto the base branch. Set `vcs.provider: local`
(`orch init` writes it for you in that case; `orch start` too once the
repository has a commit):

```yaml
dispatch:
  worktree_mode: true
vcs:
  provider: local
```

Each task still works on its own branch, in its own worktree. When it finishes
and passes the local verify, orch **squash-merges it into
`dispatch.base_branch` right here** — one commit per task, titled like the PR
would be — and deletes the branch. Tasks merge one at a time, and a dependent
is only dispatched once its dependencies are on the base, so it starts from
their work. Nothing is pushed and `gh` is never run. A task whose work
conflicts with the base gets the base merged into its worktree and goes back to
its agent to resolve the conflict, `retry.conflict` times, then blocks naming
the files (its work stays on `orch/<task-id>`). If the base branch is checked
out with uncommitted changes to the same files, the merge is refused and the
task blocks: commit or stash, then `orch task-status <id> todo`.

`orch status` shows `merged_locally` as the task's last event, the dashboard
says "Merged locally" where a PR link would be, and the run receipt lists the
merges. `orch doctor` does not ask for a remote or `gh`.

**Moving to GitHub later**: create the repository, `git remote add origin
<url>`, `git push -u origin main`, then set `vcs.provider: github` (and
`gh auth login`). From the next `orch run` on, each task opens a PR as usual.

---

## 6. Open the dashboard

```bash
$ orch dashboard --help
Serve the operator dashboard on a local HTTP port.

Shows the project's state. From this machine the operator can also
act on it — edit and steer tasks, accept or send back their work,
change config, budgets and lessons — through the same functions as
the CLI commands; it never drives orch run. The
client profile (`stakeholder` in config) gates every data route
behind a token and an allow-list, so your client sees only the
client page — see `profile` and `token` under `dashboard:` in
config.yaml, which the flags below override.

Usage:
  orch dashboard [flags]

Flags:
  -h, --help             help for dashboard
      --host string      Address to bind; beyond localhost (0.0.0.0) every remote request needs the token (default "127.0.0.1")
      --port int         Port to listen on; 0 picks any free one (default 7420)
      --profile string   Access profile: operator, stakeholder or both (default: config.yaml)
      --token string     Shared token a client session must present (default: config.yaml)
      --tunnel           Also start the Cloudflare quick tunnel (needs tunnel.enabled and cloudflared), and stop it on exit

Global Flags:
      --config string         Path to config.yaml (default: .orchestrator/config.yaml)
      --project-id string     Project id override. Env fallback: ORCH_PROJECT_ID.
      --project-root string   Project root; default = cwd. Env fallback: ORCH_PROJECT_ROOT.
```

```bash
# Everything, for you
orch dashboard

# A synthetic project with realistic history, to look around first
# (nothing is dispatched; the data is deleted when you stop it)
orch dashboard --demo

# The last finished run as Markdown to paste into a PR or a chat: tasks done
# and blocked, wall and agent time, spend per provider, PRs (--json for scripts)
orch report receipt

# The same run in plain words: what was built, why each blocked task stopped,
# and the next steps with the command for each (orch run prints it when it ends)
orch report summary

# A link to hand a client, over a Cloudflare quick tunnel (needs
# `tunnel: enabled: true` in config.yaml and cloudflared on PATH — the
# Share panel shows how to install it). Prints the client page link, with
# the token every request through the tunnel needs; Ctrl+C revokes it.
# The Share panel can also share through Tailscale (serve or Funnel).
orch dashboard --tunnel
```

The dashboard has four destinations, plus **System** (tools, providers and
quota, skills, knowledge, findings, settings) listed apart. **Now** is the first screen:
the run, each agent at work with a running clock, the blocked tasks and the
ones waiting on a budget window (with the command that unblocks each,
and from this machine the buttons that do it), phases and the budget window;
once a run has finished, its receipt with the run in plain words under it —
why each blocked task stopped and the next steps with their commands.
**Work** holds List, Kanban, Graph, Phases and Pace as tabs sharing one set of
filters in the URL; **Usage** holds tokens and spend by model, task and
attempt, and the budget; **Delivery** holds the
client page, the client summary, CI, Tests (each E2E run the local verify made, with what a
failing test left: error, step, screenshot, video, trace) and Share (a
Cloudflare quick tunnel or Tailscale). **Logs** opens as a panel over any
page. Old addresses (`/kanban`, `/budget`, `/tunnel`…) redirect to their new
place. **⌘K** (Ctrl+K) searches pages, tasks and filters and copies CLI
commands; `g` then `n`/`w`/`c`/`d` changes destination, `/` searches the task
list and `?` lists every shortcut. A task's page renders its description as
Markdown: inline code, and fenced blocks with their language, syntax colors
and a copy button (Edit still shows the raw text).

Its **Changes** tab is the review: what the task's branch `orch/<id>`
changed against its base (`git diff --merge-base`), as a file list with +/−
counts and syntax-colored hunks — a file over 400 changed lines opens
collapsed. Next to it, each time the local verify sent the work back, and the
review's findings under the `file:line` they name. The local operator acts
from there: **Accept** squash-merges the task's PR (`orch task accept`),
**Send back** reopens the task with your note for its agent (`orch task note
--retry`), **Block** holds it with the note as the reason. In local mode
(`vcs.provider: local`) there is no PR: Accept commits what the task left in
its worktree and squash-merges `orch/<id>` into the base here, as the run
does, then marks the task done; if the work conflicts with the base nothing
changes and the error names the files, for you to send it back. A client
session does not get the tab unless `api_task_diff` is in
`dashboard.stakeholder_routes`.

**System › Knowledge** lists the project's lessons (`.orch/knowledge/`) with
their area, source, task and date, filters them by area and source, and
searches them the way the MCP tool `orch_knowledge_search` does. Per lesson
it shows how many dispatch prompts carried it and, of the tasks those were for
that are done, how many finished on the first try — a lesson no prompt used,
or whose tasks still needed retries, is one to edit or delete. The local
operator adds, edits and deletes lessons there through the same function as
`orch knowledge add` / `rm`; each change is a `knowledge_edited` event, and
the file is yours to commit. **Curate now** runs
`orch knowledge curate` (#452): a dialog shows what the curation would do and
cost, and **Confirm** dispatches its one task in the background; the page then
links to that task, which the run shows like any other. One curation at a time. A client session never gets the page.

The dashboard speaks English, Spanish and Portuguese (Brazil). It starts in
your browser's language and falls back to English; the language button at the
bottom of the sidebar (or ⌘K → *Change language*) switches it and this browser
remembers the choice. `dashboard.language` in `config.yaml` is a different
setting: it picks what the **client** reads (client page, executive summary, PDF),
not the operator's screens. Commands, flags and config keys stay in English.

`--profile operator` (the default) shows everything: tasks, spend per
model, logs. `--profile stakeholder` — the client profile; the name is a
config alias — gates every data route behind the `--token` and an allow-list
of client-safe routes — no log lines, no
prompts, no per-model cost breakdown unless `dashboard.show_spend_to_stakeholder`
is on. See [`DASHBOARD-PROFILES.md`](DASHBOARD-PROFILES.md) for the access
model in full and [`DELIVERING-TO-STAKEHOLDERS.md`](DELIVERING-TO-STAKEHOLDERS.md)
for the client-facing walkthrough (executive summary, phase timeline, ETA,
blockers).

---

## 7. Inspect a run: status, tasks, events, logs

```bash
# One-screen summary: tasks by status, spend, last events, run summary
orch status

# Every task with status/routing/deps, filterable
orch tasks --status todo,in-progress
orch tasks --only 'F1.*'

# Event history for one task
orch events F1.1.T2 --tail 50

# That task's raw agent log
orch logs F1.1.T2 --tail 200
```

All four accept `--json` (except `logs`, which never had one in Python
either) for scripting, and share the project flags
(`--project-root`/`--project-id`/`--config`).

To watch a run instead of taking snapshots, `orch tui` keeps the same
picture live in the terminal: the task table with each task's elapsed time,
progress per phase, spend per provider, and the selected task's log as
steps. `↑`/`↓` (or `j`/`k`) select a task, `f` cycles the status filter,
`q` quits. It only reads, so it runs beside `orch run` in another terminal.

```bash
$ orch status --help
The quick check: task counts, costs, last events, run summary (--json for scripts)

Flags:
  -h, --help            help for status
      --json            Emit the raw snapshot as JSON
      --only string     Restrict task rows to ids matching this glob
      --status string   Comma-separated status filter (e.g. todo,in-progress)
```

---

## 8. Fix things by hand: task set, task-status, reset

```bash
# Move a task's status directly (illegal transitions exit 3)
orch task-status F1.1.T5 todo --note "re-queued after manual fix"

# Same thing, via the newer `task set` surface. There is no milestone flag:
# a milestone is a phase, and a task's phase lives in tasks.json
orch task set --id F1.1.T5 --status todo

# Route a task to another model: a model_router.yaml key, written to tasks.json
orch task set --id F1.1.T5 --model codex/gpt-5.4

# See which in-progress tasks look stuck, without touching anything
orch reset

# Actually revert them to todo
orch reset --requeue --only 'F1.*'
```

```bash
$ orch task set --help
Set a task's status, or its model

Flags:
  -h, --help            help for set
      --id string       Task ID, e.g. F1.1.T3
      --model string    Set the task's model in tasks.json: a model_router.yaml key, which names the backend
      --status string   Set the task status (e.g. done, in-progress, blocked)
```

`reset` reads the **real** runtime status from the state backend, not
`tasks.json`'s own (potentially stale) `status` field — a deliberate
difference from Python, documented in [`CLI.md`](CLI.md).

---

## 9. The model router

`model_router.yaml` maps a task's declared model to a backend CLI, its
`cli_model` name, and a cost tier.

**Using several AIs.** A task's `"model"` in `tasks.json` is a key of
`model_router.yaml`, and that entry names the CLI that runs it. Three
tasks, three CLIs:

```json
"tasks": [
  { "id": "T1", "model": "claude/sonnet", ... },
  { "id": "T2", "model": "codex/gpt-5.4", ... },
  { "id": "T3", "model": "gemini/gemini-2.5-flash", ... }
]
```

```yaml
# .orchestrator/model_router.yaml — orch init already writes these entries
claude/sonnet:
  backend: claude
  cli_model: sonnet
  tier: standard
  is_premium: false
codex/gpt-5.4:
  backend: codex
  cli_model: gpt-5.4
  tier: standard
  is_premium: false
gemini/gemini-2.5-flash:
  backend: gemini
  cli_model: gemini-2.5-flash
  tier: cheap
  is_premium: false
```

`orch router validate` checks that every task resolves to an entry, and
`orch doctor` that each CLI those entries name is installed.

**Reasoning effort.** A route's `effort:` sets how hard its CLI thinks.
`low`, `medium` and `high` work on every backend (codex, agy and muse pass
them on). codex routes also take the higher levels — `xhigh`, `max`, `ultra`
— that codex's model catalog (`~/.codex/models_cache.json`, written by codex
itself) lists for that model; without the catalog, or for a level the model
does not list, `orch validate` fails and names the levels that model takes.
The full matrix is in `docs/CONFIG.md`.

```yaml
codex/gpt-6-astra:
  backend: codex
  cli_model: gpt-6-astra
  tier: premium
  effort: xhigh
```

**Failover when a provider runs out of quota.** By default a rate-limited
task waits out `retry.rate_limit` and tries the same CLI again. Give its
route a `failover:` list of other route keys and the retry goes at once to
the first one whose provider still has quota, in the same worktree, so the
work done so far carries over. claude's own report that its window is
exhausted counts too. orch remembers the exhausted provider until its reset
(the time claude reports, else the rate-limit backoff), and the run's later
tasks on that route go straight to the failover. Spend, events and the
dashboard show the backend that actually ran. A key that is not a route
fails `orch validate`.

```yaml
claude/sonnet:
  backend: claude
  cli_model: sonnet
  tier: standard
  failover: [codex/gpt-5.4, gemini/gemini-2.5-flash]
```

```bash
# Every task.model resolves to a router entry?
orch router validate

# Append inferred entries for anything unrouted, at a given tier
orch router add-missing --tier standard
orch router add-missing --yes   # skip the confirmation prompt
```

```bash
$ orch router --help
Inspect and maintain model_router.yaml

Available Commands:
  add-missing Append inferred model_router.yaml entries for every unrouted task model
  validate    Check that every task.model resolves to a model_router.yaml entry
```

`add-missing` shows the plan and asks `y/N` before writing, same as
Python — `--yes` is how a script opts out of the prompt (never
TTY-detection: an unattended `--mode auto` run must not silently pause on
stdin).

---

## 10. Inspecting config

```bash
orch config show
```

Prints the effective config — defaults merged with `config.yaml` and any
overrides — followed by where each value came from, and any keys in your
`config.yaml` that orch doesn't recognize (a good way to catch a typo'd
key silently doing nothing). Full key reference: [`CONFIG.md`](CONFIG.md).

```bash
$ orch config --help
Config helpers

Available Commands:
  show        Print the effective config, merged with defaults, and where each value came from
```

---

## 11. MCP — letting an agent drive orch directly

```bash
$ orch mcp --help
Serve orch's state to an MCP-capable agent over stdio.

Seven tools: orch_list_tasks, orch_get_task, orch_set_status,
orch_block, orch_budget, orch_events, orch_context.

Speaks JSON-RPC on stdin/stdout; run it from an MCP client, not
from a terminal. See docs/MCP.md.
```

`orch init` already wrote a `.mcp.json` pointing at `orch mcp` — an
MCP-capable agent (Claude Code included) picks it up automatically from
the project root. There is no Python equivalent; see [`MCP.md`](MCP.md)
for the full tool list and the illegal-transition error shape (it names
every status the task may legally move to next, since the caller is a
model that can retry).

---

## 12. Installing orch's skills

```bash
$ orch install-skills --help
Install orch's skills (the orch manual, the planning pipeline, TDD, E2E) into claude, codex, opencode or cursor

Flags:
      --all              Install every embedded skill (default when --skill is omitted)
      --dry-run          Show what would be installed without writing anything
      --force            Overwrite an already-installed skill of the same name
  -h, --help             help for install-skills
      --path string      Claude target install directory (default: ~/.claude/skills)
      --skill strings    Install only this skill (repeatable); default is --all
      --target strings   Agent(s) to install into: claude, codex, opencode, cursor (repeatable) (default [claude])
```

```bash
orch install-skills                          # into ~/.claude/skills, --all
orch install-skills --target codex --target opencode
orch install-skills --dry-run                # preview first
```

`--target claude` writes real skill directories under `~/.claude/skills`.
`codex`/`opencode` have no skills mechanism of their own, so they get a
clearly-delimited, idempotently-replaceable section appended to the
project's `AGENTS.md` instead; `cursor` gets a `.cursor/rules/<name>.mdc`
file. Nine skills ship embedded in the binary: `orch-tdd`, how an agent
works a task test-first (red, green, clean); `orch-e2e`, how an agent
explores a running app in a browser and leaves Playwright specs; and `orch`,
the operating
manual for an agent working inside a project, and the planning pipeline —
`orch-plan` runs `orch-prd` (idea → `docs/prd/`), `orch-arch`
(→ `docs/arch/`), `orch-spec` (→ `specs/`, in the exact format
`orch atomize` parses) and `orch-tasks` (→ `tasks.json` via
`orch atomize --apply`), stopping for you after each document and before
anything is applied. On a repository that already has code, `orch-plan` first
runs `orch-discover` (→ `docs/arch/000-discovery.md`): what the code is, every
claim citing its file, with the tracked secrets, the boot-time side effects
and the questions for the client, and then proposes `gate.critical_paths`,
`guard.protected_paths`, rules and `verify.commands`, applying only the ones
you confirm. The dashboard's System › Skills page has a **How it works** tab
that walks these steps — each skill, what it reads and writes, the command
that starts it — and flags the ones no CLI has installed yet.

---

## 13. When something fails

### Run `orch doctor` first

```bash
$ orch doctor --help
Environment preflight — provider CLIs, VCS, worktrees, budget config, SQLite

Flags:
  -h, --help   help for doctor
      --json   Emit the full doctor report as JSON on stdout
```

```bash
orch doctor
```

Checks: provider CLIs referenced by `tasks.json`+`model_router.yaml`
actually on `PATH`, routing completeness, budget preset sanity, orphaned
git worktrees, VCS readiness (repo/remote/auth), whether `.mcp.json`
exists, and SQLite health. Same exit-code convention as `validate`: `0`
clean, `1` warnings only, `2` at least one error.

It is also a **readiness report**, in five sections — Config (config,
router, tasks.json, models), Backends, Tasks, Permissions, Environment — and
it ends on a verdict: ``Ready to `orch run`.`` or the first thing in the way.
Tasks says which tasks `orch run` would dispatch now and lists the `backlog`
tasks whose dependencies are done, with `orch task-status <id> todo` for each.
Permissions has one `<cli>.permissions` check per CLI the project uses: can
its agent report a task done (claude's and agy's rules, codex's `--add-dir`
grant of the state directory). `orch doctor --json` carries each check's
`section` and the `verdict`.

### Task blocked

1. `orch logs <task-id> --tail 100` — read the agent's own output
2. `orch events <task-id>` — see what orch itself recorded (dispatch,
   fail, retry, budget_pause…)
3. Fix the spec or the code by hand
4. `orch task-status <task-id> todo --note "..."` to re-queue it
5. `orch run` again — it only picks up `todo` tasks

### `orch` won't start — exit 1, project layout error

`tasks.json` or `.orchestrator/config.yaml` is missing or unreadable.
`orch init --force` if it's meant to be a real project, or check
`orch doctor --json`'s `config.parse`/`router.parse` entries for the
parse error itself.

### `orch validate` / `orch doctor` exit 2

At least one error-severity finding — read the human output (or `--json`)
for which check failed and why; both commands print every check they ran,
not just the failing ones.

### An illegal status transition — exit 3

`task-status`/`task set --status` refuse a transition the state machine
doesn't allow (e.g. `done` → `todo` directly). The error names the
task's current status.

---

## 14. Updating orch

```bash
# Re-run the installer — it always fetches the latest v* (non -py) release
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh

# or: orch upgrade

# Verify
orch --version
```

Upgrading the binary never touches a project's own
`.orchestrator/config.yaml`, `model_router.yaml`, or `budgets.yaml` — if
you want a new default into an existing project, run `orch init --force`
there and diff before you commit (it overwrites your project's own
tuning, so don't do it blindly).

---

## 15. Features by layer

Every command orch has, grouped by who needs it and what for. The core is the
path this manual walks; the other layers are there when you need them, and
none of them is required to start. [`CLI.md`](CLI.md) has every flag.

**Core — agents as a team, a page for your client**

| Command | What it is for |
|---|---|
| `orch start "idea"` | The first command: sets the project up, plans the idea with claude, estimates it and runs the first tasks ([§2](#2-create-a-new-project)). |
| `orch run` | Dispatches every ready task to its agent in parallel, each in its own worktree and PR, checked before the push, with CI watched ([§5](#5-run--dispatch-tasks-to-ai-agents)). |
| `orch dashboard` | Your screen: the agents at work, what needs you, each task's diff to accept or send back, and the Share button for a live client link ([§6](#6-open-the-dashboard)). |
| `orch publish` | Writes the client page as a static site, to a folder or a `gh-pages` branch (`--watch` keeps it current). |
| The client page | What your client opens: progress by phase, blockers in plain words, the estimated finish. Live behind a token, or published. |

**Agencies — several clients at once**

| Command | What it is for |
|---|---|
| `orch dashboard --portfolio "~/clients/*"` | Every client project in one dashboard, so you see which one needs you without switching folders. |
| `orch cloud setup` + `orch publish --to cloud` | Hosts each client page on a Cloudflare Worker in your own account, with a stable link that works while your machine is off ([`CLOUD.md`](CLOUD.md)). |
| `orch report pdf` | The client page as a one-page A4 PDF, to attach to an email or an invoice. |
| `orch notify digest` | The executive summary and each phase's progress, printed or sent to Slack/Discord (`--send`); cron it for a weekly update. |
| `presentation.branding` | Your agency's name, logo and colour on the client page and the PDF instead of orch's ([`CONFIG.md`](CONFIG.md)). |

**Integrations — the tools your client already uses**

| Command | What it is for |
|---|---|
| `orch sync issues` | Turns the client's labelled GitHub issues into tasks (`gh-<number>`), adding only what is new on each run. |
| `orch atomize --from-issues label:orch --model M` | Turns each open GitHub issue the search matches into a spec (`specs/issue-<n>-<slug>.md`) and its tasks; preview by default, `--apply` writes. A re-run imports nothing twice. The task's PR closes the issue on merge (`Closes`); with `vcs.provider: local` orch closes it with a comment once its last task lands. |
| `orch export clickup` | Mirrors the plan into a ClickUp Folder for a client who tracks work there; re-run it and statuses follow. |
| `orch export multica` | Creates the plan as Multica issues, one parent per phase. |
| `orch ci setup` / `ci review` / `ci usage` | Writes the CI pipeline the repository calls for, runs an AI review of each PR inside CI, and adds up the billed Actions minutes. |
| `orch mcp` | Lets an MCP-capable agent read and move orch's tasks as tools ([§11](#11-mcp--letting-an-agent-drive-orch-directly)). |

**Advanced — tuning and repair**

| Command | What it is for |
|---|---|
| `orch bench` | Runs the same project on each provider and compares done tasks, time and spend (real agents, capped by `--max-usd`). |
| `orch router suggest` / `router refresh` | Which route has worked for which kind of task, with cheaper or stronger suggestions; routes for models your CLIs gained ([§9](#9-the-model-router)). |
| `orch knowledge curate` / `knowledge stats` | Merges and prunes the lessons agents wrote, as a PR; measures first-try success before and since the first lesson. |
| `orch provider disable` / `enable` | Pauses a provider while it misbehaves; its tasks wait instead of failing. |
| `orch budget set` | Changes a provider's token budget, window or threshold without editing YAML. |
| `orch task salvage` | Accepts work an earlier attempt committed but never published, without running the agent again. |
| `orch reset` | Finds tasks stuck in progress after a crash, and requeues them with `--requeue` ([§8](#8-fix-things-by-hand-task-set-task-status-reset)). |
| `orch dashboard token` | Rotates or inspects the token a client link needs. |
| `orch config show` / `set` / `unset` | The effective config and where each value came from; edit keys keeping the file's comments ([§10](#10-inspecting-config)). |
| `orch findings` | The queue of bug reports agents wrote about orch itself; read each, then send or dismiss it. |
| `orch upgrade` | Installs the latest release over this binary, checksum-verified ([§14](#14-updating-orch)). |
| `orch claude-permissions` / `agy-permissions` | Adds the rules those CLIs need before their agents can report a task done. |

---

## Python legacy

The pre-rewrite `orch` — the FastAPI dashboard, `pipx install orch`, the
full sprint history this Go binary is replacing feature-by-feature — is
frozen on the `python-legacy` branch, tagged **`v0.11.0-py`**. It still
installs and runs exactly as documented in that tag's own manual:

```bash
git checkout v0.11.0-py
pipx install .
```

or

```bash
pipx install git+https://github.com/Guria-Studio/orch.git@v0.11.0-py
```

No new features land on that line — only the Go binary moves forward. See
[`RELEASING.md`](RELEASING.md) for why the two lines share one tag
namespace, split by a `-py` suffix rather than a separate branch prefix.

---

## References

- Full CLI flag reference, parity notes with Python: [`CLI.md`](CLI.md)
- Config keys: [`CONFIG.md`](CONFIG.md)
- Spec markdown format: [`SPEC-FORMAT.md`](SPEC-FORMAT.md)
- MCP server / tools: [`MCP.md`](MCP.md)
- Dashboard access model: [`DASHBOARD-PROFILES.md`](DASHBOARD-PROFILES.md)
- Sharing the client page: [`DELIVERING-TO-STAKEHOLDERS.md`](DELIVERING-TO-STAKEHOLDERS.md)
- Release process / tag scheme: [`RELEASING.md`](RELEASING.md)

## Feedback

This manual is a living doc, updated in the same PR that lands a new
subcommand. If you hit a case that isn't covered, open an issue at
<https://github.com/Guria-Studio/orch/issues> or send a PR with the
missing section.
