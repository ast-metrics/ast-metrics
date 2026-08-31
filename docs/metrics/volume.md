---
description: "Volume metrics count lines of code, logical lines and comments: the baseline every other metric builds on."
---

# Volume

Volume answers the simplest question there is: **how much code is this?** It
looks trivial next to the fancier metrics, but size is the measurement that
correlates most strongly with defects. More code, more bugs. Everything else
you'll read in this guide should be interpreted *relative to volume*.

## The three counters

Take this file:

```typescript
// Applies the yearly discount policy.
export function discount(order: Order): number {
  // VIPs always get 10%.
  if (order.customer.isVip) {
    return order.total * 0.10;
  }

  return 0;
}
```

AST Metrics counts it three ways:

- **LOC (lines of code)**: 9. Every line, including comments and blanks. This
  is the size of the file as your eyes experience it.
- **LLOC (logical lines of code)**: 3. Only the executable statements (the
  `if` and the two `return`s). This is how much the file actually *does*.
- **CLOC (comment lines of code)**: 2. The comment lines.

The gap between LOC and LLOC is interesting on its own. A file with 700 LOC
and 80 LLOC is mostly comments and formatting, probably fine. A file with 700
LOC and 500 LLOC is a dense wall of logic.

## How to read it

Volume is rarely a problem alone; it's a multiplier for every other problem:

- **Big and complex**: the classic danger zone. Hard to understand, hard to
  test, and statistically where bugs live. This is what the
  [risk score](risk.md) hunts for.
- **Big and simple**: usually configuration, data tables, generated code.
  Boring, and boring is fine.
- **Small**: usually safe. Watch only for "code golf", where three lines do
  the work of thirty and nobody can read them.

On [Monolog](../getting-started/your-first-analysis.md), the analysis counts
14 936 production lines, of which 3 041 are logical. The biggest file,
`Logger.php`, is 751 lines: five times the size of an average file in the
project, which is exactly why the other metrics keep an eye on it.

## See it on your code

```bash
ast-metrics analyze .
```

The `Size` block of the summary gives the project totals, split between
production and test code. In the HTML report, the code map on the overview
page draws every class as a bubble whose **size is its LOC**: the big bubbles
are your big files, no table needed.

## Keep it in check

The `volume` ruleset turns size into [lint rules](../ci/linting-architecture.md):

```bash
ast-metrics ruleset add volume
```

It gives you `max_loc` and `max_logical_loc` per file, `max_loc_by_method`,
`max_methods_per_class`, `max_parameters_per_method` and `max_nested_blocks`,
with thresholds you can adjust in `.ast-metrics.yaml`. On a pull request,
[`ast-metrics review`](../getting-started/review-changes.md) only flags the
files your branch made bigger than the rules allow.
