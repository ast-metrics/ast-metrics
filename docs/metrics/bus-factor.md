---
description: "The bus factor measures how concentrated knowledge is in your codebase, computed from git history and grouped by community."
---

# Bus Factor

The bus factor asks the uncomfortable question: **how many people can leave
before part of this codebase becomes an archaeology site?** A bus factor of 1
on a component means exactly one person understands it. Every team has such a
component; few teams know which one it is before the person resigns.

## How it's computed

No survey, no guesswork: AST Metrics reads your **git history**.

1. For each file, it measures ownership: who wrote and rewrote its lines.
2. Ownership is aggregated by [community](community-detection.md), the groups
   of classes that actually work together, rather than by folder. Folders
   lie about structure; the dependency graph doesn't.
3. A community overwhelmingly owned by one person (80%+ of the knowledge) gets
   a bus factor of 1: that whole functional area lives in one head.

Because it's git-based, analyze a clone with real history. A `--depth 1`
checkout has no authorship to read.

## How to read it

- **Bus factor 1 on a community**: critical. It's not "Alice wrote a file",
  it's "the entire payment area is Alice".
- **Bus factor 1 on a community that also contains
  [hotspots](risk.md)**: your single most urgent staffing problem. Complex
  code, changing weekly, understood by one person.
- **Higher values**: knowledge is genuinely shared there.

The point is not to blame the expert. Concentrated ownership is the natural
result of someone doing good work fast; the metric just makes the resulting
fragility visible while it's still cheap to fix.

## How to raise it

1. **Route reviews to non-owners.** The second-best way to learn a component
   is to review changes to it.
2. **Pair on the expert's next tickets.** The best way is to modify it with
   the expert watching.
3. **Write down the "why".** The owner documents intent and invariants: the
   things a newcomer cannot reverse-engineer from the code.
4. **Check the trend.** Re-run the analysis a quarter later; the chips should
   change color.

## See it on your code

```bash
ast-metrics analyze --report-html=report --open-html .
```

The report's **Team** page shows who carries the knowledge: the key people
(how many developers write half the commits), the ownership per area, and a
chip per folder, red when a single person holds it. On
[Monolog](../getting-started/your-first-analysis.md) it opens with a
reassuring verdict, "Knowledge is shared, no single person owns this code",
and names the four people who write half the commits. Run it on your own
repository before assuming you'd get the same sentence.
