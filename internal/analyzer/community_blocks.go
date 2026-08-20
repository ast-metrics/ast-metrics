package analyzer

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/graph"
)

// CommunityBlock is a group of communities that hold together at a coarser
// grain: the zoomed-out map. The communities themselves, their numbers and
// their findings do not change; a block only says which of them go together
// when the reader steps back.
type CommunityBlock struct {
	// ID is "b0", "b1"… largest first.
	ID string
	// Name is the name of the largest community of the block, then " + N"
	// when it holds more, e.g. "Organization + 4".
	Name string
	// Communities lists the member community ids, largest first.
	Communities []string
	// Size is the number of units the block holds.
	Size int
}

// blockResolution is the Louvain resolution the blocks are detected at, on
// the graph of the communities. Below 1 the modularity favours fewer and
// larger groups: this is one fixed, coarser grain than the reference one, not
// a target count, and it is the same for every project so that two runs on
// the same code draw the same zoomed-out map.
const blockResolution = 0.5

// blocksOf groups the communities into blocks, one grain coarser than the
// reference partition, and aggregates the edges between them. It returns
// nothing when there is nothing to zoom out to: fewer than three communities,
// or a grouping that keeps every community apart or folds them all into one.
func blocksOf(cm *CommunityMetrics) ([]CommunityBlock, []CommunityEdge) {
	ids := communityIDs(cm)
	if len(ids) < 3 {
		return nil, nil
	}
	sizeOf := map[string]int{}
	nameOf := map[string]string{}
	for _, c := range cm.Communities {
		sizeOf[c.ID] = c.Size
		nameOf[c.ID] = c.ShortName
	}
	// Undirected weight between each pair: both directions add up.
	pair := func(a, b string) string {
		if compareIDs(a, b) > 0 {
			a, b = b, a
		}
		return a + ">" + b
	}
	between := map[string]int{}
	exchange := map[string]map[string]int{} // community -> other -> weight
	for _, e := range cm.Edges {
		if e.Shared || e.From == e.To {
			continue
		}
		between[pair(e.From, e.To)] += e.Weight
		if exchange[e.From] == nil {
			exchange[e.From] = map[string]int{}
		}
		if exchange[e.To] == nil {
			exchange[e.To] = map[string]int{}
		}
		exchange[e.From][e.To] += e.Weight
		exchange[e.To][e.From] += e.Weight
	}
	edges := make([]graph.WeightedEdge, 0, len(between))
	for _, key := range slices.Sorted(maps.Keys(between)) {
		a, b, _ := strings.Cut(key, ">")
		edges = append(edges, graph.WeightedEdge{A: a, B: b, Weight: float64(between[key])})
	}
	partition := graph.Louvain(ids, edges, blockResolution)
	label := map[string]int{}
	members := map[int][]string{}
	for _, id := range ids {
		label[id] = partition.Community[id]
		members[label[id]] = append(members[label[id]], id)
	}
	// A community left alone joins the block it exchanges the most with,
	// when it exchanges anything at all: alone, it would be a block of one,
	// which is no zoom-out. A community linked to nothing stays on its own.
	for _, l := range slices.Sorted(maps.Keys(members)) {
		if len(members[l]) != 1 {
			continue
		}
		id := members[l][0]
		best, bestWeight := -1, 0
		for _, other := range slices.SortedFunc(maps.Keys(exchange[id]), compareIDs) {
			w := exchange[id][other]
			// ties go to the larger block, then to the lowest label
			if w > bestWeight || (w == bestWeight && best >= 0 && len(members[label[other]]) > len(members[best])) {
				best, bestWeight = label[other], w
			}
		}
		if best < 0 || best == l {
			continue
		}
		delete(members, l)
		label[id] = best
		members[best] = append(members[best], id)
	}
	// Number the blocks by size, then by their smallest member.
	labels := slices.SortedFunc(maps.Keys(members), func(x, y int) int {
		sx, sy := 0, 0
		for _, id := range members[x] {
			sx += sizeOf[id]
		}
		for _, id := range members[y] {
			sy += sizeOf[id]
		}
		if sx != sy {
			return sy - sx
		}
		return compareIDs(slices.MinFunc(members[x], compareIDs), slices.MinFunc(members[y], compareIDs))
	})
	if len(labels) < 2 || len(labels) >= len(ids) {
		return nil, nil
	}
	blocks := make([]CommunityBlock, 0, len(labels))
	blockOf := map[string]string{}
	for i, l := range labels {
		ids := members[l]
		slices.SortFunc(ids, func(x, y string) int {
			if sizeOf[x] != sizeOf[y] {
				return sizeOf[y] - sizeOf[x]
			}
			return compareIDs(x, y)
		})
		b := CommunityBlock{ID: fmt.Sprintf("b%d", i), Communities: ids, Name: nameOf[ids[0]]}
		for _, id := range ids {
			b.Size += sizeOf[id]
			blockOf[id] = b.ID
		}
		if len(ids) > 1 {
			b.Name = fmt.Sprintf("%s + %d", b.Name, len(ids)-1)
		}
		blocks = append(blocks, b)
	}
	// The edges between blocks: the community edges added up, the shared
	// kernel kept as its own end, then the cycles marked as for communities.
	weights := map[string]map[string]int{}
	for _, e := range cm.Edges {
		from, to := blockOf[e.From], blockOf[e.To]
		if e.From == SharedID {
			from = SharedID
		}
		if e.To == SharedID {
			to = SharedID
		}
		if from == "" || to == "" || from == to {
			continue
		}
		if weights[from] == nil {
			weights[from] = map[string]int{}
		}
		weights[from][to] += e.Weight
	}
	blockEdges := []CommunityEdge{}
	for _, from := range slices.SortedFunc(maps.Keys(weights), compareIDs) {
		for _, to := range slices.SortedFunc(maps.Keys(weights[from]), compareIDs) {
			blockEdges = append(blockEdges, CommunityEdge{From: from, To: to, Weight: weights[from][to], Shared: from == SharedID || to == SharedID})
		}
	}
	slices.SortStableFunc(blockEdges, func(x, y CommunityEdge) int { return y.Weight - x.Weight })
	blockIDs := make([]string, 0, len(blocks))
	for _, b := range blocks {
		blockIDs = append(blockIDs, b.ID)
	}
	markCycles(blockIDs, blockEdges)
	return blocks, blockEdges
}
