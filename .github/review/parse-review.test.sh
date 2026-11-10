#!/usr/bin/env bash
#
# Fixture tests for parse-review.py. Run anywhere:
#
#   bash .github/review/parse-review.test.sh
#
# Covers the envelopes a model actually produces (bare JSON, fenced, prose
# around it) and the malformed shapes that must NOT be allowed to turn into a
# green check.

set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
PARSE="$HERE/parse-review.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0

# ok <name> <raw> <expected-verdict>
ok() {
  local name="$1" raw="$2" want="$3"
  printf '%s' "$raw" > "$TMP/raw.txt"
  local got
  if ! got="$(python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" 2>"$TMP/err")"; then
    echo "FAIL $name — expected a verdict, got an error: $(cat "$TMP/err")"
    FAIL=$((FAIL + 1)); return
  fi
  if [ "$got" != "$want" ]; then
    echo "FAIL $name — expected verdict '$want', got '$got'"
    FAIL=$((FAIL + 1)); return
  fi
  echo "ok   $name"; PASS=$((PASS + 1))
}

# rejects <name> <raw> [expected-exit]
#
# The exit code is part of the contract, not an afterthought: 1 means the
# reviewer answered off-schema and a retry would reproduce it, 2 means it said
# nothing and the workflow retries once. Conflating them either wastes an API
# call on every malformed answer or gives up on a transient empty one.
rejects() {
  local name="$1" raw="$2" want_rc="${3:-1}"
  printf '%s' "$raw" > "$TMP/raw.txt"
  python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" >/dev/null 2>"$TMP/err"
  local rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "FAIL $name — parsed something it should have rejected"
    FAIL=$((FAIL + 1)); return
  fi
  if [ "$rc" -ne "$want_rc" ]; then
    echo "FAIL $name — exit $rc, want $want_rc ($(head -c 80 "$TMP/err"))"
    FAIL=$((FAIL + 1)); return
  fi
  echo "ok   $name (exit $rc: $(head -c 80 "$TMP/err"))"; PASS=$((PASS + 1))
}

GOOD='{"verdict":"approve","blocking":[],"minor":[],"summary":"Small doc change, nothing to flag."}'

ok "bare JSON" "$GOOD" approve

ok "fenced with a language tag" "$(printf '```json\n%s\n```' "$GOOD")" approve

ok "fenced without a language tag" "$(printf '```\n%s\n```' "$GOOD")" approve

ok "prose before and after" \
  "$(printf 'Here is my review:\n\n%s\n\nHope that helps!' "$GOOD")" approve

ok "request_changes with a blocking finding" \
  '{"verdict":"request_changes","blocking":[{"file":"internal/state/db.go","line":42,"rule":"one writer to SQLite","why":"opens a second writable *sql.DB"}],"minor":[],"summary":"One blocking issue."}' \
  request_changes

ok "comment verdict with only minor findings" \
  '{"verdict":"comment","blocking":[],"minor":[{"file":"a.go","rule":"table tests","why":"four near-identical test funcs"}],"summary":"Nothing blocking."}' \
  comment

# The verdict and the findings disagreeing is a model slip; the findings win,
# because they are the part a human can check.
ok "blocking findings force request_changes" \
  '{"verdict":"approve","blocking":[{"file":"go.mod","rule":"compatibility","why":"drops a dependency still imported"}],"minor":[],"summary":"Looks fine to me."}' \
  request_changes

ok "missing minor[] defaults to empty" \
  '{"verdict":"approve","blocking":[],"summary":"Fine."}' approve

# ---- must be rejected (→ neutral check, never success) --------------------

