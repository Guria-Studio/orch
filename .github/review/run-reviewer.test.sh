#!/usr/bin/env bash
#
# Tests for run-reviewer.sh against a fake `gemini` (GEMINI_BIN). No network,
# no API key — run it anywhere:
#
#   bash .github/review/run-reviewer.test.sh
#
# What it holds in place (#521): the prompt reaches the CLI on stdin, whole,
# past the 128 KiB that capped it as an argument; the CLI runs outside the
# checkout with every tool off; and no GitHub token reaches it.

set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
RUN="$HERE/run-reviewer.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0
pass() { echo "ok   $1"; PASS=$((PASS + 1)); }
fail() { echo "--- FAIL: $1"; FAIL=$((FAIL + 1)); }
# check <name> <command...>: pass when the command succeeds.
check() { local name="$1"; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }

# The fake CLI records what it was given and answers like the real one.
cat > "$TMP/gemini" <<'SH'
#!/usr/bin/env bash
rec="$FAKE_RECORD"
cat > "$rec/stdin"
printf '%s\n' "$@" > "$rec/argv"
pwd > "$rec/cwd"
cp .gemini/settings.json "$rec/settings.json" 2>/dev/null || true
env > "$rec/env"
echo "fake stderr line" >&2
printf '{"session_id":"x","response":"{\\"verdict\\":\\"approve\\",\\"blocking\\":[],\\"minor\\":[],\\"summary\\":\\"ok\\"}","stats":{}}'
exit "${FAKE_EXIT:-0}"
SH
chmod +x "$TMP/gemini"
mkdir -p "$TMP/rec"

# A prompt well past MAX_ARG_STRLEN (128 KiB), with a sentinel at the end.
python3 -c 'import sys; sys.stdout.write("x" * 300000 + "\nEND-OF-PROMPT\n")' > "$TMP/prompt.txt"

(
  cd "$TMP" || exit 1
  GEMINI_BIN="$TMP/gemini" FAKE_RECORD="$TMP/rec" \
  GH_TOKEN=gh-token-must-not-leak GITHUB_TOKEN=github-token-must-not-leak \
  GEMINI_MODEL=gemini-test-model \
    bash "$RUN" --prompt prompt.txt --out raw.txt --err err.txt
)
rc=$?

check "exit 0 when the CLI succeeds (got $rc)" [ "$rc" -eq 0 ]
if cmp -s "$TMP/prompt.txt" "$TMP/rec/stdin"; then
  pass "the whole 300 KB prompt arrives on stdin"
else
  fail "stdin differs from the prompt ($(wc -c < "$TMP/rec/stdin") bytes)"
fi
if grep -q "END-OF-PROMPT" "$TMP/rec/argv"; then fail "the prompt leaked into argv"; else pass "the prompt is not in argv"; fi
check "asks for JSON output" grep -qx -- "--output-format" "$TMP/rec/argv"
check "the output format is json" grep -qx json "$TMP/rec/argv"
check "GEMINI_MODEL is passed as --model" grep -qx -- "gemini-test-model" "$TMP/rec/argv"
case "$(cat "$TMP/rec/cwd")" in
  "$TMP"|"$HERE"*|"$(cd "$HERE/../.." && pwd)") fail "the CLI ran in the checkout or the caller's dir" ;;
  *) pass "the CLI runs in a directory of its own" ;;
esac
if python3 -c 'import json,sys; s=json.load(open(sys.argv[1])); sys.exit(0 if s["tools"]["core"] == [] else 1)' "$TMP/rec/settings.json"; then
  pass "every built-in tool is off"
else
  fail "settings.json does not turn the tools off"
fi
if grep -q "token-must-not-leak" "$TMP/rec/env"; then fail "a GitHub token reached the reviewer"; else pass "no GitHub token reaches the reviewer"; fi
check "the empty workspace is trusted" grep -q '^GEMINI_CLI_TRUST_WORKSPACE=true$' "$TMP/rec/env"
check "stdout lands in --out" grep -q '"response"' "$TMP/raw.txt"
check "stderr lands in --err" grep -q "fake stderr line" "$TMP/err.txt"

# The parser reads what the runner wrote: envelope in, verdict out.
if [ "$(python3 "$HERE/parse-review.py" --raw "$TMP/raw.txt" --out "$TMP/review.json" 2>/dev/null)" = approve ]; then
  pass "parse-review.py unwraps the runner's output"
else
  fail "parse-review.py could not read the runner's output"
fi

# The CLI's failure is the runner's failure.
(
  cd "$TMP" || exit 1
  GEMINI_BIN="$TMP/gemini" FAKE_RECORD="$TMP/rec" FAKE_EXIT=3 \
    bash "$RUN" --prompt prompt.txt --out raw2.txt --err err2.txt
)
rc=$?
check "the CLI's exit status is passed on (got $rc, want 3)" [ "$rc" -eq 3 ]

# Bad usage is refused before anything runs.
bash "$RUN" --prompt "$TMP/missing.txt" --out "$TMP/o" --err "$TMP/e" 2>/dev/null
rc=$?
check "a missing prompt file is refused" [ "$rc" -ne 0 ]

echo
echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
