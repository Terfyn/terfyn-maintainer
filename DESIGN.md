# DESIGN — Guarded Autonomous PR Fixer

> Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
> **before** execution.

**Status:** Draft · **Target:** v1 (single-repo, single-issue) · **Built on:** [Terfyn](https://github.com/Terfyn/terfyn)

---

## 1. The one-paragraph pitch

You hand the tool a repo, an issue, and a task. It reads the issue, inspects the
code, writes a fix on a branch, runs the tests, has an independent reviewer criticize
the patch, loops at most three times, shows you the diff, and then **stops at the
publication boundary** and asks for approval before it pushes anything or comments on
GitHub. The differentiator is not "AI fixes code" — everyone has that. It is that
`terfyn plan` prints exactly how much havoc the run is *theoretically* capable of
causing before you unleash it, and the runtime enforces that boundary at dispatch
regardless of what the model tries to do.

```
$ terfyn-maintainer --repo gombit-dev/gombit --issue 123 \
    --task "Fix the CSRF middleware bug"
```

---

## 2. Why this is a Terfyn app (and not just another agent)

The interesting UX is the **capability review**, not the code generation. Before the
agent runs, you see its full blast radius as a reviewable diff:

```
$ terfyn plan

Workflow/FixPullRequest

Invocation bounds:
  Agent/Triager       <= 1
  Agent/Implementer   <= 3
  Agent/Reviewer      <= 3

Autonomous authority:
  Triager      github.read      workspace.read
  Implementer  workspace.read   workspace.write   process.exec
  Reviewer     workspace.read   process.exec

Human-gated:
  git.push     github.write

Forbidden:
  git.force_push   github.merge   github.admin   secrets.write
```

Terfyn already computes capability/effect/authority deltas, and its control-flow
analysis derives the invocation bounds (`<= 3`) directly from the bounded `while`
loop. The reviewer holding **no** `workspace.write` grant is not a prompt instruction
that the model might ignore — it is enforced at dispatch. If the reviewer's model
decides to rewrite a file, the call is **denied by capability**, not by hope.

This doc describes how to wire Terfyn's existing pieces — the implement/review loop,
GitHub native ops, HITL suspend/resume, audit traces, planning, and effect bounds —
into one real application.

---

## 3. What already exists vs. what we build

Almost everything is already shipped in Terfyn. v1 is mostly **integration**, plus one
small new tool server.

| Capability | Status | Source |
|---|---|---|
| Bounded implement/review loop | ✅ exists | `examples/implement-review-loop` |
| Invocation bounds from `while … limit 3` | ✅ exists | control-flow analysis in `terfyn plan` |
| Capability/effect/authority deltas | ✅ exists | `terfyn plan` risk output |
| GitHub native ops (`pull_request.get/diff/fetch`, `post_comment`, `check_runs.list`) | ✅ exists | native tools, use `GITHUB_TOKEN` |
| HITL suspend / resume with a human decision | ✅ exists | `--resume <id> --decision approve` |
| Tamper-evident audit trace + verify | ✅ exists | `terfyn audit verify` |
| **Local workspace tool server** (read/patch/test/branch/push) | 🔨 **build** | §6 |
| **`FixPullRequest` workflow** wiring the agents together | 🔨 **build** | §5 |
| **`terfyn-maintainer` CLI wrapper** (issue → input JSON → run) | 🔨 **build** | §8 |

The flagship example already implements steps 4→7 of the loop below. GitHub
integration, HITL/resume, audit, planning, and effect bounds independently exist. What
remains is connecting the pieces.

---

## 4. End-to-end flow

```
                 ┌───────────────┐
                 │   GitHub PR   │
                 │  / issue #123 │
                 └───────┬───────┘
                         │
                         ▼
                  ┌─────────────┐
                  │   Triage    │   github.read + workspace.read
                  │ read only   │   (understand issue & code)
                  └──────┬──────┘
                         │
                         ▼
                ┌────────────────┐
             ┌─►│  Implementer   │   workspace.read/write + process.exec
             │  │ read/write/test│   (edit files, run tests)
             │  └───────┬────────┘
             │          │
             │          ▼
             │   ┌─────────────┐
             │   │  Reviewer   │    workspace.read + process.exec
             │   │ read + test │    (criticize patch, NO write)
             │   └──────┬──────┘
             │          │
             │     rejected  (loop, bounded to 3)
             └──────────┘
                         │ approved
                         ▼
                ┌─────────────────┐
                │ HUMAN APPROVAL  │   run suspends → status: interrupted
                │ publish changes │   git.push / github.write are HITL-gated
                └───────┬─────────┘
                        ▼
                  push branch / post comment
                        ▼
                  terfyn audit verify
```

Ten concrete steps `terfyn-maintainer` performs:

1. Read the issue (and PR, if the issue references one).
2. Inspect the repository (list/read/search).
3. Create a worktree/branch (`terfyn/fix-123`).
4. Let the Implementer agent modify files.
5. Run the test suite.
6. Have a separate Reviewer agent criticize it (read + test only).
7. Repeat at most 3 times.
8. Show the diff.
9. **Suspend** at the publication boundary.
10. Require human approval before pushing or commenting.

### Runtime UX

```
$ terfyn run workflow/FixPullRequest --input issue.json

✓ fetched issue/PR
✓ analyzed code
✓ implementation attempt 1
✓ tests
✗ reviewer rejected

✓ implementation attempt 2
✓ tests
✓ reviewer approved

⏸ suspended

Agent requests:
    git.push_branch

Branch:
    terfyn/fix-123

Effects:
    repository.write
    network.write

Approve? [y/N]
```

You can close the terminal. The run is recorded as `interrupted` (exit 0). Come back
tomorrow:

```
$ terfyn run workflow/FixPullRequest --resume 018f... --decision approve
```

and execution continues from the suspension point, enforcing the manifest from the
run's deployment snapshot (not the current deploy state). Afterward:

```
$ terfyn audit verify
✓ trace valid
```

so you know the recorded execution wasn't edited after the fact.

---

## 5. The workflow (`FixPullRequest`)

### 5.1 Agents and their grants

Grants are the hard boundary. The critical asymmetry: **Implementer can write, Reviewer
cannot.**

```agent
agent Triager {
    model openai/gpt-5
    instructions "Read the issue and the relevant code. Produce a plan. Do not modify anything."
    grants {
        tool.github.pull_request.get
        tool.github.pull_request.diff
        tool.github.issues.read
        tool.workspace.list_files
        tool.workspace.read_file
        tool.workspace.search
    }
}

agent Implementer {
    model openai/gpt-5
    instructions "Implement the fix described by the plan. Edit files, run tests until green."
    grants {
        tool.workspace.list_files
        tool.workspace.read_file
        tool.workspace.search
        tool.workspace.apply_patch    # write
        tool.workspace.git_diff
        tool.workspace.run_tests      # process.exec
    }
}

agent Reviewer {
    model openai/gpt-5
    instructions "Criticize the patch. You may read files and run tests. You may NOT edit files."
    grants {
        tool.workspace.list_files
        tool.workspace.read_file
        tool.workspace.search
        tool.workspace.git_diff
        tool.workspace.run_tests      # process.exec, read-only effect
    }
}
```

> Adding `tool.workspace.apply_patch` to the Reviewer would make `terfyn plan` print
> `[high] authority_widening: AUTONOMOUS authority WIDENED` — a visible, reviewable
> regression in the capability diff.

### 5.2 The bounded loop

The skeleton is the existing `ImplementAndReview` example:

```agent
workflow FixPullRequest(issue) {
    plan = Triager(issue)

    branch = git.create_branch(name: "terfyn/fix-${issue.number}")

    state = { plan: plan, approved: false }
    while !state.approved limit 3 {
        implementation = Implementer(state)
        state          = Reviewer(implementation)
    }

    diff = workspace.git_diff()

    # ── publication boundary — everything below is HITL-gated ──
    git.push_branch(branch: branch)              # requires approval
    github.pull_request.post_comment(            # requires approval
        number: issue.number,
        body:   review_comment(state)
    )
}
```

`limit 3` is what Terfyn's control-flow analysis reads to derive
`Agent/Implementer <= 3` and `Agent/Reviewer <= 3` in the plan. It is also a runtime
budget (`maxIterations`) — the loop is bounded whether or not the model ever converges.

### 5.3 The publication boundary

`git.push_branch` and `github.pull_request.post_comment` are listed in
`Policy.spec.approvals.requiredFor`. When the workflow reaches them, `terfyn run`
suspends with status `interrupted` (exit 0) rather than executing. Nothing leaves the
machine until a human resumes with `--decision approve`.

---

## 6. The workspace tool server (the one new component)

**We do not give the agent a shell.** `tool.shell.exec` would mean the capability
boundary is now `/bin/sh` — the entire Terfyn value proposition collapses. Instead we
build a small local tool server (native subprocess or MCP-over-stdio) that exposes a
**deliberately narrow** operation set, each mapped to a named effect.

### 6.1 Operations

| Operation | Effect | Grantable to | Notes |
|---|---|---|---|
| `workspace.list_files` | `workspace.read` | all | scoped to the checkout root |
| `workspace.read_file` | `workspace.read` | all | path must resolve inside root (no `..` escape) |
| `workspace.search` | `workspace.read` | all | ripgrep over the checkout |
| `workspace.apply_patch` | `workspace.write` | **Implementer only** | unified-diff apply, path-jailed |
| `workspace.git_diff` | `workspace.read` | Implementer, Reviewer | working-tree diff |
| `workspace.run_tests` | `process.exec` | Implementer, Reviewer | fixed command, sandboxed, timeout+resource caps |
| `git.create_branch` | `repository.write` (local) | workflow (static step) | local ref only |
| `git.push_branch` | `repository.write` + `network.write` | **HITL-gated** | the publication boundary |

### 6.2 What is deliberately *absent*

There is no `git.push_main`, no `git.force_push`, no `git.delete_branch`, no
`github.merge`, no `github.admin`, no `secrets.write`, and no general shell. These
don't need to be "denied" by policy — **they are not implemented as operations at
all**, so the plan's `Forbidden:` list is honest by construction. If a future workflow
needs one, it must be declared explicitly, which shows up as an authority delta.

### 6.3 `run_tests` is the sharp edge

`workspace.run_tests` still spawns a subprocess, so it is where a clever model could
try to break out. Constraints:

- **Fixed command** resolved from project config (e.g. `go test ./...`), not model
  input. The agent chooses *whether* to run tests, never *what* runs.
- Execute in a sandbox (container / namespace) with **no network**, a wall-clock
  timeout, and CPU/memory caps.
- Environment is scrubbed — `GITHUB_TOKEN` and other secrets are **never** present in
  the test process. Only `git.push_branch` / `github.*` (HITL-gated, outside the
  sandbox) ever see credentials.

### 6.4 Manifest pinning

The tool server declares its operation set via a manifest. Terfyn pins the manifest at
deployment (`apply`), so a live `tools/list` cannot expand the callable set at runtime.
A resumed run enforces the manifest from its snapshot, closing the "swap the tool
server between suspend and resume" gap.

---

## 7. Capability / effect model

Terfyn distinguishes two kinds of effect, both surfaced in `terfyn plan`:

- **Static** — deterministic workflow steps. `git.create_branch` and the two publish
  calls are static: we know at plan time they will be attempted.
- **Autonomous** — an agent nondeterministically selecting a tool from its grant set.
  The Implementer *may* call `apply_patch`; whether and when is up to the model, but the
  *set* it can pick from is fixed.

The plan's job is to make the **autonomous upper bound** legible:

```
Effect bound (Workflow/FixPullRequest):
high:
- [high] effect_bound: workspace.write   autonomous  Agent/Implementer may select tool.workspace.apply_patch
- [high] effect_bound: network.write     static      step git.push_branch (HITL-gated)
medium:
- [medium] effect_bound: process.exec    autonomous  Agent/Implementer, Agent/Reviewer may select tool.workspace.run_tests
- [medium] effect_bound: github.read     static      step Triager fetch
```

The security claim: *the model is nondeterministic, but **what it can do** is statically
bounded, reviewable as a diff before deployment, and enforced at dispatch.* The grant is
the boundary, not the prompt.

---

## 8. CLI wrapper (`terfyn-maintainer`)

A thin Go binary over `terfyn run`. Its whole job is: fetch the issue → build the input
JSON → invoke the workflow → surface the suspension.

```bash
terfyn-maintainer \
    --repo LAA-Software-Engineering/terfyn \
    --issue 316 \
    --task "..."          # optional; defaults to issue body
```

Responsibilities:

1. Resolve `--repo` / `--issue`, fetch metadata via the GitHub native ops
   (`pull_request.get`, `issues.read`), using `GITHUB_TOKEN` from env.
2. Prepare a clean checkout/worktree the workspace tool server is rooted at.
3. Emit `issue.json` and call `terfyn run workflow/FixPullRequest --input issue.json`.
4. On suspension, print the pending request (branch, effects) and the exact resume
   command. It does **not** auto-approve.
5. Pass through Terfyn exit codes (see §10).

Resume is just Terfyn:

```bash
terfyn run workflow/FixPullRequest --resume <run-id> --decision approve
```

---

## 9. The `.agent` authoring gap (Terfyn #316)

There is one well-timed blocker to writing the *publishing* step purely in `.agent`:
issue **#316** — `.agent` can't yet construct a string argument containing template
references, e.g.:

```agent
github.pull_request.post_comment(body: """
## Automated review

${review.summary}

${review.findings}
""")
```

The **runtime/YAML representation can already do this** — it is an authoring-language
gap, not an execution limitation. Two options for v1:

- **(A) Ship now:** author the publish step (or the whole workflow) in YAML, where
  interpolation already works. Recommended for v1 — unblocks everything else.
- **(B) Fix #316 first:** land string-interpolation in the `.agent` frontend, then
  author the entire workflow in `.agent`.

**Recommendation: (A).** The point of v1 is to fix a real bug end-to-end, not to polish
the authoring frontend. Leave the one publishing workflow in YAML and revisit #316 as a
fast-follow.

---

## 10. Exit codes (inherited from Terfyn)

| Code | Meaning | Our usage |
|---|---|---|
| 0 | Success **or** suspended (`interrupted`) | normal path — suspension at publish is exit 0 |
| 2 | Validation error | bad `.agent`/YAML or schema |
| 3 | Plan/apply conflict or config drift at run | forces a fresh `terfyn plan` |
| 4 | Execution error | tool crash, test harness failure |
| 5 | Policy denial (fail-closed) | an ungated sensitive call was attempted |

A capability denial (Reviewer attempting `apply_patch`) is enforced at dispatch and
surfaces distinctly from a policy gate — the reviewer simply cannot call the operation.

---

## 11. Threat model / non-goals

**Guards, and what they buy:**

- *Reviewer cannot self-approve by editing code* → no `workspace.write` grant; enforced
  at dispatch.
- *Agent cannot exfiltrate secrets via tests* → sandbox has no network, no tokens in env.
- *Agent cannot publish silently* → `git.push_branch` / `github.write` are HITL-gated;
  run suspends.
- *Agent cannot escape the checkout* → all paths jailed to the workspace root; no `..`.
- *Agent cannot escalate via a swapped tool server* → manifest pinned at deploy, enforced
  on resume.
- *Trace can't be doctored after the fact* → `terfyn audit verify` walks the hash chain.

**Non-goals for v1:**

- No multi-repo / monorepo-slice orchestration.
- No auto-merge — ever. `github.merge` is forbidden, full stop.
- No arbitrary shell. If a task needs a build step beyond `run_tests`, declare a new
  narrow operation, don't open a shell.
- No unattended publish. A human is always the last gate before anything leaves the box.

---

## 12. Build plan

1. **Workspace tool server** (§6) — the only genuinely new code. Ship read/patch/diff/
   test + `create_branch`/`push_branch`, path-jailed, sandboxed tests, pinned manifest.
2. **`FixPullRequest` workflow** (§5) — YAML first (§9 option A); wire Triager →
   Implementer/Reviewer loop → HITL publish gate.
3. **`terfyn-maintainer` CLI** (§8) — issue fetch → `issue.json` → `terfyn run` → surface
   suspension + resume command.
4. **Golden fixtures** — `terfyn test` fixtures that fail if the Reviewer gains write, if
   an approval gate is dropped, or if a forbidden op appears.
5. **First real run** — fix one small Terfyn/Gombit/Brainrotlang bug end-to-end: `plan`
   shows the bounds, the agent fixes it, review passes, it suspends, you approve, it
   publishes, `audit verify` passes.

The success criterion is the first time you run `terfyn plan` and see:

```
Implementer: workspace.write
Reviewer:    no workspace.write
Publish:     HUMAN-GATED
Implementer: ≤ 3 invocations
Reviewer:    ≤ 3 invocations
```

…and then it actually fixes a real bug. That is the crossing from "interesting execution
engine" to "I would use this."
