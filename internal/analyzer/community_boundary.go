package analyzer

import (
	"maps"
	"slices"
	"strings"
)

// surfaceOfCommunities reads, for each community, the members the rest of
// the code reaches into and the foreign units it reaches out to. The shared
// kernel counts as an outside user of a community, but not as a foreign use:
// leaning on the kernel is what every community does. The kernel itself gets
// no surface, it is exposed by definition.
func surfaceOfCommunities(cm *CommunityMetrics, g *unitGraph) {
	for _, c := range cm.Communities {
		if c.Shared {
			continue
		}
		exposed := map[string]int{}
		foreign := map[string]int{}
		for _, unit := range c.Units {
			for a, w := range g.In[unit] {
				if from, placed := cm.NodeToCommunity[a]; placed && from != c.ID {
					exposed[unit] += w
				}
			}
			for b, w := range g.Out[unit] {
				if to, placed := cm.NodeToCommunity[b]; placed && to != c.ID && to != SharedID {
					foreign[b] += w
				}
			}
		}
		c.Exposed = sortedByWeight(exposed)
		c.ExposedCount = len(c.Exposed)
		if c.Size > 0 {
			c.ExposedShare = float64(c.ExposedCount) / float64(c.Size)
		}
		c.ForeignUses = sortedByWeight(foreign)
		c.ForeignUsesCount = len(c.ForeignUses)
		if len(c.ForeignUses) > maxForeignUses {
			c.ForeignUses = c.ForeignUses[:maxForeignUses]
		}
	}
}

// sortedByWeight lists the keys of a weight map, heaviest first, then by name.
func sortedByWeight(weight map[string]int) []string {
	return slices.SortedFunc(maps.Keys(weight), func(x, y string) int {
		if weight[x] != weight[y] {
			return weight[y] - weight[x]
		}
		return strings.Compare(x, y)
	})
}

// communitiesReaching counts the communities, the shared kernel included,
// holding a unit that references the given one from outside its community.
func communitiesReaching(unit string, cm *CommunityMetrics, g *unitGraph) int {
	own := cm.NodeToCommunity[unit]
	reached := map[string]struct{}{}
	for a := range g.In[unit] {
		if from, placed := cm.NodeToCommunity[a]; placed && from != own {
			reached[from] = struct{}{}
		}
	}
	return len(reached)
}

// borderOfCommunities tells the members a community is sure of from the ones
// on its border, by running the detection again at other resolutions. A unit
// moved in a run when it left its community to side with another: fewer than
// half of its fellow members are still with it, and the units of other
// communities around it outnumber them. Read that way, two communities
// merging at a coarser resolution move nobody (they are still whole, only
// closer), and a community breaking into blocks of its own members moves
// nobody either (the blocks are sub-modules, not defections). A unit that
// moved in at least one run sits on the border; the confidence of a
// community is the share of its members that stayed in every run.
//
// The shared kernel is left out of every run, the way it was left out of the
// map, and never sits on a border. The runs read the graph the way the map
// did: same excluded units, same folding of the small communities.
func borderOfCommunities(cm *CommunityMetrics, g *unitGraph, shared map[string]bool) {
	stable := map[string]int{} // unit -> number of runs it stayed in
	sizeOf := map[string]int{}
	for _, c := range cm.Communities {
		sizeOf[c.ID] = c.Size
	}
	for _, resolution := range borderResolutions {
		alternative := louvainOn(g, shared, resolution)
		labels := make(map[string]int, len(alternative.Community))
		maps.Copy(labels, alternative.Community)
		labels = foldSmallCommunities(g, labels)
		// how many members of each community of the map land on each label,
		// and how many placed units in all
		together := map[string]map[int]int{}
		onLabel := map[int]int{}
		for unit, label := range labels {
			id, placed := cm.NodeToCommunity[unit]
			if !placed || id == SharedID {
				continue
			}
			if together[id] == nil {
				together[id] = map[int]int{}
			}
			together[id][label]++
			onLabel[label]++
		}
		for unit, label := range labels {
			id, placed := cm.NodeToCommunity[unit]
			if !placed || id == SharedID {
				continue
			}
			// the unit itself is counted in both: the others are one less
			fellows := together[id][label] - 1
			strangers := onLabel[label] - 1 - fellows
			if 2*fellows >= sizeOf[id]-1 || fellows >= strangers {
				stable[unit]++
			}
		}
	}
	runs := len(borderResolutions)
	stableUnits, placedUnits := 0, 0
	for _, c := range cm.Communities {
		if c.Shared {
			continue
		}
		kept := 0
		border := []string{}
		for _, unit := range c.Units {
			if stable[unit] == runs {
				kept++
			} else {
				border = append(border, unit)
			}
		}
		slices.Sort(border)
		c.Border = border
		if c.Size > 0 {
			c.Confidence = float64(kept) / float64(c.Size)
		}
		stableUnits += kept
		placedUnits += c.Size
	}
	if placedUnits > 0 {
		cm.Confidence = float64(stableUnits) / float64(placedUnits)
	} else {
		cm.Confidence = 1
	}
}
