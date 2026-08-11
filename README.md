<p align="center" style="text-align:center">
<img alt="AST Metrics" src="https://raw.githubusercontent.com/ast-metrics/ast-metrics/main/docs/logo-ast-metrics-condensed.png" height="200px"/>
</p>

<p align="center" style="text-align:center">
<b>No server. No account. One binary.</b>
<br />
AST Metrics analyzes your codebase (complexity, architecture, coupling, bus factor...) and runs anywhere.
<br />
Drop it in any CI. Works offline. Nothing to install, no SaaS, no data leaves your machine.
<br />
Fast: 20,000+ lines of code analyzed per second, on a laptop.
<br />
<br />
<code>Go</code> · <code>PHP</code> · <code>Python</code> · <code>Rust</code> · <code>Java</code> · <code>C#</code> · <code>TypeScript</code>
</p>
<br />

<p align="center" style="text-align:center">
<a href="https://github.com/ast-metrics/ast-metrics/actions/workflows/test.yml"><img src="https://github.com/ast-metrics/ast-metrics/actions/workflows/test.yml/badge.svg" alt="CI"></a>
<img src="https://img.shields.io/github/v/release/ast-metrics/ast-metrics" alt="GitHub Release">
<a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
<a href="https://github.com/sponsors/Halleck45"><img src="https://img.shields.io/static/v1?label=Sponsor&amp;message=%E2%9D%A4&amp;logo=GitHub&amp;color=%23fe8e86" alt=""></a>
<img src="https://img.shields.io/github/downloads/ast-metrics/ast-metrics/total" alt="GitHub all releases">
<a href="https://goreportcard.com/report/github.com/ast-metrics/ast-metrics"><img src="https://goreportcard.com/badge/github.com/ast-metrics/ast-metrics" alt="Go Report Card"></a>
<a href="https://codecov.io/gh/ast-metrics/ast-metrics"><img src="https://codecov.io/gh/ast-metrics/ast-metrics/branch/main/graph/badge.svg" alt="codecov"></a>
<a href="https://pkg.go.dev/github.com/ast-metrics/ast-metrics"><img src="https://pkg.go.dev/badge/github.com/ast-metrics/ast-metrics.svg" alt="Go Reference"></a>
<a href="https://github.com/avelino/awesome-go"><img src="https://awesome.re/mentioned-badge-flat.svg" alt="Mentioned in Awesome Go"></a>
<a href="https://analyze.ast-metrics.dev/ast-metrics/ast-metrics"><img src="https://img.shields.io/badge/AST--Metrics-report-181717?logo=github" alt="AST-Metrics report"></a>
</p>

<p align="center" style="text-align:center">
<a href="https://ast-metrics.dev/">Documentation</a> | <a href=".github/CONTRIBUTING.md">Contributing</a>
</p>

<p align="center" style="text-align:center">
<a href="https://analyze.ast-metrics.dev/ast-metrics/ast-metrics"><img alt="The AST Metrics report: a plain-language verdict, with scores for complexity, maintainability, test isolation and bus factor" src="https://raw.githubusercontent.com/ast-metrics/ast-metrics/main/docs/report-overview-embed.png" /></a>
<br />
<i>AST Metrics analyzing itself. <a href="https://analyze.ast-metrics.dev/ast-metrics/ast-metrics">Explore this report live</a>, or <a href="https://analyze.ast-metrics.dev">try it on any public repository</a>, without installing anything.</i>
</p>

<br />

## Getting Started

Install with Homebrew (macOS, Linux):

```console
brew install ast-metrics/tap/ast-metrics
```

or with the install script (any platform, downloads an `./ast-metrics` binary in the current directory):

```console
curl -fsSL https://install.ast-metrics.dev | sh
```

Then analyze your project:

```console
ast-metrics analyze /path/to/your/code
```

A summary is printed on the standard output: maintainability, estimated bug probability, coupling, then the usual counters and the hotspots worth refactoring first. Ask for a durable report when you need one, and nothing is ever written to disk unless you do:

```console
ast-metrics analyze --report-html=<directory> /path/to/your/code
```

Run `ast-metrics` with no argument to open the interactive menu, or add `--tui` to any analysis to explore the results in the full-screen dashboard. Anything piped, redirected or run in CI gets plain output.

