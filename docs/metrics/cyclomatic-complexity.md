---
description: "Cyclomatic complexity counts the independent paths through your code: what it means, thresholds, and how to reduce it."
---

# Cyclomatic Complexity


<video controls preload="none" src="../../animations/Cyclomatic-complexity.webm" title="How cyclomatic complexity counts the paths through a function"></video>

## What is it?

Cyclomatic Complexity (often denoted as `V(G)`) measures the **number of independent paths** through your code.
Think of your code as a maze. Every control structure adds a turn or a branch in the maze.

It counts:

- `if`, `else`, `elseif`
- `while`, `for`, `foreach`
- `case`, `default`
- `catch`
- Boolean operators (`&&`, `||`)


## Why it matters?
High complexity means:
1.  **Harder to understand**: You can't hold the logic in your head.
2.  **Harder to test**: You need at least one test case per path to cover everything. A complexity of 10 means you need at least 10 unit tests to achieve 100% branch coverage.

![what is Cyclomatic complexity](../images/ill-ccn.webp)



- **Complexity = 1**: A straight road. No decisions.
- **Complexity = 5**: A small neighborhood with a few turns.
- **Complexity = 50**: A chaotic bowl of spaghetti.

## Thresholds

!!! important "Keep it under 10."

| Score | Risk | Recommendation |
|-------|------|----------------|
| **1-10** | Low | Simple code. Good. |
| **11-20** | Moderate | More complex. Needs thorough testing. |
| **21-50** | High | High risk. **Refactor**. Split into smaller methods. |
| **> 50** | Critical | Untestable. **Rewrite**. |

## How to reduce it?
- **Extract Method**: Take a complex part of the logic and move it to a new private method.
- **Early Return**: Use `return` early to avoid deep nesting of `if/else`.
- **Strategy Pattern**: Replace complex `switch` statements with polymorphism.
