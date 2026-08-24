<p align="center" style="text-align:center">
<img alt="AST Metrics" src="https://raw.githubusercontent.com/ast-metrics/ast-metrics/main/docs/logo-ast-metrics-condensed.png" height="200px"/>
</p>

<p align="center" style="text-align:center">
<b>No server. No account. One binary.</b>
<br />
Code is written faster than it can be reviewed. AST Metrics measures your codebase
(complexity, architecture, coupling, bus factor, test quality) and, on every pull request,
flags only what got worse.
<br />
Deterministic: same code, same verdict. Works offline: no data leaves your machine.
<br />
Fast: 20,000+ lines of code analyzed per second, on a laptop.
<br />
<br />
<code>Go</code> · <code>PHP</code> · <code>Python</code> · <code>Rust</code> · <code>Java</code> · <code>C#</code> · <code>TypeScript</code> · <code>C++ (initial/basic)</code>
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

You get a summary right in your terminal: maintainability, estimated bug probability, coupling, and the hotspots worth refactoring first. Add `--report-html=<dir>` when you want a full report, or `--tui` to explore the results in a full-screen dashboard. Nothing is written to disk unless you ask for it.

> Docker image, npm, pip, Composer, `.deb`/`.rpm` packages and manual downloads: see the [installation instructions](https://ast-metrics.dev/getting-started/install/).

## Review a pull request, without the noise

```console
ast-metrics review
```

It compares your branch with its base and reports only new or worsened findings: a new overly complex function, a coupling regression, a class that lost maintainability. Legacy code stays quiet. Unlike AI code reviewers, it is deterministic and free: same code, same verdict, no per-seat pricing. Add `--fail-on=high` when you want it to block the merge.

➡️ [Reviewing your changes](https://ast-metrics.dev/getting-started/review-changes/)

## What you get

| | |
|---|---|
| **PR review** | `ast-metrics review` flags only what got worse against the base branch |
| **Architectural analysis** | Coupling, instability, community detection: catch design drift early |
| **Code metrics** | Cyclomatic complexity, maintainability index, lines of code |
| **Activity metrics** | Commit history and bus factor: know who owns what |
| **Linter** | Enforce thresholds on complexity, coupling, volume and architecture |
| **CI/CD ready** | GitHub Actions, GitLab CI, any pipeline; exits non-zero on violations |
| **Report formats** | HTML dashboard, JSON, Markdown, SARIF, OpenMetrics |
| **MCP server** | Give AI coding agents architectural awareness |

<p align="center" style="text-align:center">
<a href="https://analyze.ast-metrics.dev/ast-metrics/ast-metrics"><img alt="The interactive dependency graph: hubs, natural communities and circular dependencies at a glance" src="https://raw.githubusercontent.com/ast-metrics/ast-metrics/main/docs/report-dependencies.png" /></a>
<br />
<i>The dependency graph: hubs, natural communities and circular dependencies at a glance.</i>
</p>

## Lint your architecture

```bash
ast-metrics init                      # create a .ast-metrics.yaml config file
ast-metrics ruleset add architecture  # pick rulesets (volume, complexity...)
ast-metrics lint
```

Thresholds live in your YAML config: maximum complexity, coupling limits, forbidden dependencies between components, size limits per method... Legacy codebase with hundreds of violations? Run `ast-metrics baseline` once: it snapshots today's violations, and `lint` only fails on new ones.

➡️ [Rulesets, thresholds and baseline](https://ast-metrics.dev/ci/linting-architecture/)

## Run it in CI

`ast-metrics ci` runs the linter, generates every report (HTML, JSON, Markdown, SARIF, OpenMetrics) and exits non-zero when violations are found. On GitHub, a single step is enough:

```yaml
- uses: ast-metrics/action-ast-metrics@v2
```

On each pull request, the action runs `ast-metrics review` and comments with only the new or worsened findings.

➡️ [CI/CD guides: GitHub Actions, GitLab CI](https://ast-metrics.dev/ci/github-actions/)

## Give your AI agent architectural awareness

AI coding agents read code linearly. Running as an [MCP server](https://modelcontextprotocol.io/), AST Metrics gives Claude Code, Cursor or Copilot on-demand access to complexity, coupling, dependencies and risk, so you can ask things like *"What are the riskiest files to refactor?"* or *"What would break if I change the UserService class?"*.

```bash
ast-metrics mcp .
```

➡️ [MCP server setup and tools](https://ast-metrics.dev/ai/mcp-server/)

## Supported languages

Go, PHP, Python, Rust, Java, C#, TypeScript, and initial/basic syntax-level C++ support.

C++ discovery includes `.cpp`, `.cc`, `.cxx`, `.hpp`, `.hh`, and `.hxx`. A generic
`.h` file is claimed when its content looks like C++ (`class`, `namespace`,
`template`, `::`), so plain C headers are left alone.

The C++ engine maps Tree-sitter syntax into the common metrics representation.
Its syntax-level limits, in short:

- no preprocessing: both branches of an `#ifdef` are counted, and code wrapped
  in unexpanded macros is only partially analyzed;
- lambdas are not scopes: their complexity is attributed to the enclosing
  function;
- out-of-class definitions keep their written qualification
  (`ns::Class::method`), but without semantic analysis an unqualified type
  reference resolves against the namespace of the class that uses it, never
  across files.

## Contributing

Discussions, bug reports and pull requests are welcome: [start here](.github/CONTRIBUTING.md). AST Metrics is open-source software [licensed under the MIT license](LICENSE).

## Support the project

AST Metrics is built and maintained on free time. If it saved you some of yours:

- ⭐ **Star the repository**. It costs nothing and it is how most developers discover the tool.
- ❤️ **[Become a sponsor](https://github.com/sponsors/Halleck45)**. Sponsorship directly funds maintenance time and the addition of new languages and metrics.
