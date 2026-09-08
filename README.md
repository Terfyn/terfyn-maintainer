# terfyn-maintainer

A guarded autonomous PR fixer built on [Terfyn](https://github.com/Terfyn/terfyn) **v0.4.6** —
Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
**before** execution.

A Triager plans, an Implementer edits and runs the tests, and an independent Reviewer
passes a `CodingState` back and forth inside a bounded loop (**at most 3 rounds**) until
the change is approved. The run then commits the approved change and **stops at the
publication boundary**, requiring human approval before it pushes the branch, opens the
PR, or comments on GitHub. Before anything runs, `terfyn plan` prints exactly how much
authority each agent can exercise — and the runtime enforces that boundary at dispatch,
not via the prompt. The full loop has been exercised end to end against a live repo.

**There is no code, and no YAML.** As of Terfyn v0.4.1, `.agent` is the sole authoring
surface (ADR 007, #430) — there is no `project.yaml`; the provider and defaults are
declared inline. The whole program — provider, defaults, agents, workflow, tools, and
policies — is one declarative [`main.agent`](main.agent) file, with the agents' prompts in
[`prompts/`](prompts). The agents navigate the target repo with the native `list_dir` /
`glob` / `grep` discovery ops (#452) instead of guessing paths, read spans with
`read_file` `offset`/`limit`, and patch with a surgical `edit` (str_replace, #512); a read
that misses is a recoverable observation rather than a fatal run error (#451), and each
agent's output is bound to the `CodingState` schema (#510). Git branch/commit/push is
Terfyn's native adapter; the bounded retry is `retry until … limit 3`; the capability
guarantee is a declarative test.

## Layout

| Path | What |
|---|---|
| [`main.agent`](main.agent) | the entire program: the `provider` + `defaults`, Triager / Implementer / Reviewer, the bounded `FixPullRequest` workflow (`retry until … limit 3`), and the inline `tool` + `policy` declarations |
| [`prompts/`](prompts) | the agents' prompts (`triager.md` / `implementer.md` / `reviewer.md`), loaded via `instructions file("…")` |
| [`schemas/`](schemas) | `FixTask` (input) and `CodingState` (loop state) |
| [`tests/capabilities.yaml`](tests/capabilities.yaml) | declarative capability invariants checked by `terfyn test` |
| [`issue.json`](issue.json) | the workflow input (owner / repo / number / task) |
| [`.env.example`](.env.example) | template for `.env` (API key + workspace settings; `.env` is gitignored) |
| [`scripts/terfyn-maintain.sh`](scripts/terfyn-maintain.sh) | runs `terfyn run`, sourcing `.env` and the input JSON |
| [`DESIGN.md`](DESIGN.md) | the full design |

## Install

```bash
go install github.com/Terfyn/terfyn/cmd/terfyn@v0.4.6   # the engine (Go ≥ 1.25) — the only install
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

Executing the loop needs API keys (the Implementer runs on `anthropic/claude-sonnet-5`,
the Triager and Reviewer on `anthropic/claude-haiku-4-5`; `validate`/`plan`/`test` are
static and need none) and a workspace sandbox. Configuration comes from `.env`; the input
is a JSON file (the **issue** to fix); one script runs it.

```bash
cp .env.example .env      # ANTHROPIC_API_KEY + GITHUB_TOKEN + TERFYN_WORKSPACE_ROOT
$EDITOR issue.json        # owner / repo / number / task  (the GitHub issue to fix)

scripts/terfyn-maintain.sh            # = terfyn run workflow/FixPullRequest --input-file issue.json
scripts/terfyn-maintain.sh other.json # a different input file
```

The Triager reads the issue, the Implementer/Reviewer loop produces an approved fix, the
workflow **commits** it (locally, unattended), and the publication boundary **pushes the
branch, opens a PR, and comments on the issue**. Those three are gated, so at the boundary
the run **suspends** (`interrupted`, exit 0). Review what it's about to publish, then
resume:

```bash
scripts/terfyn-maintain.sh --resume <run-id> approve
```

`read_file` / `write_file` and the `list_dir` / `glob` / `grep` discovery ops are
confined to `TERFYN_WORKSPACE_ROOT` (a `..` escape is rejected), `run_tests` runs only
`TERFYN_WORKSPACE_TEST_COMMAND`, `git.push_branch` refuses the default branch and is
human-gated — so the capability boundary holds at the filesystem, the test runner, and
the network.

See [DESIGN.md](DESIGN.md) for the full design and threat model.
