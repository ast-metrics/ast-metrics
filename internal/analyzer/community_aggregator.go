package analyzer

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	graph "github.com/ast-metrics/ast-metrics/internal/analyzer/graph"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/namer"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// CommunityMetrics holds computed community-related KPIs for the aggregate
type CommunityMetrics struct {
	Communities          map[string][]string // label -> node ids
	NodeToCommunity      map[string]string   // node id -> label
	CommunitiesCount     int
	AvgSize              float64
	MaxSize              int
	GraphDensity         float64
	ModularityQ          float64            // placeholder 0.0 unless implemented later
	PurityPerCommunity   map[string]float64 // label -> purity [0,1]
	TopNamespacesPerComm map[string][]string
	TopClassesPerComm    map[string][]string
	InboundEdgesPerComm  map[string]int
	OutboundEdgesPerComm map[string]int
	BoundaryNodes        []string

	// New aggregates for architecture-oriented views
	EdgesBetweenCommunities []EdgeBetweenCommunities   // [{from,to,edges}]
	Levels                  map[string]int             // community -> level in condensation DAG layering
	CouplingRatioPerComm    map[string]float64         // outbound/(in+out+self)
	BetweennessPerComm      map[string]float64         // betweenness on community graph (undirected approx)
	MatrixOrder             []string                   // order of communities for matrix
	TopPaths                map[string][]CommunityPath // entry -> top k paths

	// Human-friendly display names derived generically (no project-specific rules)
	DisplayNamePerComm map[string]string // community id -> display name

	// Commiters per community
	TopCommittersPerCommunity map[string]map[string]int // community -> {commiter -> commits}
	BusFactorPerCommunity     map[string]int            // community -> bus factor
}

// EdgeBetweenCommunities represents an aggregated directed edge between communities
type EdgeBetweenCommunities struct {
	From  string
	To    string
	Edges int
}

// CommunityPath stores a path and its cumulative percentage weight
type CommunityPath struct {
	Path []string
	Pct  float64
}

// Calculate basic graph density
func density(g *pb.Graph) float64 {
	if g == nil || len(g.Nodes) == 0 {
		return 0
	}
	// use undirected approximation: count unique undirected edges
	seen := map[string]struct{}{}
	for u, n := range g.Nodes {
		for _, v := range n.Edges {
			a, b := u, v
			if a > b {
				a, b = b, a
			}
			key := a + "::" + b
			if a == b { // ignore self
				continue
			}
			seen[key] = struct{}{}
		}
	}
	m := float64(len(seen))
	n := float64(len(g.Nodes))
	if n <= 1 {
		return 0
	}
	return (2 * m) / (n * (n - 1))
}

// computeModularityQ computes Newman-Girvan modularity using an undirected approximation.
// Q = (1/2m) sum_{ij} [A_ij - (k_i k_j / 2m)] delta(c_i, c_j)
// Self-loops are ignored; multiple edges count once.
func computeModularityQ(g *pb.Graph, node2comm map[string]string) float64 {
	if g == nil || len(g.Nodes) == 0 || len(node2comm) == 0 {
		return 0
	}
	// Build undirected simple graph representation and degrees
	// Use unique undirected edges set
	type pair struct{ a, b string }
	edges := map[pair]struct{}{}
	deg := map[string]int{}
	for u, n := range g.Nodes {
		if _, ok := deg[u]; !ok {
			deg[u] = 0
		}
		for _, v := range n.Edges {
			if u == v {
				continue
			}
			// ensure nodes exist in deg map
			if _, ok := deg[v]; !ok {
				deg[v] = 0
			}
			a, b := u, v
			if a > b {
				a, b = b, a
			}
			p := pair{a: a, b: b}
			if _, ok := edges[p]; !ok {
				edges[p] = struct{}{}
				deg[a]++
				deg[b]++
			}
		}
	}
	m := 0.0
	for range edges {
		m += 1
	}
	if m == 0 {
		return 0
	}
	twoM := 2.0 * m
	// Precompute communities per node. The edges are summed in a fixed order:
	// float addition is not associative, so walking the set in map order would
	// move the last digits of Q from one run to the next.
	orderedEdges := slices.SortedFunc(maps.Keys(edges), func(x, y pair) int {
		if x.a != y.a {
			return strings.Compare(x.a, y.a)
		}
		return strings.Compare(x.b, y.b)
	})
	Q := 0.0
	for _, e := range orderedEdges {
		ci := node2comm[e.a]
		cj := node2comm[e.b]
		if ci == "" || cj == "" {
			continue
		}
		if ci == cj {
			// A_ij = 1 for edge, subtract expected term
			ki := float64(deg[e.a])
			kj := float64(deg[e.b])
			Q += 1.0 - (ki*kj)/twoM
		}
	}
	return Q / twoM
}

