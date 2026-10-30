# Show HN draft — orch in Go

Draft, not published. The author posts it; nothing here is scheduled.

**Every number below has a source.** The "Sources" section at the end gives the
command that produced each one, re-measured on 2026-10-29 against the
`v0.29.0` release artifacts and `main` at `ba0fff4` (`docs/launch/measure.sh`
reruns all of them; its output is `docs/launch/measurements.txt`). Anything
nobody has measured is absent rather than estimated: there is no performance
comparison against the Python version, because nobody has measured one.

---

## Before posting

Each item says who can unblock it. The post must not go out with any of them
open — two of them make the first command in the post fail.

| # | Item | State on 2026-10-29 | Who |
|---|---|---|---|
| 1 | **A Go release exists** | ✅ done — `v0.29.0` is the latest of 18 Go releases (`v0.12.0` onwards) | — |
| 2 | **Run the install line on a clean machine** and paste what it prints into the first comment | **Linux: it failed, and is fixed in the PR that adds this line.** On a fresh `ubuntu:24.04` the documented `curl … \| sh` died on line 15 (`set: Illegal option -o pipefail`): Ubuntu's `sh` is dash 0.5.12. `scripts/install.sh` is POSIX sh now; the transcript before/after is `docs/launch/clean-install-linux.txt`. The fix reaches users only once merged (the one-liner reads `main`). **macOS: not run** — no Mac on the build host. | the author (macOS) |
| 3 | **Homebrew tap** | ❌ **dropped for the launch (the author's decision)**. The `brew` lines are gone from the post, README, manuals, site and templates, and `.goreleaser.yaml` has no `brews:` block. Install paths: the script, a release tarball, `go install` (CLI only). | — |
| 4 | **The GIF** — `docs/media/GIF-SCRIPT.md` with the Go binary (G7.3) | recorded on 2026-10-29 with one real provider (`claude`); see `docs/media/GIF-NOTES.md` for what is real, what the script asked for that could not be shot, and why | the author (re-shoot with three CLIs if the 1.0 gate insists) |
| 5 | **GitHub Pages** live for `site/` and `orch publish --to git` | **not right yet**: Pages is on, but in legacy mode serving `main:/docs` at `hectorcanaimero.github.io/orch/`, with no custom domain — `tryorch.us` resolves to GitHub Pages and answers 404. `site/` is deployed by `.github/workflows/pages.yml`, which needs Pages' source set to *GitHub Actions* and the `tryorch.us` CNAME. | the author (repo setting) |
| 6 | Re-measure the numbers below on the release commit and update "Sources" | ✅ done on `v0.29.0` / `ba0fff4` | — |
| 7 | Re-check the rivals' star counts and features in the comparison table | ✅ done on 2026-10-29 (GitHub API + each README) | re-check on posting day |
| 8 | Post on a **Tuesday, Wednesday or Thursday** | no date chosen | the author |

---

## Title

> Show HN: Orch – dispatch a task DAG to Claude, Codex or opencode, as a single binary

Alternatives, in case the first reads as a product pitch:

> Show HN: I rewrote my AI-agent orchestrator from Python to Go as a single binary
> Show HN: Orch – single-binary orchestrator for AI coding agents, and the 31 bugs porting found

The phrase to keep is **single binary**: it is what changed for someone
installing this, and it is checkable in one command.

---

## The post

I built orch to stop babysitting AI coding agents.

You write a spec, it becomes a `tasks.json` DAG, and orch walks the graph: it
works out which tasks are ready, dispatches each one to whichever CLI you routed
it to (`claude`, `codex`, `opencode`, `gemini`, `agy`, `muse`), isolates it in its own
git worktree, opens a PR, waits for CI, and retries or blocks on the result.
There is a per-provider budget guardrail that stops dispatching before a bad
night spends a month's tokens, a dashboard that shows what is running, and a
read-only page for the client who is paying for the work.

It shipped as a Python package for eleven releases. It is now one Go binary with
the dashboard's React app embedded in it. No virtualenv, no pip, no server
process to keep alive.

```
curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
orch init my-project --template python-api
```

Most of the code — Python and Go — was written by AI agents (Claude sessions,
several in parallel) that I directed and reviewed, with a second model (Gemini)
reviewing every PR in CI against a checklist in the repo. That is relevant to
what follows, so it goes up front.

The rewrite is done — `main` has held no Python since G7.5 — but not everything
is smooth, and this post does not pretend it is. What follows is what is true
today.

---

## First comment (the one that should actually be read)

**Why Go.** orch spends its life doing four things: starting CLI processes and
supervising them for hours (including killing the whole process group when a
task times out), parsing their JSON output, writing SQLite, and serving HTTP. Go
does all four in its standard library or close to it, and cross-compiles to a
static binary with no C toolchain — the SQLite driver is pure Go, so there is no
cgo and no libsqlite to find. Rust would have done it too; for a first project
in the language, async subprocess supervision and the borrow checker would have
roughly doubled the time for nothing a user would notice.

The reason that mattered most was not performance, it was the install. orch is
handed to people already fighting their own toolchain; "make a virtualenv, it
needs Python ≥3.11" is a step where people leave.

**What got thrown away.** Deliberately, not "not yet":

- `orch findings` — an internal dogfooding tool, not part of the product.
- The architecture-diagram generator, which depended on an external skill.
- The file-based state backend. Go only speaks SQLite; `orch migrate` imports an
  old project once.
- Three dashboard pages (Architecture, Documents, Doctor — the last one is
  `orch doctor` now) and the web setup wizard. The operator JS bundle went from
  680 kB to 483 kB at that trim. (It has grown back to 990 kB since, with the
  backoffice redesign — that is what new pages cost.)
- `orch stop`. Ctrl+C drains and exits; a second one kills the agents.
- The plan said the tunnel supervisor would support cloudflared. The Python code
  never had cloudflared, only autossh (Pinggy) and bore, so that is what got
  ported. Porting what the plan said instead of what the code did would have
  been a new feature with nothing real to test it against.

(Jinja was already gone before the rewrite started; the dashboard had become a
React app while still in Python.)

**What it weighs.**

- The binary: **20.5 MB** on linux/amd64, 19.3 MB linux/arm64, 20.8 MB
  darwin/amd64, 19.8 MB darwin/arm64 — the `v0.29.0` release artifacts as
  `install.sh` downloads them (8.1–8.7 MB as `.tar.gz`), with the operator app
  (990 kB of JS) and the client page (242 kB) embedded.
- Startup: `orch --version` takes **4.8 ms** (median of 50, p95 5.1 ms);
  `orch validate` on a fresh `python-api` project 6.3 ms, `orch tasks` 10.9 ms
  — release binary, Ryzen 7 5700G, Linux, update check off.
- Dependencies: the Python package had 4 runtime dependencies; the Go module has
  10 direct ones, 9 of them shipped in the binary (`cobra`/`pflag`, `yaml.v3`,
  `modernc.org/sqlite`, the MCP SDK, `uuid`, `fpdf`, `go-landlock`, `x/term`)
  plus one used only by tests.
- **The Go tree is not smaller.** 66,268 lines of non-test Go against 21,279 of
  non-test Python, and 58,953 lines of Go tests against 26,606 of Python tests.
  The migration plan estimated about 9,500 lines of Go; at the cut-over
  (`v0.12.0`) it was already 32,051, and most of the rest is what came after
  (below), not the port.

**What the Go version does that Python did not:** an MCP server with 13
`orch_*` tools so a dispatched agent can report status without shell scripts,
`orch explain`, `orch publish` (the client page as a static site, to a
directory, a `gh-pages` branch or a Cloudflare Worker), `orch report pdf`,
`orch sync issues` (labelled GitHub issues become tasks), `orch estimate`,
`orch bench`, a local verify step before the push, a Landlock sandbox for
agents on Linux, a portfolio view of several projects in one dashboard, and a
planning pipeline shipped as agent skills (PRD → architecture → spec → tasks).

**What is still rough, stated plainly:**

- **All six backends have adapters, and every success path is tested against a
  real captured run.** Capturing them is how I learned that gemini refused to
  run in a directory it was not told to trust — and orch always dispatches into
  a fresh worktree. That is fixed: orch now sets `GEMINI_CLI_TRUST_WORKSPACE`
  for gemini only, the same door claude's and codex's approval flags already
  open.
- As of the cut-over, on a ChatGPT-plan login `codex` refused every codex model
  id in the default router, and 11 of the 43 opencode model ids in the default
  router did not exist in opencode's catalogue. Neither is re-checked here
  (neither CLI is on the machine these numbers come from); `orch doctor`'s
  `models.available` check now warns about the second per machine.
- The demo GIF was shot with one provider (`claude`), not three. The first
  take is also where I found that a private repository on an account with a
  billing problem makes every CI job "fail" before it starts, and orch read
  that as a red test run and retried.

---

## The part I would actually want to discuss

**Porting found 32 numbered bugs in the Python version. Its test suite — 1,530
passing tests — was green through every one of them.**

They were found by someone trying to reproduce a behaviour in another language
and discovering there was no behaviour to reproduce, or that it was not the one
the code appeared to describe; by capturing the real output of the CLIs orch
drives instead of writing sample output by hand; and in one case by a Go test:
bug 20 surfaced because a Go end-to-end script never terminated, where the
Python loop spun forever on a task whose model had no route.

A few, to show the shape:

- **CI polling never worked, in either version.** orch asked `gh pr checks` for
  a field `gh` does not have, got an error, and read the error as "still
  pending" — forever. Under that, `gh` reports states in upper case and orch
  compared against lower case. No CI result was ever seen, no CI retry ever
  fired, nothing was ever auto-merged. The tests fed the parser output written
  from the parser's own expectations.
- **The budget guardrail did nothing on SQLite projects**, which had been the
  default for a release: the gate read the old log files, the SQLite backend
  only wrote the table. 750,000 tokens against a 600-token cap said "go ahead"
  while the dashboard showed the real spend.
- **The client page showed spend with `show_spend_to_stakeholder` off.** The
  flag gated one route; the summary the page actually renders carried the
  figures unconditionally, and a test asserted them.
- **The retry backoff was computed and ignored.** The ready-set pass relaunched
  the task in the same tick, and could launch it twice. The test checked the
  timestamp was stamped, not that anything waited.
- **Every opencode failure reached the operator as a Python dict repr**, because
  the error sentence is one level deeper in the JSON than the parser looked.
  Found only by capturing a real failure.

**Why the tests did not catch them** is the interesting half, and it was
consistent enough to turn into review rules. The repository's review checklist
has 30 rules; the last eight came out of this migration, each from a specific
failure:

| Rule | Written because |
|---|---|
| 23 — a test that normalises before comparing must justify each normalisation | Three tests kept passing over the bug they existed to catch. One asserted `"…: specs/" in text`, which `specs/specs/…` satisfies. One flattened a Python golden's `null` to `""` — in the file whose job was catching `null` vs `""`. One compared timestamps as strings, where `+00:00` and `Z` spell the same instant. |
| 24 — a new test must be seen to fail | A test written after the fix and never seen red tests the author's model of the bug, not the bug. |
| 25 — a short-circuit is asserted by what it did not do | "Returns the right answer" does not distinguish a gate that skipped the database from one that queried it and ignored the result. |
| 26 — a golden carries a test of its own coverage | Regenerating a fixture can silently drop the boundary case while the comparison keeps passing. |
| 27 — parity expectations come from running Python, not reading it | `999_999` formats as `"1000.0k"`, not `"1m"`. `repr(1.0)` is `"1.0"`, not `"1"`. No careful reading survives those. |
| 28 — a shipped fixture beats an invented one | A hand-written sample tests the parser; the real file also tests that what we ship still parses. The CI-polling bug above is the same failure one step out: invented `gh` output. |
| 29 — run what you built and read its output once | A wizard's model-tier prompt rendered as `[agy/pro/claude/claude-opus-4-7/codex/gpt-5.6/…]` — unreadable. Eight tests passed: each asserted that the summary *displayed* an answer, never that the prompt was legible. |
| 30 — `tasks.json` does not know a task's status; anything that renders one hydrates first | Status and comments moved from the file to SQLite, and readers that nobody moved kept serving the file's frozen copy: a graph that drew a half-done project as untouched, a task API that returned no comments for every task orch had actually run. Empty, and plausible. |

If there is one transferable thing here, it is rule 23. A test that lower-cases,
trims, sorts, or accepts two spellings before comparing has decided not to see a
difference — and that is exactly where the bug is.

The second is rule 28: sample output written by the person who wrote the parser
tests the parser against its author's beliefs. Capture the real thing.

---

## Honest comparison

Against the tools people in this space actually use. Re-checked on 2026-10-29
against the GitHub API and each project's README; re-check on posting day.

| | Multica | Vibe Kanban | Agetor | Claude Squad | **orch** |
|---|:---:|:---:|:---:|:---:|:---:|
| GitHub stars | 54.3k | 28.5k | 104 | 8.7k | **2** |
| Stack | Go + Next.js + Postgres | Rust | TypeScript (Electrobun) | Go | Go |
| License | Multica License (Apache-2.0 + additional conditions) | Apache-2.0 | MIT | AGPL-3.0 | MIT |
| CLIs/agents supported | 26 | 10+ | 8 | ~4 named + any CLI | 6 |
| Desktop app | ✅ Electron | ❌ | ✅ macOS (Apple Silicon) | ❌ (TUI) | ❌ |
| Mobile app | ✅ iOS (build from source) | ❌ | ❌ | ❌ | ❌ |
| Cloud-hosted option | ✅ multica.ai | ❌ (Bloop's cloud shut down; self-host guide) | ❌ | ❌ | ❌ |
| Single binary | ❌ | ❌ | ❌ | ✅ | ✅ |
| Read-only stakeholder view | ❌ | ❌ | ❌ | ❌ | ✅ |
| Per-provider budget guardrail (blocks dispatch) | ❌ | ❌ | ❌ | ❌ | ✅ |
| Spec → tasks pipeline | ❌ | ❌ | ❌ (agent pipelines, not spec → DAG) | ❌ | ✅ |
| Static export of the client page | ❌ | ❌ | ❌ | ❌ | ✅ (`orch publish`) |

What changed since the 2026-09-13 draft: Multica 49.6k → 54.3k stars and its
license is now its own "Multica License" (Apache-2.0 text plus conditions,
GitHub reports `NOASSERTION`); Vibe Kanban 28.1k → 28.5k; Agetor 65 → 104 stars
and 5 → 8 harnesses (Claude Code, Codex, Cursor, Gemini CLI, fx, Antigravity,
GitHub Copilot, OpenCode); Claude Squad 8.5k → 8.7k, its README names Claude
Code, Codex, Gemini and Aider plus any CLI as a profile (last push 2026-08-20);
orch 1 → 2 stars and 5 → 6 backends (`muse`). Multica's "usage analytics" shows
cost per run, but nothing in its README blocks a dispatch on a budget.

And against itself:

| | Python (v0.11.0) | Go (v0.29.0) |
|---|---|---|
| Install | `pipx install`, needs Python ≥3.11 | one binary, 19.3–20.8 MB (8.1–8.7 MB download) |
| Runtime dependencies on the machine | Python + 4 packages | none |
| Dispatch backends | claude, codex, opencode, gemini, agy | those five + muse (gemini dispatchable since the trust fix) |
| Command surfaces | 21 | 37 (36 in `orch --help` + hidden `sandbox-exec`; no `findings`, no `stop`) |
| Dashboard | FastAPI + uvicorn + a built SPA on disk | `net/http`, SPA embedded |
| New in Go | — | MCP server (13 tools), `explain`, `publish`, `report pdf`, `sync issues`, `estimate`, `bench`, verify, sandbox, portfolio |
| Non-test lines | 21,279 | 66,268 |
| Tests | 1,530 passing pytest cases (1,327 `test_` functions) | 1,805 test functions (3,103 passing tests and subtests in `go test ./...`), 44 testscript files, 247 web test cases |

Two caveats that belong next to the table, not in a footnote:

- **"No runtime dependencies" is about the machine, not the code.** The Go binary
  compiles its dependencies in. Same parts, shipped differently.
- **The test counts count different things** — Go test functions, many of them
  table-driven, against pytest's collected cases including parametrised ones.

---

## Sources

Run on 2026-10-29: release `v0.29.0` (artifacts from GitHub, checksums
verified) and `main` = `ba0fff4` for everything counted in the tree; Go 1.27.1,
linux/amd64, AMD Ryzen 7 5700G. `sh docs/launch/measure.sh v0.29.0` reruns the
lot; `docs/launch/measurements.txt` is its output.

| Number | Command |
|---|---|
| binary 20.5 / 19.3 / 20.8 / 19.8 MB (20,521,144 / 19,333,304 / 20,848,048 / 19,767,730 bytes); archives 8,627,626 / 8,076,899 / 8,721,879 / 8,324,955 | `gh release download v0.29.0 -p 'orch_v0.29.0_*' -p checksums.txt`, `sha256sum -c`, untar, `stat -c %s` (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64). MB = bytes / 1,000,000. `main` built the same way with `.goreleaser.yaml`'s `-s -w` is ~1–4 % larger (21,438,624 bytes linux/amd64): the welcome page landed after the tag |
| was 15.7 MB at `v0.12.0-dev` | the 2026-09-13 draft (`5db7091`), MB = bytes / 1,048,576 there |
| startup 4.8 ms / 6.3 ms / 10.9 ms | `measure.sh`'s timing block: 5 warm-ups then 50 runs of the release binary with `ORCH_NO_UPDATE_CHECK=1` — `orch --version`, `orch validate` and `orch tasks` inside a fresh `orch init --template python-api` project; median, p95 alongside |
| operator JS 990 kB, client page JS 242 kB | `make web`, then `du -cb internal/dashboard/dist/build/assets/*.js` and `internal/publish/dist/stakeholder/assets/*.js` (989,885 / 241,631 bytes) |
| operator bundle 680 → 483 kB at the trim | PR #163 |
| 66,268 / 58,953 Go lines | `rg --files -g '*.go' -g '!*_test.go' internal cmd \| xargs cat \| wc -l`, and the same with `-g '*_test.go'` |
| 32,051 Go lines at the cut-over | the 2026-09-13 draft, same command at `5db7091` |
| 21,279 / 26,606 Python lines | the 2026-09-13 draft (Python is on `python-legacy` now; not re-run) |
| ≈9,500 lines estimated | the migration plan's Python → Go map (the "Orch en Go" planning document) |
| 1,805 Go test functions | `rg -c '^func Test' --glob '*_test.go' internal cmd` summed |
| 3,103 passing tests and subtests, 0 failing | `make web && go test ./... -count=1 -json`, counting `"Action":"pass"` / `"fail"` events that carry a `Test` |
| 44 testscript files, 247 web test cases | `rg --files -g '*.txtar' \| wc -l`; `rg -c '^\s*(it\|test)\(' -g '*.test.ts*' web/src` summed |
| 1,327 / 1,530 Python tests | the 2026-09-13 draft (pytest baseline then in `CLAUDE.md`) |
| 4 Python runtime dependencies | `dependencies` in `pyproject.toml` on `python-legacy` |
| 10 direct Go modules, 9 shipped, 1 test-only | the first `require` block of `go.mod`; `rogpeppe/go-internal` is imported only from `_test.go` files (`rg -l` per module) |
| 21 Python command surfaces | the 2026-09-13 draft |
| 37 Go command surfaces | `orch --help` (36, excluding `help` and `completion`) plus the hidden `sandbox-exec` (`rg 'Hidden:\s*true' internal/cli`) |
| 30 checklist rules | `rg -c '^[0-9]+\. \*\*' .github/review/CHECKLIST.md` |
| rules 23–30 and their origins | the rules themselves, in `.github/review/CHECKLIST.md` |
| 32 numbered bugs | highest number in `rg -o -i 'bug ([0-9]+)' -r '$1' docs internal web/src .github \| sort -n -u` |
| bug 20 found by a Go test | `docs/brainstorm/go-migration-notes/orch-98.md`: "a testscript hung until its 540 s timeout" |
| 6 backends with adapters | `internal/providers/{claude,codex,opencode,gemini,agy,muse}.go`; real captures in `internal/providers/testdata/` |
| gemini trust fix | `HeadlessEnv` in `internal/providers/gemini.go` (`GEMINI_CLI_TRUST_WORKSPACE=true`) |
| codex model refusal, 11 of 43 opencode ids | PR #215 and `docs/brainstorm/go-migration-notes/sonnet-2.md` — as of the cut-over, not re-checked |
| 297 PRs merged since #91 | `gh pr list --state merged --limit 1000 --json number --jq '[.[] \| select(.number >= 91)] \| length'` |
| 45 packages under `internal/` | `rg --files -g '*.go' -g '!*_test.go' internal \| xargs -n1 dirname \| sort -u \| wc -l` |
| 13 MCP tools | `rg -o 'Name:\s*"orch_[a-z_]+"' internal/mcp`, unique; matches `docs/MCP.md` |
| 18 Go releases, latest `v0.29.0` | `gh release list` |
| rivals' stars, licenses, agents | `gh api repos/<owner>/<repo>` (`stargazers_count`, `license`, `pushed_at`) and each README, 2026-10-29 |
| clean Linux install | `docs/launch/clean-install-linux.txt` (fresh `ubuntu:24.04` container) |

**Deliberately not in this post**, because nobody has measured them: memory
use, throughput, and any "N× faster" claim against Python. If someone asks in
the thread, the honest answer is that the rewrite was about distribution and
correctness, and the performance question has not been asked yet.
