---
description: "Afferent coupling, efferent coupling and instability: how entangled your components are and which way dependencies point."
---

# Coupling & Instability

Coupling answers: **when I change this class, what else moves?** Two numbers
describe it, and they are just the two directions of the same arrow.

## Afferent coupling (Ca): who uses me?

Ca counts the classes that depend on this one. Say `Money` is used by 40
classes: its Ca is 40.

- High Ca means **responsibility**: change `Money` and 40 classes feel it.
- Typical high-Ca citizens: domain entities, core interfaces, shared
  utilities.
- The requirement that comes with it: be stable, be tested, break nothing.

## Efferent coupling (Ce): whom do I use?

Ce counts the classes this one depends on. A `CheckoutController` that pulls
in twelve services has a Ce of 12.

- High Ce means **fragility**: this class breaks when any of its twelve
  dependencies changes.
- Typical high-Ce citizens: controllers, orchestrators, facades.

## Instability: which way should the arrow point?

Instability folds both into a ratio from 0 to 1:

```
I = Ce / (Ca + Ce)
```

- **I close to 0**: everyone depends on me, I depend on nobody. Stable.
  `Money` again: hard to change, and that's fine, because it shouldn't.
- **I close to 1**: I depend on many, nobody depends on me. Volatile.
  A controller: easy to change, and that's exactly what you want from it.

Neither end is bad. What's bad is the *direction of dependencies* between
them, which is the Stable Dependencies Principle:

!!! tip "Depend toward stability"
    A component should only depend on components more stable than itself.
    Controllers (unstable) depending on entities (stable): healthy. An entity
    depending on a controller: every UI whim now shakes your domain model.

A concrete smell: a class with **both** high Ca and high Ce. Half the codebase
depends on it, and it depends on half the codebase. Congratulations, you've
found the class nobody dares to touch.

On [Monolog](../getting-started/your-first-analysis.md), seven classes
(`LogRecord`, `Level`, `FormatterInterface`...) receive 56% of all
dependencies: extreme afferent coupling, largely deliberate in a library
whose core contracts everyone implements. The
[architecture analysis](architecture-map.md) sets those classes apart as the
shared kernel, and says out loud when such a centre of gravity stops looking
deliberate.

## See it on your code

```bash
ast-metrics analyze .
```

The `Coupling` block of the summary gives the averages and the instability.
The HTML report's Dependencies page shows the graph itself, and the
[Architecture Map](architecture-map.md) shows how coupling shapes your layers.

To enforce limits, the `architecture` ruleset provides `max_afferent_coupling`
and `max_efferent_coupling`, plus a `coupling` rule to forbid specific
dependencies by name (for instance, `Controller` must never reach
`Repository` directly). See
[Rulesets & Linting](../ci/linting-architecture.md):

```bash
ast-metrics ruleset add architecture
```
