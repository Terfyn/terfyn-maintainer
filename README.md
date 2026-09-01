# terfyn-maintainer

A guarded autonomous PR fixer built on [Terfyn](https://github.com/Terfyn/terfyn) **v0.2.0** —
Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
**before** execution.

A Triager plans, an Implementer and an independent Reviewer pass a `CodingState` back
and forth inside a bounded loop (**at most 3 rounds**), and the run then **stops at the
publication boundary** and requires human approval before it pushes a branch or comments
on GitHub. Before anything runs, `terfyn plan` prints exactly how much authority each
agent can exercise — and the runtime enforces that boundary at dispatch, not via the
prompt.

## Layout

| Path | What |
|---|---|
| [`main.agent`](main.agent) | the Triager / Implementer / Reviewer agents and the bounded `FixPullRequest` workflow |
| [`tools/`](tools) | native `workspace` + `github` tools; the custom `git` publish tool |
| [`policies/`](policies) | per-agent policies + the `publishing` gate (`approvals.requiredFor`) |
| [`schemas/`](schemas) | `FixTask` (input) and `CodingState` (loop state) |
| [`cmd/terfyn-git-publish`](cmd/terfyn-git-publish) | MCP tool: `create_branch` / `push_branch` (the one op Terfyn lacks natively) |
| [`cmd/terfyn-maintainer`](cmd/terfyn-maintainer) | thin CLI over `terfyn run` |
| [`DESIGN.md`](DESIGN.md) | the full design |

## Install

```bash
go install github.com/Terfyn/terfyn/cmd/terfyn@v0.2.0   # the engine (Go ≥ 1.25)
go install ./cmd/terfyn-git-publish                      # the branch-publish tool
go install ./cmd/terfyn-maintainer                       # the CLI
```

## Inspect the capability boundary (offline, no API keys)

```bash
terfyn validate
terfyn plan
```

`plan` prints the review that makes this a Terfyn app — reproduced here:

```
Invocation bounds:
  agent Implementer: ≤ 3 per run
  agent Reviewer:    ≤ 3 per run
  agent Triager:     ≤ 1 per run

Effect bound (Agent/Implementer):
- [high] workspace.write   autonomous  may select tool.workspace.write_file
Effect bound (Agent/Reviewer):
- [low]  workspace.write   unreachable no grant path to tool.workspace.write_file
```

The Reviewer's `workspace.write` is **unreachable — no grant path**: it holds no
`write_file` grant, so a review that tries to edit is denied at dispatch, not by the
prompt. Add that grant and `terfyn plan` flags it as `AUTONOMOUS authority WIDENED`.

## Run it for real

Executing the loop needs a real model (the mock model can `validate`/`plan`/`apply` but
not drive the tools) and a sandbox for the workspace tool:

```bash
export ANTHROPIC_API_KEY=…            # and set the agents' model to anthropic/… in main.agent
export TERFYN_WORKSPACE_ROOT="$(git -C /path/to/checkout rev-parse --show-toplevel)"
export TERFYN_WORKSPACE_TEST_COMMAND="go test ./..."

terfyn-maintainer \
    --repo Terfyn/terfyn --issue 316 \
    --task "Fix the CSRF middleware bug" \
    --workspace "$TERFYN_WORKSPACE_ROOT"
```

At the publication boundary the run **suspends** (`interrupted`, exit 0). Review the
pending push, then resume:

```bash
terfyn-maintainer --resume <run-id> --decision approve
```

`read_file` / `write_file` are confined to `TERFYN_WORKSPACE_ROOT` (a `..` escape is
rejected), `run_tests` runs only `TERFYN_WORKSPACE_TEST_COMMAND`, and `git.push_branch`
is human-gated — so the capability boundary holds at the filesystem, the test runner, and
the network.

See [DESIGN.md](DESIGN.md) for the full design and threat model.
