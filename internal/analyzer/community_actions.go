package analyzer

import (
	"fmt"
	"slices"
	"strings"
)

// CommunityAction is one concrete thing to do first, derived from the findings.
type CommunityAction struct {
	// Kind is one of "cut", "move", "gather", "invert".
	Kind string
	// Title is imperative and names the classes or communities: "Cut Component\Gamification → Organization".
	Title string
	// Detail says why in one sentence and names the references or classes that carry it.
	Detail string
	// Effort is the size of the change, in plain words: "2 references", "3 classes".
	Effort      string
	Communities []string
	Units       []string
}

// Bounds of the actions.
const (
	// At most this many actions in all, and this many cuts among them.
	maxActions    = 3
	maxCutActions = 2
	// At most this many carriers named in an action, and kept as its units.
	maxActionCarriers = 3
	maxActionUnits    = 5
)

// actionsOf derives, from the findings, the few things to do first, the most
// valuable first: cut a back edge, move a file next to the one it changes
// with, gather the entries of an exposed community, free the kernel from a
// community it references. At most maxActions.
func actionsOf(cm *CommunityMetrics) []CommunityAction {
	actions := []CommunityAction{}
	if cm == nil || len(cm.Communities) == 0 {
		return actions
	}
	byID := map[string]*Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}
	unitWord := "classes"
	if cm.Granularity == GranularityNamespace {
		unitWord = "packages"
	}
	name := func(id string) string {
		if c := byID[id]; c != nil {
			return c.ShortName
		}
		return id
	}

	// 1. Cut the back edges, lightest first.
	backs := []CommunityEdge{}
	for _, e := range cm.Edges {
		if e.Back && !e.Shared {
			backs = append(backs, e)
		}
	}
	slices.SortStableFunc(backs, func(x, y CommunityEdge) int {
		if x.Weight != y.Weight {
			return x.Weight - y.Weight
		}
		if c := compareIDs(x.From, y.From); c != 0 {
			return c
		}
		return compareIDs(x.To, y.To)
	})
	for i, e := range backs {
		if i >= maxCutActions {
			break
		}
		carriers := carriersBetween(cm, e.From, e.To)
		actions = append(actions, CommunityAction{
			Kind:        "cut",
			Title:       fmt.Sprintf("Cut %s → %s", name(e.From), name(e.To)),
			Detail:      fmt.Sprintf("It closes a cycle: %s, carried by %s.", plural(e.Weight, "reference", "references"), describeCarriers(cm, carriers)),
			Effort:      plural(e.Weight, "reference", "references"),
			Communities: []string{e.From, e.To},
			Units:       unitsOfCarriers(carriers),
		})
	}

	// 2. Move the file that keeps changing with one of another community.
	for _, f := range cm.Findings {
		if f.Kind != "history-crossed" || len(f.Communities) < 2 {
			continue
		}
		a, b := byID[f.Communities[0]], byID[f.Communities[1]]
		if a == nil || b == nil {
			break
		}
		var pair UnitLink
		for _, cc := range a.ChangesWith {
			if cc.ID == b.ID {
				pair = cc.Pair
			}
		}
		if pair.Weight == 0 {
			break
		}
		units := []string{}
		if unit := unitOfLabel(cm, a, pair.From); unit != "" {
			units = append(units, unit)
		}
		if unit := unitOfLabel(cm, b, pair.To); unit != "" {
			units = append(units, unit)
		}
		actions = append(actions, CommunityAction{
			Kind:        "move",
			Title:       fmt.Sprintf("Move %s next to %s", pair.From, pair.To),
			Detail:      fmt.Sprintf("They changed together in %s this year while sitting in %s and %s.", plural(pair.Weight, "commit", "commits"), a.ShortName, b.ShortName),
			Effort:      "1 file",
			Communities: []string{a.ID, b.ID},
			Units:       units,
		})
		break
	}

	// 3. Gather the entries of the most exposed community.
	for _, f := range cm.Findings {
		if f.Kind != "exposed" || len(f.Communities) < 1 {
			continue
		}
		c := byID[f.Communities[0]]
		if c == nil {
			break
		}
		top := c.Exposed
		if len(top) > maxActionCarriers {
			top = top[:maxActionCarriers]
		}
		actions = append(actions, CommunityAction{
			Kind:        "gather",
			Title:       fmt.Sprintf("Gather the entries of %s", c.ShortName),
			Detail:      fmt.Sprintf("%d of its %d %s are used from outside; the most used, %s, could front the others.", c.ExposedCount, c.Size, unitWord, joinNames(labelsOf(top, cm.Labels))),
			Effort:      fmt.Sprintf("%d %s exposed", c.ExposedCount, unitWord),
			Communities: []string{c.ID},
			Units:       top,
		})
		break
	}

	// 4. Free the kernel from the community it references the most.
	if cm.Shared != nil && len(cm.Shared.Uses) > 0 {
		l := cm.Shared.Uses[0]
		carriers := carriersBetween(cm, SharedID, l.ID)
		actions = append(actions, CommunityAction{
			Kind:        "invert",
			Title:       fmt.Sprintf("Free the shared kernel from %s", name(l.ID)),
			Detail:      fmt.Sprintf("The kernel references %s %s: those references should be inverted or moved out.", name(l.ID), plural(l.Weight, "time", "times")),
			Effort:      plural(l.Weight, "reference", "references"),
			Communities: []string{SharedID, l.ID},
			Units:       unitsOfCarriers(carriers),
		})
	}

	if len(actions) > maxActions {
		actions = actions[:maxActions]
	}
	return actions
}

// carriersBetween lists the references from one community to another,
// heaviest first, then by name.
func carriersBetween(cm *CommunityMetrics, from, to string) []UnitReference {
	carriers := []UnitReference{}
	for _, r := range cm.CrossReferences {
		if cm.NodeToCommunity[r.From] == from && cm.NodeToCommunity[r.To] == to {
			carriers = append(carriers, r)
		}
	}
	slices.SortStableFunc(carriers, func(x, y UnitReference) int {
		if x.Weight != y.Weight {
			return y.Weight - x.Weight
		}
		if x.From != y.From {
			return strings.Compare(x.From, y.From)
		}
		return strings.Compare(x.To, y.To)
	})
	return carriers
}

// describeCarriers writes the heaviest carriers as "A → B (2), C → D (1) and
// 3 more".
func describeCarriers(cm *CommunityMetrics, carriers []UnitReference) string {
	parts := []string{}
	for i, r := range carriers {
		if i >= maxActionCarriers {
			return strings.Join(parts, ", ") + fmt.Sprintf(" and %d more", len(carriers)-i)
		}
		parts = append(parts, fmt.Sprintf("%s → %s (%d)", cm.Labels[r.From], cm.Labels[r.To], r.Weight))
	}
	return strings.Join(parts, ", ")
}

// unitsOfCarriers lists the units on the from side of the heaviest carriers:
// the ones to change.
func unitsOfCarriers(carriers []UnitReference) []string {
	units := []string{}
	for _, r := range carriers {
		if slices.Contains(units, r.From) {
			continue
		}
		units = append(units, r.From)
		if len(units) == maxActionUnits {
			break
		}
	}
	return units
}

// unitOfLabel finds, among the members of a community, the unit bearing a
// label; an empty string when none does. The members are sorted: the first
// one wins.
func unitOfLabel(cm *CommunityMetrics, c *Community, label string) string {
	for _, unit := range c.Units {
		if cm.Labels[unit] == label {
			return unit
		}
	}
	return ""
}
