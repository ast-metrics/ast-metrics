# Using AST Metrics in GitHub Actions

The [AST Metrics GitHub Action](https://github.com/ast-metrics/action-ast-metrics) prevents architectural regressions in your pull requests.

On each pull request, it compares your branch with the target branch and reports **only new or worsened issues**. Existing debt is never reported, so the check stays actionable on a legacy codebase. Typical findings:

- a method that became too complex;
- a strong maintainability drop on modified code;
- a significant coupling increase on a modified file;
- new violations of your configured architecture rules (for example, forbidden dependencies);
- notable improvements, so the report is not only negative.

On `push` events, the action runs a full analysis instead, and publishes the report in the job summary with the HTML report as an artifact.

!!! info "No account, no data sent"

    The analysis runs entirely on the runner. Your code never leaves the GitHub infrastructure.

## Quick start

Create a `.github/workflows/ast-metrics.yml` file with the following content:

```yaml
name: AST Metrics
on:
  pull_request:

permissions:
  contents: read
  pull-requests: write   # optional: allows the action to comment on the pull request

jobs:
  ast-metrics:
    runs-on: ubuntu-latest
    steps:
      - uses: ast-metrics/action-ast-metrics@v2
```

That is it. Each pull request now gets a check with a short, stable summary:

```text
AST Metrics: quality gate passed

3 file(s) changed, 0 new critical issue(s), 2 other regression(s)

Regressions:
- [MEDIUM] CheckoutService::pay (src/Checkout/CheckoutService.php:42)
      Cyclomatic complexity: 8 -> 15 (threshold: 10)
      Suggested action: Extract smaller, well-named functions to reduce decision points

Existing debt is not reported. Methodology v1.0
```

Add `push:` to the triggers if you also want a full analysis on your main branch.

## Blocking a pull request

By default the check never fails: it only informs. Once your team trusts the signal, make the gate blocking with `fail-on`:

```yaml
- uses: ast-metrics/action-ast-metrics@v2
  with:
    fail-on: high
```

## Architecture rules

If your repository has an [`.ast-metrics.yaml`](./linting-architecture.md) configuration with requirements (forbidden dependencies, complexity budgets, and so on), the review also reports **new** violations introduced by the pull request, and only those.

## Inputs

| Input | Default | Description |
|---|---|---|
| `version` | `latest` | AST Metrics version to install. Pinning (for example `v0.41.1`) is recommended for reproducible checks. `local` reuses an `ast-metrics` binary already present in the `PATH` instead of downloading a release. |
| `directory` | `.` | Directory to analyze. |
| `base` | base branch of the PR | Git reference to compare with. |
| `fail-on` | `never` | Fail the check when a regression of at least this severity is introduced: `high`, `medium`, `any` or `never`. |
| `comment` | `true` | Post and update a single comment on the pull request (best effort). |
| `annotations` | `auto` | Annotate the changed files with the new findings, directly from the workflow (no GitHub Advanced Security required). `auto` enables them unless `sarif` is enabled, since code scanning already annotates the same findings. `true` forces both channels, `false` disables them. |
| `sarif` | `false` | Upload regressions to GitHub code scanning. Alerts are reported by the GitHub Advanced Security bot under the Security tab; use `annotations` for plain quality annotations. |
| `sarif-max-level` | `warning` | Ceiling for the level of the SARIF results: `error`, `warning` or `note`. Code scanning fails its own check as soon as a new `error` alert appears in the diff, so the default keeps it informative like `fail-on: never`. Set to `error` to let code scanning block the pull request. |
| `html-artifact` | `auto` | Upload the full HTML report as an artifact: `true` on push, `false` on pull requests by default. |
| `max-findings` | `5` | Maximum number of regressions displayed in the summary and the comment. |

## Permissions

The action degrades gracefully depending on the permissions you grant:

| Feature | Required permission | Behavior when missing |
|---|---|---|
| Check status and job summary | none | Always works, including pull requests from forks. |
| Inline annotations (`annotations`) | none | Always works, including pull requests from forks. |
| Pull request comment | `pull-requests: write` | Skipped with a notice; the report stays in the job summary. |
| SARIF upload (`sarif: true`) | `security-events: write` (and GitHub Advanced Security on private repositories) | Skipped without failing the build. |

!!! note "Pull requests from forks"

    Pull requests coming from forks always run with a read-only token. The comment is skipped, and the job summary is used instead.

??? info "Migrating from v1"

    - The `push` behavior is unchanged: full analysis, job summary, HTML artifact.
    - The `report_html_directory` and `report_markdown_filename` inputs were removed. Reports are now written to the runner temporary directory and published as a job summary or an artifact.
    - The pull request review mode is new: add `pull_request:` to your workflow triggers to enable it.