rejects "empty output"               "" 2
rejects "prose with no JSON at all"  "I reviewed the PR and it looks good to me!"
rejects "truncated JSON"             '{"verdict":"approve","blocking":[],"summary":'
rejects "verdict outside the enum"   '{"verdict":"lgtm","blocking":[],"minor":[],"summary":"ok"}'
rejects "empty summary"              '{"verdict":"approve","blocking":[],"minor":[],"summary":"  "}'
rejects "finding missing rule"       '{"verdict":"request_changes","blocking":[{"file":"a.go","why":"bad"}],"minor":[],"summary":"x"}'
rejects "finding missing why"        '{"verdict":"request_changes","blocking":[{"file":"a.go","rule":"r"}],"minor":[],"summary":"x"}'
rejects "blocking is not a list"     '{"verdict":"approve","blocking":"none","minor":[],"summary":"x"}'
rejects "top level is a list"        '[{"verdict":"approve"}]'

# ---- the Gemini CLI envelope (run 34638647467) -----------------------------
#
# `run-gemini-cli` sets its `summary` output to `.response` from the CLI's
# JSON, and falls back to the WHOLE envelope when `.response` is empty. The
# parser used to swallow that envelope as valid JSON, find no `verdict`, and
# report "`verdict` must be one of (...), got None" — which reads like the
# model answered badly when it had not answered at all, and sent the next
# reader looking for a prompt problem.

ENVELOPE_EMPTY='{"session_id":"0e2a9d39","response":"","stats":{"models":{"gemini-3.1-flash-lite":{"api":{"totalRequests":1}}}}}'

rejects "the CLI envelope with an empty response" "$ENVELOPE_EMPTY" 2

# A tool-call attempt rendered as text (run 34663899673, #176): every tool is
# disabled, the CLI hands the attempt back as the answer, and nothing was
# reviewed. Retryable, like an empty response — not a schema violation.
TOOL_CALL='‹call:glob{pattern:"web/**/*test*"}›'
rejects "a tool-call attempt instead of a verdict" "$TOOL_CALL" 2

# A finding with no `file` (#207: a "Conventional commit" finding about the PR
# title). Valid JSON, a real verdict, and it must parse; the finding renders
# against "(PR)" instead of failing the whole review.
NO_FILE='{"verdict": "comment", "blocking": [], "minor": [{"rule": "Conventional commit", "why": "The PR title starts with ci(review):"}, {"file": ".github/workflows/review.yml", "rule": "New behaviour has a test", "why": "cannot verify from the diff"}], "summary": "Fine, two minors."}'
printf '%s' "$NO_FILE" > "$TMP/raw.txt"
if python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" --comment "$TMP/c.md" >/dev/null 2>"$TMP/err" && grep -qF -- '`(PR)` — **Conventional commit**' "$TMP/c.md"; then
  echo "ok   a finding without a file parses and renders against (PR)"; PASS=$((PASS + 1))
else
  echo "FAIL a finding without a file should parse: $(cat "$TMP/err")"; FAIL=$((FAIL + 1))
fi
ENVELOPE_TOOL_CALL='{"session_id":"0e2a9d39","response":"‹call:glob{pattern:\"web/**/*test*\"}›","stats":{}}'
rejects "the CLI envelope wrapping a tool-call attempt" "$ENVELOPE_TOOL_CALL" 2

# And the error has to name what actually happened, plus the model, because
# the next question is always "which model did that?".
printf '%s' "$ENVELOPE_EMPTY" > "$TMP/raw.txt"
python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" >/dev/null 2>"$TMP/err" || true
for needle in "empty response" "gemini-3.1-flash-lite"; do
  if grep -qF -- "$needle" "$TMP/err"; then
    echo "ok   the empty-response error mentions '$needle'"; PASS=$((PASS + 1))
  else
    echo "FAIL the empty-response error is missing '$needle': $(cat "$TMP/err")"
    FAIL=$((FAIL + 1))
  fi
done

# A NON-empty envelope is unwrapped and the review inside is used.
ok "the CLI envelope wrapping a real verdict" \
  '{"session_id":"x","response":"{\"verdict\":\"approve\",\"blocking\":[],\"minor\":[],\"summary\":\"Nothing to flag.\"}","stats":{}}' \
  approve

