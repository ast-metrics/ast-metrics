# Running AST Metrics for the first time

> If you haven't installed AST Metrics yet, please refer to the [installation guide](./install.md).

Point AST Metrics at the directory that holds your source code:

```bash
ast-metrics analyze /var/www/my-project
```

The analysis takes a few seconds, then a summary is printed in your terminal:
maintainability, estimated bug probability, coupling, size, test isolation, and the
hotspots worth refactoring first.

![The Hotspots section of the terminal summary](../images/capture-hotspots-cli.png)

Nothing is written to disk unless you ask for a report.

## The interactive dashboard

Prefer to explore? Add `--tui` to open a full-screen dashboard in your terminal:

```bash
ast-metrics analyze --tui /var/www/my-project
```

Navigate with the arrow keys, press `Enter` to expand a section, `Ctrl+F` to search,
and `Esc` or `Ctrl+C` to quit.

You can also run `ast-metrics` with no argument at all: an interactive menu walks
you through the available actions.

> In CI, or when the output is piped or redirected, AST Metrics automatically falls
> back to plain text: no flag needed.

## Generating an HTML Report

You can also generate a static HTML report to share with your team or keep as an artifact.

```bash
ast-metrics analyze /path/to/project --report-html=./report
```

This creates a `report` directory containing the full analysis, opening with a
plain-language verdict on your codebase.

![The HTML report overview, with a plain-language verdict](../images/report-overview.png)

> Next step: [understand what the numbers mean](./understand.md), or
> [generate reports in other formats](./generate-reports.md) (JSON, Markdown, SARIF,
> OpenMetrics).
