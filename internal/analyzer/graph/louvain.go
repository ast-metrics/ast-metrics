package graph

import (
	"math"
	"slices"
	"strings"
)

// WeightedEdge is an undirected edge between two nodes named by id. Two edges
// on the same pair add up, and an edge from a node to itself is ignored.
type WeightedEdge struct {
	A, B   string
	Weight float64
}

// Partition is the outcome of a community detection: which community each node
// belongs to, and the modularity of that assignment.
type Partition struct {
	// Community maps a node id onto a community index. Communities are numbered
	// from zero by decreasing size, then by their smallest member, so that the
	// same graph always yields the same numbers.
	Community map[string]int
	// Count is the number of communities.
	Count int
	// Modularity is Newman's Q of the partition, between -0.5 and 1. Higher
	// means the communities keep more of the edges inside themselves than a
	// random wiring of the same degrees would.
	Modularity float64
}

// Louvain finds the communities of an undirected weighted graph by modularity
// optimisation (Blondel et al., 2008): every node moves to the neighbouring
// community that improves the modularity the most, the communities are then
// folded into single nodes, and the two steps repeat until nothing moves.
//
// The result is fully determined by the input: nodes are visited in the order
// of their ids and ties are broken on the lowest community index, so that two
// runs on the same graph draw the same map. Resolution is Reichardt-Bornholdt's
// gamma: 1 is plain modularity, above 1 favours more and smaller communities,
// below 1 fewer and larger ones.
//
// Nodes named in edges only are added to the graph; nodes named without any
// edge each end up alone in their community.
func Louvain(nodes []string, edges []WeightedEdge, resolution float64) Partition {
	if resolution <= 0 {
		resolution = 1
	}
	// Index the nodes in a fixed order.
	ids := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, id := range nodes {
		add(id)
	}
	for _, e := range edges {
		add(e.A)
		add(e.B)
	}
	slices.Sort(ids)
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}
	n := len(ids)
	if n == 0 {
		return Partition{Community: map[string]int{}}
	}

	// Build the symmetric adjacency, summing parallel edges and dropping loops.
	adj := make([]map[int]float64, n)
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	for _, e := range edges {
		if e.Weight <= 0 || e.A == e.B {
			continue
		}
		a, b := index[e.A], index[e.B]
		adj[a][b] += e.Weight
		adj[b][a] += e.Weight
	}

	level := newLevel(adj, nil)
	// membership[i] is the community of the original node i, expressed as a node
	// index of the current level.
	membership := make([]int, n)
	for i := range membership {
		membership[i] = i
	}
	for {
		moved := level.moveNodes(resolution)
		if !moved {
			break
		}
		next, renumbered := level.aggregate()
		for i := range membership {
			membership[i] = renumbered[membership[i]]
		}
		if next.size() == level.size() {
			level = next
			break
		}
		level = next
	}

	// Number the communities by decreasing size, then by smallest member id.
	type comm struct {
		label    int
		size     int
		smallest string
	}
	byLabel := map[int]*comm{}
	for i, id := range ids {
		c := byLabel[membership[i]]
		if c == nil {
			c = &comm{label: membership[i], smallest: id}
			byLabel[membership[i]] = c
		}
		c.size++
		if id < c.smallest {
			c.smallest = id
		}
	}
	comms := make([]*comm, 0, len(byLabel))
	for _, c := range byLabel {
		comms = append(comms, c)
	}
	slices.SortFunc(comms, func(x, y *comm) int {
		if x.size != y.size {
			return y.size - x.size
		}
		return strings.Compare(x.smallest, y.smallest)
	})
	renumber := make(map[int]int, len(comms))
	for i, c := range comms {
		renumber[c.label] = i
	}
	result := Partition{Community: make(map[string]int, n), Count: len(comms)}
	for i, id := range ids {
		result.Community[id] = renumber[membership[i]]
	}
	result.Modularity = modularityOf(adj, membership, resolution)
	return result
}

// louvainLevel is one graph of the multi-level scheme: the original graph at
// first, then the graph of communities folded into nodes.
type louvainLevel struct {
	adj    []map[int]float64 // neighbour -> weight, without loops
	loops  []float64         // weight of the edges folded inside each node
	degree []float64         // sum of incident weights, loops counted twice
	m      float64           // total weight of the edges, each counted once
	// community assignment of the level's nodes and per-community sums
	community []int
	total     []float64 // sum of degrees of the nodes in each community
}

