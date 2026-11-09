#!/usr/bin/env bash
#
# Decide which type:/priority: labels and which milestone the issue triage
# applies, given what the issue already has and what Gemini proposed.
#
# Pure function of its inputs: reads JSON, writes `key=value` lines to stdout.
# No network, no `gh`. triage-labels.test.sh drives it with fixture issues.
#
# Usage:
#   triage-labels.sh --labels labels.json --event opened|workflow_dispatch \
#                    --priority urgent|planned --type bug|improvement|feature \
#                    [--has-milestone true|false]
#
#   labels.json  ["type:bug", "priority:planned", ...]
#                (the shape of `jq '[.labels[].name]'` over `gh issue view --json labels`)
#
# Output (stdout, one per line):
#   keep=true|false       the issue's own labels win; nothing is added or removed
#   add=<csv>             labels to add (empty when keep=true)
#   remove=<csv>          labels to remove: only a priority:/type: label that
#                         differs from the proposal (empty when keep=true)
#   milestone=true|false  whether to set the proposed milestone
#   differs=<csv>         when keep=true, the proposed labels the issue does
#                         not have (for the comment); empty otherwise
#
# The rules (#588):
#   - On `opened`, an issue that already carries both a type:* and a
#     priority:* label was labelled by its author: keep=true. Its milestone,
#     if any, is kept too.
#   - Otherwise (no labels yet, only one of the two, or a workflow_dispatch
#     re-triage), the proposal replaces the previous one: add it, remove
#     the priority:/type: labels that are not part of it. A label that is
#     both present and proposed is never removed — `gh issue edit` applies
#     --remove-label after --add-label, so listing it would strip it.
#   - On `opened`, an existing milestone is never replaced; a re-triage
#     sets the proposed one.
#
# Exit status 0 whenever the decision was made; 2 when the inputs were
# unusable.

set -euo pipefail

die() { echo "triage-labels.sh: $*" >&2; exit 2; }

LABELS_JSON=""
EVENT=""
PRIORITY=""
TYPE=""
HAS_MILESTONE=false

while [ $# -gt 0 ]; do
  case "$1" in
    --labels)        LABELS_JSON="${2:-}"; shift 2 ;;
    --event)         EVENT="${2:-}"; shift 2 ;;
    --priority)      PRIORITY="${2:-}"; shift 2 ;;
    --type)          TYPE="${2:-}"; shift 2 ;;
    --has-milestone) HAS_MILESTONE="${2:-}"; shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ -n "$LABELS_JSON" ] && [ -f "$LABELS_JSON" ] || die "--labels: file not found: '$LABELS_JSON'"
case "$EVENT" in opened|workflow_dispatch) ;; *) die "--event: want opened or workflow_dispatch, got '$EVENT'" ;; esac
case "$PRIORITY" in urgent|planned) ;; *) die "--priority: got '$PRIORITY'" ;; esac
case "$TYPE" in bug|improvement|feature) ;; *) die "--type: got '$TYPE'" ;; esac
case "$HAS_MILESTONE" in true|false) ;; *) die "--has-milestone: want true or false, got '$HAS_MILESTONE'" ;; esac
jq -e 'type == "array" and all(.[]; type == "string")' "$LABELS_JSON" >/dev/null 2>&1 \
  || die "--labels: want a JSON array of strings"

want_p="priority:$PRIORITY"
want_t="type:$TYPE"

has_both=$(jq -r 'any(.[]; startswith("priority:")) and any(.[]; startswith("type:"))' "$LABELS_JSON")

if [ "$EVENT" = opened ] && [ "$has_both" = true ]; then
  differs=$(jq -r --arg p "$want_p" --arg t "$want_t" \
    '[$p, $t] - . | join(",")' "$LABELS_JSON")
  echo "keep=true"
  echo "add="
  echo "remove="
  echo "milestone=$([ "$HAS_MILESTONE" = true ] && echo false || echo true)"
  echo "differs=$differs"
  exit 0
fi

remove=$(jq -r --arg p "$want_p" --arg t "$want_t" \
  '[.[] | select((startswith("priority:") or startswith("type:")) and . != $p and . != $t)] | join(",")' \
  "$LABELS_JSON")

milestone=true
if [ "$EVENT" = opened ] && [ "$HAS_MILESTONE" = true ]; then
  milestone=false
fi

echo "keep=false"
echo "add=$want_p,$want_t"
echo "remove=$remove"
echo "milestone=$milestone"
echo "differs="
