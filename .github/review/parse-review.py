#!/usr/bin/env python3
"""Turn the reviewer's raw stdout into a validated verdict, or fail loudly.

The model is asked for JSON and nothing else. It will sometimes wrap it in a
```json fence or add a sentence first, so this is deliberately forgiving about
the envelope and strict about the contents: a verdict outside the enum, or a
finding missing `rule`, is a parse failure — and a parse failure means a
`neutral` check, never a green one.

Usage:
    parse-review.py --raw raw.txt --out review.json [--comment comment.md]
                    [--eligible true|false] [--eligible-reason "..."]
                    [--run-url https://...] [--checklist CHECKLIST.md]

Two calibrations from the #521 measurement (docs/analysis/
code-review-measurement-2026-11.md) happen here, after validation:

- A minor finding that only says the rule "cannot be verified from the diff"
  is not a finding about the code (42 of 97 measured, 3 of them useful). They
  are collapsed into one "not checkable" line and kept in review.json under
  `unverifiable`.
- A blocking finding whose rule is not merge-blocking is moved to `minor`.
  The merge-blocking rule numbers are read from the checklist's
  `<!-- merge-blocking-rules: ... -->` line, the one source of truth that the
  reviewer reads too (docs/CI-REVIEW.md, "When the review blocks a merge").
  A finding whose rule carries no number stays where the reviewer put it.

Exit status:
    0  valid verdict written to --out
    1  unreadable: the reviewer answered, but not to the schema. Retrying
       would just produce the same shape, so the caller marks the check
       neutral.
    2  empty: the reviewer ran and said nothing. Non-deterministic and worth
       ONE retry before giving up.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys

VERDICTS = ("approve", "request_changes", "comment")

# Lets a re-run find and update its own comment instead of stacking a new one.
MARKER = "<!-- orch:gemini-review -->"


# A tool-call attempt rendered as text by the CLI: `‹call:name{...}›`, or the
# ASCII-bracket form some builds print.
TOOL_CALL_RE = re.compile(r"^\s*[‹<]call:[A-Za-z_]+")


# Where a finding points when it is about the PR rather than a file.
PR_LEVEL = "(PR)"


# "cannot be verified from the diff", "not visible in the diff", "the test is
# truncated", "outside the diff" — the shapes the measured NV findings took.
UNVERIFIABLE_RE = re.compile(
    r"\b(?:cannot|can't|can not|could not|couldn't|unable to|impossible to)\s+"
    r"(?:be\s+)?(?:verif|check|confirm|see|determin|assess|tell)"
    r"|\bnot\s+(?:visible|verifiable|checkable|shown|included)\b"
    r"|\btruncated\b"
    r"|\b(?:outside|beyond|absent from|missing from)\s+(?:of\s+)?the\s+(?:provided\s+)?diff\b"
    r"|\bfrom the diff alone\b",
    re.IGNORECASE,
)

# `19. Errors are wrapped`, `Rule 19`, `#19 — ...`, `19 (errors)`.
RULE_NUMBER_RE = re.compile(r"^\s*(?:rule\s*)?#?(\d{1,2})\b", re.IGNORECASE)

BLOCKING_MARKER_RE = re.compile(r"<!--\s*merge-blocking-rules:\s*([\d\s,]+?)\s*-->")


def rule_number(rule: str) -> int | None:
    m = RULE_NUMBER_RE.match(rule)
    return int(m.group(1)) if m else None


def merge_blocking_rules(checklist_text: str) -> set[int] | None:
    """The rule numbers that may block a merge, or None when the checklist
    does not say (then nothing is demoted)."""
    m = BLOCKING_MARKER_RE.search(checklist_text)
    if not m:
        return None
    return {int(n) for n in re.findall(r"\d+", m.group(1))}


def calibrate(review: dict, blocking_rules: set[int] | None) -> dict:
    """Demote non-blocking rules and set aside the unverifiable minors."""
    blocking, minor = [], list(review["minor"])
    for f in review["blocking"]:
        n = rule_number(f["rule"])
        if blocking_rules is not None and n is not None and n not in blocking_rules:
            minor.append(dict(f, demoted=True))
        else:
            blocking.append(f)
    kept, unverifiable = [], []
    for f in minor:
        (unverifiable if UNVERIFIABLE_RE.search(f["why"]) else kept).append(f)

    verdict = review["verdict"]
    if blocking:
        verdict = "request_changes"
    elif verdict == "request_changes":
        # Everything blocking was demoted: the minors remain.
        verdict = "comment"
    return dict(review, verdict=verdict, blocking=blocking, minor=kept, unverifiable=unverifiable)


class EmptyReview(ValueError):
    """The reviewer ran but produced no text. Distinct from a malformed
    verdict, because the fix is different: an empty response is worth
    retrying, a schema violation is not."""


def unwrap_cli_envelope(text: str) -> str:
    """Return the model's own text when `text` is the Gemini CLI's envelope.

    `run-gemini-cli` runs `gemini --output-format json`, whose stdout is
    `{"session_id": ..., "response": "...", "stats": {...}}`, and sets its
    `summary` output to `.response`. When `.response` is EMPTY it falls back
    to emitting that whole envelope instead.

    Observed on run 34638647467 attempt 1: the model returned
    `"response": ""`, the action emitted the envelope, and this parser
    dutifully parsed it as JSON, found no `verdict`, and reported
    "`verdict` must be one of (...), got None" — which reads like the model
    answered badly when in fact it did not answer at all. That message sent
    the next reader looking for a prompt problem.

    So: recognise the envelope, and say what actually happened.
    """
    try:
        outer = json.loads(text)
    except (json.JSONDecodeError, ValueError):
        return text
    if not isinstance(outer, dict):
        return text
    if "verdict" in outer:
        return text  # already the review; not an envelope
    if "response" not in outer:
        return text
    inner = outer.get("response")
    if isinstance(inner, str) and inner.strip():
        return inner
    raise EmptyReview(
        "the reviewer ran but returned an empty response "
        f"(model: {_envelope_model(outer)}). Nothing was reviewed."
    )


def _envelope_model(outer: dict) -> str:
    """Best-effort model name out of the CLI's stats block, for the error."""
    try:
        models = outer["stats"]["models"]
        if isinstance(models, dict) and models:
            return ", ".join(sorted(models))
    except (KeyError, TypeError):
        pass
    return "unknown"


