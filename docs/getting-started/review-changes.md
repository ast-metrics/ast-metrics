# Reviewing your changes

Most quality tools greet you with thousands of pre-existing issues. You close the
window and never come back.

`ast-metrics review` takes the opposite approach: it compares your branch with its
base and reports **only what you made worse**. Existing debt is never mentioned. On
a twenty-year-old codebase, the first run is just as actionable as on a new one.

```bash
ast-metrics review
```

No configuration, no baseline to store, no account. AST Metrics checks out the base
version in a temporary worktree, analyzes both, and diffs the results.

## Reading the output

```
AST Metrics found regressions

0 new critical issue(s), 2 other regression(s), 1 improvement(s)

Summary:
- ⚠️  Complexity: +12
- ➖ Ease of maintenance: -
- ➖ Outgoing dependencies: -
- ⚠️  Probability of bugs: +0.14

Regressions:
- [MEDIUM] internal/engine/php/php_halstead_test.go (internal/engine/php/php_halstead_test.go:47)
      LOC too high in method TestPhpAccessorsAreNotPerfectlyMaintainable(): got 33 (max: 30)
- [MEDIUM] internal/engine/php/php_halstead_test.go (internal/engine/php/php_halstead_test.go:16)
      Function/method name 'TestPhpOperatorsOfAPlainReturn()' contains package name 'php'

Improvements:
- TreeSitterAdapter::ExtractOperatorsOperands (internal/engine/csharp/tree_sitter_csharp_adapter.go:398): Cyclomatic complexity: 14 -> 2
```

Four things are worth noting:

- the **Summary** checklist always shows the same four metrics. Each line is the net
  change introduced by your branch; the icon judges the direction (a rising
  maintainability is good news, a rising complexity is not), so the numbers can stay
  honest about what actually changed;
- every finding names a **file and a line**, so it is actionable without opening the
  report;
- **improvements are reported too**. A review that only ever says "you made things
  worse" is a review people learn to ignore;
- by default the command **never fails**. It informs. You decide when to make it
  blocking.

## Choosing the base

Without `--base`, AST Metrics tries `origin/main`, `origin/master`, `main`, then
`master`. Pass it explicitly when your default branch differs, or to review against
any branch, tag or commit:

```bash
ast-metrics review --base=develop
ast-metrics review --base=HEAD~5
```

## Making it blocking

Once your team trusts the signal, turn the review into a gate with `--fail-on`:

```bash
ast-metrics review --fail-on=high
```

| Value | The command fails when... |
|---|---|
| `never` (default) | never. The review only informs. |
| `high` | a high-severity regression appears. |
| `medium` | a medium or high regression appears. |
| `any` | any regression appears, including low ones. |

Start with `never` for a couple of weeks, look at what the tool actually reports on
your real pull requests, then raise the bar. Turning on `any` from day one is the
surest way to have the check disabled a month later.

## Other output formats

```bash
ast-metrics review --format=markdown          # ready to paste in a pull request
ast-metrics review --report-json=review.json  # the full result, nothing truncated
ast-metrics review --report-sarif=review.sarif
```

The text and Markdown outputs show the five most important regressions; raise the
limit with `--max-findings`, or read the JSON report for the complete list.

!!! tip "Architecture rules count too"
    If your project has an [`.ast-metrics.yaml`](../ci/linting-architecture.md) with
    requirements (forbidden dependencies, complexity budgets, and so on), the review
    also reports the **new** violations your branch introduces, and only those.

## In your pipeline

This is exactly what runs on your pull requests when you install the
[GitHub Action](../ci/github-actions.md), which needs a single line of YAML. See also
[GitLab CI](../ci/gitlab-ci.md).
