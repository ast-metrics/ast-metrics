---
description: "LCOM4 measures class cohesion: whether the methods of a class belong together, and how to split a class that does too much."
---

# LCOM4 (Lack of Cohesion of Methods)

<video controls preload="none" src="../../animations/lcom.webm" title="How LCOM4 detects a class doing several unrelated things"></video>

## What is it?

LCOM4 measures how well the methods in a class belong together. It checks if methods use the same fields.

It answers the question: **"Is this class doing one thing, or multiple unrelated things?"**

![what is Lack of Cohesion of Methods](../images/ill-lcom4.webp)


## How it works
Imagine a graph where:
- **Nodes** are methods.
- **Edges** connect methods if they access the same field or call each other.

LCOM4 is the number of connected components in this graph.

- **LCOM4 = 1**: **Cohesive**. All methods are related. The class acts as a single unit.
- **LCOM4 > 1**: **Not Cohesive**. The class is doing too many things. It might be two or more classes stuck together.
- **LCOM4 = 0**: Empty class or no methods.

## Visualizing LCOM4
Imagine drawing lines between methods and the fields they use.
- If everything is connected, LCOM4 = 1.
- If you have two separate islands of connections, LCOM4 = 2.

## How to fix it?
!!! warning "Refactoring Opportunity"

    If LCOM4 > 1, you can usually split the class into two separate classes without breaking anything.

1.  Identify the "islands" of methods/fields.
2.  Extract each island into a new class.
3.  Inject the new classes into the original one (or replace usages).
