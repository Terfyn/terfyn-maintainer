# terfyn-maintainer

A guarded autonomous PR fixer built on [Terfyn](https://github.com/Terfyn/terfyn) **v0.3.0** —
Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
**before** execution.

A Triager plans, an Implementer and an independent Reviewer pass a `CodingState` back
and forth inside a bounded loop (**at most 3 rounds**), and the run then **stops at the
publication boundary** and requires human approval before it pushes a branch or comments
on GitHub. Before anything runs, `terfyn plan` prints exactly how much authority each
agent can exercise — and the runtime enforces that boundary at dispatch, not via the
prompt.

**There is no code.** The whole program — agents, workflow, tools, and policies — is one
declarative [`main.agent`](main.agent) file (Terfyn v0.3.0 inline declarations). Git
branch/push is Terfyn's native adapter; the capability guarantee is a declarative test.

## Layout

| Path | What |
|---|---|
| [`main.agent`](main.agent) | the entire program: Triager / Implementer / Reviewer, the bounded `FixPullRequest` workflow, and the inline `tool` + `policy` declarations |
| [`schemas/`](schemas) | `FixTask` (input) and `CodingState` (loop state) |
| [`tests/capabilities.yaml`](tests/capabilities.yaml) | declarative capability invariants checked by `terfyn test` |
| [`project.yaml`](project.yaml) | provider + defaults (nothing to import — it's all inline) |
| [`scripts/terfyn-maintain.sh`](scripts/terfyn-maintain.sh) | optional convenience wrapper over `terfyn run` |
| [`DESIGN.md`](DESIGN.md) | the full design |

## Install

```bash
go install github.com/Terfyn/terfyn/cmd/terfyn@v0.3.0   # the engine (Go ≥ 1.25) — the only install
```

No project binaries: the workspace, GitHub, and git tools are all native to Terfyn.

## Inspect and check the boundary (offline, no API keys)

```bash
terfyn validate
terfyn plan     # prints the capability review
terfyn test     # checks the capability invariants
```

`plan` prints the review that makes this a Terfyn app:

```
Invocation bounds:
  agent Implementer: ≤ 3 per run
  agent Reviewer:    ≤ 3 per run

Effect bound (Agent/Implementer):
- [high] workspace.write   autonomous  may select tool.workspace.write_file
Effect bound (Agent/Reviewer):
- [low]  workspace.write   unreachable no grant path to tool.workspace.write_file
```

The Reviewer's `workspace.write` is **unreachable — no grant path**: it holds no
`write_file` grant, so a review that tries to edit is denied at dispatch, not by the
prompt. `terfyn test` enforces that as an invariant — add the grant and it fails:

```
tests/capabilities.yaml  forbid Reviewer → workspace.write  fail
  agent "Reviewer" can reach "workspace.write" (tool.workspace.write_file) but is forbidden from it
```

## Run it for real

Executing the loop needs a real model (the mock model can `validate`/`plan`/`test`/`apply`
but not drive the tools) and a sandbox for the workspace tool:

```bash
export ANTHROPIC_API_KEY=…            # and set the agents' model to anthropic/… in main.agent
export TERFYN_WORKSPACE_ROOT="$(git -C /path/to/checkout rev-parse --show-toplevel)"
export TERFYN_WORKSPACE_TEST_COMMAND="go test ./..."

printf '{"owner":"Terfyn","repo":"terfyn","number":316,"task":"Fix the CSRF middleware bug"}' > issue.json
terfyn run workflow/FixPullRequest --input-file issue.json
# or: scripts/terfyn-maintain.sh Terfyn/terfyn 316 "Fix the CSRF middleware bug" /path/to/checkout
```

At the publication boundary the run **suspends** (`interrupted`, exit 0). Review the
pending push, then resume:

```bash
terfyn run workflow/FixPullRequest --resume <run-id> --decision approve
# or: scripts/terfyn-maintain.sh --resume <run-id> approve
```

`read_file` / `write_file` are confined to `TERFYN_WORKSPACE_ROOT` (a `..` escape is
rejected), `run_tests` runs only `TERFYN_WORKSPACE_TEST_COMMAND`, `git.push_branch`
refuses the default branch and is human-gated — so the capability boundary holds at the
filesystem, the test runner, and the network.

See [DESIGN.md](DESIGN.md) for the full design and threat model.