def extract_json(raw: str) -> dict:
    """Pull the JSON object out of whatever the model actually printed."""
    text = raw.strip()
    if not text:
        raise EmptyReview("the reviewer produced no output at all")

    text = unwrap_cli_envelope(text)

    # The model tried to call a tool instead of answering. Every tool is
    # disabled in the workflow, so the CLI hands the attempt back as text
    # (`‹call:glob{pattern:"web/**/*test*"}›`, run 34663899673 on #176: the
    # reviewer went looking for tests to satisfy rule 24). Nothing was
    # reviewed, and a second run usually answers, so this is the same class
    # as an empty response: retry, and say what happened.
    if TOOL_CALL_RE.match(text):
        raise EmptyReview(
            "the reviewer tried to call a tool instead of answering "
            f"(first 120 chars: {text[:120]!r}). Nothing was reviewed."
        )

    # ```json ... ``` or ``` ... ```
    fenced = re.search(r"```(?:json)?\s*\n(.*?)\n\s*```", text, re.DOTALL)
    if fenced:
        text = fenced.group(1).strip()

    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass

    # Prose before or after the object: take the outermost braces.
    start, end = text.find("{"), text.rfind("}")
    if start == -1 or end <= start:
        raise ValueError(
            "no JSON object found in the reviewer output "
            f"(first 200 chars: {text[:200]!r})"
        )
    return json.loads(text[start : end + 1])


def normalise_findings(value, bucket: str) -> list[dict]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError(f"`{bucket}` must be a list, got {type(value).__name__}")
    out = []
    for i, item in enumerate(value):
        if not isinstance(item, dict):
            raise ValueError(f"`{bucket}[{i}]` must be an object")
        for required in ("rule", "why"):
            if not str(item.get(required, "")).strip():
                raise ValueError(f"`{bucket}[{i}]` is missing `{required}`")
        # `file` is optional: a finding about the PR itself (its title, its
        # body, a missing test the reviewer cannot place) has no file, and the
        # reviewer omits the key rather than inventing one. #207's verdict was
        # valid JSON that failed here on a "Conventional commit" finding about
        # the title — the third "unreadable verdict" of the day, and the first
        # one whose raw text survived to be read.
        entry = {
            "file": str(item.get("file") or "").strip() or PR_LEVEL,
            "rule": str(item["rule"]).strip(),
            "why": str(item["why"]).strip(),
        }
        line = item.get("line")
        if line not in (None, "", 0):
            try:
                entry["line"] = int(line)
            except (TypeError, ValueError):
                pass  # a non-numeric line is not worth failing the review over
        out.append(entry)
    return out


