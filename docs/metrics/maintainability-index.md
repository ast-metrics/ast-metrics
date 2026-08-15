---
description: "The maintainability index is a 0-100 score of how easy code is to change, combining volume, complexity and comments."
---

# Maintainability Index

## What is it?
The Maintainability Index (MI) is a composite score (0-100) designed to indicate how maintainable (easy to support and change) the source code is.

It is calculated using a polynomial equation that combines:
- **Halstead Volume**: Measures the size of the implementation (vocabulary and length).
- **Cyclomatic Complexity**: Measures the control flow complexity.
- **Lines of Code**: Measures the physical size.

## How to read it?
It gives you a single number to judge a file's health at a glance.

| Score | Rating | Meaning |
|-------|--------|---------|
| **85-100** | 🟢 A | **Excellent**. Easy to maintain. |
| **65-84** | 🟡 B | **Good**. Moderate maintainability. |
| **< 65** | 🔴 C | **Bad**. Hard to maintain. Consider refactoring. |

## Limitations
!!! note "Context matters"

    A complex algorithm (like a parser or a mathematical computation) might naturally have a lower score. But for standard business logic, controllers, or services, you should aim for green.

The Maintainability Index is best used as a **trend metric**. If it drops over time, your technical debt is increasing.
