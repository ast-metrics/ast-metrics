---
description: "The bus factor measures how concentrated knowledge is in your codebase, computed from git history and grouped by community."
---

# Bus Factor

<video controls preload="none" src="../../animations/bus-factor.webm" title="How the bus factor measures knowledge concentration"></video>

## What is it?
The Bus Factor is a risk metric that answers: **"How many team members have to be hit by a bus for the project to stall?"**

It estimates the concentration of knowledge in your codebase.

- **Bus Factor = 1**: **Critical Risk**. Only one person understands a key part of the system. If they leave, that knowledge is lost.
- **Bus Factor = High**: **Healthy**. Knowledge is shared among multiple developers.

## How is it calculated?
AST Metrics analyzes the **Git history** (authorship of lines).
1.  It calculates the "ownership" of each file (who wrote the most lines).
2.  It aggregates this ownership by **Community** (not just folders).
3.  If a community is 80%+ owned by a single person, the Bus Factor for that community is 1.

## How to improve it?
!!! important "Share Knowledge"

    If you have a low Bus Factor:
    
    1.  **Pair Programming**: Have the expert pair with others on that component.
    2.  **Code Reviews**: Ensure others review changes to critical files.
    3.  **Documentation**: Write down the implicit knowledge.
