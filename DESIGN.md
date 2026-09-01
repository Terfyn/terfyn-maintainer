# DESIGN — Guarded Autonomous PR Fixer

> Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
> **before** execution.

**Status:** Draft · **Target:** v1 (single-repo, single-issue) · **Built on:** [Terfyn **v0.2.0**](https://github.com/Terfyn/terfyn) (`go install github.com/Terfyn/terfyn/cmd/terfyn@v0.2.0`)

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

As of Terfyn v0.2.0 this is **mostly assembly, not construction**: the bounded
implement/review loop, the native workspace adapter, native GitHub operations, HITL
suspend/resume, manifest pinning, and the authority diff all ship. §3 is the honest
inventory; §9 is the single component that still has to be built.

---

## 2. Why this is a Terfyn app (and not just another agent)

The interesting UX is the **capability review**, not the code generation. Before the
agent runs, you see its full blast radius as a reviewable diff. This is real v0.2.0
`terfyn plan` output from the shipped `implement-review-loop` example, extended with the
publication boundary this design adds:

```
$ terfyn plan

Invocation bounds:
  agent Implementer: ≤ 3 per run
  agent Reviewer:    ≤ 3 per run

Effect bound (Agent/Implementer):
- [high] workspace.read    autonomous  may select tool.workspace.read_file
- [high] workspace.write   autonomous  may select tool.workspace.write_file
- [high] process.exec      autonomous  may select tool.workspace.run_tests

Effect bound (Agent/Reviewer):
- [high] workspace.read    autonomous  may select tool.workspace.read_file
- [high] process.exec      autonomous  may select tool.workspace.run_tests
- [low]  workspace.write   unreachable no grant path to tool.workspace.write_file

Human-gated (approvals.requiredFor):
  tool.git.push_branch
  tool.github.pull_request.post_comment
```

The invocation bound `≤ 3 per run` is derived by control-flow analysis directly from the
bounded `while … limit 3` loop (Terfyn #293). The Reviewer's `workspace.write` reads
**`unreachable — no grant path`**: it holds no `write_file` grant, so the operation is
never advertised to its model. That is not a prompt instruction the model might ignore —
a Reviewer that (hallucinating, prompt-injected, or mistaken) tries to write is **denied
at tool resolution**, verified by Terfyn's own `TestImplementReviewLoop_ReviewerCannotWrite`.

Adding `write_file` to the Reviewer's grants is a one-line change that `terfyn plan`
surfaces as a high-severity **`AUTONOMOUS authority WIDENED`** line — visible before
`apply`, not discovered in production.

---

## 3. What already exists vs. what we build

The picture inverted between the original brief and v0.2.0. The workspace tool server the
brief told us to build is now **native**; almost nothing is left to construct.

| Capability | Status in v0.2.0 | Source |
|---|---|---|
| Bounded implement/review loop, `while … limit 3` | ✅ shipped | `examples/implement-review-loop` (#292) |
| Per-agent invocation bounds from the loop | ✅ shipped | `terfyn plan` (#293) |
| Multiple operations per agent grant | ✅ shipped | #291 |
| Capability / effect / authority deltas in `plan` | ✅ shipped | `terfyn plan` |
| **Native `workspace` adapter** (`read_file` / `write_file` / `run_tests`, sandbox-rooted, path-jailed) | ✅ **native** | `internal/tools/native` |
| **Native GitHub ops** (`pull_request.get`/`diff`/`fetch`/`post_comment`, `check_runs.list`) | ✅ **native**, live with `GITHUB_TOKEN` | `internal/tools/native` |
| `.agent` `${...}` string interpolation in args | ✅ shipped | #316 |
| Closed-world manifest pin (dispatch-time + resume) | ✅ shipped | #204 / #207 |
| HITL suspend/resume, durable across loop iterations | ✅ shipped | execir (#275) |
| Tamper-evident audit trace + verify | ✅ shipped | `terfyn audit verify` |
| **Git-publish tool** (`create_branch` / `push_branch`) | 🔨 **build** (the only real gap) | §9 |
| **`terfyn-maintainer` CLI** (issue → input → `terfyn run`) | 🔨 **build** (thin) | §8 |

Everything except the last two rows is assembly of shipped parts.

---

## 4. End-to-end flow

```
                 ┌───────────────┐
                 │  GitHub issue │
                 │    / PR #123  │
                 └───────┬───────┘
                         │  github.pull_request.get   (native, GITHUB_TOKEN)
                         ▼
                  ┌─────────────┐
                  │   Triager   │   github.read + workspace.read
                  │  read only  │   (understand issue & code, produce a plan)
                  └──────┬──────┘
                         │  git.create_branch  (custom tool, §9)
                         ▼
                ┌────────────────┐   ┌──── while !approved limit 3 ────┐
             ┌─►│  Implementer   │   workspace.read/write + process.exec
             │  │ read/write/test│   (read_file, write_file, run_tests — native)
             │  └───────┬────────┘
             │          ▼
             │   ┌─────────────┐
             │   │  Reviewer   │    workspace.read + process.exec
             │   │ read + test │    (read_file, run_tests — NO write_file)
             │   └──────┬──────┘
             │     rejected
             └──────────┘  approved
                         ▼
                ┌─────────────────┐   git.push_branch + github.pull_request.post_comment
                │ HUMAN APPROVAL  │   → approvals.requiredFor → run suspends (interrupted, exit 0)
                │ publish changes │   → resume with --decision approve
                └───────┬─────────┘
                        ▼
                  push branch / post comment  →  terfyn audit verify
```

### Runtime UX

```
$ terfyn run workflow/FixPullRequest --input-file issue.json

✓ fetched issue          github.pull_request.get
✓ triaged                Triager
✓ branch created         git.create_branch
✓ implement attempt 1  → tests → reviewer REJECTED
✓ implement attempt 2  → tests → reviewer APPROVED

⏸ suspended  (interrupted, exit 0)

Agent requests:  tool.git.push_branch
Branch:          terfyn/fix-123
Effects:         repository.write, network.write

Approve? [y/N]
```

Close the terminal; the run is recorded as `interrupted`. Resume tomorrow:

```
$ terfyn run workflow/FixPullRequest --resume <run-id> --decision approve
```

Execution continues from the suspension point, enforcing the manifest pinned in the run's
deployment snapshot (#207), not the current deploy state. Because resume is durable across
loop iterations (execir, #275), a pause between Implementer and Reviewer resumes at the
correct iteration without re-issuing a completed effect. Afterward:

```
$ terfyn audit verify
✓ trace valid
```

---

## 5. The workflow (`FixPullRequest`), authored in `.agent`

In v0.2.0 the **entire** program — agents, their grants, and the bounded loop — is
authored in `.agent`. String interpolation in tool arguments works (#316), so even the
publish comment lives in `.agent`; there is no YAML fallback. Only resource *configuration*
(tool declarations, policies, project config, JSON schemas) stays in YAML.

### 5.1 Agents and their grants

Grants are the hard boundary. The critical asymmetry — **Implementer can write, Reviewer
cannot** — is a grant the Reviewer does not hold, exactly as in the flagship example.

```agent
agent Triager {
    model    anthropic/claude-sonnet-4-5
    policy   triage-readonly
    instructions """
    Read the issue and the relevant code. Produce a concise plan for the fix.
    Do not modify anything.
    """
    grants {
        tool.workspace.read_file
        tool.workspace.run_tests
    }
    input  FixTask
    output FixPlan
}

agent Implementer {
    model    anthropic/claude-sonnet-4-5
    policy   coding-agent
    instructions """
    Implement the requested change in the workspace. You may read files, write
    files, and run tests. If review feedback is present, address it first.
    """
    grants {
        tool.workspace.read_file
        tool.workspace.write_file    // ← write
        tool.workspace.run_tests
    }
    input  CodingState
    output CodingState
}

agent Reviewer {
    model    anthropic/claude-sonnet-4-5
    policy   reviewer
    instructions """
    Independently review the implementation. You may read files and run tests.
    You must not modify the workspace. Set approved=true only if acceptable;
    otherwise populate feedback with concrete findings.
    """
    grants {
        tool.workspace.read_file
        tool.workspace.run_tests
        // no write_file — the boundary is this absence, not the prompt.
    }
    input  CodingState
    output CodingState
}
```

### 5.2 The bounded loop and the publication boundary

```agent
workflow FixPullRequest(input: FixTask) -> CodingState
    effects {
        github.read, workspace.read, workspace.write,
        process.exec, repository.write, network.write
    }
{
    issue = github.pull_request.get(
        owner: input.owner, repo: input.repo, number: input.number)
    plan  = Triager(input)

    git.create_branch(name: "terfyn/fix-${input.number}")

    state = { task: input.task, plan: plan, approved: false }
    while !state.approved limit 3 {
        implementation = Implementer(state)
        state          = Reviewer(implementation)
    }

    // ── publication boundary — both calls are approvals.requiredFor ──
    git.push_branch(branch: "terfyn/fix-${input.number}")
    github.pull_request.post_comment(
        owner: input.owner, repo: input.repo, number: input.number,
        body: """
        ## Automated fix

        ${state.summary}
        """
    )
    return state
}
```

`limit 3` is what control-flow analysis reads to print `Implementer ≤ 3 per run` /
`Reviewer ≤ 3 per run`, and it is a hard runtime bound: there is no silent fourth attempt.
The `effects { … }` clause is the workflow's declared effect envelope; `plan` checks the
body's reachable effects against it.

---

## 6. The workspace: native, not something we build

The original brief's central task — "build a small local workspace tool/MCP server" — is
**obsolete in v0.2.0**. Terfyn ships a native `workspace` adapter that is exactly that:

```yaml
# tools/workspace.yaml
apiVersion: agentic.dev/v0
kind: Tool
metadata: { name: workspace }
spec:
  type: native
  safety: { trusted: true, sideEffects: true }
  operations:                 # the closed callable set; the effect bound is computed over it
    read_file:  { effects: [workspace.read] }
    write_file: { effects: [workspace.write] }
    run_tests:  { effects: [process.exec] }
```

- `read_file` / `write_file` operate **inside a sandbox root** (`TERFYN_WORKSPACE_ROOT`); a
  `..` path that would escape is rejected — the jail the brief wanted, enforced in-tree
  (`internal/tools/native`, `TestWorkspace…` escape tests).
- `run_tests` runs an **operator-configured** command (`TERFYN_WORKSPACE_TEST_COMMAND`),
  never a command the agent chooses. The agent decides *whether* to test, never *what* runs.
- Declaring `operations:` makes it a **closed-world manifest** (#204): an operation not
  listed is not callable, pinned at deploy and re-pinned on resume (#207).

**We still do not give the agent a shell.** The capability boundary stays the small, named
operation set — not `/bin/sh`.

---

## 7. Capability / effect model

Terfyn separates two axes, both surfaced by `terfyn plan`:

- **Effect bound** — the *union* of reachable effect classes for an agent/workflow, each
  with the concrete operation that reaches it. Autonomous (an agent nondeterministically
  selecting a granted tool) ranks higher-severity than static (a deterministic workflow
  step). The Implementer's bound includes `workspace.write` (autonomous, via `write_file`);
  the Reviewer's marks it `unreachable`.
- **Invocation bound** — how many times a bounded loop body runs (`≤ 3`). These are
  **independent**: Terfyn does not multiply effects by iterations.

The security claim, made concrete: *the model is nondeterministic, but **what it can do** is
statically bounded, reviewable as a diff before deployment, and enforced at dispatch.* The
grant is the boundary, not the prompt.

---

## 8. CLI wrapper (`terfyn-maintainer`)

A thin binary over `terfyn run`. In v0.2.0 most of its former job is native, so it mainly
shapes input and surfaces the suspension.

```bash
terfyn-maintainer --repo Terfyn/terfyn --issue 316 --task "…"
```

1. Resolve `--repo`/`--issue`; fetch metadata (native `pull_request.get` / `issues`), using
   `GITHUB_TOKEN`.
2. Set `TERFYN_WORKSPACE_ROOT` to a clean checkout/worktree and `TERFYN_WORKSPACE_TEST_COMMAND`.
3. Emit `issue.json`, call `terfyn run workflow/FixPullRequest --input-file issue.json`.
4. On suspension, print the pending request (branch, effects) and the exact
   `--resume … --decision approve` command. It does **not** auto-approve.
5. Pass through Terfyn exit codes (§10).

---

## 9. The one component to build: a git-publish tool

Native GitHub ops cover **read + comment** (`pull_request.get`/`diff`/`fetch`/`post_comment`,
`check_runs.list` — `post_comment` posts live with `GITHUB_TOKEN`, else simulated). What
v0.2.0 does **not** provide is branch publication: there is **no native `create_branch` or
`push_branch`.** That is the single genuine build item.

Build a tiny tool (native subprocess or MCP-over-stdio) exposing exactly two operations,
rooted at the same checkout as the workspace sandbox:

```yaml
# tools/git.yaml
apiVersion: agentic.dev/v0
kind: Tool
metadata: { name: git }
spec:
  type: mcp          # or native
  mcp: { transport: stdio, command: terfyn-git-publish }
  operations:
    create_branch: { effects: [repository.write] }   # local ref only
    push_branch:   { effects: [network.write]  }      # the publication boundary
```

Deliberately **absent** — not implemented as operations at all, so §11's "forbidden by
construction" holds: `push_main`, `force_push`, `delete_branch`, `merge`, `admin`, and any
general shell.

**Alternative for a first cut:** scope v1 to *comment-only* publishing — drop
`git.push_branch`, keep the gated `post_comment`, and let a human or CI push the reviewed
branch. That ships with **zero** custom tools, purely from native Terfyn.

### Gating

`git.push_branch` and `github.pull_request.post_comment` go in the workflow policy's
`approvals.requiredFor`. Reaching either suspends the run (`interrupted`, exit 0) until a
human resumes with `--decision approve`. Note (Terfyn semantics): HITL suspend/resume fires
for these workflow tool calls; keep the publish calls as workflow-level steps so they gate,
rather than burying them inside an agent's autonomous loop.

---

## 10. Exit codes (inherited from Terfyn)

| Code | Meaning | Our usage |
|---|---|---|
| 0 | Success **or** suspended (`interrupted`) | suspension at publish is exit 0 |
| 2 | Validation error | bad `.agent`/YAML or schema |
| 3 | Plan/apply conflict or config drift at run | forces a fresh `terfyn plan` |
| 4 | Execution error | tool crash, test harness failure |
| 5 | Policy denial / budget (fail-closed) | ungated sensitive call, or a granted op denied for missing approval |

---

## 11. Threat model / non-goals

**Guards, and what they buy:**

- *Reviewer cannot self-approve by editing code* → no `write_file` grant; denied at tool
  resolution (Terfyn's own test asserts this).
- *Agent cannot escape the checkout* → native workspace sandbox root; `..` rejected.
- *Agent cannot pick what `run_tests` runs* → command fixed by `TERFYN_WORKSPACE_TEST_COMMAND`.
- *Agent cannot publish silently* → `push_branch` / `post_comment` are `approvals.requiredFor`;
  the run suspends.
- *Agent cannot escalate via a swapped tool set* → closed-world manifest pinned at deploy and
  re-pinned on resume (#204/#207).
- *Trace can't be doctored after the fact* → `terfyn audit verify` walks the hash chain.

**Non-goals for v1:** no multi-repo orchestration; **no auto-merge, ever** (`merge` is
forbidden, not implemented); no arbitrary shell; no unattended publish — a human is always
the last gate before anything leaves the box.

---

## 12. Build plan

1. **Git-publish tool** (§9) — the only genuinely new code: `create_branch` + `push_branch`,
   path-rooted to the checkout, closed manifest. *(Or skip it for a comment-only first cut.)*
2. **`FixPullRequest` `.agent` program** (§5) — extend the shipped `implement-review-loop`
   with the Triager, the native GitHub fetch, and the gated publish boundary.
3. **Policies** — `triage-readonly`, `coding-agent`, `reviewer` (as shipped), plus the
   workflow policy carrying `approvals.requiredFor` for the two publish ops.
4. **`terfyn-maintainer` CLI** (§8) — issue fetch → `issue.json` + workspace env → `terfyn run`
   → surface suspension + resume command.
5. **Golden fixtures** (`terfyn test`) — fail if the Reviewer gains `write_file`
   (`AUTONOMOUS WIDENED`), if a publish approval gate is dropped, or if a forbidden op appears.
6. **First real run** — fix one small Terfyn/Gombit/Brainrotlang bug end-to-end: `plan` shows
   the bounds, the agent fixes it, review passes, it suspends, you approve, it publishes,
   `audit verify` passes.

The success criterion is the first `terfyn plan` that prints:

```
Invocation bounds:  Implementer ≤ 3   Reviewer ≤ 3
Implementer:        workspace.write   (autonomous)
Reviewer:           workspace.write   unreachable — no grant path
Publish:            HUMAN-GATED
```

…and then it actually fixes a real bug. In v0.2.0 that first line already prints for the
shipped example — the crossing from "interesting execution engine" to "I would use this" is
one git-publish tool and one CLI away.

---

## Appendix — verified against Terfyn v0.2.0

Module renamed to `github.com/Terfyn/terfyn` in v0.2.0 (was `LAA-Software-Engineering`).
Install: `go install github.com/Terfyn/terfyn/cmd/terfyn@v0.2.0` (requires Go ≥ 1.25).
The plan output in §2 was reproduced from the `implement-review-loop` example on the mock
model — `validate` / `plan` / `apply` run offline with no API keys; executing the loop needs
a real model plus `TERFYN_WORKSPACE_ROOT` and `TERFYN_WORKSPACE_TEST_COMMAND`.
