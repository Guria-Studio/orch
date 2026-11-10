# CI review

Every pull request gets an automated review from Gemini. It leaves one comment
and two status checks. The reviewer itself has never merged anything and cannot
— it is handed no credentials. What can merge is the `policy` job beside it,
for PRs that touch no protected zone, and only while the `ORCH_AUTOMERGE`
repository variable says `on`.

---

## What runs

`.github/workflows/review.yml`, on `pull_request` (`opened`, `synchronize`,
`reopened`), for branches in this repository only. Two jobs:

| Job | Check it publishes | What it does |
|---|---|---|
| `gemini-review` | `gemini-review` | Sends the checklist, the linked issues, the PR and its diff to Gemini, gets JSON back, posts the comment. Runs on **every** PR, auto-merge eligible or not |
| `policy` | `automerge-eligible` | Decides whether the PR qualifies for auto-merge, and arms or disarms GitHub's auto-merge accordingly |

A new push cancels the review of the commit it replaced.

### What the reviewer receives

The prompt is assembled by `.github/review/build-prompt.py` from files the
job fetched, in this order:

1. Instructions and the JSON schema.
2. `.github/review/CHECKLIST.md`.
3. **The spec**: title and body of each issue the PR closes or refers to —
   GitHub's `closingIssuesReferences` plus every `Closes #N`, `Fixes #N`,
   `Resolves #N`, `Refs #N` or `Part of #N` in the body, at most five issues
   and 8 KB. Fenced as untrusted data; checklist rule 0 judges the diff
   against it. No linked issue, no section, and rule 0 is skipped.
4. The PR title and body, **read when the job runs** (`gh pr view`), not
   from the event payload: a re-run after editing the body sees the edit.
5. The list of changed files, and the diff.

The prompt goes to the Gemini CLI on stdin, capped at **600 KB** (about
150k tokens). The old cap was 120 KB because the prompt travelled as one
environment string, and 7 of the 36 PRs measured in #521 were reviewed with
part of the diff missing. When a diff still does not fit, code and tests go
in first, then fixtures and generated files, then docs, and the prompt names
every file it left out.

The log line `prompt is N bytes (diff D, budget B, spec issues I, files left
out F)` says what the reviewer got.

### What the reviewer can and cannot do

The reviewer is handed that prompt. That is all it gets:

- **No GitHub token.** The step that runs Gemini has no credentials for this
  repo: `.github/review/run-reviewer.sh` removes `GH_TOKEN` and
  `GITHUB_TOKEN` from the CLI's environment. It cannot comment, push, merge,
  or read anything it was not given. The issues are fetched by the step
  before it, with the job's token, and passed in as text.
- **No tools, no checkout.** The Gemini CLI runs with `"tools": {"core": []}`
  — no shell, no file access, no MCP servers — in an empty temporary
  directory, so nothing in the PR (a `GEMINI.md`, a `.gemini/settings.json`)
  can reach it.
- **The workflow publishes on its behalf.** The comment and the checks are
  written by the job, from the JSON the model returned, after that JSON has
  been validated against a fixed schema.

`pull_request`, never `pull_request_target`: the latter would run this with a
writable token in the base repo's context while reading a contributor's diff.
Both jobs also skip PRs from forks outright.

---

It never opens a browser, so nothing about a rendered page is covered here —
a route that answers 200 with the wrong body, a component that throws on
first render, a login form shown to a profile that needs no token. See
[`UI-CHECKS.md`](UI-CHECKS.md) for how those are checked.

## Before you open the PR

Run what you built and read its output once (checklist rule 29). The reviewer
reads a diff; it cannot see that a prompt renders as an unreadable run-on or
that a banner names a file that is not there. If the PR changes anything a
user sees — a prompt, a banner, an error, a report — paste it in the body.

A green suite is not evidence about output no test reads.

## Reading the comment

One comment per PR, edited in place on every push — so the newest review is
always the one you are looking at, and it never buries the human conversation.

```
## Gemini review

🛑 Changes requested.

Two issues in the dispatch loop; the rest of the diff is clean.

**Blocking**

- `internal/engine/loop.go:88` — **os/exec timeout**: no context deadline on the provider call.

**Minor**

- `internal/cli/run.go` — **table tests**: three near-identical test cases.

🔒 Not auto-merge eligible — touches protected paths: internal/engine/loop.go.
```

Two lines are written by the workflow, not by the model:

- **"Not checkable from the diff (N): …"** — the minor findings that only said
  a rule could not be verified from the diff. In #521 these were 42 of 97
  findings and 3 of them were useful, so they are collapsed into one line
  with the rules they named. The full list stays in `review.json`.