> Docker image, `.deb`/`.rpm` packages and manual downloads: see the detailed [installation instructions](https://ast-metrics.dev/getting-started/install/).

## What you get

| | |
|---|---|
| **Architectural analysis** | Community detection, coupling, instability — catch design drift early |
| **Code metrics** | Cyclomatic complexity, maintainability index, lines of code |
| **Activity metrics** | Commit history, bus factor — know who owns what |
| **Linter** | Enforce thresholds on coupling, complexity, LOC per method |
| **CI/CD ready** | GitHub Actions, GitLab CI, any pipeline — exits non-zero on violations |
| **Multiple report formats** | HTML dashboard, JSON, Markdown, SARIF, OpenMetrics |
| **MCP server** | Give AI coding agents architectural awareness via Model Context Protocol |

<p align="center" style="text-align:center">
<a href="https://analyze.ast-metrics.dev/ast-metrics/ast-metrics"><img alt="The interactive dependency graph: hubs, natural communities and circular dependencies at a glance" src="https://raw.githubusercontent.com/ast-metrics/ast-metrics/main/docs/report-dependencies.png" /></a>
<br />
<i>The dependency graph: hubs, natural communities and circular dependencies at a glance.</i>
</p>

[Read more in the documentation](https://ast-metrics.dev/)


## Linting your code

Run:

```bash
# create a .ast-metrics.yaml config file
ast-metrics init 

# Add ruleset to your config file
ast-metrics ruleset add architecture
ast-metrics ruleset add volume
ast-metrics ruleset list # see the list of available rulesets

# Run the linter
ast-metrics lint
```

You can declare thresholds in your YAML config (*Lines of code per method, Coupling, Maintainability...*).

Example:

```yaml
requirements:
  rules:
    architecture:
      coupling:
        forbidden:
          - from: Controller
            to: Repository
          - from: Repository
            to: Service
      max_afferent_coupling: 10
      max_efferent_coupling: 10
      min_maintainability: 70
    volume:
      max_loc: 1000
      max_logical_loc: 600
      max_loc_by_method: 30
      max_logical_loc_by_method: 20
    complexity:
      max_cyclomatic: 10
    golang:
      no_package_name_in_method: true
      max_nesting: 4
      max_file_size: 1000
      max_files_per_package: 50
      slice_prealloc: true
      ignored_error: true
      context_missing: true
      context_ignored: true
```

This makes it **easy to enforce architecture and quality at scale**.

Run `ast-metrics ruleset list` to see the list of available rulesets. Then `ast-metrics ruleset add <ruleset-name>` to apply a ruleset to your project.

### "I have hundreds of violations on my existing codebase, now what?"

Generate a baseline: it snapshots today's violations so only new ones get reported.

```bash
ast-metrics baseline
```

Commit the resulting `.ast-metrics-baseline.yaml`, then `ast-metrics lint` only fails on violations introduced afterwards. Re-run `ast-metrics baseline` anytime to shrink it as you clean things up.

## CI usage

Use the dedicated CI command to run lint and generate all reports in one go:

```bash
ast-metrics ci [options] /path/to/your/code
```

Notes:
- This command runs the linter first, then generates HTML, Markdown, JSON, OpenMetrics and SARIF reports.
- If any lint violations are found, the command exits with a non-zero status but still produces the reports.
- The previous alias `analyze --ci` is deprecated and will display a warning. Please migrate to `ast-metrics ci`.

## Github Action

Create a `.github/workflows/ast-metrics.yml` file in your project with the following content:

```yaml
name: "AST Metrics"
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

On each pull request, the action runs `ast-metrics review` and publishes the result in the check summary and, when permissions allow it, as a single updated comment. On `push` events it runs a full analysis instead. See [action-ast-metrics](https://github.com/ast-metrics/action-ast-metrics) for all options (`fail-on`, `sarif`, `html-artifact`...).


## MCP Server for AI agents

AI coding agents (Claude Code, Cursor, Copilot...) read code linearly and lack architectural awareness. AST Metrics can act as an [MCP server](https://modelcontextprotocol.io/) to give them on-demand access to complexity, coupling, dependency graphs, community detection, risk scoring, and test quality — without reading every file.

```bash
ast-metrics mcp .
```

This starts a stdio MCP server exposing 8 tools:

| Tool | Purpose |
|---|---|
| `analyze_project` | High-level overview: languages, complexity, maintainability, top risks |
| `get_file_metrics` | Detailed metrics for a specific file |
| `find_risky_code` | Files/classes with highest risk scores |
| `find_complex_code` | Functions/classes above a complexity threshold |
| `get_dependencies` | Dependency subgraph around a component |
| `get_coupling` | Afferent/efferent coupling for a component |
| `get_communities` | Architectural community detection and metrics |
| `get_test_quality` | Test isolation, traceability, god tests, orphan classes |

Once configured, just talk to your AI agent naturally. For example:

*"What are the riskiest files to refactor?"* · *"Show me the dependencies of the UserService class — what would break if I change it?"* · *"Are there complex classes with no tests?"* · *"I need to work on src/billing/invoice.go, what should I know?"*

To use it with Claude Code or any MCP-compatible agent, add a `.mcp.json` at your project root:

```json
{
  "mcpServers": {
    "ast-metrics": {
      "command": "ast-metrics",
      "args": ["mcp", "."]
    }
  }
}
```

## Supported languages

+ ✅ **Golang** `any version`
+ ✅ **Python** `Python 2, Python 3`
+ ✅ **Rust** `any version`
+ ✅ **PHP** `<= PHP 8.5`
+ ✅ **Java** `any version`
+ ✅ **C#** `any version`
+ ✅ **TypeScript** `any version`
+ 🕛 **Flutter**
+ 🕛 **C++**
+ 🕛 **Ruby**

## License

AST Metrics is open-source software [licensed under the MIT license](LICENSE)


## Contributing

AST Metrics is an actively evolving project.

We welcome discussions, bug reports, and pull requests.

➡️ Start [contributing here](.github/CONTRIBUTING.md)

## Support the project

AST Metrics is built and maintained on free time. If it saved you some of yours:

- ⭐ **Star the repository**. It costs nothing and it is how most developers discover the tool.
- ❤️ **[Become a sponsor](https://github.com/sponsors/Halleck45)**. Sponsorship directly funds maintenance time and the addition of new languages and metrics.
