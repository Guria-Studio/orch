# Dogfooding — agents report problems with orch

Every agent running under orch will notice bugs, missing features and quirks
of orch itself. The MCP tool **`orch_report_finding`** is the paved path from
"the agent noticed something" to "a GitHub issue on the orch repo", without
the operator copying it out by hand — and without anything leaving before
the operator has read it (#536).

The agent's call **queues** the report in `orch.db`, already redacted. The
operator reviews the queue with `orch findings` or on the dashboard's
System › Findings page, which show each report exactly as it would be filed,
and sends (`orch findings send ID`, or `--all`) or dismisses it. `orch run`
ends by saying how many wait. The Python line's `findings:` config block, a
different design, is ignored with a warning (see
[`CONFIG.md`](CONFIG.md#keys-that-are-ignored)).

---

## On in a new project

Sending files **public issues under your `gh` login**, so `orch init` (its
wizard) and `orch start` ask once — "Send orch the bugs your agents find in
it? You review each one before it is sent." — yes by default, and write the
answer in `.orchestrator/config.yaml`:

```yaml
report_findings:
  enabled: true
```

with a comment saying the issues are public and under whose login, and both
print a notice saying it is on and how to turn it off (#399). It was off by
default for a while (#376), with the first `orch run` that finished a task
offering it; almost nobody turned it on, and findings are how orch learns
about its own bugs from real projects. A project scaffolded in that window
still gets the offer, once.
`orch init --no-report-findings` writes `enabled: false`.
A config without the block — every project scaffolded before #315, or one you
wrote by hand — is **off**: upgrading orch never turns it on.
To turn it on in an older project, add the block with `enabled: true`.

It also needs:

- orch's MCP server loaded by the agent CLI. `orch init` writes `.mcp.json`
  for that; `orch doctor`'s `mcp.config` check says when it is missing (see
  [`MCP.md`](MCP.md#setup)).
- `gh` installed and authenticated (`gh auth status`), for the tool's search
  and for sending. The tool runs it from the MCP server process, so it works
  even when the agent's own Bash is denied.

With the key off, the tool is still listed, and a call answers with an error
telling the agent to tell the operator instead.

---

## What agents are asked

With `report_findings.enabled: true`, every dispatch prompt ends with an
optional block, after the report-back step:

```
Feedback about orch itself (optional, after you report back):
- If orch got in your way, or you noticed something orch should do better or does
  not do at all (a missing command or flag, a step you had to do by hand, a
  confusing message or status), report it once with orch_report_finding:
  type "bug" | "improvement" | "feature", title, summary.
- It is about orch, not this project: no secrets, no project code, no project or
  customer names. Skip it if nothing comes to mind.
```

With the key off, the prompt is unchanged. An agent you run outside `orch run`
(your own sub-agents, an interactive session in the project) can call the
tool too, as long as it has orch's MCP server.

---

## The call

`{type, title, summary, evidence?, repro?, suggested_fix?, confidence?, confirm_new?}`

- **`type`** — `bug` (orch misbehaves; include a repro), `improvement` (a
  quality-of-life correction, not a full feature) or `feature` (something
  orch does not do).
- **`title`** — one line naming the problem or the idea.
- **`summary`** — what happens and what should happen instead.
- **`evidence`**, **`repro`**, **`suggested_fix`** — optional sections of the
  issue body: log lines, command output, `file:line` in orch, minimal steps.
- **`confidence`** — `high`, `medium` or `low`, for whoever triages it. If
  the agent is guessing, `low`. Recorded, not enforced.

It is **only about orch**. A problem in the project's own code belongs on the
project's tracker; a sent report always goes to `Guria-Studio/orch`.

---

## What the tool enforces

| Check | Behavior |
|---|---|
| `report_findings.enabled` off | Error; nothing queued |
| `type` not bug / improvement / feature | Error; nothing queued |
| `title` or `summary` empty | Error; nothing queued |
| A closed `auto-reported` issue with the same title, ignoring case and punctuation | Returns `duplicate`; nothing queued |
| A likely duplicate: a report already waiting in the queue, an issue this project already filed (remembered in `orch.db`), or an open `auto-reported` one the search finds | Returns `duplicate` (and the issue's `url`); nothing queued, nothing commented |
| Other `auto-reported` issues match the title search | Returns `similar`; nothing queued unless the call sets `confirm_new: true` |
| Project root or home directory in the title or body | Replaced with `<project>` and `~` |
| A token, key or `password=`-style value | Replaced with `[redacted]` |
| Anything else | Queued for the operator, who sends or dismisses it |

Every sent issue gets the `auto-reported` label. `orch sync issues` refuses
to ingest that label, so a report never loops back into a project as a task.
The body records the type, confidence, capture time, orch version and OS, and
ends with a line saying it came through `orch_report_finding`.

The result is `{queued, queue_id?, url?, duplicate?, similar?, message}`.

**Likely duplicate** (#441) is a plain test, no model call: the two titles
share at least half their key words (lowercased, punctuation and stopwords
like "a", "the", "orch" dropped), or they share two key words and the same
quoted command or error string (a `` `backticked` `` or `"double-quoted"`
span in the title or summary). The search asks GitHub for the title's key
words with the `auto-reported` label. GitHub indexes a new issue only after a
while, which is how one config typo (`tue` for `true`) became #433, #434 and
#435 within a minute; so the project also remembers every issue it files, in
the `filed_findings` table of `orch.db`, and checks those first. A remembered
issue counts as open unless the search saw it closed.

The redaction knows the two paths and the common token shapes, not what else
in your project is private — a client's name, a line of their code. That is
why the prompt and the tool's description ask for none, and why nothing is
sent before you have read it.

---

## For the operator

- **Review and send**: `orch findings` lists what waits, as it would be
  filed; `orch findings send ID` (or `--all`) files it, `orch findings
  dismiss ID` drops it. The dashboard's System › Findings page does the same,
  and Now says when some wait.
- **See what was sent**: `orch findings --all`, or
  `gh issue list --repo Guria-Studio/orch --label auto-reported --author @me`.
- **Stop it**: set `report_findings.enabled: false`. `orch run` and `orch mcp`
  read the key when they start, so the next run's prompts drop the block and
  the next MCP server refuses the call.
- **Report by hand**: `gh issue create --repo Guria-Studio/orch` works the
  same without the tool.

The tool's reference, with the rest of the MCP surface, is in
[`MCP.md`](MCP.md#orch_report_finding).
