<p align="center">
  <img src="logos/export/logo.svg" alt="orch — task orchestrator" width="360" />
</p>

# orch

**Run AI agents as a team. Show clients a live page — not a Slack thread.**

Site: **[tryorch.us](https://tryorch.us)** — [try a spec in your browser](https://tryorch.us/playground/).

orch is a local task orchestrator for freelancers and agencies building with AI.
You define the work as a DAG (`tasks.json`) and dispatch each task to the CLI you
route it to — `claude`, `codex`, `opencode`, `gemini`, `agy` or `muse` — in parallel.

- **Your client stops asking how it is going.** One page — phases, blockers in
  plain language, hours left — published as a static site or served live behind
  a token. You write no status update.
- **The agents run in parallel without burning your quota.** Each task gets its
  own git worktree, branch and pull request; a rolling token window per provider
  pauses that provider before it locks you out of your own terminal.
- **It runs on your machine, with your CLIs and your keys.** One Go binary, no
  daemon, no hosted service. orch never holds a model key.

![orch in 29 seconds: orch init, the plan, orch run dispatching two claude tasks that each open a PR with green CI, then the client page moving from 0% to 50% to 100% on its own](docs/media/orch-demo.gif)

<sub>A real run, sped up: `orch init --template python-api`, `orch run` with `claude`, two PRs with green CI, and the client page updating itself. What is real and what was set up off camera: [`docs/media/GIF-NOTES.md`](docs/media/GIF-NOTES.md).</sub>

---

## How it works

```
1. orch atomize --apply   # spec.md → tasks.json → SQLite
2. orch run               # dispatch tasks to AI agents in parallel
3. orch publish           # a page for your client; --tunnel to serve it live
```

---

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/hectorcanaimero/orch/main/scripts/install.sh | sh
orch dashboard            # outside a project: the welcome page walks you through it
```

Run outside a project, `orch dashboard` opens a welcome page: it checks your AI
CLIs, sets up a folder, lets claude plan your idea, shows the tasks and their
estimate, and runs the first ones. In a terminal the same path is:

```bash
orch dashboard --demo     # a full synthetic project; touches nothing of yours
cd my-project             # a git repository
orch start "what you want to build"
```

`orch start` sets the project up with the CLIs it finds, lets claude plan the
idea with you (`/orch-plan`), checks and estimates the plan, and runs the first
two tasks with the dashboard open. `orch init` + `orch run` is the same path
step by step; `orch dashboard --tunnel` gives your client a tokenized link.

`orch` is a single Go binary — no Python, no venv, no `pip`. See
[Install](#install) below for the manual download, and the
[manual](docs/MANUAL.en.md) for the full walkthrough.

---

## Install

**Script** (Linux/macOS, `amd64`/`arm64`):

```bash
curl -fsSL https://raw.githubusercontent.com/hectorcanaimero/orch/main/scripts/install.sh | sh
```

Downloads the matching tarball from the latest GitHub Release, verifies
its checksum, and installs to `~/.local/bin` (`INSTALL_DIR=...` to
override).

**Manual**: grab a tarball from the
[Releases page](https://github.com/hectorcanaimero/orch/releases) and put
`orch` on your `PATH` yourself.

**From source** (`go install`, Go ≥ 1.25): the CLI only.

```bash
go install github.com/hectorcanaimero/orch/cmd/orch@latest
```

It builds without the web bundles, so `orch dashboard` answers "no index.html
in the embedded SPA — run `make web`", `orch publish` has no client page, and
`orch --version` reads `dev`. For those, use the script or a release tarball
(or `make build` in a checkout).

Verify either way with `orch --version`. See
[`docs/RELEASING.md`](docs/RELEASING.md) for the tag scheme and how
releases are built, and [`docs/MANUAL.en.md`](docs/MANUAL.en.md) for the
full CLI walkthrough.

---

## What makes it different

orch isn't competing with LangChain or Devin — it's one of several small,
open-source boards for running CLI coding agents (Claude Code, Codex,
OpenCode…) in parallel. Here's an honest comparison against the projects
builders in that space actually reach for today.

| | [Multica](https://github.com/multica-ai/multica) | [Vibe Kanban](https://github.com/BloopAI/vibe-kanban) | [Agetor](https://github.com/alamops/agetor) | [Claude Squad](https://github.com/smtg-ai/claude-squad) | **orch** |
|---|:---:|:---:|:---:|:---:|:---:|
| GitHub stars | 49.6k | 28.1k | 65 | 8.5k | **2** |
| Stack | Go + Next.js + Postgres | Rust | TypeScript (Electrobun) | Go | Go |
| License | Apache-2.0 + commercial | Apache-2.0 | MIT | AGPL-3.0 | MIT |
| CLIs/agents supported | 26 | 10+ | 5 | ~6 | 6 (claude, codex, opencode, gemini, agy, muse) |
| Desktop app | ✅ Electron | ❌ | ✅ macOS | ❌ (TUI) | ❌ |
| Mobile app | ✅ iOS | ❌ | ❌ | ❌ | ❌ |
| Cloud-hosted option | ✅ multica.ai | ❌ (shut down w/ Bloop) | ❌ | ❌ | ❌ |
| Single binary | ❌ | ❌ | ❌ | ✅ | ✅ |
| Read-only stakeholder view | ❌ | ❌ | ❌ | ❌ | ✅ |
| Per-provider budget guardrail (blocks dispatch) | ❌ | ❌ | ❌ | ❌ | ✅ |
| Spec → tasks pipeline | ❌ | ❌ | ❌ | ❌ | ✅ |
| Deterministic executive summary | ❌ | ❌ | ❌ | ❌ | ✅ |
| Static export | ❌ | ❌ | ❌ | ❌ | ✅ `orch publish` |

*Vibe Kanban: Bloop shut down in April 2026 and handed the project to the
community as Apache-2.0; cloud features were switched off, local/self-host
use continues. Star counts and feature notes are current as of this
writing — verify before citing them elsewhere, these projects move fast.*

---

## Configuration

One file. `orch init` writes it for you:

```yaml
# .orchestrator/config.yaml
concurrency:
  global_max: 4
  per_provider:
    claude: 2
    gemini: 2

state:
  backend: sqlite         # default; `file` is legacy JSONL

dispatch:
  worktree_mode: true     # default; each task runs in its own git worktree

vcs:
  auto_pr: true           # default; one PR per finished task

dashboard:
  kanban:
    refresh_interval_s: 10

tunnel:
  enabled: false          # flip to share a link with your client (Cloudflare quick tunnel)

github:
  test_command: pytest    # CI workflow orch generates for you
  auto_merge: false       # opt-in; requires branch protection

notifications:
  slack_webhook: ""       # optional: get pinged on task blocks / CI failures
```

Optional overrides drop in as their own files at the project root: `budgets.yaml` (spend guardrails), `model_router.yaml` (routing table — `orch router add-missing` populates it on demand).

---

## What your client sees

With `tunnel: enabled: true` in `config.yaml`, `orch dashboard --tunnel` (or
the **Start** button on the dashboard's Tunnel page) opens a Cloudflare quick
tunnel — no Cloudflare account needed, just `cloudflared`, whose install steps
the page shows — and gives you a client-portal link with a token in it. Send
it; the numbers update themselves, and stopping the tunnel revokes the link.
The client gets:

![The stakeholder view — executive summary, phase timeline, ETA and blockers](docs/media/stakeholder.png)

- **Executive summary** in plain business language, recomputed on every page load — no LLM call, so it is free and always says the same thing twice.
- **Phase timeline** — progress per phase, bar width proportional to estimated effort, with the blocked phase called out.
- **Blockers with the reason attached**, quoted straight into the summary: _"Stripe sandbox key still pending from the client."_
- **ETA** — hours remaining at the current measured pace, with how fresh the numbers are.

Spend is **off by default**: flip `dashboard.show_spend_to_stakeholder` if you
want your client to see what the run has cost. Either way they never see a log
line, a prompt, a diff, or a per-model cost breakdown.

> _The demo GIF at the top was shot from [`docs/media/GIF-SCRIPT.md`](docs/media/GIF-SCRIPT.md). Walk-through in [`docs/DELIVERING-TO-STAKEHOLDERS.md`](docs/DELIVERING-TO-STAKEHOLDERS.md)._

---

## Roadmap

**Shipped** — worktrees, a pull request and CI watch per task, the budget window
per provider, the client page and its PDF, the guided `orch init` wizard with
five templates, the operator dashboard (Now, ⌘K, run receipts) in English,
Spanish and Portuguese, `orch dashboard --demo`, and a Cloudflare quick tunnel
that requires a token. The [releases](https://github.com/hectorcanaimero/orch/releases)
are the living list.

**Next** — a DAG visual editor, a VS Code extension, and `orch dashboard
--portfolio` for agencies running several clients at once.

Every pull request here is reviewed in CI by an AI against a written checklist
before a human merges it — see [`docs/CI-REVIEW.md`](docs/CI-REVIEW.md).

---

## Follow the build

A release a week, in the open: every change, what broke and the numbers behind
it. [Releases](https://github.com/hectorcanaimero/orch/releases) ·
[Atom feed](https://github.com/hectorcanaimero/orch/releases.atom) ·
[LinkedIn, in Portuguese](https://www.linkedin.com/in/knaimero/) ·
[X, in Spanish](https://x.com/hectorcanaimero) ·
[tryorch.us](https://tryorch.us)

---

## Documentation

- [English manual](docs/MANUAL.en.md)
- [Manual en español](docs/MANUAL.es.md)
- [Manual em português](docs/MANUAL.pt.md)
- [CLI reference (flags, parity with Python)](docs/CLI.md)
- [Dashboard / stakeholder guide](docs/DELIVERING-TO-STAKEHOLDERS.md)
- [Release process / tag scheme](docs/RELEASING.md)
- [Developer notes](docs/README-dev.md)
- [Roadmap (living doc)](docs/brainstorm/next-sprints.md)

---

## Python legacy

`orch` was originally a Python CLI + FastAPI dashboard; it's being
rewritten as a single Go binary (this README, and everything above,
describes the Go version). The last Python release is frozen on the
`python-legacy` branch, tagged **`v0.11.0-py`** — no new features land on
that line, only the Go binary moves forward.

```bash
pipx install git+https://github.com/hectorcanaimero/orch.git@v0.11.0-py
```

Its own manual is preserved at that tag. See
[`docs/RELEASING.md`](docs/RELEASING.md) for why the two lines share one
tag namespace, split by a `-py` suffix.