func newLevel(adj []map[int]float64, loops []float64) *louvainLevel {
	n := len(adj)
	l := &louvainLevel{adj: adj, loops: loops, degree: make([]float64, n), community: make([]int, n), total: make([]float64, n)}
	if l.loops == nil {
		l.loops = make([]float64, n)
	}
	for i := range adj {
		d := 2 * l.loops[i]
		for _, w := range adj[i] {
			d += w
		}
		l.degree[i] = d
		l.m += l.loops[i]
		for _, w := range adj[i] {
			l.m += w / 2
		}
		l.community[i] = i
		l.total[i] = d
	}
	return l
}

func (l *louvainLevel) size() int { return len(l.adj) }

// moveNodes runs the local moving phase until no node changes community, and
// reports whether any node moved at all.
func (l *louvainLevel) moveNodes(resolution float64) bool {
	if l.m == 0 {
		return false
	}
	movedOnce := false
	twoM := 2 * l.m
	for {
		moved := false
		for i := range l.adj {
			current := l.community[i]
			// weight from i to each neighbouring community
			toComm := map[int]float64{}
			for j, w := range l.adj[i] {
				toComm[l.community[j]] += w
			}
			// take i out of its community
			l.total[current] -= l.degree[i]
			bestComm := current
			bestGain := toComm[current] - resolution*l.total[current]*l.degree[i]/twoM
			// candidates in a fixed order so that ties resolve the same way
			candidates := make([]int, 0, len(toComm))
			for c := range toComm {
				candidates = append(candidates, c)
			}
			slices.Sort(candidates)
			for _, c := range candidates {
				if c == current {
					continue
				}
				gain := toComm[c] - resolution*l.total[c]*l.degree[i]/twoM
				if gain > bestGain+1e-12 || (math.Abs(gain-bestGain) <= 1e-12 && c < bestComm && gain > 0) {
					bestGain = gain
					bestComm = c
				}
			}
			l.total[bestComm] += l.degree[i]
			if bestComm != current {
				l.community[i] = bestComm
				moved = true
				movedOnce = true
			}
		}
		if !moved {
			return movedOnce
		}
	}
}

// aggregate folds every community into one node and returns the new level with
// the mapping from the nodes of this level onto the nodes of the new one.
func (l *louvainLevel) aggregate() (*louvainLevel, []int) {
	labels := make([]int, 0)
	seen := map[int]int{}
	renumbered := make([]int, len(l.community))
	for i, c := range l.community {
		if _, ok := seen[c]; !ok {
			seen[c] = len(labels)
			labels = append(labels, c)
		}
		renumbered[i] = seen[c]
	}
	n := len(labels)
	adj := make([]map[int]float64, n)
	for i := range adj {
		adj[i] = map[int]float64{}
	}
	loops := make([]float64, n)
	for i := range l.adj {
		ci := renumbered[i]
		loops[ci] += l.loops[i]
		for j, w := range l.adj[i] {
			cj := renumbered[j]
			if ci == cj {
				// each internal edge is seen from both ends
				loops[ci] += w / 2
			} else {
				adj[ci][cj] += w
			}
		}
	}
	return newLevel(adj, loops), renumbered
}

// modularityOf computes Q for an assignment of the original nodes.
func modularityOf(adj []map[int]float64, membership []int, resolution float64) float64 {
	m := 0.0
	degree := make([]float64, len(adj))
	for i := range adj {
		for _, w := range adj[i] {
			degree[i] += w
			m += w / 2
		}
	}
	if m == 0 {
		return 0
	}
	in := map[int]float64{}
	tot := map[int]float64{}
	for i := range adj {
		tot[membership[i]] += degree[i]
		for j, w := range adj[i] {
			if membership[i] == membership[j] {
				in[membership[i]] += w / 2
			}
		}
	}
	// Communities are summed in a fixed order: float addition is not
	// associative and Q is written in reports.
	labels := make([]int, 0, len(tot))
	for c := range tot {
		labels = append(labels, c)
	}
	slices.Sort(labels)
	q := 0.0
	for _, c := range labels {
		q += in[c]/m - resolution*math.Pow(tot[c]/(2*m), 2)
	}
	return q
}
