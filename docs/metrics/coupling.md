---
description: "Afferent coupling, efferent coupling and instability: how entangled your components are and which way dependencies point."
---

# Coupling & Instability

Coupling measures how dependent classes are on each other. High coupling makes code rigid and fragile.

## Afferent Coupling (Ca)
**"Who uses me?"**
- The number of classes that depend on this class.
- **High Ca**: This class is **Critical** or **Responsible**.
- Examples: Core domain entities, Utility classes, Shared libraries.
- **Risk**: If you change this class, you might break many things (high impact).

## Efferent Coupling (Ce)
**"Who do I use?"**
- The number of classes this class depends on.
- **High Ce**: This class is **Dependent**.
- Examples: Orchestrators, Facades.
- **Risk**: This class is fragile because it breaks if any of its dependencies change.

## Instability (I)
Instability is a ratio between 0 and 1 derived from coupling.

`I = Ce / (Ca + Ce)`

### 0: Stable
**I am used by many, but I use no one.**
- Hard to change because many depend on it.
- Should be very robust and abstract.
- Example: `String`, `Integer`, Core Interfaces.

### 1: Unstable
**I use many, but nobody uses me.**
- Easy to change because nobody depends on it.
- Can be concrete and volatile.
- Example: Controllers, CLI Commands, Scripts.

!!! tip "The Stable Dependencies Principle (SDP)"

    Dependencies should point in the direction of stability.
    A component should only depend on components that are more stable than itself.
    
    **Unstable (Variable)** → **Stable (Abstract)**
