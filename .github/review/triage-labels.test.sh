#!/usr/bin/env bash
#
# Fixture tests for triage-labels.sh. No network, no GitHub — run it anywhere:
#
#   bash .github/review/triage-labels.test.sh
#
# Wired into triage.yml's self-test step, so a change that breaks these
# fixtures fails the run before it can strip a real issue's labels (#588).

set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$HERE/triage-labels.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PASS=0
FAIL=0

# run <name> <labels-json> <event> <priority> <type> <has-milestone> <expected output>
# The whole output is compared, line for line: every key is part of the contract.
run() {
  local name="$1" labels="$2" event="$3" priority="$4" type="$5" has_ms="$6" want="$7"
  printf '%s' "$labels" > "$TMP/labels.json"

  local got
  if ! got="$(bash "$SCRIPT" --labels "$TMP/labels.json" --event "$event" \
      --priority "$priority" --type "$type" --has-milestone "$has_ms" 2>&1)"; then
    echo "--- FAIL: $name — triage-labels.sh exited non-zero:"
    printf '%s\n' "$got" | sed 's/^/      /'
    FAIL=$((FAIL + 1))
    return
  fi
  if [ "$got" != "$want" ]; then
    echo "--- FAIL: $name"
    echo "    want:"; printf '%s\n' "$want" | sed 's/^/      /'
    echo "    got:";  printf '%s\n' "$got"  | sed 's/^/      /'
    FAIL=$((FAIL + 1))
    return
  fi
  echo "ok   $name"
  PASS=$((PASS + 1))
}

# ---- opened with both labels: the author's labels win (#588) ---------------

run "opened with both labels equal to the proposal keeps them" \
  '["type:bug","priority:planned"]' opened planned bug false \
'keep=true
add=
remove=
milestone=true
differs='

run "opened with both labels that differ keeps them and reports the proposal" \
  '["type:improvement","priority:urgent","area:ci"]' opened planned bug false \
'keep=true
add=
remove=
milestone=true
differs=priority:planned,type:bug'

run "opened with both labels and a milestone keeps the milestone" \
  '["type:bug","priority:planned"]' opened planned bug true \
'keep=true
add=
remove=
milestone=false
differs='

# ---- opened without both labels: the proposal is applied -------------------

run "opened with no labels gets the proposal" \
  '[]' opened urgent feature false \
'keep=false
add=priority:urgent,type:feature
remove=
milestone=true
differs='

run "opened with only a matching type keeps it (not removed)" \
  '["type:bug"]' opened planned bug false \
'keep=false
add=priority:planned,type:bug
remove=
milestone=true
differs='

run "opened with only a different priority replaces it" \
  '["priority:urgent","good first issue"]' opened planned bug false \
'keep=false
add=priority:planned,type:bug
remove=priority:urgent
milestone=true
differs='

run "opened without labels but with a milestone keeps the milestone" \
  '[]' opened planned bug true \
'keep=false
add=priority:planned,type:bug
remove=
milestone=false
differs='

# ---- workflow_dispatch: a re-triage replaces the previous proposal ---------

run "re-triage with the same labels removes nothing" \
  '["type:bug","priority:planned"]' workflow_dispatch planned bug true \
'keep=false
add=priority:planned,type:bug
remove=
milestone=true
differs='

run "re-triage with a new proposal removes only the stale labels" \
  '["type:bug","priority:planned","claude:go"]' workflow_dispatch urgent bug true \
'keep=false
add=priority:urgent,type:bug
remove=priority:planned
milestone=true
differs='

run "re-triage removes every stale label, including duplicates" \
  '["type:bug","type:feature","priority:urgent","priority:planned"]' workflow_dispatch planned improvement false \
'keep=false
add=priority:planned,type:improvement
remove=type:bug,type:feature,priority:urgent
milestone=true
differs='

# ---- unusable inputs exit 2 ------------------------------------------------

printf '%s' '{"not":"an array"}' > "$TMP/bad.json"
if bash "$SCRIPT" --labels "$TMP/bad.json" --event opened --priority planned --type bug >/dev/null 2>&1; then
  echo "--- FAIL: a labels file that is not an array is rejected"; FAIL=$((FAIL + 1))
else
  echo "ok   a labels file that is not an array is rejected"; PASS=$((PASS + 1))
fi
printf '%s' '[]' > "$TMP/empty.json"
if bash "$SCRIPT" --labels "$TMP/empty.json" --event issues --priority planned --type bug >/dev/null 2>&1; then
  echo "--- FAIL: an unknown event is rejected"; FAIL=$((FAIL + 1))
else
  echo "ok   an unknown event is rejected"; PASS=$((PASS + 1))
fi

echo
echo "$PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