# ...including when the model fenced its answer inside the envelope.
ok "envelope wrapping a fenced verdict" \
  '{"session_id":"x","response":"```json\n{\"verdict\":\"comment\",\"blocking\":[],\"minor\":[],\"summary\":\"Minor only.\"}\n```","stats":{}}' \
  comment

# A bare object that merely HAS a `response` key but is a real verdict must
# not be mistaken for an envelope.
ok "a verdict that happens to carry a response field" \
  '{"verdict":"approve","response":"ignored","blocking":[],"minor":[],"summary":"Fine."}' \
  approve

# An off-schema answer is exit 1, not 2: retrying reproduces the same shape.
rejects "off-schema stays non-retryable" \
  '{"verdict":"lgtm","blocking":[],"minor":[],"summary":"ok"}' 1

# ---- the rendered comment --------------------------------------------------

printf '%s' '{"verdict":"request_changes","blocking":[{"file":"internal/engine/loop.go","line":88,"rule":"os/exec timeout","why":"no context deadline on the provider call"}],"minor":[{"file":"internal/cli/run.go","rule":"table tests","why":"three near-identical cases"}],"summary":"One blocking issue in the dispatch loop."}' > "$TMP/raw.txt"
python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" --comment "$TMP/c.md" \
  --eligible false --eligible-reason "touches protected paths: internal/engine/loop.go" \
  --run-url "https://example.invalid/run/1" >/dev/null

check_comment() {
  local what="$1" needle="$2"
  if grep -qF -- "$needle" "$TMP/c.md"; then
    echo "ok   comment $what"; PASS=$((PASS + 1))
  else
    echo "FAIL comment $what — missing '$needle'"; FAIL=$((FAIL + 1))
  fi
}

check_comment "carries the hidden marker"  "<!-- orch:gemini-review -->"
check_comment "shows the verdict"          "Changes requested"
check_comment "shows file:line"            "internal/engine/loop.go:88"
check_comment "has a Blocking section"     "**Blocking**"
check_comment "has a Minor section"        "**Minor**"
check_comment "reports ineligibility"      "Not auto-merge eligible"
check_comment "says nothing auto-merges"   "nothing merges automatically yet"
check_comment "links the run"              "https://example.invalid/run/1"

# ---- calibration (#521) ----------------------------------------------------
#
# The merge-blocking rules come from the checklist's marker line. A fixture
# checklist keeps these tests independent of the real one's list; the last
# test reads the real one.

printf '# c\n<!-- merge-blocking-rules: 0 2 19 22 -->\n' > "$TMP/checklist.md"

# calibrated <name> <raw> <expected-verdict> <jq-check>
calibrated() {
  local name="$1" raw="$2" want="$3" check="$4"
  printf '%s' "$raw" > "$TMP/raw.txt"
  local got
  if ! got="$(python3 "$PARSE" --raw "$TMP/raw.txt" --out "$TMP/out.json" \
      --comment "$TMP/c.md" --checklist "$TMP/checklist.md" 2>"$TMP/err")"; then
    echo "FAIL $name — parse error: $(cat "$TMP/err")"; FAIL=$((FAIL + 1)); return
  fi
  if [ "$got" != "$want" ]; then
    echo "FAIL $name — verdict '$got', want '$want'"; FAIL=$((FAIL + 1)); return
  fi
  if ! python3 -c "import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if ($check) else 1)" "$TMP/out.json"; then
    echo "FAIL $name — review.json fails: $check"; cat "$TMP/out.json"; FAIL=$((FAIL + 1)); return
  fi
  echo "ok   $name"; PASS=$((PASS + 1))
}

