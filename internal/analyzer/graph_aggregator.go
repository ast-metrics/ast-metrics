package analyzer

import (
	"maps"
	"slices"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

type GraphAggregator struct{}

func NewGraphAggregator() *GraphAggregator { return &GraphAggregator{} }

func (ga *GraphAggregator) Calculate(aggregate *Aggregated) {
	if aggregate == nil {
		return
	}
	if aggregate.Graph == nil {
		aggregate.Graph = &pb.Graph{Nodes: make(map[string]*pb.Node)}
	}

	ensureNode := func(id string, name *pb.Name) *pb.Node {
		if id == "" {
			return nil
		}
		if aggregate.Graph.Nodes[id] == nil {
			n := &pb.Node{Id: id}
			if name != nil {
				n.Name = name
			} else {
				n.Name = &pb.Name{Qualified: id, Short: id}
			}
			aggregate.Graph.Nodes[id] = n
		}
		return aggregate.Graph.Nodes[id]
	}

	// keep a per-run set of edges to avoid O(N^2) duplicate checks
	edgesSeen := make(map[string]map[string]struct{})

	addEdge := func(from, to string) {
		if from == "" || to == "" || from == to {
			return
		}
		n := ensureNode(from, nil)
		ensureNode(to, nil)
		if n == nil {
			return
		}
		if edgesSeen[from] == nil {
			edgesSeen[from] = make(map[string]struct{})
		}
		if _, exists := edgesSeen[from][to]; exists {
			return
		}
		edgesSeen[from][to] = struct{}{}
		n.Edges = append(n.Edges, to)
	}

	// Prepare weighted package edges (package-only projection)
	edgesCount := make(map[string]map[string]int)

	// The dependencies are gathered before any of them is reduced: the depth a
	// namespace is cut at depends on the root the whole project shares, which is
	// only known once every source has been seen. They are kept rather than read
	// twice, as reading them means stating every file again.
	dependenciesOf := make(map[*pb.File][]*pb.StmtExternalDependency, len(aggregate.ConcernedFiles))
	for _, file := range aggregate.ConcernedFiles {
		// Skip empty files
		if file == nil || file.Stmts == nil {
			continue
		}
		// Gather dependencies at file level
		dependenciesOf[file] = engine.GetDependenciesInFile(file)
	}

	// The root is looked for among the sources of the dependencies, and among
	// them only: they are the nodes the graph has to keep apart, whereas a target
	// may well belong to a framework, and a class that depends on nothing is
	// never drawn.
	scopes := namespacesOfScopes(aggregate.ConcernedFiles)
	aggregate.NamespaceReducers = engine.NewNamespaceReducers(
		sourcesOfDependencies(aggregate.ConcernedFiles, func(file *pb.File) []*pb.StmtExternalDependency {
			return dependenciesOf[file]
		}, scopes),
		engine.DefaultNamespaceDepth)

	// A target inside the project is brought back to the namespace of the file
	// declaring it before it is reduced. Reducing the name of the class itself
	// would only work while that name is deeper than the depth kept: on a
	// project rooted at App, App\Entity\User is three levels deep and would
	// stay a node of its own beside App\Entity, as if it were a package that
	// App\Entity depends on.
	index := indexProject(aggregate.ConcernedFiles)
	// The nodes the project owns: the namespaces of its own files, reduced the
	// way the edges are. Every other node is foreign to it.
	internalNodes := make(map[string]bool)
	for _, file := range index.files {
		if namespace := index.namespaceOfFile[file]; namespace != "" {
			internalNodes[aggregate.NamespaceReducers.Reduce(file.GetProgrammingLanguage(), namespace)] = true
		}
	}

	for _, file := range aggregate.ConcernedFiles {
		for _, dep := range dependenciesOf[file] {
			if dep == nil {
				continue
			}
			// Reduce the namespaces to keep the nodes at the scale of a module
			// rather than of a class, and the edges meaningful
			fromNs := aggregate.NamespaceReducers.Reduce(file.GetProgrammingLanguage(), scopes.sourceOf(dep))
			target := dep.Namespace
			if namespace, internal := index.targetNamespaceOf(dep, file); internal {
				target = namespace
			}
			toNs := aggregate.NamespaceReducers.Reduce(file.GetProgrammingLanguage(), target)
			if fromNs == "" || toNs == "" || fromNs == toNs {
				continue
			}
			if edgesCount[fromNs] == nil {
				edgesCount[fromNs] = make(map[string]int)
			}
			edgesCount[fromNs][toNs]++
		}
	}
	// A source is the project's own even when its file declares no namespace.
	for fromNs := range edgesCount {
		internalNodes[fromNs] = true
	}

	// Apply combined filtering: abs + relative threshold and top-K per source, with fallback top-1
	const (
		absThreshold = 1
		relThreshold = 0.10 // 10% of total outgoing from source
		topK         = 5
	)
	// compute total outgoing per source
	totalOut := make(map[string]int, len(edgesCount))
	for fromNs, tos := range edgesCount {
		sum := 0
		for _, w := range tos {
			sum += w
		}
		totalOut[fromNs] = sum
	}
	// Sources and targets are walked in a fixed order: the edge list of a node is
	// a slice, so letting Go's map iteration decide would give a different graph
	// layout, and different community labels, on every run.
	// Note that with absThreshold at 1 no candidate is ever filtered out, since
	// an edge only exists once it has been counted at least once. The top-K and
	// relative thresholds only start to bite if absThreshold is raised.
	for _, fromNs := range slices.Sorted(maps.Keys(edgesCount)) {
		tos := edgesCount[fromNs]
		// ensure source node exists and is marked as package
		ensureNode(fromNs, &pb.Name{Qualified: fromNs, Short: fromNs, Package: fromNs})
		// collect and sort targets by weight desc, then by name to break ties
		type pair struct {
			to string
			w  int
		}
		arr := make([]pair, 0, len(tos))
		for toNs, w := range tos {
			arr = append(arr, pair{to: toNs, w: w})
		}
		slices.SortFunc(arr, func(a, b pair) int {
			if a.w != b.w {
				return b.w - a.w
			}
			return strings.Compare(a.to, b.to)
		})
		kept := make([]string, 0, len(arr))
		for i, p := range arr {
			if i < topK || p.w >= absThreshold || float64(p.w) >= relThreshold*float64(totalOut[fromNs]) {
				kept = append(kept, p.to)
			}
			// always ensure target node exists for visibility even if edge filtered
			ensureNode(p.to, &pb.Name{Qualified: p.to, Short: p.to, Package: p.to})
		}
		// fallback: if nothing kept but at least one candidate, keep the strongest edge
		if len(kept) == 0 && len(arr) > 0 {
			kept = append(kept, arr[0].to)
		}
		for _, toNs := range kept {
			addEdge(fromNs, toNs)
		}
	}

	// Mark the nodes foreign to the project: frameworks, libraries and the
	// standard library. A module of the project that depends on nothing is
	// still the project's own.
	if aggregate.ExternalNodes == nil {
		aggregate.ExternalNodes = make(map[string]bool)
	}
	for id := range aggregate.Graph.Nodes {
		if !internalNodes[id] {
			aggregate.ExternalNodes[id] = true
		}
	}
}
