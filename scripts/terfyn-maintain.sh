#!/usr/bin/env bash
#
# terfyn-maintain — run the FixPullRequest workflow.
#
# Reads configuration from .env (in the repo root) and takes the input as a JSON
# file (default: issue.json). It does NO git or network work of its own — anything
# that touches the repo or the network is a capability-bounded tool inside the
# workflow (visible in `terfyn plan`), never shell glue here.
#
# Usage:
#   scripts/terfyn-maintain.sh [input.json]              # fresh run (default issue.json)
#   scripts/terfyn-maintain.sh --resume <run-id> [approve|reject]
#
# .env (copy from .env.example) provides ANTHROPIC_API_KEY, TERFYN_WORKSPACE_ROOT,
# TERFYN_WORKSPACE_TEST_COMMAND, and TERFYN_GIT_REMOTE.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
die() { echo "terfyn-maintain: $*" >&2; exit 1; }

# Load .env as shell assignments and export them for terfyn's native adapters.
if [[ -f "$root/.env" ]]; then
  set -a; . "$root/.env"; set +a
fi

command -v terfyn >/dev/null || die "terfyn is not installed (go install github.com/Terfyn/terfyn/cmd/terfyn@v0.3.1)"

# Resume path: carry a human decision back into a suspended run.
if [[ "${1:-}" == "--resume" ]]; then
  [[ -n "${2:-}" ]] || die "usage: --resume <run-id> [approve|reject]"
  decision="${3:-approve}"
  [[ "$decision" == "approve" || "$decision" == "reject" ]] || die "decision must be approve or reject"
  exec terfyn run workflow/FixPullRequest --project "$root" --resume "$2" --decision "$decision"
fi

# Fresh run: input comes from a JSON file (default issue.json).
input="${1:-$root/issue.json}"
[[ -f "$input" ]] || die "input JSON not found: $input (copy/edit issue.json)"
[[ -n "${TERFYN_WORKSPACE_ROOT:-}" ]] || die "TERFYN_WORKSPACE_ROOT is unset (set it in .env)"
[[ -d "$TERFYN_WORKSPACE_ROOT" ]] || die "TERFYN_WORKSPACE_ROOT is not a directory: $TERFYN_WORKSPACE_ROOT"

echo "→ terfyn run workflow/FixPullRequest --input-file $input" >&2
echo "  workspace: $TERFYN_WORKSPACE_ROOT" >&2
echo "  suspends at the publication boundary; resume with:" >&2
echo "    scripts/terfyn-maintain.sh --resume <run-id> approve" >&2
exec terfyn run workflow/FixPullRequest --project "$root" --input-file "$input"
