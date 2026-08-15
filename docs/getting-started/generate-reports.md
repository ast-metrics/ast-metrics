---
description: "Generate HTML, Markdown, JSON, SARIF and OpenMetrics reports from a single ast-metrics analyze run."
---

# Generate reports

All report options belong to the `analyze` command, and several can be combined in
a single run:

```bash
ast-metrics analyze --report-html=./report --report-json=./report.json .
```

!!! tip "Generating everything at once"
    In a pipeline, use [`ast-metrics ci`](../ci/tips.md) instead: it runs the
    linter, then produces every report in one go.

## 🌐 HTML report

The HTML report is the one to share with your team. Every page opens on a sentence
that states what the analysis found, backed by the few figures that support it:

- a dashboard, with the distribution of your metrics
- one page per concern (complexity, coupling, class cohesion, risk, bus factor)
- a **class explorer**, sortable and filterable, exposing every computed metric
- a **dependency graph** you can search and zoom, which highlights circular
  dependencies
- one view per analyzed folder, next to the global and per-language views

To generate a report, run the following command in your terminal:

```bash
ast-metrics analyze --report-html=<report-directory> /path/to/your/project
```

Where `<report-directory>` is the directory where the report will be saved. Add
`--open-html` to open it in your browser as soon as it is ready.

## 📄 Markdown report

AST Metrics can also generate Markdown reports. The reports provide an overview of the codebase, in markdown format.

To generate a report, run the following command in your terminal:

```bash
ast-metrics analyze --report-markdown=<report-file.md> /path/to/your/project
```

Where `<report-file.md>` is the file where the report will be saved.

## 📄 JSON report

AST Metrics can also generate JSON reports. The reports provide an overview of the codebase, in JSON format.

To generate a report, run the following command in your terminal:

```bash
ast-metrics analyze --report-json=<report-file.json> /path/to/your/project
```

Where `<report-file.json>` is the file where the report will be saved.

## 📄 SARIF report

AST Metrics can generate [SARIF](https://sarifweb.azurewebsites.net/) (Static Analysis Results Interchange Format) reports. SARIF is a standard format for the output of static analysis tools, widely supported by security and code quality platforms like GitHub Advanced Security, Azure DevOps, and many CI/CD tools.

To generate a SARIF report, run the following command in your terminal:

```bash
ast-metrics analyze --report-sarif=<report-file.sarif> /path/to/your/project
```

Where `<report-file.sarif>` is the file where the report will be saved.

Use `--sarif-max-level` to cap the severity of the results (`error`, `warning` or
`note`). GitHub code scanning fails its own check as soon as a new `error` alert
appears, so keeping the ceiling at `warning` makes the upload informative rather
than blocking.

### Use Cases

SARIF reports are particularly useful for:

- **GitHub Code Scanning**: Upload SARIF files to GitHub to display code quality issues directly in pull requests
- **CI/CD Integration**: Many CI/CD platforms support SARIF for automated code quality checks
- **Security Analysis**: SARIF is the standard format for security scanning tools
- **Tool Interoperability**: Share analysis results between different static analysis tools

## 📄 OpenMetrics report (Gitlab CI)

[OpenMetrics](../ci/gitlab-ci.md) is a standard for metrics exposition. AST Metrics can generate OpenMetrics reports, which can be easily integrated into your CI/CD pipeline, like GitLab CI.

To generate an OpenMetrics report, run the following command in your terminal:

```bash
ast-metrics analyze --report-openmetrics=<report-file.openmetrics> /path/to/your/project
```

Where `<report-file.openmetrics>` is the file where the report will be saved.
