---
description: "The concepts behind AST Metrics output: abstract syntax trees, dependency graphs, and the insights derived from both."
---

# Understanding the output

You don't need a PhD in Computer Science to use AST Metrics, but understanding a few concepts will help you get the most out of it.

## 1. Everything is a Tree (AST)
First, you need to understand that any source code can be represented as a tree. This tree is called an [Abstract Syntax Tree (AST)](https://en.wikipedia.org/wiki/Abstract_syntax_tree).

For example, this code:

```python
while b ≠ 0:
    if a > b:
        a := a - b
    else:
        b := b - a
return a
```

Can be represented as this tree:

<figure markdown="span">
  ![Wikipedia](https://upload.wikimedia.org/wikipedia/commons/thumb/c/c7/Abstract_syntax_tree_for_Euclidean_algorithm.svg/531px-Abstract_syntax_tree_for_Euclidean_algorithm.svg.png){ align=center }
  <figcaption>The AST of the code, from Wikipedia</figcaption>
</figure>

**AST Metrics analyzes this tree** to calculate complexity, volume, and other code-level metrics.

## 2. The Architecture is a Graph
Just like code forms a tree, **dependencies between your files form a graph**.

- When Class A uses Class B, there is a link.
- When Class B uses Class C, the chain continues.

AST Metrics analyzes this graph to find:

- **Communities**: Groups of classes that work together.
- **Cycles**: Circular dependencies that lock your system.
- **Coupling**: How tightly connected your components are.

## 3. From Math to Insights
By combining the AST analysis (micro-view) and the Graph analysis (macro-view), AST Metrics uses mathematical models to uncover hidden truths about your project:

- **Bus Factor**: Who is indispensable?
- **Risk**: Where are bugs likely to hide?
- **Architecture Violations**: Where is the code not doing what you think it is?

---

<div class="grid cards" markdown>

-   :material-chart-bar: **Ready to dive deep?**

    Check out the detailed guide for every metric available in AST Metrics.

    [Explore the Metrics Guide :arrow_right:](../metrics/index.md)

</div>