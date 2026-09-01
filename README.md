# terfyn-maintainer

A guarded autonomous PR fixer built on [Terfyn](https://github.com/Terfyn/terfyn) —
Codex/Claude Code, but the dangerous parts are structurally bounded and reviewable
**before** execution.

```bash
terfyn-maintainer --repo gombit-dev/gombit --issue 123 \
    --task "Fix the CSRF middleware bug"
```

It reads the issue, inspects the repo, writes a fix on a branch, runs the tests, has an
independent reviewer criticize the patch (bounded to 3 iterations), then **stops at the
publication boundary** and requires human approval before it pushes or comments. Before
you run it, `terfyn plan` prints exactly how much havoc it is theoretically capable of
causing — and the runtime enforces that boundary at dispatch, not via the prompt.

See [DESIGN.md](DESIGN.md) for the full design.
