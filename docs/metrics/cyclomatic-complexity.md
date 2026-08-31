---
description: "Cyclomatic complexity counts the independent paths through your code: what it means, thresholds, and how to reduce it."
---

# Cyclomatic Complexity

Cyclomatic complexity answers the question: **how many different journeys can
the execution take through this function?** Every `if` opens a fork, every
loop a detour, every `&&` a shortcut. Each journey is something you must
understand when you read the code, and something you must exercise when you
test it.

## Count it yourself, once

A function starts at 1 (the straight road with no decisions). Then each
`if`, `elseif`, loop, `case`, `catch`, `&&` and `||` adds one:

```typescript
function shippingCost(order: Order): number {  // 1: the function itself
  if (order.isDigital) {                       // +1 → 2
    return 0;
  }
  let cost = 5;
  for (const item of order.items) {            // +1 → 3
    if (item.fragile && item.weight > 10) {    // +2 → 5 (the if, the &&)
      cost += 12;
    }
  }
  return cost;
}
```

Complexity: **5**. And that number has a very concrete meaning: you need at
least 5 test cases to cover every branch of this function. A function with a
complexity of 40 needs at least 40. That's why complexity is really a
testability metric wearing a readability costume.

![Low, high and critical complexity, seen as mazes](../images/ill-ccn.webp)

## Thresholds

| Score | Risk | What to do |
|-------|------|------------|
| **1-10** | Low | Nothing. This is what good code looks like. |
| **11-20** | Moderate | Acceptable, but test it thoroughly. |
| **21-50** | High | Refactor: split it into smaller functions. |
| **> 50** | Critical | Effectively untestable. Plan a rewrite. |

Keep the averages low and worry about the maximums. On
[Monolog](../getting-started/your-first-analysis.md), the average method has a
complexity of 2.44, textbook healthy, while the `Logger` class concentrates
72. One number describes the codebase, the other points at the class that
needs the most careful tests.

## How to reduce it

**Return early.** Deep nesting multiplies paths; flipping conditions into
guard clauses flattens them:

=== "Before: three levels of nesting"

    ```typescript
    function pay(o: Order) {
      if (o != null) {
        if (o.isPaid == false) {
          if (o.total > 0) {
            charge(o);
          }
        }
      }
    }
    ```

=== "After: guard clauses"

    ```typescript
    function pay(o: Order) {
      if (o == null) return;
      if (o.isPaid) return;
      if (o.total <= 0) return;

      charge(o);
    }
    ```

**Extract methods.** A complexity-30 function is usually three complexity-10
functions wearing a trench coat. Each extracted piece gets a name, and names
are documentation.

**Replace `switch` with polymorphism.** A `switch` on a type code that appears
in several places is complexity paid repeatedly; moving each branch into its
own class pays it once.

## See it on your code

```bash
ast-metrics analyze .
```

The `Complexity` block of the summary gives the totals, averages and maximums.
In the HTML report, the code map colors every bubble by complexity, and the
Classes page sorts by it. To enforce a ceiling:

```bash
ast-metrics ruleset add complexity
```

adds a `max_cyclomatic` rule to your
[`.ast-metrics.yaml`](../ci/linting-architecture.md). Existing offenders can
be [baselined](../ci/linting-architecture.md), so
[`ast-metrics review`](../getting-started/review-changes.md) only blocks the
functions your pull request made worse.