def validate(payload: dict) -> dict:
    if not isinstance(payload, dict):
        raise ValueError("the reviewer returned a JSON value that is not an object")

    verdict = str(payload.get("verdict", "")).strip().lower()
    if verdict not in VERDICTS:
        raise ValueError(
            f"`verdict` must be one of {VERDICTS}, got {payload.get('verdict')!r}"
        )

    blocking = normalise_findings(payload.get("blocking"), "blocking")
    minor = normalise_findings(payload.get("minor"), "minor")
    summary = str(payload.get("summary", "")).strip()
    if not summary:
        raise ValueError("`summary` is empty")

    # A verdict that contradicts its own findings is a model slip, not a
    # judgement call — trust the findings, which are the checkable part.
    if blocking and verdict != "request_changes":
        verdict = "request_changes"

    return {
        "verdict": verdict,
        "blocking": blocking,
        "minor": minor,
        "summary": summary,
    }


def render_findings(title: str, findings: list[dict]) -> list[str]:
    if not findings:
        return []
    lines = [f"**{title}**", ""]
    for f in findings:
        where = f["file"] + (f":{f['line']}" if "line" in f else "")
        note = " _(not merge-blocking: moved from Blocking)_" if f.get("demoted") else ""
        lines.append(f"- `{where}` — **{f['rule']}**: {f['why']}{note}")
    lines.append("")
    return lines


def render_comment(
    review: dict,
    *,
    eligible: str | None,
    eligible_reason: str | None,
    run_url: str | None,
) -> str:
    headline = {
        "approve": "✅ **Approve** — nothing blocking.",
        "request_changes": "🛑 **Changes requested.**",
        "comment": "💬 **Comments only** — nothing blocking.",
    }[review["verdict"]]

    lines = [
        MARKER,
        "## Gemini review",
        "",
        headline,
        "",
        review["summary"],
        "",
    ]
    lines += render_findings("Blocking", review["blocking"])
    lines += render_findings("Minor", review["minor"])

    unverifiable = review.get("unverifiable") or []
    if unverifiable:
        rules = []
        for f in unverifiable:
            if f["rule"] not in rules:
                rules.append(f["rule"])
        lines += [
            f"_Not checkable from the diff ({len(unverifiable)}): "
            + "; ".join(rules)
            + ". Not findings — answer them in the PR body if they matter._",
            "",
        ]

    if eligible is not None:
        if eligible == "true":
            lines += [f"🔓 **Auto-merge eligible** — {eligible_reason}."]
        else:
            lines += [f"🔒 **Not auto-merge eligible** — {eligible_reason}."]
        lines += [
            "",
            "_Eligibility is informational: nothing merges automatically yet "
            "(see `docs/CI-REVIEW.md`)._",
            "",
        ]

    lines += [
        "---",
        "",
        "_Advisory. A human still owns the merge._ "
        + (f"[Run log]({run_url})" if run_url else ""),
    ]
    return "\n".join(lines).rstrip() + "\n"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--raw", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--comment")
    ap.add_argument("--eligible", choices=["true", "false"])
    ap.add_argument("--eligible-reason", default="")
    ap.add_argument("--run-url", default="")
    ap.add_argument(
        "--checklist",
        default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "CHECKLIST.md"),
    )
    args = ap.parse_args()

    blocking_rules = None
    try:
        with open(args.checklist, encoding="utf-8") as fh:
            blocking_rules = merge_blocking_rules(fh.read())
    except OSError as exc:
        print(f"parse-review: no checklist ({exc}); nothing is demoted", file=sys.stderr)

    try:
        with open(args.raw, encoding="utf-8") as fh:
            raw = fh.read()
    except OSError as exc:
        print(f"parse-review: cannot read {args.raw}: {exc}", file=sys.stderr)
        return 1

    try:
        review = calibrate(validate(extract_json(raw)), blocking_rules)
    except EmptyReview as exc:
        # Exit 2, not 1: the caller can retry this one.
        print(f"parse-review: {exc}", file=sys.stderr)
        return 2
    except (ValueError, json.JSONDecodeError) as exc:
        print(f"parse-review: {exc}", file=sys.stderr)
        return 1

    with open(args.out, "w", encoding="utf-8") as fh:
        json.dump(review, fh, indent=2)
        fh.write("\n")

    if args.comment:
        body = render_comment(
            review,
            eligible=args.eligible,
            eligible_reason=args.eligible_reason or "no reason given",
            run_url=args.run_url or None,
        )
        with open(args.comment, "w", encoding="utf-8") as fh:
            fh.write(body)

    print(review["verdict"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
