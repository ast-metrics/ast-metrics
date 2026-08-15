---
description: "The risk score combines complexity and churn to point at the hotspots where bugs are most likely to hide."
---

# Risk Score

## What is it?
The Risk score is **the probability that the code needs refactoring**.

It is a composite metric that identifies "Hotspots" in your codebase.

## How is it calculated?
The Risk score is calculated based on two main factors:
1.  **Complexity**: How hard is the code to understand? (Cyclomatic Complexity)
2.  **Activity**: How often is this code changed? (Git Churn)

`Risk = Complexity × Churn`

## Why it matters?
- **Complex code that never changes** is not a high risk. It works, leave it alone.
- **Simple code that changes often** is fine.
- **Complex code that changes often** is a **Time Bomb**. This is where bugs are most likely to be introduced.

## How to use it?
Run `ast-metrics analyze`: the summary printed in your terminal ends with a `Hotspots` section listing the files with the highest Risk score, with their maintainability index and recent commit count.

![The Hotspots section of the terminal summary](../images/capture-hotspots-cli.png)

The HTML report tells the same story visually: the code map on the overview page draws one bubble per class, where size is the lines of code, color the complexity, and the ring the recent git activity. Big, red, ringed bubbles are your time bombs.

!!! tip "Prioritize Refactoring"

    Don't just refactor everything. Focus your energy on the High Risk files first. These will give you the best Return on Investment (ROI).
