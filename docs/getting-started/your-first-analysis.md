---
description: "A hands-on tutorial: analyze Monolog with AST Metrics, read the real output line by line, and find the code worth refactoring."
---

# Tutorial: your first analysis

The best way to understand what AST Metrics tells you is to run it on code you
did not write. In this tutorial you will analyze [Monolog](https://github.com/Seldaek/monolog),
one of the most installed PHP libraries in the world, and read the results
together, number by number.

It takes about ten minutes. You need `git`, a terminal, and
[AST Metrics installed](install.md), nothing else. Monolog is PHP, but you
won't read a line of PHP here: the point is learning to read the metrics, and
they look exactly the same for Go, Python, TypeScript, Rust, Java or C#.

## 1. Grab a real project

Clone Monolog with its full history:

```bash
git clone https://github.com/Seldaek/monolog.git
cd monolog
```

Don't use `--depth 1` here. AST Metrics reads the git history to compute churn
(how often files change) and the bus factor (who knows what). A shallow clone
would blind those metrics.

## 2. Run the analysis

```bash
ast-metrics analyze .
```

A few seconds later, a summary lands in your terminal. Let's read it block by
block. The numbers below come from a real run (v0.43 on Monolog's main branch,
yours may differ slightly).

## 3. Read the verdict, top to bottom

### Maintainability: the health check

```text
Maintainability
  Maintainability index              81  (moderate)
  Classes below the 65 threshold     5 of 111 measured
```

One number to start with: **81 out of 100**. Monolog is a healthy codebase,
which is what you would expect from a library this popular. Only 5 classes out
of 111 fall below the warning threshold. If you remember one number from an
analysis, make it this one: it blends size, complexity and documentation into
a single health score.

### Complexity: where the logic piles up

```text
Complexity
  Cyclomatic complexity (total)      3247
    per class (avg / max)            14.06 / 72.00
    per method (avg / max)           2.44 / 20.00
```

The average method makes **2.44 decisions** (ifs, loops, cases). That's low,
and low is good: simple methods are easy to test and hard to break.

But look at the max per class: **72**. One class concentrates 72 decision
points. Which one? The HTML report (we generate it in a minute) has a Classes
page where every column sorts. Do it and one name tops the complexity column:
`Logger.php`, the heart of the library, 751 lines, complexity 72. Is that
a problem? Not necessarily. `Logger` is Monolog's front door, everything goes
through it, and it is heavily tested. A metric is a question, not a verdict:
"this class carries a lot of logic, does someone have it under control?" Here
the answer is yes.

### Size: the shape of the project

```text
Size
  Files                              217
    production                       125
    test                             92
  Production lines of code (LOC)     14936
  Test lines of code (LOC)           13352
```

Almost as much test code as production code. That ratio alone tells you a lot
about how Monolog survived 15 years of changes.

### Architecture: the shape in five lines

Inside the `Coupling` block, five lines describe the whole structure:

```text
  Communities detected               6
    largest one                      32 classes
    dependencies kept inside         38%
    cycles between communities       1
    commits agreeing with them       67%
```

Monolog's 115 classes naturally form **6 communities**: groups that depend on
each other more than on the rest, found from the dependency graph alone,
never from folder names. One **cycle** ties three of them together, and 67%
of last year's commits stayed within one community: the boundaries mostly
match how people actually work on the code. The report turns these five lines
into a full page, with a map and the exact dependencies to cut; we'll open it
in a minute.

### Tests: what the suite actually protects

```text
Tests
  Classes covered by a test          74.4%  (96 / 129)
  Isolation score                    57  (Semi-isolated)
  God tests                          15
  Classes without any test           33
```

- **74.4%** of production classes are referenced by at least one test. The
  other 33 have no test at all: not "low coverage", *zero* tests.
- The **isolation score** of 57 says the average test drags a few
  collaborators along: a failure points at a neighborhood, not always at a
  single class.
- **15 god tests** touch five or more classes each: when one of those fails,
  you get to guess which of its many dependencies broke.

No test was executed to compute this. It's all static analysis, which is why
it takes seconds. The [Test Quality](../metrics/test-quality.md) page explains
each number.

### Hotspots: where to look first

The summary ends with the list you should read first:

```text
Hotspots (top 5 of 21 files at risk)
  1.43  src/Monolog/Handler/StreamHandler.php  (MI 66, 5 commits)
  1.08  src/Monolog/Handler/RotatingFileHandler.php  (MI 72, 5 commits)
  0.93  src/Monolog/Handler/TelegramBotHandler.php  (MI 82, 4 commits)
  0.75  src/Monolog/Formatter/JsonFormatter.php  (MI 78, 3 commits)
  0.71  src/Monolog/Formatter/LineFormatter.php  (MI 63, 2 commits)
```

This is complexity crossed with git activity, and it changes how you
prioritize. `Logger.php`, our complexity champion, is **not** on the list: it
barely changes anymore, so its complexity is dormant. `StreamHandler.php` is
less complex but modified five times recently: complex code that people keep
touching is where the next bug gets written. If you were about to refactor
Monolog, the metrics just handed you the priority order.

That's the core skill of reading an analysis: **a scary number on frozen code
matters less than a mediocre number on code your team edits every week.**

## 4. Open the visual report

The terminal gives you the verdict; the HTML report lets you explore it:

```bash
ast-metrics analyze --report-html=report --open-html .
```

Three pages are worth your first visit:

- **Overview**: a plain-language paragraph about the codebase, followed by the
  code map: one bubble per class, size is lines of code, color is complexity,
  the ring is recent git activity. Big red ringed bubbles are your hotspots.
- **Architecture → Natural groups**: the architecture stated in plain words.
  On Monolog the verdict reads "Some of these communities cannot change
  alone", names the three communities locked in a cycle, calls the shared
  kernel what it is (a centre of gravity: 7 classes receive 56% of all
  dependencies), and lists where to start: the first cut frees a community
  for the price of 3 references. The
  [Architecture Map](../metrics/architecture-map.md) page teaches you to read
  it.
- **Test quality**: the god tests and untested classes, sorted so the worst
  offenders come first.

## 5. Now, your codebase

Run the same command at the root of a project you know well:

```bash
ast-metrics analyze .
```

Then check your instincts against the numbers:

- Does the hotspot list match the files you already dread touching? (It
  usually does. Now you have evidence.)
- Is your untested-classes count a surprise?
- Does the maintainability index confirm what your gut says about the oldest
  module?

Nothing leaves your machine: the analysis, the git history reading and the
report generation are all local.

## Where to go next

- [Review your changes](review-changes.md): on a pull request,
  `ast-metrics review` compares your branch to its base and flags only what
  got worse. This is the command that turns metrics into a daily habit.
- [The metrics guide](../metrics/index.md): every number from this tutorial,
  explained with thresholds and refactoring advice.
- [Run it in CI](../ci/github-actions.md): one line of YAML and every PR gets
  this analysis automatically.
