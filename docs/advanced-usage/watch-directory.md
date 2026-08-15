---
description: "Use ast-metrics analyze --watch to monitor a directory and refresh the analysis on every file change."
---

# Watching a directory

It is sometimes convenient to be able to monitor a directory to detect changes in files, without having to rerun the analysis.

AST Metrics offers an option to watch a directory and display the results in real time.

To watch a directory, run the following command in your terminal:

```console
ast-metrics analyze --watch /path/to/your/project
```