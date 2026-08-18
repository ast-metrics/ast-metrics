---
description: "Enforce complexity, coupling and volume rules with the AST Metrics linter and its .ast-metrics.yaml configuration."
---

# Rulesets & Linting

AST Metrics allows you to enforce rules on your codebase (Linting). You can check complexity, coupling, volume, and more.

## Creating the configuration file

Rules live in an `.ast-metrics.yaml` file at the root of your project. Create it with:

```bash
ast-metrics init
```

You now have a documented starting point to edit, either by hand or by importing
rulesets.

## Managing Rulesets (CLI)

The easiest way to add rules is to use the `ruleset` command. It allows you to import pre-defined sets of rules.

### Available Rulesets

You can list available rulesets with:

```bash
ast-metrics ruleset list
```

| Ruleset | Description |
|---------|-------------|
| **architecture** | Architecture-related constraints (e.g., coupling) |
| **volume** | Volume metrics (e.g., lines of code) |
| **complexity** | Complexity metrics (e.g., cyclomatic complexity) |
| **golang** | Golang-specific best practices and API hygiene |

### Installing a Ruleset

To add a ruleset to your configuration:

```bash
ast-metrics ruleset add architecture
ast-metrics ruleset add volume
```

### Detailed Rules

#### 🏗️ Architecture Ruleset
`ast-metrics ruleset add architecture`

| Rule Name | Description |
|-----------|-------------|
| **coupling** | Checks for forbidden coupling between packages |
| **max_afferent_coupling** | Checks the afferent coupling of files/classes |
| **max_efferent_coupling** | Checks the efferent coupling of files/classes |
| **min_maintainability** | Checks the maintainability of the code |
| **no_circular_dependencies** | Detect circular dependencies between classes |
| **max_responsibilities** | Maximum number of responsibilities (LCOM) per class |
| **no_god_class** | Avoid God Classes (too many methods/properties) |
| **no_community_cycles** | Fails when communities depend on each other in a cycle |
| **max_community_cross_share** | Maximum share (%) of dependencies crossing from one community to another |
| **no_cross_community_dependencies** | Fails on every dependency crossing between communities; meant to be frozen with `ast-metrics baseline` |

#### 📏 Volume Ruleset
`ast-metrics ruleset add volume`

| Rule Name | Description |
|-----------|-------------|
| **max_loc** | Checks the lines of code in a file |
| **max_logical_loc** | Checks the logical lines of code in a file |
| **max_loc_by_method** | Checks the lines of code by method/function |
| **max_logical_loc_by_method** | Checks the logical lines of code by method/function |
| **max_methods_per_class** | Maximum number of methods per class |
| **max_switch_cases** | Maximum number of cases in switch statements |
| **max_parameters_per_method** | Maximum number of parameters per method |
| **max_nested_blocks** | Maximum nesting depth of blocks |
| **max_public_methods** | Maximum number of public methods per class |

#### 🧠 Complexity Ruleset
`ast-metrics ruleset add complexity`

| Rule Name | Description |
|-----------|-------------|
| **max_cyclomatic** | Checks the cyclomatic complexity of functions |

#### 🐹 Golang Ruleset
`ast-metrics ruleset add golang`

| Rule Name | Description |
|-----------|-------------|
| **no_package_name_in_method** | Do not include the package name in exported function or method identifiers |
| **max_nesting** | Limit nested depth of control structures (if/for/switch) |
| **max_file_size** | Limit file size (LOC) |
| **max_files_per_package** | Limit number of source files per package (excluding doc.go) |
| **slice_prealloc** | Check if slice preallocation is used |
| **context_missing** | Check if context is missing in function arguments |
| **context_ignored** | Check if context is ignored |

---

## Manual Configuration

You can also manually edit the `.ast-metrics.yaml` file at the root of your project.

```yaml
sources:
  - ./internal
exclude: []
reports:
  html: ./build/report
  markdown: ./build/report.md
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
      context_missing: true
      context_ignored: true
```

Check your rules with:

```bash
ast-metrics lint
```

The command exits with a non-zero status as soon as a requirement is violated, which
makes it usable as-is in a pipeline. Add `--report-sarif=lint.sarif` to publish the
violations to a platform that reads SARIF.

## Starting on a legacy codebase: the baseline

On an existing project, the first `ast-metrics lint` can easily report hundreds of
violations. You are not going to fix them all today, and they should not block your
pipeline. Snapshot them instead:

```bash
ast-metrics baseline
```

This writes an `.ast-metrics-baseline.yaml` file recording every current violation.
Commit it: from now on, `ast-metrics lint` (and `ast-metrics ci`) ignores the
recorded violations and only fails on new ones.

Re-run `ast-metrics baseline` whenever you want to shrink the file as you pay off
the debt. If you keep the file somewhere else, point the linter at it with
`ast-metrics lint --baseline=<path>`.

## Freezing the community boundaries

Three project rules read the [communities](../metrics/community-detection.md)
the analysis finds on the dependency graph. They all pass when the project has
fewer than two communities.

```yaml
requirements:
  rules:
    architecture:
      no_community_cycles: true
      max_community_cross_share: 20
      no_cross_community_dependencies: true
```

`no_community_cycles` fails once per cycle and names the arrows to cut, lightest
first. `max_community_cross_share` fails when more than the given percentage of
the dependencies cross from one community to another, the shared kernel left
aside. `no_cross_community_dependencies` fails on every single crossing, so it
only makes sense with a baseline:

```bash
ast-metrics baseline   # accept today's crossings
ast-metrics lint       # fails only on a crossing added since
```

Each crossing is filed under the file of the class that depends, with the
message `Foo depends on Bar, which sits in another community`. Community names
stay out of the message on purpose: they change as the code moves, and the
baseline recognizes an entry by rule, file and message. The communities page of
the HTML report shows how many crossings are frozen and how many are new.

!!! note "Baseline or review?"
    The two mechanisms are complementary. The **baseline** freezes today's violations
    in a committed file, so `lint` stays green on legacy code. [`ast-metrics
    review`](../getting-started/review-changes.md) compares your branch with its base
    on the fly and needs no stored file: it is what gates your pull requests.