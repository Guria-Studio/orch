#!/usr/bin/env bash
#
# Run the Gemini CLI on a prompt file and keep what it printed.
#
#   run-reviewer.sh --prompt prompt.txt --out raw.txt --err err.txt
#
# The prompt goes in on STDIN. The run-gemini-cli action passed it as one
# environment string and then as one argv string, and Linux caps either at
# 128 KiB (MAX_ARG_STRLEN), which is why the diff used to be cut at ~120 KB
# (#521: 7 of 36 measured PRs were reviewed with a truncated diff). The CLI
# reads up to 8 MB from stdin and prepends it to `--prompt`.
#
# The CLI runs in an empty temporary directory, not the checkout: nothing in
# the PR (a GEMINI.md, a .gemini/settings.json turning tools back on) can
# reach it. Every built-in tool is off, and the GitHub tokens are removed from
# its environment: the reviewer gets the API key and nothing else.
#
# Environment: GEMINI_API_KEY (required by the CLI, never printed),
# GEMINI_MODEL (optional; empty = the CLI's default), GEMINI_BIN (tests).
#
# Exit status: the CLI's. stdout (the CLI's JSON envelope) is in --out, which
# parse-review.py unwraps; stderr is in --err.

set -uo pipefail

PROMPT="" OUT="" ERR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --prompt) PROMPT="$2"; shift 2 ;;
    --out)    OUT="$2"; shift 2 ;;
    --err)    ERR="$2"; shift 2 ;;
    *) echo "run-reviewer: unknown argument $1" >&2; exit 64 ;;
  esac
done
if [ -z "$PROMPT" ] || [ -z "$OUT" ] || [ -z "$ERR" ]; then
  echo "usage: run-reviewer.sh --prompt FILE --out FILE --err FILE" >&2
  exit 64
fi
if [ ! -r "$PROMPT" ]; then
  echo "run-reviewer: cannot read $PROMPT" >&2
  exit 66
fi

# Absolute paths: the CLI runs from another directory.
abs() { case "$1" in /*) printf '%s' "$1" ;; *) printf '%s/%s' "$PWD" "$1" ;; esac; }
PROMPT=$(abs "$PROMPT"); OUT=$(abs "$OUT"); ERR=$(abs "$ERR")

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/.gemini"
cat > "$WORK/.gemini/settings.json" <<'JSON'
{
  "model": { "maxSessionTurns": 3 },
  "tools": { "core": [] },
  "mcpServers": {}
}
JSON

ARGS=(--output-format json --prompt "Answer with the single JSON object the instructions above ask for.")
if [ -n "${GEMINI_MODEL:-}" ]; then
  ARGS+=(--model "$GEMINI_MODEL")
fi

# The CLI refuses an untrusted directory; this one is empty and was made here.
(
  cd "$WORK" || exit 70
  env -u GH_TOKEN -u GITHUB_TOKEN -u ACTIONS_RUNTIME_TOKEN -u ACTIONS_ID_TOKEN_REQUEST_TOKEN \
    GEMINI_CLI_TRUST_WORKSPACE=true \
    "${GEMINI_BIN:-gemini}" "${ARGS[@]}" < "$PROMPT" > "$OUT" 2> "$ERR"
)
