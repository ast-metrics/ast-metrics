---
description: "The risk score combines complexity and churn to point at the hotspots where bugs are most likely to hide."
---

# Risk Score

The risk score answers the question every refactoring plan should start with:
**where is the next bug most likely to be written?** It multiplies two things
that are dangerous together and harmless apart:

```
Risk = Complexity × Churn
```

- **Complexity**: how hard the code is to get right
  ([cyclomatic complexity](cyclomatic-complexity.md)).
- **Churn**: how often the code actually changes, read from your git history.

## Why the multiplication matters

Think of your files in four quadrants:

| | Rarely changes | Changes often |
|---|---|---|
| **Simple** | Fine. | Fine: easy edits stay easy. |
| **Complex** | Dormant. It works, leave it alone. | **The hotspot.** Hard edits, made often, by people in a hurry. |

Only one quadrant is dangerous, and neither metric finds it alone. This is the
lesson from analyzing [Monolog](../getting-started/your-first-analysis.md):
`Logger.php` is the most complex class of the project (complexity 72), yet it
is *not* the top risk, because nobody touches it anymore. The top hotspot is
`StreamHandler.php`, less complex (MI 66) but modified five times recently.
Complexity tells you where reading is hard; risk tells you where reading is
hard *and happening this week*.

This also makes risk the best budget allocator you have: refactoring a
hotspot pays back on every future change, while refactoring dormant complex
code pays back never.

## See it on your code

```bash
ast-metrics analyze .
```

The summary ends with the list that matters:

```text
Hotspots (top 5 of 21 files at risk)
  1.43  src/Monolog/Handler/StreamHandler.php  (MI 66, 5 commits)
  1.08  src/Monolog/Handler/RotatingFileHandler.php  (MI 72, 5 commits)
  ...
```

Each line gives the risk score, the maintainability index, and the recent
commit count, so you can see which ingredient dominates.

The HTML report tells the same story visually: the code map on the overview
page draws one bubble per class, where **size is lines of code, color is
complexity, and the ring is recent git activity**. Big, red, ringed bubbles
are your hotspots; you'll spot them in two seconds.

!!! tip "Refactor the top three, not everything"
    The hotspot list is a priority queue, not a to-do list. Fixing the top
    three entries usually removes most of the risk; fixing entry twenty is
    procrastination with extra steps.

## Prerequisite

Churn comes from git, so analyze a clone with real history: a `--depth 1`
checkout has no churn to read, and every file will look dormant.
