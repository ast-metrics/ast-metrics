---
description: "Integrate AST Metrics into GitLab CI with OpenMetrics reports and gate merge requests on new findings."
---

# Using AST Metrics in GitLab CI

AST Metrics is compatible with the [OpenMetrics](https://github.com/prometheus/OpenMetrics/blob/main/specification/OpenMetrics.md) standard. This means that you can easily integrate AST Metrics into your GitLab CI/CD pipeline.

Create a `.gitlab-ci.yml` file with the following content:

```yaml
stages:
  - test

test:
    stage: test
    image: ubuntu:latest
    script:
        - curl -fsSL https://install.ast-metrics.dev|sh
        - ./ast-metrics ci --report-openmetrics=metrics.txt .
```

This configuration downloads the latest version of AST Metrics and generates an OpenMetrics report for the current directory. This report is saved in the `metrics.txt` file, and will be available as a [metrics report](https://docs.gitlab.com/ee/ci/testing/metrics_reports.html) in GitLab.

## Blocking a merge request

On merge requests, [`ast-metrics review`](../getting-started/review-changes.md)
reports only what the branch changed, and returns a non-zero status when a
regression crosses the severity you chose:

```yaml
review:
    stage: test
    image: ubuntu:latest
    script:
        - curl -fsSL https://install.ast-metrics.dev|sh
        - ./ast-metrics review --base=origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME --fail-on=high .
    rules:
        - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

The job needs the target branch to exist locally, so make sure the clone is not
shallow (set `GIT_DEPTH: 0`) or fetch it explicitly.