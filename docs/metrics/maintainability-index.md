---
description: "The maintainability index is a 0-100 score of how easy code is to change, combining volume, complexity and comments."
---

# Maintainability Index

The maintainability index (MI) answers: **if I had to change this file
tomorrow, how much would it hurt?** It compresses three measurements into one
score from 0 to 100:

- **Halstead volume**: how much vocabulary the code juggles (operators,
  operands, their repetitions).
- **[Cyclomatic complexity](cyclomatic-complexity.md)**: how many execution
  paths run through it.
- **[Lines of code](volume.md)**: how physically big it is.

A short, simple, documented file scores near 100. A long file full of branches
scores badly, whatever the language.

## How to read it

| Score | Rating | Meaning |
|-------|--------|---------|
| **85-100** | 🟢 | Easy to change. Touch it without fear. |
| **65-84** | 🟡 | Moderate. Changes need attention, not heroism. |
| **< 65** | 🔴 | Hard to change safely. Refactor before you need to. |

Two habits make this metric useful rather than decorative:

**Read the worst files, not the average.** A project average of 81, like
[Monolog](../getting-started/your-first-analysis.md)'s, is reassuring, but the
actionable information is the 5 classes that score below 65. On Monolog the
worst is `LineFormatter.php` at 63: that's the file where a "quick change"
takes an afternoon.

**Watch the trend.** A file whose MI drifts from 80 to 70 over six months is
telling you where technical debt is accumulating, long before anyone says the
word "rewrite". This is what
[`ast-metrics review`](../getting-started/review-changes.md) automates on
every pull request: it compares your branch to its base and flags the files
whose maintainability got worse.

## The exception to know

Some code is irreducibly dense: parsers, cryptographic routines, numeric
algorithms. A low MI there describes the domain, not the developer. Don't
chase green on a Fourier transform; do chase it on controllers, services and
everything a teammate edits weekly.

## See it on your code

```bash
ast-metrics analyze .
```

The `Maintainability` block opens the summary: the project index, and the
count of classes below the 65 threshold. The HTML report shows the score per
class, sortable, on the Classes page. To enforce a floor, the `architecture`
ruleset ships a `min_maintainability` rule for your
[`.ast-metrics.yaml`](../ci/linting-architecture.md):

```bash
ast-metrics ruleset add architecture
```
