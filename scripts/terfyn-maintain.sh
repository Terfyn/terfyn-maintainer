#!/usr/bin/env bash
#
# terfyn-maintain — a thin convenience wrapper around
#   terfyn run workflow/FixPullRequest
#
# It only shapes input and wires the workspace sandbox; it does NO git or network
# work of its own. Anything that touches the repo or the network is a capability-
# bounded tool inside the workflow (visible in `terfyn plan`), never shell glue here.
#
# Usage:
#   terfyn-maintain <owner/name> <issue-number> <task> <workspace-dir>
#   terfyn-maintain --resume <run-id> [approve|reject]
#
# Environment (optional overrides):
#   TERFYN_WORKSPACE_TEST_COMMAND   command run_tests executes   (default: go test ./...)
#   TERFYN_GIT_REMOTE               remote push_branch targets   (default: origin)
#   TERFYN_PROJECT                  project root                 (default: script's repo)
set -euo pipefail

workflow="workflow/FixPullRequest"
project="${TERFYN_PROJECT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

die() { echo "terfyn-maintain: $*" >&2; exit 1; }
command -v terfyn >/dev/null || die "terfyn is not installed (go install github.com/Terfyn/terfyn/cmd/terfyn@v0.2.0)"

# --- resume path: carry a human decision back into a suspended run ------------
if [[ "${1:-}" == "--resume" ]]; then
  [[ -n "${2:-}" ]] || die "--resume requires a run id"
  decision="${3:-approve}"
  [[ "$decision" == "approve" || "$decision" == "reject" ]] || die "decision must be approve or reject"
  exec terfyn run "$workflow" --project "$project" --resume "$2" --decision "$decision"
fi

# --- fresh run ---------------------------------------------------------------
[[ $# -eq 4 ]] || die "usage: terfyn-maintain <owner/name> <issue> <task> <workspace-dir>"
repo="$1"; issue="$2"; task="$3"; workspace="$4"

[[ "$repo" == */* ]]            || die "repo must be owner/name, got '$repo'"
[[ "$issue" =~ ^[0-9]+$ ]]      || die "issue must be a number, got '$issue'"
[[ -d "$workspace" ]]           || die "workspace dir does not exist: $workspace"

owner="${repo%%/*}"; name="${repo#*/}"
export TERFYN_WORKSPACE_ROOT
TERFYN_WORKSPACE_ROOT="$(cd "$workspace" && pwd)"
export TERFYN_WORKSPACE_TEST_COMMAND="${TERFYN_WORKSPACE_TEST_COMMAND:-go test ./...}"
export TERFYN_GIT_REMOTE="${TERFYN_GIT_REMOTE:-origin}"

# Build the FixTask input with correct JSON escaping (jq, falling back to python3).
input="$(mktemp -t terfyn-maintain.XXXXXX.json)"
trap 'rm -f "$input"' EXIT
if command -v jq >/dev/null; then
  jq -n --arg o "$owner" --arg r "$name" --argjson n "$issue" --arg t "$task" \
     '{owner:$o, repo:$r, number:$n, task:$t}' > "$input"
elif command -v python3 >/dev/null; then
  OWNER="$owner" REPO="$name" NUMBER="$issue" TASK="$task" python3 - > "$input" <<'PY'
import json, os
print(json.dumps({"owner": os.environ["OWNER"], "repo": os.environ["REPO"],
                  "number": int(os.environ["NUMBER"]), "task": os.environ["TASK"]}))
PY
else
  die "need jq or python3 to build the input JSON safely"
fi

echo "→ terfyn run $workflow (workspace: $TERFYN_WORKSPACE_ROOT)" >&2
echo "  suspends at the publication boundary; resume with:" >&2
echo "    terfyn-maintain --resume <run-id> approve" >&2
exec terfyn run "$workflow" --project "$project" --input-file "$input"
