---
description: "Find which parts of your code depend on a library, directly or through other files, with ast-metrics who-uses: the question a vulnerability report or a sovereignty review raises, answered from the dependency graph."
---

# Who depends on this library?

A critical vulnerability is published on a Friday evening. Within hours,
every team asks the same question: *do we use it, and where?* Many took days
to answer in December 2021, when Log4Shell dropped, and the difficulty was
never the flaw itself. It was that few teams knew the real structure of
their own system.

`ast-metrics who-uses` answers that question from the dependency graph:

```console
ast-metrics who-uses log4j ./src
```

## Why a grep is not enough

A grep on the library's name finds the files that import it. That is the
right first move, and it is often wrong by an order of magnitude.

Picture a team that wrapped its logging library in a `Logger` class six years
ago, to be able to swap it one day. Good engineering. The grep returns one
file, and everyone concludes the library is barely used. In fact three
hundred files go through that wrapper, including the ones that log incoming
HTTP headers. The grep found the name; the chain of dependencies is written
nowhere as text, so it never saw it.

A software bill of materials (SBOM) and a composition scanner answer *do we
have this component?* in thirty seconds, and you should have them. They give
you an inventory. The graph answers *who, in our code, depends on it, how
far does that go, and which parts of the system are exposed?* That is a
topology. The two are complementary, and the second one is what decides
where to look first when you cannot audit everything.

The same question comes up outside of security. Knowing what your system
stands on, and how much of it, is a matter of sovereignty: a framework or a
vendor SDK that eighty percent of the code reaches is a dependency you do not
choose anymore. The answer is worth having before the licence changes, the
maintainer leaves or the region it is hosted in becomes a problem.

## How it works

Every engine records the modules a file imports, spelled the way the import
spells them: `org.apache.logging.log4j` in Java, `github.com/sirupsen/logrus`
in Go, `react` in TypeScript, `Monolog\Logger` in PHP, `tracing` in Rust,
`loguru` in Python, `Serilog` in C#. The ones that resolve to no file of the
project are its libraries.

The name you give is matched anywhere in those modules, case-insensitively:
`log4j` finds `org.apache.logging.log4j` and `org.apache.logging.log4j.core`
alike. From the files importing them, AST Metrics follows the dependents
level by level on the file graph: the files depending on an importer, then
the files depending on those, until nothing new is reached. Each file is
reported once, at the shortest distance found.

```console
$ ast-metrics who-uses --limit 3 psr ./src

  21 imported modules matching
    Psr\Http\Message\RequestInterface
    Psr\Http\Message\ResponseInterface
    ...

  38 of 41 files depend on it (93%)
  A file that imports it is one step away; a file depending on that file is two steps away, and so on.

  Import it directly · 29 files
    BodySummarizer.php
    Client.php
    ...
  Depend on a file that imports it · 9 files
    Cookie/FileCookieJar.php
    ...

  Communities reached
    GuzzleHttp    ██████████  21 of 21 files (100%)
    Cookie        ████████░░  4 of 5 files (80%)
```

The distance matters more than the total. In a monolith, the closure from any
widely used module covers most of the project, and a total alone would say
that everything depends on everything. What the levels tell you is where the
exposure is concentrated: the files importing the library, and the ones one
step behind them, are where a review starts.

The last block maps the reached files onto the
[communities](../metrics/community-detection.md) the code forms, the most
exposed first. This is the answer in the words of an architecture diagram
rather than a file list: *billing is entirely exposed, the catalog is not*.

## Options

| Option | Effect |
|---|---|
| `--format json` | The full answer as JSON: matched modules, files per level, scope, communities. |
| `--max-depth N` | Stop N steps past the importing files. `0` (the default) follows to the end. |
| `--limit N` | Files listed per level in the text output, `20` by default. `0` lists them all. |
| `--exclude`, `--config`, `--*-extensions` | The same file selection options as `analyze`. |

Flags go before the library name and the paths. The command exits with a
non-zero status when no imported module matches, so a script can tell
"nobody depends on it" from "the analysis did not run".

## Where else the answer lives

**The JSON report** of `ast-metrics analyze --report-json` carries a
`libraries` section: every imported module with the number of files
importing it (`importers`), the number of files depending on it at any
distance (`reach`), and `standard: true` for the modules of the language
itself (`fmt`, `java.util`, `System.IO`, `std::collections`, PHP's global
classes). A job can watch it over time:

```console
$ jq '.libraries[] | select(.standard | not) | select(.reach > 100) | .module' report.json
```

**The MCP server** exposes the same question as the `who_uses` tool, so an
AI assistant wired to [the server](../ai/mcp-server.md) can answer *what
would be exposed if this package had a flaw?* from the code rather than from
a guess.

## What it does not do

The graph describes the code, not its execution: a possible path is not a
path taken. Static analysis also misses reflection, dependency-injection
containers and dynamic loading, so the answer is an upper bound of what a
flaw can touch, and an incomplete one where the wiring is dynamic. An import
is not a data flow either: the command tells you which files stand on the
library, not whether the tainted input reaches it.

Versions are out of scope on purpose. Whether the log4j you depend on is the
vulnerable release is the job of your SBOM and your composition scanner; this
command tells you how much of your code stands behind the answer.

!!! tip "Forbid a library where it does not belong"

    The [`coupling` rule](../ci/linting-architecture.md) of the linter can
    forbid a dependency from a part of the code, by class or by package:

    ```yaml
    requirements:
      rules:
        architecture:
          coupling:
            forbidden:
              - from: "Domain"
                to: "org\\.apache\\.logging"
    ```
