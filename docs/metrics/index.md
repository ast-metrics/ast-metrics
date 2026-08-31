---
description: "Overview of every metric AST Metrics computes: volume, complexity, maintainability, risk, coupling, cohesion and more."
---

# Metrics Overview

Every metric in AST Metrics exists to answer one practical question about your
code. You don't need to learn them all: start from the question you're asking,
and read the page that answers it.

If you have never run an analysis, do the
[hands-on tutorial](../getting-started/your-first-analysis.md) first: it walks
through a real analysis of Monolog and teaches you to read the numbers in ten
minutes. Every page below then goes deeper on one metric, always in the same
shape: what it measures, how to read it, and what to do about it.

## "Where should I start refactoring?"

- [**Risk Score**](risk.md) crosses complexity with git activity and points at
  the handful of files where the next bug is statistically waiting. This is
  the first list to read on any codebase.

## "Is this code too complicated?"

- [**Cyclomatic Complexity**](cyclomatic-complexity.md) counts the independent
  paths through a function: every path is something to understand and to test.
- [**Volume**](volume.md) counts lines of code, logical lines and comments.
  Basic, but size is the metric that correlates most strongly with defects.
- [**Maintainability Index**](maintainability-index.md) blends both into a
  single 0-100 health score per file, perfect for trends and thresholds.

## "Is this class well designed?"

- [**Class Cohesion (LCOM4)**](lcom4.md) detects classes that are really two
  or three unrelated classes stuck together.
- [**Coupling & Instability**](coupling.md) measures who depends on whom, and
  whether your dependencies point in a direction that keeps change cheap.

## "Do my tests actually protect me?"

- [**Test Quality**](test-quality.md) measures test isolation, god tests, and
  the production classes that have no test at all, without running a single
  test.

## "Does my architecture match what I think it is?"

- [**Architecture Map**](architecture-map.md) reads your real structure and
  states it in plain words: the communities your classes form, the layered
  map, the cycles with the exact dependencies to cut, and the rules that
  freeze the boundaries in CI.
- [**Community Detection**](community-detection.md) is the deep dive behind
  it: how the communities are found, every finding, the git history read
  against the boundaries.

## "What happens if someone leaves?"

- [**Bus Factor**](bus-factor.md) reads the git history to find the parts of
  the codebase only one person knows.

---

Every metric is available in the terminal summary (`ast-metrics analyze`), the
HTML report (`--report-html`), the JSON report, and to AI agents through the
[MCP server](../ai/mcp-server.md). Most of them can also be enforced as
[lint rules](../ci/linting-architecture.md) with thresholds you choose.
