---
description: "Volume metrics count lines of code, logical lines and comments: the baseline every other metric builds on."
---

# Volume Metrics

## What is it?
Volume metrics are the simplest form of measurement: **counting things**.

- **LOC (Lines of Code)**: The total number of lines in a file.
- **LLOC (Logical Lines of Code)**: The number of executable statements (ignoring comments and whitespace).
- **CLOC (Comment Lines of Code)**: The number of comment lines.

## Why it matters?
It might seem basic, but **Volume is the metric that correlates most strongly with defects**.
Statistically, the more code you have, the more bugs you will have. It's a law of nature in software engineering.

!!! tip "Use Volume as a baseline"

    A class with high complexity is bad. A *huge* class with high complexity is worse.
    Always look at other metrics in the context of volume.

## How to read it?
- **High Volume + High Complexity**: 🚩 **Red Flag**. Hard to maintain, prone to bugs.
- **High Volume + Low Complexity**: Often data structures or configuration. Usually safe.
- **Low Volume**: Generally safe, unless it's "code golf" (overly clever one-liners).
