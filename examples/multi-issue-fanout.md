# Sketch: multi-issue fan-out (`parallel for`)

> **Status: illustrative sketch, not wired into the project.** This file is Markdown on
> purpose — Terfyn discovers resources by recursively walking the project root for
> `*.agent` files (`internal/project/agent_sources.go`), so a real `.agent` file here
> would be compiled into the main project and its agents would collide with `main.agent`.
> Keeping it in a fenced block means it documents the shape without touching the running
> program. To actually run it you'd add it to a **separate** project directory (its own
> root), or merge the `FixIssues` workflow into `main.agent` deliberately.

## The principle

Parallelize **across independent units of work, never within a producer→consumer chain.**
A single issue's `Triager → Implementer → Reviewer → publish` pipeline must stay
sequential (each step consumes the previous one's output). *Different* issues, though, are
independent — so the fan-out lives one level up: run N whole pipelines concurrently, each
reusing the existing `FixPullRequest` as a **sub-workflow**.

```
                 ┌─ FixPullRequest(issue₁)   (triage→impl→review→publish, sequential)
 FixIssues ─────►├─ FixPullRequest(issue₂)   ── run concurrently, bounded ──
 (parallel for) └─ FixPullRequest(issue₃)
```

## The sketch

A batch input schema (`schemas/FixBatch.json`) — a list of the existing `FixTask`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "FixBatch",
  "type": "object",
  "properties": {
    "issues": { "type": "array", "items": { "$ref": "FixTask.json" } }
  },
  "required": ["issues"],
  "additionalProperties": false
}
```

The batch workflow — **reuses** `FixPullRequest` unchanged, so nothing about the
single-issue behavior changes:

```agent
// FixIssues — batch variant. Fans out over input.issues, running each issue's full
// FixPullRequest pipeline concurrently (bounded). Each sub-run is itself the correct
// sequential triage→implement→review→publish; only DIFFERENT issues overlap.
workflow FixIssues(input: FixBatch) -> BatchReport
    policy publishing
    effects {
        github.read, github.write, workspace.read, workspace.write,
        process.exec, repository.write, network.write
    }
{
    parallel for issue in input.issues {
        // Sub-workflow call: the whole existing pipeline, one issue.
        // issue is a FixTask (owner/repo/number/task); its branch is
        // terfyn/fix-${issue.number}, distinct per issue, so no collision.
        outcome = FixPullRequest(issue)
    }

    // Fan-in: see "Open questions" — how per-iteration outputs are collected
    // back out of a dynamic parallel-for is the part to verify against the engine.
    return report
}
```

## Two design decisions this forces

1. **N publication boundaries.** `FixPullRequest` suspends at `push_branch` /
   `pull_request.create` / `issues.comment` (all `approvals.requiredFor`). Fanning out N
   issues means **N suspensions** — N separate `--resume … approve` decisions. If you'd
   rather review everything once, split the pipeline: a `PrepareFix` sub-workflow that
   does triage→implement→review and returns the branch **without publishing**, fan that
   out, then publish sequentially behind a single gate:

   ```agent
   parallel for issue in input.issues {
       prepared = PrepareFix(issue)          // no publish inside
   }
   // then, sequentially and gated, publish each prepared branch
   ```

2. **Bounded concurrency, but multiplied API pressure.** `parallel for` runs iterations
   with *bounded* concurrency (the engine caps it — it's "a loop wearing a graph
   costume," ADR 002 §1), so it won't launch 100 pipelines at once. But even a few
   concurrent Implementers stack input-token throughput against the per-model
   500k/min limit — the 429 you already hit becomes N× more likely. #512 (surgical edits)
   and #514 (configurable `maxTokens`) landed in v0.4.3, which lowers per-run token cost,
   but **do not reach for fan-out until the 429-backoff gap is addressed** (still unfiled):
   concurrency amplifies every per-run limit, and rate-limit backoff is the one that bites
   hardest under fan-out.

## Open questions to verify against the engine before relying on it

- **Result fan-in.** `parallel for`'s loop variable binds inside the body only
  (`internal/lang/ast.go` `ForStmt`). How per-iteration outputs (`outcome`) are collected
  into a returnable `BatchReport` needs checking — there may be a designated aggregation
  form, or the batch workflow may need to return nothing and rely on each sub-run's own
  PR/comment as the observable result.
- **Sub-workflow suspension semantics.** How a `FixPullRequest` suspension inside a
  parallel iteration surfaces (does the whole `FixIssues` suspend and resume per child?)
  is engine behavior to confirm — this is why the split-publish variant above is the
  safer default.
- **Effect/authority bounds.** `terfyn plan` on `FixIssues` should still show each agent's
  bounded authority; verify the per-issue invocation bounds compose sensibly under
  fan-out.

## Bottom line

The single-issue `FixPullRequest` is correct as a sequential pipeline and should stay
that way. `parallel for` is the right tool only for the *queue* case — processing many
independent issues — and even then the publication boundary and rate-limit multiplication
mean the split-prepare-then-publish shape is usually what you want.
```