- **"(not merge-blocking: moved from Blocking)"** — the reviewer put a
  finding under a rule that cannot block a merge; it is shown as minor. See
  [When the review blocks a merge](#when-the-review-blocks-a-merge).

**Verdicts.** `approve` — nothing to flag. `comment` — minor findings only.
`request_changes` — at least one blocking finding.

**Blocking vs minor.** Blocking means a rule in
[`.github/review/CHECKLIST.md`](../.github/review/CHECKLIST.md) was broken, and
every finding names the rule it came from. If a finding does not name a rule
you can point at, the review is wrong — and the fix is a PR against the
checklist, not an argument in the thread. That is why it is versioned.

**The check.**

| `gemini-review` | Meaning |
|---|---|
| ✅ success | Verdict was `approve` or `comment` |
| ❌ failure | A finding under a merge-blocking rule, **or the review did not produce one** — the CLI failed, the reviewer returned nothing twice, or it answered off-schema |
| ⚪ neutral | **Only** "no API key" — a repository setting, not a review outcome |

**A review that did not happen is a failure, not a neutral**, and that is a
deliberate change. A neutral check leaves the PR reading "clean and
mergeable" in the checks summary, so an unread verdict looks exactly like an
approval at a glance — #206 nearly merged that way. The only thing that stays
neutral is a missing API key, because that is a repository that has not
switched the reviewer on rather than a review that went wrong.

The failure title says which happened:

- **"Reviewer failed"** — the Gemini CLI step did not complete.
- **"Reviewer returned nothing"** — it ran and produced an empty response,
  twice. This one is non-deterministic (the CLI does it occasionally on a long
  prompt), so the workflow retries once before giving up. If it starts
  happening often, pin a stronger model with `GEMINI_REVIEW_MODEL` — the empty
  responses observed so far came from `gemini-3.1-flash-lite`, the CLI's
  default.
- **"Unreadable verdict"** — it answered, but not to the schema.

### When the verdict is unreadable, read what it said

The raw reviewer output is uploaded as an artifact **before** parsing, so a
parser crash cannot lose it:

```bash
gh run download <run-id> -n reviewer-raw-<pr>-<attempt>
cat reviewer-raw.txt
```

Kept for 7 days. **Download it before re-running.** The validated verdict,
`review.json` (findings, demotions and the "not checkable" list), is uploaded
as `review-json-<pr>-<attempt>` and kept for 90 days, for the next
measurement. A re-run may well produce
a readable verdict and hide the reason the first one was not — the unreadable
verdicts seen so far were a finding about the PR itself (its title) with no
`file` to attach it to, which the parser required and now treats as optional,
rendering it as "(PR)".

Neither neutral nor an unread failure is a pass. Both mean "nobody reviewed
this".

**A human owns the merge.** A wrong `request_changes` is a reason to merge
anyway and open a PR against the checklist — with the comment described
below.

---

## When the review blocks a merge

`gemini-review` is a required check on `main`, so a red one stops the merge.
It turns red only for a finding under one of the **merge-blocking rules** of
`.github/review/CHECKLIST.md`: rules **0, 1, 2, 3, 4, 5, 6, 7, 8, 16, 17, 18, 19, 22 and 31**, each as
the checklist words it:

| Rule | Blocks when |
|---|---|
| 0 | the diff visibly fails an acceptance criterion of the linked issue |
| 1–4 | `tasks.json`, `config.yaml`, SQLite or CLI compatibility breaks without a migration |
| 5, 6, 7 | a real secret, a real person's path, a third-party runtime call |
| 8 | new behaviour without a test, **only when the branch changes state or output** |
| 16, 17, 18 | an illegal status transition, a second SQLite writer, a provider exec without a process group or timeout |
| 19 | a dropped error **on a path that writes state or decides a result** |
| 22 | `time.Sleep` to wait in a test, or a wall-clock assertion without slack |
| 31 | an instruction aimed at agents in `.orch/knowledge/` |

Everything else is minor and never turns the check red. The list lives in one
place, the `<!-- merge-blocking-rules: ... -->` line at the top of the
checklist: the reviewer reads it there, and `parse-review.py` reads the same
line and moves any blocking finding under another rule to minor before the
check is published. `parse-review.test.sh` fails if this table and that line
disagree.

Where the list comes from: the measurement of 50 PRs in
[`analysis/code-review-measurement-2026-11.md`](analysis/code-review-measurement-2026-11.md)
(#521) proposed rules 0, 2–5, 8, 16–19 and 22. Rules 1, 6, 7 and 31 are added
because they are the same kind of contract or safety rule and produced no
noise in the sample; rules 5, 6, 19 and 22 were recalibrated in the same
change (fake secrets in a secret scanner's tests, placeholder paths, errors
on read-only paths, wall-clock asserts).

### Merging on red

Possible (`enforce_admins: false`), and sometimes right: the reviewer is
wrong about one blocking finding in three. But it takes **a comment on the
PR saying, per blocking finding, why it does not apply** — or a commit that
fixes it. None of the five PRs merged on red in the measured period had one,
and two of them shipped the real defect the review had pointed at (#564, #488).
A finding that is wrong because the checklist is wrong gets a PR against the
checklist too.

### Measure, then keep or turn off

The review stays required while it earns it. Over the **next 30 PRs merged
after these changes**, measure the same way the report did (the
`review-json-*` artifacts make it a script, not an archaeology dig):

- **Fewer than 50 % of the blocking findings useful**, or **half of the
  `request_changes` verdicts merged on red** → make it comment-only: remove
  `gemini-review` from the required checks (the protection call below,
  without it in `contexts`). The comment keeps coming.
- And if, on top of that, **fewer than 0.2 useful findings per PR** → turn
  it off: `gh workflow disable "PR review" --repo Guria-Studio/orch`.
- Otherwise it stays as it is, and the next measurement is in another 30.

"Useful" is the report's definition: it points at a defect or a gap worth
closing, not only at something literally true.

---

## Auto-merge eligibility

`.github/review/policy.json` holds the rules. A PR is eligible when **all** of:

- **≤ 400 changed lines** (additions + deletions)
- **no protected paths touched** — `.github/**`, `go.mod`, `go.sum`,
  `.goreleaser.yaml`, `internal/engine/**`, `internal/state/**`,
  `internal/providers/**`, `internal/dashboard/auth*`
- **no `needs-human` label**

The reasoning behind each protected path is in `policy.json` itself.

This publishes the `automerge-eligible` check (`success` when eligible,
`neutral` with the reason when not) and a line in the comment — and, when
`ORCH_AUTOMERGE` is `on`, arms GitHub's auto-merge on the eligible ones.

**Eligible PRs are reviewed too.** `gemini-review` needs `policy` only for its
eligibility line, and runs whatever `policy` did (`if: always()`). Until #521
it did not: arming failed on every eligible PR of the measured period
("Resource not accessible by integration"), the failed `policy` job skipped
the review, the required `gemini-review` check never arrived, and the PR sat
BLOCKED. A failure to arm is now a warning on the run, not a failed job:
the PR gets its review and a human merges it.
Arming is not merging: GitHub holds the PR until every required check passes,
so the reviewer's verdict is what releases it. An ineligible PR is never
armed, and one that *was* armed and has stopped qualifying is disarmed. See
[Branch protection and auto-merge](#branch-protection-and-auto-merge-g47).

To force a human review regardless of size: `gh pr edit <N> --add-label needs-human`.

The rules are covered by fixtures. Run them anywhere:

```bash
bash .github/review/policy.test.sh        # the eligibility rules
bash .github/review/parse-review.test.sh  # the verdict parser and its calibration
bash .github/review/build-prompt.test.sh  # the prompt: spec, fresh PR text, diff budget
bash .github/review/run-reviewer.test.sh  # the CLI runner: stdin, no token, no tools
```

All of them also run inside their own jobs, before the code they test is trusted.

---

## Setup

One secret, set once by someone with admin on the repo:

```bash
gh secret set GEMINI_API_KEY --repo Guria-Studio/orch
# paste the key from https://aistudio.google.com/apikey
```

Without it, `gemini-review` publishes a `neutral` check saying so and posts no
comment. It does not fail the build, and it does not quietly pass either.

### Choosing a model

The workflow leaves `gemini_model` empty, which means the Gemini CLI picks its
own current default — the same thing the action's own examples do. Pinning a
model name in a workflow file is a guess that goes stale.

To pin one:

```bash
gh variable set GEMINI_REVIEW_MODEL --body 'gemini-2.5-pro' --repo Guria-Studio/orch
gh variable delete GEMINI_REVIEW_MODEL --repo Guria-Studio/orch   # back to the default
```

The review runs the Gemini CLI itself (`@google/gemini-cli`, pinned to an
exact version in `review.yml`), not the `run-gemini-cli` action, because the
action passes the prompt as an argument and caps it at 128 KiB. Bumping the
CLI is a deliberate edit, not a surprise. The issue triage
(`triage.yml`) still uses the action, pinned to a commit SHA (`v0.1.22`): an
issue fits.

---

## Turning it off

Least invasive first:

```bash
# 1. Stop reviewing, keep everything in place: delete the key.
gh secret delete GEMINI_API_KEY --repo Guria-Studio/orch

# 2. Disable the workflow entirely (checks stop appearing).
gh workflow disable "PR review" --repo Guria-Studio/orch
gh workflow enable  "PR review" --repo Guria-Studio/orch

# 3. Skip it for one PR.
gh pr edit <N> --add-label needs-human   # blocks eligibility, review still runs
```

Deleting `.github/workflows/review.yml` also works and is the honest option if
the thing turns out not to earn its keep.

---

## Branch protection and auto-merge (G4.7)

The code side ships enabled. What makes it act is two repository settings and
one variable, none of which live in the repo — apply them by hand, in this
order.

### 1. Protect `main`

```bash
gh api -X PUT repos/Guria-Studio/orch/branches/main/protection \
  --input - <<'JSON'
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["go-test", "go-lint", "gemini-review"]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
JSON
```

Required checks match on **name**, and a name that matches nothing never
arrives — the PR waits on it forever. So the list above is the job names as
they are actually written today:

| Context | Where it comes from |
| --- | --- |
| `go-test` | the `go-test` job in `.github/workflows/go.yml` |
| `go-lint` | the `go-lint` job in the same file — it is `go-lint`, not `lint` |
| `gemini-review` | a check run this workflow publishes through the API; there is no job by that name |

`parity` (Go against Python on the same project) and `smoke` (the Python wheel)
were jobs until G7.5 archived the Python line on `python-legacy`; neither
exists any more, and a protection rule still naming either would hold every PR
forever. `automerge-eligible` is deliberately **not** required: it publishes
`neutral` when a PR needs a human, and `neutral` does not satisfy a required
check, so requiring it would block exactly the PRs it is meant to route to a
person.

`go.yml` does not filter on `paths`, so a docs-only PR does run `go-test` and
can genuinely go green. Nothing to special-case.

### 2. Let PRs merge themselves

```bash
gh api -X PATCH repos/Guria-Studio/orch --field allow_auto_merge=true
gh variable set ORCH_AUTOMERGE --body 'on' --repo Guria-Studio/orch
```

Both are needed. Without `allow_auto_merge` the `Arm auto-merge` step fails
with a GraphQL error that names no cause — the step turns it into a
warning pointing back here, but the fix is the flag.

"Resource not accessible by integration" is the other cause, and the one seen
on every eligible PR in November 2026: the `policy` job's token has
`contents: read`, and arming auto-merge with the workflow token most likely
needs `contents: write` as well (not verified here: it would take arming a
real PR). Granting it is a decision, not a fix — that token
would then be able to push — so it is not made here. Until someone makes it,
eligible PRs are reviewed and wait for a human like the rest; set
`ORCH_AUTOMERGE` to `off` if the warning is noise.

### The switch

`ORCH_AUTOMERGE` is global and reversible:

```bash
gh variable set ORCH_AUTOMERGE --body 'off' --repo Guria-Studio/orch
```

Anything other than `on` — including the variable not existing — means off.
The `policy` job then not only stops arming but **disarms** what is already
armed, on the next event each open PR receives. That matters: auto-merge is a
standing instruction stored on the PR, so a switch that only stopped arming
would leave yesterday's eligible PRs merging themselves today. The same step
covers a PR that was eligible and stopped being one, when a later push added
a protected path or someone applied `needs-human`.

To take one PR out without touching the switch:

```bash
gh pr edit <N> --add-label needs-human   # ineligible; disarmed on the next push
gh pr merge <N> --disable-auto           # right now, this PR only
```

### Worth knowing

- A `neutral` conclusion does **not** satisfy a required check. A missing
  `GEMINI_API_KEY` therefore blocks merges rather than waving them through —
  the right way round, but it does make the key load-bearing.
- `enforce_admins: false` leaves you a way out when the reviewer is wrong and
  the fix is urgent. It also means "no direct pushes to `main`" holds for
  everyone *except* admins; set it to `true` if you want that literally, and
  accept that the escape hatch closes with it.
- `strict: true` requires a PR to carry the tip of `main`. Every merge
  therefore re-runs the checks of the PRs still open, and they land one at a
  time. That is fine at this volume; if it stops being fine, `strict: false`
  or a merge queue is the trade.
- Protected zones are enforced by *not arming* the PR, not by GitHub. A human
  still has to press merge, and branch protection still holds the required
  checks — but nothing stops an admin merging a protected-zone PR without a
  second pair of eyes. Add `required_pull_request_reviews` or a CODEOWNERS
  file the day this repo has more than one maintainer.
- Both jobs skip fork PRs, so `gemini-review` never appears on one and it can
  never satisfy its required check. Fork contributions need an admin merge
  until that is addressed.

---

## Issue triage

`.github/workflows/triage.yml` runs on every new issue (and by hand, with
`workflow_dispatch` and an issue number, to triage one again). Gemini reads
the issue, `.github/review/TRIAGE.md` and the repository map from `CLAUDE.md`,
and answers with JSON that `.github/review/parse-triage.py` validates. The
workflow then:

| It sets | From |
|---|---|
| `priority:urgent` or `priority:planned` | the rubric: urgent means state lost, the main flow blocked, work wrongly marked done, install broken, security or a regression, **and** no workaround |
| `type:bug`, `type:improvement` or `type:feature` | the rubric |
| a milestone | the newest `vX.Y.Z` tag: urgent → `vX.Y.Z+1`, planned → `vX.Y+1.0` (created when missing) |
| one comment (`<!-- orch:gemini-triage -->`, edited on a re-triage) | severity, workaround, likely areas, the reasoning; `cc @owner` only when urgent |

Same guarantees as the reviewer: the model gets no token and no tools, the
issue text reaches it only inside the prompt (never through `${{ }}` in a
`run:` block) and is marked untrusted, and a malformed answer labels nothing
and fails the run. An empty answer is retried once.

An issue **opened with both a `type:*` and a `priority:*` label** keeps them:
triage adds and removes nothing, and its comment says which labels were kept
and what Gemini would have proposed. A milestone set at creation is never
replaced either. A
re-triage (`workflow_dispatch`) replaces the previous proposal, removing only
the `type:`/`priority:` labels that differ from the new one. The rules live in
`.github/review/triage-labels.sh`, tested by `triage-labels.test.sh` at the
start of every run (#588).

It is a **proposal**. Change the labels or the milestone when you disagree:
the workflow does not run again on its own.

### `claude:go` — handing an issue to Claude

Nothing is worked on until the maintainer adds **`claude:go`**. A Claude Code
cloud routine (set up with `/schedule`, running on the maintainer's Claude
subscription) checks every hour, takes **one** issue per run (urgent first),
and moves it through:

| Label | Meaning |
|---|---|
| `claude:go` | approved by the maintainer, waiting for the routine |
| `claude:working` | taken; the next run will not take it again |
| `claude:pr-open` | a PR with `Closes #N` is open, and the issue has a comment saying what changed |
| `claude:blocked` | the routine could not fix it; its comment says why |

The routine never merges, never tags and never releases. For something that
cannot wait an hour, ask a Claude session directly.

---

## `orch ci review` in your own project

Everything above is orch's own repository: `review.yml` runs the Gemini CLI
action and `.github/review/parse-review.py` reads its answer. `orch ci review`
is the review orch offers **the projects it manages**, and it is a different
thing: the project's workflow installs orch on the runner and runs one command,
which builds the prompt, calls the provider, parses the verdict in Go and posts
the comment.

- **Any of four providers**, through orch's own CLI adapters: Claude
  (`claude`), Gemini (`gemini`), OpenAI (`codex`) and OpenRouter (`opencode`
  with an `openrouter/…` model). Each runs read-only in an empty directory.
- **The project's checklist**, `.github/orch-review.md` by default, or a
  built-in general one.
- **Its own comment**, marked `<!-- orch:ci-review -->`, so it never edits the
  Gemini reviewer's `<!-- orch:gemini-review -->` one.
- **Advisory unless `--blocking`**: without it, a provider that fails is a
  warning and the job passes.

A job looks like this:

```yaml
review:
  if: github.event.pull_request.head.repo.full_name == github.repository
  runs-on: ubuntu-latest
  permissions:
    contents: read
    pull-requests: write
  steps:
    - uses: actions/checkout@v4
      with:
        fetch-depth: 0
    - run: curl -fsSL https://raw.githubusercontent.com/Guria-Studio/orch/main/scripts/install.sh | sh
    - run: npm install -g @anthropic-ai/claude-code
    - run: orch ci review --provider claude --model claude-sonnet-4-5 --post
      env:
        ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        GH_TOKEN: ${{ github.token }}
```

The flags, the provider-to-secret table and the exit codes are in
[`CLI.md`](CLI.md) under `ci review`.

---

## Related

- [`.github/review/CHECKLIST.md`](../.github/review/CHECKLIST.md) — the rules the reviewer applies
- [`.github/review/policy.json`](../.github/review/policy.json) — the eligibility rules
- [`docs/brainstorm/go-migration-notes.md`](brainstorm/go-migration-notes.md) — the rewrite this CI is being built for
