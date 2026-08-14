package graph

import (
	"math"
	"math/rand"
	"slices"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

// no directed graph
func undirectedAdj(g *pb.Graph) map[string][]string {
	adj := make(map[string][]string, len(g.Nodes))
	for id := range g.Nodes {
		adj[id] = adj[id] // init
	}
	for u, n := range g.Nodes {
		for _, v := range n.Edges {
			// u->v
			if !contains(adj[u], v) {
				adj[u] = append(adj[u], v)
			}
			// symétrie: v->u
			if !contains(adj[v], u) {
				adj[v] = append(adj[v], u)
			}
		}
	}
	// g.Nodes is a map, so the neighbours land in an arbitrary order. The label
	// histogram below sums 1/sqrt(deg) weights and compares them with ==, which
	// makes it sensitive to the order the terms are added in: sort the lists so
	// two runs on the same graph agree on the same communities.
	for id := range adj {
		slices.Sort(adj[id])
	}
	return adj
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Label Propagation
type LPAOptions struct {
	MaxIters      int     // default ~15 for speed
	Seed          int64   // deterministic when provided
	ShufflePer    bool    // shuffle order per iteration; default false for stability
	Resolution    float64 // >1.0 encourages more, smaller communities; <1.0 fewer, larger
	MinCommSize   int     // communities smaller than this will be merged to neighbors
	TieBreakFavor string  // "small" favors splitting (pick smallest label), "large" merges (pick largest)
}

func LabelPropagationCommunities(g *pb.Graph, opts LPAOptions) map[string][]string {
	if opts.MaxIters <= 0 {
		opts.MaxIters = 15
	}
	if opts.Seed == 0 {
		// leave rng nil for deterministic behavior when no seed provided
	} else {
		// initialize rng only if randomization is requested by options
	}
	var rng *rand.Rand
	if opts.Seed != 0 {
		rng = rand.New(rand.NewSource(opts.Seed))
	}

	adj := undirectedAdj(g)

	// labels init = id du nœud
	labels := make(map[string]string, len(adj))
	nodes := make([]string, 0, len(adj))
	for id := range adj {
		labels[id] = id
		nodes = append(nodes, id)
	}

	changed := true
	for iter := 0; iter < opts.MaxIters && changed; iter++ {
		changed = false
		// Optional shuffle; only if rng is set and ShufflePer is true
		if opts.ShufflePer && rng != nil {
			rng.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
		}
		// Deterministic order by node id for stability
		slices.Sort(nodes)
		for _, u := range nodes {
			// weighted histogram of neighbor labels with hub dampening 1/sqrt(deg(v))
			count := map[string]float64{}
			maxC := 0.0
			var best []string
			for _, v := range adj[u] {
				l := labels[v]
				// degree of neighbor v
				d := len(adj[v])
				w := 1.0
				if d > 0 {
					w = 1.0 / math.Sqrt(float64(d))
				}
				count[l] += w
				if count[l] > maxC {
					maxC = count[l]
					best = []string{l}
				} else if count[l] == maxC {
					best = append(best, l)
				}
			}
			if len(best) == 0 {
				continue
			}
			// deterministic tie-break: pick smallest label for stability
			slices.Sort(best)
			newLabel := best[0]
			if newLabel != labels[u] {
				labels[u] = newLabel
				changed = true
			}
		}
	}

	// grouper by label
	comms := map[string][]string{}
	for u, l := range labels {
		comms[l] = append(comms[l], u)
	}
	// sort nodes in each community
	for k := range comms {
		slices.Sort(comms[k])
	}

	return comms
}
