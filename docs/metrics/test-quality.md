---
description: How AST Metrics measures test isolation, traceability, god tests and untested classes, and how to enforce thresholds.
---

# Test Quality

## What is it?

AST Metrics measures the quality of your test suite by static analysis alone:
no coverage tooling, no instrumentation, no test execution. It parses your test
files, matches the production classes they reference, and answers two
questions:

- **Which production classes are exercised by at least one test?**
- **How focused is each test file?**

Test files are detected by each language's conventions: `_test.go` in Go,
`*.test.ts`, `*.spec.ts` or a `__tests__` directory in TypeScript, `*Test.php`
or a class extending `TestCase` in PHP, and so on.

The results appear in the **Tests** section of the terminal summary, in the
**Test Quality** page of the HTML report, and in the JSON report. AI agents can
read them through the `get_test_quality` [MCP tool](../ai/mcp-server.md).

## The metrics

### SUT fan-out

The number of **distinct production classes a test file touches**, resolved
from its imports and references. A focused unit test touches one class, maybe
two. A test that touches ten classes is really an integration test, whatever
its name says: it can fail for ten unrelated reasons, and it pins ten classes
down at once.

### Isolation score

Each test file gets a score between 0 and 100:

```
isolation = 100 - 10 × fan-out - 5 × depth
```

where `depth` is how far the dependency chains of the touched classes go,
measured by a breadth-first walk through production classes only, capped at 4
levels. The score is clamped between 0 and 100. Touching one class with no
production dependencies scores 90; touching five classes whose dependencies
run two levels deep scores 40.

| Score | Label | Meaning |
|---|---|---|
| 80 to 100 | **Isolated** | The test exercises one small unit. Failures point at the cause. |
| 50 to 79 | **Semi-isolated** | A few collaborators are involved. Still readable, watch the trend. |
| 0 to 49 | **Coupled** | The test drags in a subsystem. Failures need investigation. |

The project-level **isolation score** is the average across all test files,
labelled on the same scale.

### God tests

A test file with a **fan-out of 5 or more**. The report lists them worst first.
God tests are expensive: they break on refactorings that changed nothing
observable, and when they fail, nothing tells you which of their many
dependencies is guilty.

### Traceability

The **percentage of production classes referenced by at least one test file**.
Top-level functions count as classes here, so functional codebases are measured
too.

This is not code coverage. Coverage tells you which lines ran; traceability
tells you which classes have no test at all, without running anything. A class
can be traced and still poorly tested, but an untraced class is untested for
certain.

### Orphan classes

Production classes with **zero tests**, each weighted by how much it matters:

```
weight = cyclomatic complexity × (1 + efferent coupling + afferent coupling)
```

with a minimum of 1. A trivial value object with no test is fine. A complex
class that half the codebase depends on, with no test, is where the next
regression comes from. The report sorts orphans by weight so you fix the
heaviest first.

!!! tip "The report shows 20, the analysis keeps everything"
    The HTML report displays the 20 worst god tests and the 20 heaviest orphan
    classes. The analysis itself keeps the complete lists: a lint rule checks
    every offender, and fixing one never promotes the next one into a "new"
    violation.

## How to improve

- **Split god tests.** One test file per unit under test. If a test needs five
  classes to say anything, the unit boundary is probably wrong, or missing.
- **Cut dependency depth.** Inject interfaces or fakes instead of letting the
  test reach real implementations three levels down. Depth is what turns a
  small change into a distant test failure.
- **Test the heavy orphans first.** Sorting by weight is a priority list:
  complexity times coupling is exactly the code you least want to change blind.
- **Raise traceability gradually.** Going from 40% to 60% by testing the
  heaviest orphans is worth more than going from 90% to 95% on value objects.

## Enforcing thresholds

The `testing` ruleset turns these metrics into lint rules. Add it to your
[`.ast-metrics.yaml`](../ci/linting-architecture.md) with:

```bash
ast-metrics ruleset add testing
```

which writes default thresholds you can then adjust:

```yaml
requirements:
  rules:
    testing:
      min_traceability: 60
      min_isolation_score: 50
      max_god_test_fan_out: 5
      max_orphan_weight: 20
```

| Rule | Fails when... | Severity |
|---|---|---|
| `min_traceability` | the percentage of tested classes is below the minimum. | high |
| `min_isolation_score` | the global isolation score is below the minimum. | medium |
| `max_god_test_fan_out` | a test file touches more classes than the maximum. | medium |
| `max_orphan_weight` | an untested class weighs more than the maximum. | medium |

Check the rules locally with `ast-metrics lint`; in CI, the
[review command](../getting-started/review-changes.md) reports only the new
violations your branch introduces.