calibrated "a blocking finding under a non-blocking rule becomes minor" \
  '{"verdict":"request_changes","blocking":[{"file":"a_test.go","rule":"20. Table tests","why":"five near-identical funcs"}],"minor":[],"summary":"x"}' \
  comment 'not r["blocking"] and r["minor"][0]["rule"].startswith("20") and r["minor"][0]["demoted"]'

calibrated "a blocking finding under a blocking rule stays" \
  '{"verdict":"request_changes","blocking":[{"file":"a.go","rule":"19. Errors are not dropped","why":"RowsAffected discarded on the write path"},{"file":"b.go","rule":"Rule 20 table tests","why":"x"}],"minor":[],"summary":"x"}' \
  request_changes 'len(r["blocking"]) == 1 and r["blocking"][0]["file"] == "a.go" and len(r["minor"]) == 1'

calibrated "a blocking finding with no rule number is left alone" \
  '{"verdict":"request_changes","blocking":[{"file":"a.go","rule":"One writer to SQLite","why":"second writable handle"}],"minor":[],"summary":"x"}' \
  request_changes 'len(r["blocking"]) == 1'

calibrated "cannot-verify minors are set aside" \
  '{"verdict":"comment","blocking":[],"minor":[{"file":"a.go","rule":"8. New behaviour has a test","why":"The test cannot be verified from the diff alone."},{"file":"b.go","rule":"24. Seen failing","why":"b_test.go is truncated from the diff"},{"file":"c.go","rule":"20. Table tests","why":"four near-identical funcs"}],"summary":"x"}' \
  comment 'len(r["minor"]) == 1 and r["minor"][0]["file"] == "c.go" and len(r["unverifiable"]) == 2'

if grep -qF "Not checkable from the diff (2): 8. New behaviour has a test; 24. Seen failing" "$TMP/c.md" \
   && ! grep -qF "b_test.go is truncated" "$TMP/c.md"; then
  echo "ok   the unverifiable minors render as one line"; PASS=$((PASS + 1))
else
  echo "FAIL the unverifiable minors should collapse into one line:"; cat "$TMP/c.md"; FAIL=$((FAIL + 1))
fi

calibrated "a concrete minor that mentions the diff is kept" \
  '{"verdict":"comment","blocking":[],"minor":[{"file":"a.go","rule":"20. Table tests","why":"the diff adds three near-identical tests"}],"summary":"x"}' \
  comment 'len(r["minor"]) == 1 and r["unverifiable"] == []'

calibrated "request_changes with nothing left blocking reads as comment" \
  '{"verdict":"request_changes","blocking":[{"file":"a.md","rule":"10. Conventional commit","why":"title"}],"minor":[],"summary":"x"}' \
  comment 'r["verdict"] == "comment"'

# The real checklist carries the marker, and its list is the one the docs
# document (docs/CI-REVIEW.md, "When the review blocks a merge").
REAL=$(python3 - "$HERE/CHECKLIST.md" "$PARSE" <<'PY'
import importlib.util, sys
spec = importlib.util.spec_from_file_location("pr", sys.argv[2])
mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
print(" ".join(str(n) for n in sorted(mod.merge_blocking_rules(open(sys.argv[1]).read()) or [])))
PY
)
if [ "$REAL" = "0 1 2 3 4 5 6 7 8 16 17 18 19 22 31" ]; then
  echo "ok   the real checklist lists the documented merge-blocking rules"; PASS=$((PASS + 1))
else
  echo "FAIL the real checklist's merge-blocking rules are '$REAL'"; FAIL=$((FAIL + 1))
fi
if grep -qF "0, 1, 2, 3, 4, 5, 6, 7, 8, 16, 17, 18, 19, 22 and 31" "$HERE/../../docs/CI-REVIEW.md"; then
  echo "ok   docs/CI-REVIEW.md names the same rules"; PASS=$((PASS + 1))
else
  echo "FAIL docs/CI-REVIEW.md does not list the merge-blocking rules as the checklist does"; FAIL=$((FAIL + 1))
fi

echo
echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