// CommunityAggregator computes communities using Label Propagation
type CommunityAggregator struct{}

func NewCommunityAggregator() *CommunityAggregator { return &CommunityAggregator{} }

func (ca *CommunityAggregator) Calculate(aggregate *Aggregated) {
	if aggregate == nil || aggregate.Graph == nil || len(aggregate.Graph.Nodes) == 0 {
		return
	}
	// run label propagation with fewer iters and deterministic tie-break (no shuffle)
	comms := graph.LabelPropagationCommunities(aggregate.Graph, graph.LPAOptions{MaxIters: 15, ShufflePer: false})

	// map node->community
	node2comm := map[string]string{}
	maxSize := 0
	for label, nodes := range comms {
		if len(nodes) > maxSize {
			maxSize = len(nodes)
		}
		for _, u := range nodes {
			node2comm[u] = label
		}
	}

	// Post-process: merge very small communities into their strongest neighbor to provide more architectural recul
	// Adjusted to 1 to preserve even singleton communities (increase circles) while avoiding unnecessary merges
	const minCommSize = 1
	if len(comms) > 0 {
		// Build inter-community connectivity (both directions) based on current assignment
		outW := map[string]map[string]int{}
		inW := map[string]map[string]int{}
		for u, n := range aggregate.Graph.Nodes {
			cu := node2comm[u]
			for _, v := range n.Edges {
				cv := node2comm[v]
				if cu == "" || cv == "" || cu == cv {
					continue
				}
				if outW[cu] == nil {
					outW[cu] = map[string]int{}
				}
				if inW[cv] == nil {
					inW[cv] = map[string]int{}
				}
				outW[cu][cv]++
				inW[cv][cu]++
			}
		}
		// Determine small communities
		sizes := map[string]int{}
		for cid, nodes := range comms {
			sizes[cid] = len(nodes)
		}
		// For determinism, collect small IDs sorted
		small := []string{}
		for cid, sz := range sizes {
			if sz > 0 && sz < minCommSize {
				small = append(small, cid)
			}
		}
		// simple sort lexicographically (manual to avoid importing extra packages)
		for i := 0; i < len(small); i++ {
			for j := i + 1; j < len(small); j++ {
				if small[j] < small[i] {
					small[i], small[j] = small[j], small[i]
				}
			}
		}
		// Merge pass: choose best neighbor by (out+in) weight; if tie, pick lexicographically smallest
		for _, s := range small {
			best := ""
			bestW := -1
			// check outbound neighbors
			if m := outW[s]; m != nil {
				for t, w := range m {
					ww := w
					if inW[s] != nil {
						ww += inW[s][t]
					}
					if ww > bestW || (ww == bestW && t < best) {
						best = t
						bestW = ww
					}
				}
			}
			// also consider pure inbound-only neighbors
			if m := inW[s]; m != nil {
				for t, w := range m {
					if outW[s] != nil {
						if _, ok := outW[s][t]; ok {
							continue
						}
					}
					ww := w
					if ww > bestW || (ww == bestW && t < best) {
						best = t
						bestW = ww
					}
				}
			}
			if best == "" {
				continue
			}
			// reassign nodes of s to best
			for _, u := range comms[s] {
				node2comm[u] = best
			}
		}
		// Rebuild communities and maxSize after merges
		newComms := map[string][]string{}
		newMax := 0
		// By node id rather than in map order: these lists are read as-is by
		// the report and by the matrix ordering below.
		for _, u := range slices.Sorted(maps.Keys(aggregate.Graph.Nodes)) {
			c := node2comm[u]
			if c == "" {
				continue
			}
			newComms[c] = append(newComms[c], u)
		}
		for _, nodes := range newComms {
			if len(nodes) > newMax {
				newMax = len(nodes)
			}
		}
		if len(newComms) > 0 {
			comms = newComms
			maxSize = newMax
		}
	}

	// purity per community based on Name.Package/namespace prefix
	purity := map[string]float64{}
	topNamespaces := map[string][]string{}
	topClasses := map[string][]string{}
	inbound := map[string]int{}
	outbound := map[string]int{}

	// helper: namespace of node
	getNs := func(id string, n *pb.Node) string {
		if n != nil && n.Name != nil && n.Name.Package != "" {
			return n.Name.Package
		}
		if n != nil && n.Name != nil && n.Name.Qualified != "" {
			q := n.Name.Qualified
			// approximate namespace = everything before last '.' or '\\'
			last := -1
			for i := 0; i < len(q); i++ {
				if q[i] == '.' || q[i] == '\\' || q[i] == '/' {
					last = i
				}
			}
			if last > 0 {
				return q[:last]
			}
		}
		return ""
	}
	// compute inbound/outbound at community level and purity
	// freq of namespaces per community
	nsFreq := map[string]map[string]int{}
	for label, nodes := range comms {
		nsFreq[label] = map[string]int{}
		for _, u := range nodes {
			n := aggregate.Graph.Nodes[u]
			ns := getNs(u, n)
			if ns != "" {
				nsFreq[label][ns]++
			}
		}
		// purity = max(ns count)/size
		maxC := 0
		maxNS := ""
		for ns, c := range nsFreq[label] {
			if c > maxC {
				maxC = c
				maxNS = ns
			}
		}
		if len(nodes) > 0 {
			purity[label] = float64(maxC) / float64(len(nodes))
		} else {
			purity[label] = 0
		}
		if maxNS != "" {
			topNamespaces[label] = []string{maxNS}
		}
		// top classes: pick first up to 5
		k := 0
		for _, u := range nodes {
			if k >= 5 {
				break
			}
			topClasses[label] = append(topClasses[label], u)
			k++
		}
	}

	// community edge aggregation (directed, weighted)
	edgeW := map[string]map[string]int{}
	for u, n := range aggregate.Graph.Nodes {
		cu := node2comm[u]
		for _, v := range n.Edges {
			cv := node2comm[v]
			if cu == "" || cv == "" {
				continue
			}
			if cu == cv {
				continue
			}
			if _, ok := edgeW[cu]; !ok {
				edgeW[cu] = map[string]int{}
			}
			edgeW[cu][cv]++
			outbound[cu]++
			inbound[cv]++
		}
	}
	// build edges_between_communities array
	ebc := make([]EdgeBetweenCommunities, 0)
	for from, m := range edgeW {
		for to, w := range m {
			ebc = append(ebc, EdgeBetweenCommunities{From: from, To: to, Edges: w})
		}
	}

	// boundary nodes: nodes with neighbors in multiple communities
	boundarySet := map[string]struct{}{}
	for u, n := range aggregate.Graph.Nodes {
		cu := node2comm[u]
		seen := map[string]struct{}{}
		for _, v := range n.Edges {
			cv := node2comm[v]
			if cv != "" && cv != cu {
				seen[cv] = struct{}{}
			}
		}
		if len(seen) >= 2 {
			boundarySet[u] = struct{}{}
		}
	}
	boundary := make([]string, 0, len(boundarySet))
	for u := range boundarySet {
		boundary = append(boundary, u)
	}

	// per-community coupling ratio
	coupling := map[string]float64{}
	for cid, nodes := range comms {
		internal := 0
		// count internal by iterating node edges
		for _, u := range nodes {
			n := aggregate.Graph.Nodes[u]
			for _, v := range n.Edges {
				if node2comm[v] == cid {
					internal++
				}
			}
		}
		inb := inbound[cid]
		outb := outbound[cid]
		tot := internal + inb + outb
		if tot > 0 {
			coupling[cid] = float64(outb) / float64(tot)
		} else {
			coupling[cid] = 0
		}
	}

	// Build community adjacency (undirected) for betweenness and ordering
	und := map[string]map[string]struct{}{}
	for a, m := range edgeW {
		if _, ok := und[a]; !ok {
			und[a] = map[string]struct{}{}
		}
		for b := range m {
			if _, ok := und[b]; !ok {
				und[b] = map[string]struct{}{}
			}
			und[a][b] = struct{}{}
			und[b][a] = struct{}{}
		}
	}

	// Betweenness centrality (Brandes) on undirected community graph
	bet := map[string]float64{}
	// init
	for c := range comms {
		bet[c] = 0
	}
	// nodes list
	V := make([]string, 0, len(comms))
	for c := range comms {
		V = append(V, c)
	}
	for _, s := range V {
		// stack
		S := []string{}
		// predecessors
		P := map[string][]string{}
		// sigma
		sigma := map[string]float64{}
		// dist
		dist := map[string]int{}
		for _, v := range V {
			P[v] = []string{}
			sigma[v] = 0
			dist[v] = -1
		}
		sigma[s] = 1
		dist[s] = 0
		// queue
		Q := []string{s}
		for len(Q) > 0 {
			v := Q[0]
			Q = Q[1:]
			S = append(S, v)
			for w := range und[v] {
				if dist[w] < 0 {
					Q = append(Q, w)
					dist[w] = dist[v] + 1
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					P[w] = append(P[w], v)
				}
			}
		}
		// accumulation
		delta := map[string]float64{}
		for _, v := range V {
			delta[v] = 0
		}
		for len(S) > 0 {
			w := S[len(S)-1]
			S = S[:len(S)-1]
			for _, v := range P[w] {
				if sigma[w] > 0 {
					delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
				}
			}
			if w != s {
				bet[w] += delta[w]
			}
		}
	}

	// normalize
	nC := float64(len(V))
	if nC > 2 {
		norm := 1.0 / ((nC - 1) * (nC - 2))
		for k := range bet {
			bet[k] *= norm
		}
	}

	// SCC condensation DAG and levels (longest path from sources)
	// Build directed adjacency on communities
	dagAdj := map[string]map[string]struct{}{}
	for from, m := range edgeW {
		if _, ok := dagAdj[from]; !ok {
			dagAdj[from] = map[string]struct{}{}
		}
		for to := range m {
			dagAdj[from][to] = struct{}{}
		}
	}
	// Kosaraju SCC on community digraph
	order := []string{}
	visited := map[string]bool{}
	var dfs1 func(u string)
	dfs1 = func(u string) {
		visited[u] = true
		for v := range dagAdj[u] {
			if !visited[v] {
				dfs1(v)
			}
		}
		order = append(order, u)
	}
	for c := range comms {
		if !visited[c] {
			dfs1(c)
		}
	}
	// build reverse graph
	rev := map[string]map[string]struct{}{}
	for u, m := range dagAdj {
		for v := range m {
			if _, ok := rev[v]; !ok {
				rev[v] = map[string]struct{}{}
			}
			rev[v][u] = struct{}{}
		}
	}
	compID := map[string]int{}
	var dfs2 func(u string, cid int)
	dfs2 = func(u string, cid int) {
		compID[u] = cid
		for v := range rev[u] {
			if compID[v] == 0 {
				dfs2(v, cid)
			}
		}
	}
	cid := 0
	for i := len(order) - 1; i >= 0; i-- {
		u := order[i]
		if compID[u] == 0 {
			cid++
			dfs2(u, cid)
		}
	}
	// build condensation DAG
	condAdj := map[int]map[int]struct{}{}
	for u, m := range dagAdj {
		cu := compID[u]
		for v := range m {
			cv := compID[v]
			if cu != cv {
				if _, ok := condAdj[cu]; !ok {
					condAdj[cu] = map[int]struct{}{}
				}
				condAdj[cu][cv] = struct{}{}
			}
		}
	}
	// topological order of condensation and level via longest path from sources
	indeg := map[int]int{}
	for u := 1; u <= cid; u++ {
		indeg[u] = 0
	}
	for _, m := range condAdj {
		for v := range m {
			indeg[v]++
		}
	}
	queue := []int{}
	for u := 1; u <= cid; u++ {
		if indeg[u] == 0 {
			queue = append(queue, u)
		}
	}
	topo := []int{}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		topo = append(topo, u)
		for v := range condAdj[u] {
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	levelByComp := map[int]int{}
	for _, u := range topo {
		maxp := 0
		// find predecessors to compute level
		for p, m := range condAdj {
			if _, ok := m[u]; ok {
				if levelByComp[p]+1 > maxp {
					maxp = levelByComp[p] + 1
				}
			}
		}
		levelByComp[u] = maxp
	}
	levels := map[string]int{}
	for c := range comms {
		levels[c] = levelByComp[compID[c]]
	}

	// Matrix order via simple RCM on undirected graph
	orderRCM := func() []string {
		// degree map
		deg := map[string]int{}
		for c := range comms {
			deg[c] = len(und[c])
		}
		visited := map[string]bool{}
		seq := []string{}
		// Communities and neighbours are walked by name rather than in map
		// order: this produces a slice, so equal degrees have to be broken by
		// something stable or the matrix comes out shuffled on every run.
		orderedComms := slices.Sorted(maps.Keys(comms))
		// multiple components: start from min-degree unvisited
		for len(seq) < len(comms) {
			// pick min-degree unvisited node
			var start string
			minD := 1 << 30
			for _, c := range orderedComms {
				if !visited[c] && deg[c] < minD {
					minD = deg[c]
					start = c
				}
			}
			if start == "" {
				break
			}
			// BFS
			q := []string{start}
			visited[start] = true
			comp := []string{}
			for len(q) > 0 {
				u := q[0]
				q = q[1:]
				comp = append(comp, u)
				// neighbors sorted by degree, then by name
				neigh := []string{}
				for v := range und[u] {
					if !visited[v] {
						neigh = append(neigh, v)
					}
				}
				slices.SortFunc(neigh, func(a, b string) int {
					if deg[a] != deg[b] {
						return deg[a] - deg[b]
					}
					return strings.Compare(a, b)
				})
				for _, v := range neigh {
					visited[v] = true
					q = append(q, v)
				}
			}
			// reverse comp to get RCM effect
			for i := len(comp) - 1; i >= 0; i-- {
				seq = append(seq, comp[i])
			}
		}
		return seq
	}()

	// Top paths from entry nodes (in-degree 0 in condensation graph) on community graph DAG approximation
	// Convert to probabilities per source community
	totalOut := map[string]int{}
	for a, m := range edgeW {
		sum := 0
		for _, w := range m {
			sum += w
		}
		totalOut[a] = sum
	}

	// collect entry communities (condensation components with indegree 0)
	entries := map[string]struct{}{}
	indegComm := map[string]int{}
	for a := range comms {
		indegComm[a] = 0
	}
	for _, m := range edgeW {
		for b := range m {
			indegComm[b]++
		}
	}
	for a := range comms {
		if indegComm[a] == 0 {
			entries[a] = struct{}{}
		}
	}

	topPaths := map[string][]CommunityPath{}
	const K = 5
	const MaxDepth = 8
	const ProbCutoff = 1e-3
	const MaxAcc = 5000
	var dfs func(start, u string, path []string, prob float64, depth int, acc *[]CommunityPath)
	// helper to push into acc with cap MaxAcc
	pushAcc := func(acc *[]CommunityPath, path []string, prob float64) {
		if len(*acc) >= MaxAcc {
			return
		}
		*acc = append(*acc, CommunityPath{Path: append([]string{}, path...), Pct: prob * 100})
	}
	for entry := range entries {
		acc := []CommunityPath{}
		dfs = func(start, u string, path []string, prob float64, depth int, acc *[]CommunityPath) {
			path2 := append(append([]string{}, path...), u)
			// terminal conditions
			if len(edgeW[u]) == 0 || depth >= MaxDepth {
				pushAcc(acc, path2, prob)
				return
			}
			if prob < ProbCutoff {
				pushAcc(acc, path2, prob)
				return
			}
			// expand to neighbors sorted by weight
			type nxt struct {
				v string
				p float64
			}
			nx := []nxt{}
			tot := float64(totalOut[u])
			if tot == 0 {
				pushAcc(acc, path2, prob)
				return
			}
			for v, w := range edgeW[u] {
				nx = append(nx, nxt{v: v, p: float64(w) / tot})
			}
			// simple sort descending p
			for i := 0; i < len(nx); i++ {
				for j := i + 1; j < len(nx); j++ {
					if nx[j].p > nx[i].p {
						nx[i], nx[j] = nx[j], nx[i]
					}
				}
			}

			for _, e := range nx {
				// avoid cycles by stopping if v already in path
				seen := false
				for _, x := range path2 {
					if x == e.v {
						seen = true
						break
					}
				}
				if seen {
					continue
				}
				if len(*acc) >= MaxAcc {
					return
				}
				dfs(start, e.v, path2, prob*e.p, depth+1, acc)
			}
		}

		dfs(entry, entry, []string{}, 1.0, 1, &acc)
		// sort acc by Pct desc and keep top K
		for i := 0; i < len(acc); i++ {
			for j := i + 1; j < len(acc); j++ {
				if acc[j].Pct > acc[i].Pct {
					acc[i], acc[j] = acc[j], acc[i]
				}
			}
		}
		if len(acc) > K {
			acc = acc[:K]
		}
		topPaths[entry] = acc
	}

	// Derive display names per community using the namer
	display := map[string]string{}
	namerInstance, err := namer.NewNamer()
	if err != nil {
		// Fallback to community ID if namer initialization fails
		for cid := range comms {
			display[cid] = cid
		}
	} else {
		for cid, nodes := range comms {
			// Collect qualified names of nodes in this community
			classNames := []string{}
			for _, u := range nodes {
				n := aggregate.Graph.Nodes[u]
				if n != nil && n.Name != nil {
					// Prefer Qualified, fallback to Short
					name := n.Name.Qualified
					if name == "" {
						name = n.Name.Short
					}
					if name != "" {
						classNames = append(classNames, name)
					}
				}
			}
			// Use namer to generate display name
			if len(classNames) > 0 {
				display[cid] = namerInstance.Names(classNames)
			} else {
				display[cid] = cid
			}
		}
	}

	aggregateCommunity := &CommunityMetrics{
		Communities:      comms,
		NodeToCommunity:  node2comm,
		CommunitiesCount: len(comms),
		AvgSize: func() float64 {
			sum := 0
			for _, ns := range comms {
				sum += len(ns)
			}
			if len(comms) == 0 {
				return 0
			}
			return float64(sum) / float64(len(comms))
		}(),
		MaxSize:                 maxSize,
		GraphDensity:            density(aggregate.Graph),
		ModularityQ:             computeModularityQ(aggregate.Graph, node2comm),
		PurityPerCommunity:      purity,
		TopNamespacesPerComm:    topNamespaces,
		TopClassesPerComm:       topClasses,
		InboundEdgesPerComm:     inbound,
		OutboundEdgesPerComm:    outbound,
		BoundaryNodes:           boundary,
		EdgesBetweenCommunities: ebc,
		Levels:                  levels,
		CouplingRatioPerComm:    coupling,
		BetweennessPerComm:      bet,
		MatrixOrder:             orderRCM,
		TopPaths:                topPaths,
		DisplayNamePerComm:      display,
	}
	aggregate.Community = aggregateCommunity

	// Build backend suggestions for Communities view (server-side, no JS)
	// Heuristics mirror the previous frontend logic:
	// - Introduce façade for communities with high outbound coupling ratio (>0.7)
	// - Split module for communities with big size (>50) and low purity (<0.6)
	// - Refactor boundary nodes (top N boundary nodes listed)
	if aggregate.Suggestions == nil {
		aggregate.Suggestions = make([]Suggestion, 0)
	}
	seen := map[string]bool{}
	// 1) Introduce façade for high coupling communities
	for cid, coup := range aggregateCommunity.CouplingRatioPerComm {
		if coup > 0.7 {
			pct := int(coup*100 + 0.5)
			msg := "Introduce façade for community " + cid + " (coupling " + fmt.Sprintf("%d%%)", pct)
			if !seen[msg] {
				aggregate.Suggestions = append(aggregate.Suggestions, Suggestion{
					Summary:             "Introduce façade for community " + cid,
					Location:            cid,
					Why:                 fmt.Sprintf("High outbound coupling ratio: %d%% (> 70%%)", pct),
					DetailedExplanation: "This community depends on many others. Introduce a façade or API boundary to reduce direct dependencies and stabilize interactions.",
					Category:            "coupling",
				})
				seen[msg] = true
			}
		}
	}
	// 2) Split module for large impure communities
	for cid, members := range aggregateCommunity.Communities {
		size := len(members)
		pur := aggregateCommunity.PurityPerCommunity[cid]
		if size > 50 && pur < 0.6 {
			msg := fmt.Sprintf("Split module for community %s (size %d, purity %d%%)", cid, size, int(pur*100+0.5))
			if !seen[msg] {
				aggregate.Suggestions = append(aggregate.Suggestions, Suggestion{
					Summary:             "Split module for community " + cid,
					Location:            cid,
					Why:                 fmt.Sprintf("Large and impure community: size=%d (>50), purity=%d%% (<60%%)", size, int(pur*100+0.5)),
					DetailedExplanation: "This community aggregates several concerns. Consider splitting it into smaller, cohesive modules aligned by domain or namespace to improve purity and maintainability.",
					Category:            "purity",
				})
				seen[msg] = true
			}
		}
	}
	// 3) Refactor boundary nodes (take up to 5 boundary nodes)
	maxBoundary := 5
	for i, nid := range aggregateCommunity.BoundaryNodes {
		if i >= maxBoundary {
			break
		}
		msg := "Refactor boundary node " + nid + " (boundary crossing)"
		if !seen[msg] {
			aggregate.Suggestions = append(aggregate.Suggestions, Suggestion{
				Summary:             "Refactor boundary node " + nid,
				Location:            nid,
				Why:                 "Boundary node detected: participates in edges crossing communities",
				DetailedExplanation: "This node connects multiple communities and can create tight coupling. Consider introducing anti-corruption layers, moving responsibilities, or clarifying ownership to reduce boundary crossings.",
				Category:            "boundary",
			})
			seen[msg] = true
		}
	}

	// should maybe be moved
	calculator := NewCommunitySubMetricsCalculator()
	calculator.Calculate(aggregate)
}
