package analyzer

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	graph "github.com/ast-metrics/ast-metrics/internal/analyzer/graph"
)

// CommunityMetrics describes the communities of the project: the groups of
// classes (or packages) that depend on each other more than on the rest of the
// code, found on the dependency graph alone, without looking at folder names.
type CommunityMetrics struct {
	// Granularity says what a unit is: "class" or "namespace".
	Granularity string
	// Communities, largest first. The shared kernel, when there is one, comes
	// last and is flagged Shared.
	Communities []*Community
	// NodeToCommunity maps a unit (a class qualified name or a namespace node)
	// onto the id of its community.
	NodeToCommunity map[string]string
	// Edges are the dependencies between communities, heaviest first.
	Edges []CommunityEdge
	// Cycles lists the groups of communities that depend on each other, each
	// group sorted by community id. The shared kernel takes no part in them.
	Cycles [][]string
	// Findings are the observations worth reading, most important first.
	Findings []CommunityFinding
	// Actions are the few things to do first, at most three, the most
	// valuable first, derived from the findings.
	Actions []CommunityAction

	// CommunitiesCount is the number of communities, the shared kernel left
	// out.
	CommunitiesCount int
	// MaxSize is the number of units of the largest community.
	MaxSize int
	// UnitCount is the number of units placed in a community or in the shared
	// kernel.
	UnitCount int
	// IsolatedUnits is the number of units of the project that neither use
	// nor are used by another unit of the project, or nearly so.
	IsolatedUnits int
	// InternalShare, SharedShare and CrossShare split the dependencies into
	// the ones staying inside a community, the ones going to the shared
	// kernel and the ones crossing from a community to another. They add up
	// to 1.
	InternalShare float64
	SharedShare   float64
	CrossShare    float64
	// Modularity is Newman's Q of the partition.
	Modularity float64
	// Confidence is the share of the units that stay with their community
	// when the detection is run again at other resolutions, weighted by the
	// size of the communities, the shared kernel left out. 1 when nothing
	// moves, 0 when it was not computed (a folder analysed on its own).
	Confidence float64
	// LargestCycle is the number of communities of the largest cycle, 0 when
	// there is none.
	LargestCycle int
	// Root is the namespace prefix every unit shares, empty when there is
	// none. Short names strip it.
	Root string
	// Labels gives the short name of every unit: the class name, or the
	// package path without the root.
	Labels map[string]string
	// UnitFiles gives the file declaring each unit, when it is known: a
	// class or a file of top-level code. Packages have none.
	UnitFiles map[string]string
	// CrossReferences are the references between units of different
	// communities, the shared kernel included: what the page needs to redraw
	// the map of a folder.
	CrossReferences []UnitReference
	// Folders holds the analysis of each folder taken on its own, sorted by
	// path.
	Folders []FolderCommunities
	// Verdict is the one sentence the page opens on; VerdictNote qualifies it.
	Verdict     string
	VerdictNote string
	// CohesiveCount is the number of communities drawn from a single namespace.
	CohesiveCount int
	// Shared is the shared kernel, nil when none stands out.
	Shared *Community
	// History: HistoryAvailable is true when the files carry any git data,
	// HistoryCommits is the number of distinct commits touching a placed
	// unit, and HistoryAgreement the share of the commits touching several
	// files whose files all stay in one community (the shared kernel
	// allowed): how far the history agrees with the boundaries.
	HistoryAvailable bool
	HistoryCommits   int
	HistoryAgreement float64
}

// Community is a group of units that depend on each other more than on the
// rest of the project.
type Community struct {
	ID string
	// Name is derived from the namespaces the members come from; ShortName
	// strips the root of the project.
	Name      string
	ShortName string
	// Hint names the units at the heart of the community.
	Hint string
	// Shared is true for the shared kernel: the units the whole codebase
	// leans on, which belong to no community in particular.
	Shared bool
	// Units, sorted.
	Units []string
	Size  int
	// Namespaces the units come from, largest share first.
	Namespaces []NamespaceShare
	// Cohesive is true when one namespace accounts for most of the units.
	Cohesive bool
	// Uses and UsedBy are the communities this one depends on and the ones
	// depending on it, heaviest first. The shared kernel is listed among them.
	Uses   []CommunityLink
	UsedBy []CommunityLink
	// Externals are the foreign packages the community relies on, most used
	// first, at most five.
	Externals []ExternalUse
	// Hubs are the units carrying the most dependencies inside the community;
	// for the shared kernel, the units used by the most communities.
	Hubs []string
	// InternalWeight counts the dependencies between members; OutWeight and
	// InWeight the ones leaving and reaching the community, the shared kernel
	// included; SharedWeight the part of OutWeight going to the shared kernel.
	InternalWeight int
	OutWeight      int
	InWeight       int
	SharedWeight   int
	// Exposed lists the members referenced from outside the community, from
	// another community or from the shared kernel, the most referenced first:
	// the surface the community offers to the rest of the code. ExposedShare
	// is ExposedCount over Size. Not computed for the shared kernel, which is
	// exposed by definition.
	Exposed      []string
	ExposedCount int
	ExposedShare float64
	// ForeignUses lists the units of other communities this one references,
	// the shared kernel left out, the most referenced first, at most ten;
	// ForeignUsesCount counts them all.
	ForeignUses      []string
	ForeignUsesCount int
	// Border lists the members that another resolution of the detection
	// places in another community, sorted: the units the map is least sure
	// about. Confidence is the share of the members every resolution keeps
	// here, 1 when no resolution moves any of them, 0 when it was not
	// computed (a folder analysed on its own).
	Border     []string
	Confidence float64
	// Owners: bus factor and the committers weighing the most, when git data
	// is available.
	BusFactor     int
	TopCommitters []CommitterShare
	CommitCount   int
	// History: what the commits of the year say of the community.
	// HistoryCommits is the number of distinct commits touching at least one
	// of its files, HistoryMultiFileCommits the ones among them touching
	// several analysed files, and HistoryCohesion the share of those staying
	// inside the community (the shared kernel allowed), 0 when they are too
	// few to mean anything. ChangesWith lists the other communities the same
	// commits touch, the most often first, at most five.
	HistoryCommits          int
	HistoryMultiFileCommits int
	HistoryCohesion         float64
	ChangesWith             []CommunityCoChange
}

// NamespaceShare is the part of a community drawn from one namespace.
type NamespaceShare struct {
	Namespace string
	// Label is the namespace without the root of the project, for display.
	Label string
	Count int
	Share float64
}

// CommunityLink is a dependency between two communities, seen from one side.
type CommunityLink struct {
	ID     string
	Name   string
	Weight int
	// Details are the heaviest unit-to-unit references behind the link, at
	// most five, heaviest first: which classes actually carry it.
	Details []UnitLink
}

// UnitLink is a reference from one unit to another, by their labels.
type UnitLink struct {
	From   string
	To     string
	Weight int
}

// UnitReference is a reference from one unit to another, by their ids.
type UnitReference struct {
	From   string
	To     string
	Weight int
}

// maxLinkDetails bounds the references detailed under a link.
const maxLinkDetails = 5

// ExternalUse counts the uses of a foreign package.
type ExternalUse struct {
	Namespace string
	Count     int
}

// CommitterShare is a committer and the commits they made on the files of a
// community.
type CommitterShare struct {
	Name    string
	Commits int
}

// CommunityEdge is an aggregated directed dependency between two communities.
type CommunityEdge struct {
	From   string
	To     string
	Weight int
	// InCycle is true when To also reaches From, directly or through others.
	InCycle bool
	// Back is true for the edges that, cut, would leave no cycle: the
	// lightest way through the cycles, found by a greedy heuristic. They are
	// the ones drawn against the flow of the map.
	Back bool
	// Shared is true when either end is the shared kernel.
	Shared bool
}

// CommunityFinding is an observation about the communities, in plain words.
type CommunityFinding struct {
	// Kind is one of "cycle", "shared", "shared-leak", "exposed", "split",
	// "spread", "layered", "bridge", "history-crossed", "history-loose".
	Kind string
	// Title states the observation; Detail gives the figures behind it and
	// what it usually means.
	Title  string
	Detail string
	// Communities and Units are the ones the finding is about.
	Communities []string
	Units       []string
	// Category is the one the suggestions page files it under.
	Category string
}

// SharedID is the id of the shared kernel among the communities.
const SharedID = "shared"

// Thresholds of the community analysis.
const (
	// A community drawn at least this much from a single namespace is cohesive.
	cohesiveShare = 0.75
	// A community is named after a single namespace once that namespace holds
	// this share of it; below, the next namespaces join the name.
	singleNameShare = 0.5
	// Communities smaller than this are folded into the neighbour they depend
	// on the most: two classes are a pair, not a module.
	minCommunitySize = 3
	// A unit belongs to the shared kernel when at least sharedMinUsers units
	// from sharedMinCommunities namespaces use it, at most sharedMaxUserLinkage
	// of the pairs of those users know each other, and the project has at
	// least sharedMinPartition namespaces for "shared by all" to mean
	// something.
	sharedMinUsers       = 3
	sharedMinCommunities = 3
	sharedMinDegree      = 3
	sharedMaxUserLinkage = 0.15
	sharedMinPartition   = 4
	// A namespace is split when its units are spread over at least this many
	// communities, none holding half of them.
	splitMinCommunities = 3
	splitMinUnits       = 6
	// A community is worth reporting as spread across namespaces once it holds
	// this many units.
	spreadMinUnits = 5
	// A bridge is a unit that reaches this many other communities.
	bridgeMinCommunities = 2
	// A community of at least spreadMinUnits units has no boundary once this
	// share of its members is used from outside.
	exposedMinShare = 0.5
	// At most this many foreign units are listed per community.
	maxForeignUses = 10
	// A cycle holding at least this many communities and this share of them
	// is one block rather than one cycle among others.
	blobMinCommunities = 4
	blobMinShare       = 0.34
	// At most this many findings of each kind.
	maxFindingsPerKind = 5
	// A kernel drawing at least this share of the dependencies, or holding
	// this share of the units, is the centre of gravity of the code rather
	// than a kernel.
	kernelHeavyShare     = 0.25
	kernelHeavyUnitShare = 0.10
)

// borderResolutions are the other resolutions the detection is run at to tell
// the units a community is sure of from the ones on its border: a little
// coarser and a little finer than the map itself.
var borderResolutions = []float64{0.7, 0.85, 1.2, 1.4}

// CommunityAggregator finds the communities of the project.
type CommunityAggregator struct{}

func NewCommunityAggregator() *CommunityAggregator { return &CommunityAggregator{} }

func (ca *CommunityAggregator) Calculate(aggregate *Aggregated) {
	if aggregate == nil || len(aggregate.ConcernedFiles) == 0 {
		return
	}
	g := buildUnitGraph(aggregate)
	cm := computeCommunities(g, aggregate)
	aggregate.Community = cm
	cm.Folders = folderCommunities(g, cm)
}

// computeCommunities runs the analysis on a unit graph. With an aggregate,
// the owners, the findings and the suggestions are computed as well; without
// one (a folder analysed on its own), only the communities and their edges.
func computeCommunities(g *unitGraph, aggregate *Aggregated) *CommunityMetrics {
	cm := &CommunityMetrics{
		Granularity:     g.Granularity,
		NodeToCommunity: map[string]string{},
		Root:            commonRootOfNamespaces(g),
		Labels:          map[string]string{},
		UnitFiles:       map[string]string{},
	}
	for _, unit := range g.Units {
		cm.Labels[unit] = labelOfUnit(unit, g, cm.Root)
		if file := g.FileOf[unit]; file != nil && file.Path != "" {
			cm.UnitFiles[unit] = file.Path
		}
	}
	if len(g.Units) == 0 {
		cm.IsolatedUnits = g.Total
		cm.Verdict, cm.VerdictNote = verdictOf(cm)
		return cm
	}

	// The shared kernel is taken out before the communities are detected: the
	// units the whole codebase leans on land in one community or another by
	// chance and would tie it to every other.
	shared := sharedKernelOf(g)
	partition := louvainOn(g, shared, 1)
	membership := make(map[string]int, len(partition.Community))
	maps.Copy(membership, partition.Community)
	membership = foldSmallCommunities(g, membership)
	cm.Modularity = partition.Modularity

	// Number the communities by size, then by smallest member. A pair of
	// classes linked to nothing else is not a community: it stands apart, and
	// is counted with the units that take no part.
	members := map[int][]string{}
	for _, unit := range g.Units {
		if shared[unit] {
			continue
		}
		c := membership[unit]
		members[c] = append(members[c], unit)
	}
	standingApart := 0
	for label, units := range members {
		if len(units) >= minCommunitySize {
			continue
		}
		delete(members, label)
		standingApart += len(units)
		for _, unit := range units {
			delete(membership, unit)
		}
	}
	labels := slices.SortedFunc(maps.Keys(members), func(x, y int) int {
		if len(members[x]) != len(members[y]) {
			return len(members[y]) - len(members[x])
		}
		return strings.Compare(members[x][0], members[y][0])
	})
	communities := make([]*Community, 0, len(labels)+1)
	for i, label := range labels {
		c := &Community{ID: fmt.Sprintf("%d", i), Units: members[label], Size: len(members[label])}
		communities = append(communities, c)
		for _, unit := range c.Units {
			cm.NodeToCommunity[unit] = c.ID
		}
	}
	cm.CommunitiesCount = len(communities)
	if len(communities) > 0 {
		cm.MaxSize = communities[0].Size
	}
	if len(shared) > 0 {
		units := slices.Sorted(maps.Keys(shared))
		cm.Shared = &Community{ID: SharedID, Shared: true, Units: units, Size: len(units)}
		communities = append(communities, cm.Shared)
		for _, unit := range units {
			cm.NodeToCommunity[unit] = SharedID
		}
	}
	cm.Communities = communities
	cm.UnitCount = len(g.Units) - standingApart
	cm.IsolatedUnits = max(0, g.Total-cm.UnitCount)

	// Namespaces, names and hubs.
	for _, c := range communities {
		c.Namespaces = namespaceSharesOf(c.Units, g)
		for i := range c.Namespaces {
			c.Namespaces[i].Label = stripRoot(namespaceLabel(c.Namespaces[i].Namespace), cm.Root)
		}
		c.Cohesive = len(c.Namespaces) > 0 && c.Namespaces[0].Share >= cohesiveShare
		if c.Cohesive && !c.Shared {
			cm.CohesiveCount++
		}
		if c.Shared {
			c.Hubs = sharedHubsOf(c.Units, g, cm.NodeToCommunity)
		} else {
			c.Hubs = hubsOf(c.Units, g, cm.NodeToCommunity)
		}
		c.Hint = strings.Join(labelsOf(c.Hubs, cm.Labels), ", ")
		c.Externals = externalsOf(c.Units, g)
	}
	for _, c := range communities {
		if cm.Granularity == GranularityNamespace {
			c.Name = nameOfPackageCommunity(c.Units, cm.Root, labelsOf(c.Hubs, cm.Labels))
		} else {
			c.Name = nameOfCommunity(c.Namespaces, labelsOf(c.Units, cm.Labels), labelsOf(c.Hubs, cm.Labels))
		}
	}
	disambiguateNames(communities)
	for _, c := range communities {
		if c.Shared {
			// named as what it is, wherever it is quoted
			c.Name = "Shared kernel (" + c.Name + ")"
		}
		c.ShortName = stripRoot(c.Name, cm.Root)
	}

	// Edges between communities and the weights.
	byID := map[string]*Community{}
	for _, c := range communities {
		byID[c.ID] = c
	}
	weights := map[string]map[string]int{}
	// the references behind each community edge, to tell which units carry it
	carriers := map[string][]UnitLink{}
	totalWeight, internalWeight, sharedWeight := 0, 0, 0
	for a, tos := range g.Out {
		ca, placed := cm.NodeToCommunity[a]
		if !placed {
			continue
		}
		for b, w := range tos {
			cb, placed := cm.NodeToCommunity[b]
			if !placed {
				continue
			}
			totalWeight += w
			if ca == cb {
				byID[ca].InternalWeight += w
				internalWeight += w
				continue
			}
			if weights[ca] == nil {
				weights[ca] = map[string]int{}
			}
			weights[ca][cb] += w
			carriers[ca+">"+cb] = append(carriers[ca+">"+cb], UnitLink{From: a, To: b, Weight: w})
			cm.CrossReferences = append(cm.CrossReferences, UnitReference{From: a, To: b, Weight: w})
			byID[ca].OutWeight += w
			byID[cb].InWeight += w
			if cb == SharedID {
				byID[ca].SharedWeight += w
				sharedWeight += w
			}
		}
	}
	slices.SortFunc(cm.CrossReferences, func(x, y UnitReference) int {
		if x.From != y.From {
			return strings.Compare(x.From, y.From)
		}
		return strings.Compare(x.To, y.To)
	})
	if totalWeight > 0 {
		cm.InternalShare = float64(internalWeight) / float64(totalWeight)
		cm.SharedShare = float64(sharedWeight) / float64(totalWeight)
		cm.CrossShare = 1 - cm.InternalShare - cm.SharedShare
	}
	for _, from := range slices.SortedFunc(maps.Keys(weights), compareIDs) {
		for _, to := range slices.SortedFunc(maps.Keys(weights[from]), compareIDs) {
			w := weights[from][to]
			details := carriers[from+">"+to]
			slices.SortFunc(details, func(x, y UnitLink) int {
				if x.Weight != y.Weight {
					return y.Weight - x.Weight
				}
				if x.From != y.From {
					return strings.Compare(x.From, y.From)
				}
				return strings.Compare(x.To, y.To)
			})
			if len(details) > maxLinkDetails {
				details = details[:maxLinkDetails]
			}
			labelled := make([]UnitLink, 0, len(details))
			for _, d := range details {
				labelled = append(labelled, UnitLink{From: cm.Labels[d.From], To: cm.Labels[d.To], Weight: d.Weight})
			}
			cm.Edges = append(cm.Edges, CommunityEdge{From: from, To: to, Weight: w, Shared: from == SharedID || to == SharedID})
			byID[from].Uses = append(byID[from].Uses, CommunityLink{ID: to, Name: byID[to].ShortName, Weight: w, Details: labelled})
			byID[to].UsedBy = append(byID[to].UsedBy, CommunityLink{ID: from, Name: byID[from].ShortName, Weight: w, Details: labelled})
		}
	}
	slices.SortStableFunc(cm.Edges, func(x, y CommunityEdge) int { return y.Weight - x.Weight })
	for _, c := range communities {
		slices.SortStableFunc(c.Uses, func(x, y CommunityLink) int { return y.Weight - x.Weight })
		slices.SortStableFunc(c.UsedBy, func(x, y CommunityLink) int { return y.Weight - x.Weight })
	}

	// Cycles between communities, the shared kernel left out.
	cm.Cycles = cyclesOf(cm)
	inCycle := map[string]string{}
	for i, cycle := range cm.Cycles {
		for _, id := range cycle {
			inCycle[id] = fmt.Sprintf("%d", i)
		}
	}
	for i := range cm.Edges {
		e := &cm.Edges[i]
		if a, ok := inCycle[e.From]; ok && inCycle[e.To] == a {
			e.InCycle = true
		}
	}
	markBackEdges(cm)
	for _, cycle := range cm.Cycles {
		cm.LargestCycle = max(cm.LargestCycle, len(cycle))
	}
	surfaceOfCommunities(cm, g)

	cm.Verdict, cm.VerdictNote = verdictOf(cm)
	if aggregate == nil {
		return cm
	}
	// The border of each community costs four more detections: a folder
	// analysed on its own goes without it.
	borderOfCommunities(cm, g, shared)
	ownersOfCommunities(aggregate, cm, g)
	historyOf(aggregate, cm, g)
	cm.Findings = withHistoryFindings(findingsOf(cm, g, weights), historyFindingsOf(cm))
	cm.Actions = actionsOf(cm)

	// The suggestions page files the findings with the other observations.
	if aggregate.Suggestions == nil {
		aggregate.Suggestions = make([]Suggestion, 0)
	}
	for _, f := range cm.Findings {
		location := ""
		if len(f.Units) > 0 {
			location = f.Units[0]
		} else if len(f.Communities) > 0 {
			names := make([]string, 0, len(f.Communities))
			for _, id := range f.Communities {
				names = append(names, byID[id].ShortName)
			}
			location = strings.Join(names, ", ")
		}
		aggregate.Suggestions = append(aggregate.Suggestions, Suggestion{
			Summary:             f.Title,
			Location:            location,
			Why:                 f.Detail,
			DetailedExplanation: f.Detail,
			Category:            f.Category,
		})
	}
	return cm
}

// FolderCommunities is the analysis of one folder taken on its own: the
// communities its units form among themselves, as if the folder had been
// analysed alone. The page draws it when the reader zooms on the folder.
type FolderCommunities struct {
	// Path of the folder, relative to the directory every unit file shares.
	Path string
	// UnitCount is the number of units the folder holds.
	UnitCount   int
	Communities []*Community
	Edges       []CommunityEdge
	Shared      *Community
	Verdict     string
	VerdictNote string
}

// Thresholds of the analysis per folder.
const (
	// A folder is analysed on its own from this many units.
	folderMinUnits = 6
	// At most this many folders are analysed, the largest first.
	folderMaxCount = 300
)

// folderCommunities analyses each folder of the project on its own, so that
// zooming on a folder shows the communities its code forms by itself, the
// way analysing that folder alone would.
func folderCommunities(g *unitGraph, cm *CommunityMetrics) []FolderCommunities {
	if len(cm.UnitFiles) == 0 {
		return nil
	}
	paths := make([]string, 0, len(cm.UnitFiles))
	for _, path := range cm.UnitFiles {
		paths = append(paths, filepath.ToSlash(path))
	}
	slices.Sort(paths)
	root := commonDirectoryOfPaths(slices.Compact(paths))
	// the units under each folder
	unitsOf := map[string][]string{}
	for _, unit := range g.Units {
		path, ok := cm.UnitFiles[unit]
		if !ok {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(filepath.ToSlash(path), root), "/")
		parts := strings.Split(relative, "/")
		folder := ""
		for i := 0; i < len(parts)-1; i++ {
			if folder == "" {
				folder = parts[i]
			} else {
				folder = folder + "/" + parts[i]
			}
			unitsOf[folder] = append(unitsOf[folder], unit)
		}
	}
	folders := slices.SortedFunc(maps.Keys(unitsOf), func(x, y string) int {
		if len(unitsOf[x]) != len(unitsOf[y]) {
			return len(unitsOf[y]) - len(unitsOf[x])
		}
		return strings.Compare(x, y)
	})
	out := []FolderCommunities{}
	for _, folder := range folders {
		units := unitsOf[folder]
		if len(units) < folderMinUnits || len(units) == len(g.Units) {
			continue
		}
		if len(out) >= folderMaxCount {
			break
		}
		local := computeCommunities(subgraphOf(g, units), nil)
		out = append(out, FolderCommunities{
			Path:        folder,
			UnitCount:   len(units),
			Communities: local.Communities,
			Edges:       local.Edges,
			Shared:      local.Shared,
			Verdict:     local.Verdict,
			VerdictNote: local.VerdictNote,
		})
	}
	slices.SortFunc(out, func(x, y FolderCommunities) int { return strings.Compare(x.Path, y.Path) })
	return out
}

// commonDirectoryOfPaths returns the directory every path sits under.
func commonDirectoryOfPaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := strings.Split(filepath.Dir(paths[0]), "/")
	for _, path := range paths[1:] {
		parts := strings.Split(filepath.Dir(path), "/")
		n := 0
		for n < len(prefix) && n < len(parts) && prefix[n] == parts[n] {
			n++
		}
		prefix = prefix[:n]
		if n == 0 {
			return ""
		}
	}
	return strings.Join(prefix, "/")
}

// louvainOn detects the communities of the unit graph at the given
// resolution, the excluded units and their edges left out.
func louvainOn(g *unitGraph, excluded map[string]bool, resolution float64) graph.Partition {
	units := make([]string, 0, len(g.Units))
	for _, unit := range g.Units {
		if !excluded[unit] {
			units = append(units, unit)
		}
	}
	edges := make([]graph.WeightedEdge, 0)
	for _, a := range slices.Sorted(maps.Keys(g.Out)) {
		if excluded[a] {
			continue
		}
		for _, b := range slices.Sorted(maps.Keys(g.Out[a])) {
			if excluded[b] {
				continue
			}
			edges = append(edges, graph.WeightedEdge{A: a, B: b, Weight: float64(g.Out[a][b])})
		}
	}
	return graph.Louvain(units, edges, resolution)
}

// sharedKernelOf returns the units the whole codebase leans on: a base
// class, a value object, an interface everybody implements. Such a unit is
// used from several namespaces, and the units using it have nothing to do
// with each other: they meet there and nowhere else. The second condition is
// what tells a kernel from the heart of a feature: an entity used by its
// repository, its controller and its form is used from several namespaces
// too, but those three know each other.
//
// The kernel is read off the graph before any community is drawn, so that it
// does not depend on where the detection happened to place a hub.
func sharedKernelOf(g *unitGraph) map[string]bool {
	shared := map[string]bool{}
	// undirected neighbourhoods
	neighbours := map[string]map[string]struct{}{}
	link := func(a, b string) {
		if neighbours[a] == nil {
			neighbours[a] = map[string]struct{}{}
		}
		neighbours[a][b] = struct{}{}
	}
	inWeight := map[string]int{}
	for a, tos := range g.Out {
		for b, w := range tos {
			link(a, b)
			link(b, a)
			inWeight[b] += w
		}
	}
	// the namespaces the project has: "shared by all" needs enough of them
	namespaces := map[string]struct{}{}
	for _, unit := range g.Units {
		namespaces[g.Namespace[unit]] = struct{}{}
	}
	if len(namespaces) < sharedMinPartition {
		return shared
	}
	for _, unit := range g.Units {
		users := g.In[unit]
		if len(users) < sharedMinUsers || inWeight[unit] < sharedMinDegree {
			continue
		}
		fromNamespaces := map[string]struct{}{}
		for a := range users {
			fromNamespaces[g.Namespace[a]] = struct{}{}
		}
		if len(fromNamespaces) < sharedMinCommunities {
			continue
		}
		// how many pairs of users know each other
		list := slices.Sorted(maps.Keys(users))
		pairs, linked := 0, 0
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				pairs++
				if _, ok := neighbours[list[i]][list[j]]; ok {
					linked++
				}
			}
		}
		if pairs > 0 && float64(linked) <= sharedMaxUserLinkage*float64(pairs) {
			shared[unit] = true
		}
	}
	return shared
}

// foldSmallCommunities moves the units of the communities smaller than
// minCommunitySize into the community they exchange the most with. A small
// community linked to nothing else stays as it is: it is a corner of the code
// living on its own, and there is nowhere to fold it.
func foldSmallCommunities(g *unitGraph, membership map[string]int) map[string]int {
	for {
		size := map[int]int{}
		for _, unit := range g.Units {
			if label, placed := membership[unit]; placed {
				size[label]++
			}
		}
		small := []int{}
		for label, n := range size {
			if n < minCommunitySize {
				small = append(small, label)
			}
		}
		if len(small) == 0 {
			return membership
		}
		slices.Sort(small)
		moved := false
		for _, label := range small {
			// weight exchanged with each other community
			exchanged := map[int]int{}
			for _, unit := range g.Units {
				if l, placed := membership[unit]; !placed || l != label {
					continue
				}
				for b, w := range g.Out[unit] {
					if lb, placed := membership[b]; placed && lb != label {
						exchanged[lb] += w
					}
				}
				for a, w := range g.In[unit] {
					if la, placed := membership[a]; placed && la != label {
						exchanged[la] += w
					}
				}
			}
			best, bestW := -1, 0
			for _, other := range slices.Sorted(maps.Keys(exchanged)) {
				// prefer the heaviest exchange, then the larger community
				if exchanged[other] > bestW || (exchanged[other] == bestW && best >= 0 && size[other] > size[best]) {
					best, bestW = other, exchanged[other]
				}
			}
			if best < 0 {
				continue
			}
			for _, unit := range g.Units {
				if l, placed := membership[unit]; placed && l == label {
					membership[unit] = best
				}
			}
			size[best] += size[label]
			size[label] = 0
			moved = true
		}
		if !moved {
			return membership
		}
	}
}

// namespaceSharesOf counts the namespaces the units come from.
func namespaceSharesOf(units []string, g *unitGraph) []NamespaceShare {
	count := map[string]int{}
	for _, unit := range units {
		count[g.Namespace[unit]]++
	}
	shares := make([]NamespaceShare, 0, len(count))
	for _, namespace := range slices.Sorted(maps.Keys(count)) {
		shares = append(shares, NamespaceShare{Namespace: namespace, Count: count[namespace], Share: float64(count[namespace]) / float64(len(units))})
	}
	slices.SortStableFunc(shares, func(x, y NamespaceShare) int { return y.Count - x.Count })
	return shares
}

// hubsOf returns the three units exchanging the most with the rest of their
// community.
func hubsOf(units []string, g *unitGraph, community map[string]string) []string {
	if len(units) == 0 {
		return nil
	}
	own := community[units[0]]
	degree := map[string]int{}
	for _, unit := range units {
		for b, w := range g.Out[unit] {
			if community[b] == own {
				degree[unit] += w
				degree[b] += w
			}
		}
	}
	return topByDegree(units, degree)
}

// sharedHubsOf returns the three units of the shared kernel reaching the most
// communities, then carrying the most references.
func sharedHubsOf(units []string, g *unitGraph, community map[string]string) []string {
	degree := map[string]int{}
	for _, unit := range units {
		reached := map[string]struct{}{}
		weight := 0
		for a, w := range g.In[unit] {
			if community[a] != SharedID {
				reached[community[a]] = struct{}{}
				weight += w
			}
		}
		degree[unit] = len(reached)*1000 + weight
	}
	return topByDegree(units, degree)
}

func topByDegree(units []string, degree map[string]int) []string {
	sorted := slices.SortedFunc(slices.Values(units), func(x, y string) int {
		if degree[x] != degree[y] {
			return degree[y] - degree[x]
		}
		return strings.Compare(x, y)
	})
	hubs := []string{}
	for _, unit := range sorted {
		if degree[unit] == 0 || len(hubs) == 3 {
			break
		}
		hubs = append(hubs, unit)
	}
	return hubs
}

// externalsOf lists the five foreign packages the units rely on the most.
func externalsOf(units []string, g *unitGraph) []ExternalUse {
	count := map[string]int{}
	for _, unit := range units {
		for ns, n := range g.Externals[unit] {
			count[ns] += n
		}
	}
	uses := make([]ExternalUse, 0, len(count))
	for _, ns := range slices.Sorted(maps.Keys(count)) {
		uses = append(uses, ExternalUse{Namespace: ns, Count: count[ns]})
	}
	slices.SortStableFunc(uses, func(x, y ExternalUse) int { return y.Count - x.Count })
	if len(uses) > 5 {
		uses = uses[:5]
	}
	return uses
}

// nameOfCommunity derives a name for a community of classes: the namespace
// holding most of them, else the word its members share, the way a feature
// cutting across the layers of an application does (User, Invoice, Tag),
// else the unit at its heart. A shared word naming a layer (Repository,
// Handler) takes the namespace holding most of the members beside it
// ("Repository · Component\Scm"), so that the reader recognizes the code. A sum of namespaces ("Admin + Billing + …")
// says where the members were filed, not what they do together, and is kept
// as a last resort, two namespaces at most.
func nameOfCommunity(shares []NamespaceShare, unitLabels []string, hubLabels []string) string {
	if len(shares) == 0 {
		return "unnamed"
	}
	if shares[0].Namespace != "" && (shares[0].Share >= singleNameShare || len(shares) == 1) {
		return namespaceLabel(shares[0].Namespace)
	}
	if token := dominantToken(unitLabels); token != "" {
		// a layer word alone (Repository, Handler) says what kind of classes
		// they are, not which: the namespace holding most of them says where
		if looksLikeLayer(token) && shares[0].Namespace != "" {
			return token + " · " + namespaceLabel(shares[0].Namespace)
		}
		return token
	}
	if len(hubLabels) > 0 {
		return hubLabels[0]
	}
	if len(shares) >= 2 && shares[0].Namespace != "" && shares[1].Namespace != "" {
		return namespaceLabel(shares[0].Namespace) + " + " + namespaceLabel(shares[1].Namespace)
	}
	return namespaceLabel(shares[0].Namespace)
}

// topLevelCodeMark is appended to the label of a namespace unit standing
// among classes.
const topLevelCodeMark = " (top-level code)"

// isWord tells whether a token is made of letters and digits only: a bracket
// or a dot left by a label cannot name a community.
func isWord(token string) bool {
	for _, r := range token {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// labelOfUnit is the short name of a unit: the class name, or the package
// path without the root of the project.
func labelOfUnit(unit string, g *unitGraph, root string) string {
	if g.IsClass[unit] {
		return lastSegment(unit)
	}
	if g.IsFile[unit] {
		// a file of top-level code among classes
		return filepath.Base(unit) + " (file)"
	}
	if unit == "." {
		// the module at the root of a package, in TypeScript
		return "(package root)"
	}
	if g.Granularity == GranularityClass {
		// a namespace among classes: the code written outside any class
		return stripRoot(unit, root) + topLevelCodeMark
	}
	return stripRoot(unit, root)
}

// labelsOf maps units onto their labels.
func labelsOf(units []string, labels map[string]string) []string {
	names := make([]string, 0, len(units))
	for _, unit := range units {
		if label, ok := labels[unit]; ok {
			names = append(names, label)
		} else {
			names = append(names, lastSegment(unit))
		}
	}
	return names
}

// nameOfPackageCommunity names a community of packages: after the prefix its
// packages share when it says more than the root of the project, else after
// the packages at its heart. A namespace share would name every community
// after the parent folder they all sit in.
func nameOfPackageCommunity(units []string, root string, hubLabels []string) string {
	if prefix := commonPrefixOfNamespaces(units); prefix != "" && len(prefix) > len(root) {
		return prefix
	}
	names := hubLabels
	if len(names) > 2 {
		names = names[:2]
	}
	if len(names) > 0 {
		return strings.Join(names, " & ")
	}
	if len(units) > 0 {
		return units[0]
	}
	return "unnamed"
}

// namespaceLabel names a namespace for a reader, the empty one included.
func namespaceLabel(namespace string) string {
	if namespace == "" {
		return "(no namespace)"
	}
	return namespace
}

// nameTokenStopList lists the words of a class name that say what kind of
// class it is rather than what it is about, and cannot name a community.
var nameTokenStopList = map[string]bool{
	"Interface": true, "Abstract": true, "Base": true, "Impl": true, "Default": true,
	"Simple": true, "Generic": true, "Test": true, "Trait": true, "Enum": true,
	"Class": true, "Object": true, "Item": true, "Data": true, "Info": true,
	"Php": true, "Java": true, "Csharp": true, "Python": true, "Rust": true, "Golang": true,
}

// dominantToken returns the word most of the units share in their names, or
// an empty string when no word is shared by at least two of them and two
// fifths of them.
func dominantToken(labels []string) string {
	if len(labels) < 2 {
		return ""
	}
	count := map[string]int{}
	for _, label := range labels {
		seen := map[string]bool{}
		// a file or a package of top-level code is named by its path and a
		// mark of what it is: the mark says nothing about the feature
		name := strings.TrimSuffix(label, topLevelCodeMark)
		if strings.HasSuffix(name, " (file)") {
			name = strings.TrimSuffix(name, " (file)")
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		for _, token := range splitCamel(lastSegment(name)) {
			if len(token) < 3 || nameTokenStopList[token] || seen[token] || !isWord(token) {
				continue
			}
			seen[token] = true
			count[token]++
		}
	}
	best, bestCount := "", 0
	for _, token := range slices.Sorted(maps.Keys(count)) {
		n := count[token]
		if n > bestCount || (n == bestCount && len(token) > len(best)) {
			best, bestCount = token, n
		}
	}
	if bestCount < 2 || bestCount*5 < len(labels)*2 {
		return ""
	}
	return best
}

// splitCamel splits a class name on its capitals and separators:
// "TagArrayToStringTransformer" gives Tag, Array, To, String, Transformer,
// "user_repository" gives User, Repository, and an acronym holds together.
func splitCamel(name string) []string {
	tokens := []string{}
	current := []rune{}
	flush := func() {
		if len(current) > 0 {
			word := string(current)
			word = strings.ToUpper(word[:1]) + word[1:]
			tokens = append(tokens, word)
			current = current[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ':
			flush()
		case r >= 'A' && r <= 'Z':
			// a capital starts a word, unless it continues an acronym that
			// the next letter does not end (HTTPClient: HTTP, Client)
			if len(current) > 0 {
				last := current[len(current)-1]
				nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if !(last >= 'A' && last <= 'Z') || nextLower {
					flush()
				}
			}
			current = append(current, r)
		default:
			current = append(current, r)
		}
	}
	flush()
	return tokens
}

// disambiguateNames tells apart the communities bearing the same name: the
// largest keeps the plain name, the others are told by the units at their
// heart. A community named after its first hub takes its second hub beside
// it ("GithubOrganization · LogEventVisit"); one named after a namespace or a
// word takes its first hub in brackets ("Billing (Invoice)").
func disambiguateNames(communities []*Community) {
	taken := map[string]bool{}
	byName := map[string][]*Community{}
	for _, c := range communities {
		if c.Shared {
			// the kernel is always presented as such
			continue
		}
		byName[c.Name] = append(byName[c.Name], c)
	}
	// communities come largest first, and the names are visited in that
	// order too, so that a name freed by nobody is claimed the same way each
	// time
	for _, c := range communities {
		same := byName[c.Name]
		if c.Shared || len(same) < 2 || same[0] == c {
			taken[c.Name] = true
			continue
		}
		hubs := hubsOfHint(c.Hint)
		candidate := ""
		switch {
		case len(hubs) >= 2 && c.Name == hubs[0]:
			candidate = hubs[0] + " · " + hubs[1]
		case len(hubs) >= 1 && c.Name != hubs[0]:
			candidate = c.Name + " (" + hubs[0] + ")"
		}
		if candidate == "" || taken[candidate] {
			candidate = c.Name + " #" + c.ID
		}
		c.Name = candidate
		taken[candidate] = true
	}
}

// hubsOfHint reads the hub labels back from a hint.
func hubsOfHint(hint string) []string {
	if hint == "" {
		return nil
	}
	return strings.Split(hint, ", ")
}

// markBackEdges flags the edges that, cut, would leave the community graph
// without a cycle, choosing light edges over heavy ones: the sequence of
// communities is built with the Eades-Lin-Smyth heuristic on the weighted
// graph, and the edges going against that sequence are the back edges.
func markBackEdges(cm *CommunityMetrics) {
	ids := []string{}
	for _, c := range cm.Communities {
		if !c.Shared {
			ids = append(ids, c.ID)
		}
	}
	outW := map[string]map[string]int{}
	inW := map[string]map[string]int{}
	for _, e := range cm.Edges {
		if e.Shared || !e.InCycle {
			continue
		}
		if outW[e.From] == nil {
			outW[e.From] = map[string]int{}
		}
		if inW[e.To] == nil {
			inW[e.To] = map[string]int{}
		}
		outW[e.From][e.To] += e.Weight
		inW[e.To][e.From] += e.Weight
	}
	remaining := map[string]bool{}
	for _, id := range ids {
		if len(outW[id]) > 0 || len(inW[id]) > 0 {
			remaining[id] = true
		}
	}
	head, tail := []string{}, []string{}
	sum := func(m map[string]int) int {
		total := 0
		for other, w := range m {
			if remaining[other] {
				total += w
			}
		}
		return total
	}
	for len(remaining) > 0 {
		moved := true
		for moved {
			moved = false
			for _, id := range slices.SortedFunc(maps.Keys(remaining), compareIDs) {
				if sum(outW[id]) == 0 {
					tail = append(tail, id)
					delete(remaining, id)
					moved = true
				}
			}
			for _, id := range slices.SortedFunc(maps.Keys(remaining), compareIDs) {
				if sum(inW[id]) == 0 {
					head = append(head, id)
					delete(remaining, id)
					moved = true
				}
			}
		}
		if len(remaining) == 0 {
			break
		}
		best, bestDelta := "", 0
		for _, id := range slices.SortedFunc(maps.Keys(remaining), compareIDs) {
			delta := sum(outW[id]) - sum(inW[id])
			if best == "" || delta > bestDelta {
				best, bestDelta = id, delta
			}
		}
		head = append(head, best)
		delete(remaining, best)
	}
	slices.Reverse(tail)
	position := map[string]int{}
	for i, id := range append(head, tail...) {
		position[id] = i
	}
	for i := range cm.Edges {
		e := &cm.Edges[i]
		if e.Shared || !e.InCycle {
			continue
		}
		e.Back = position[e.From] > position[e.To]
	}
}

// cyclesOf finds the strongly connected components of the community graph
// with more than one member, sorted by id. The shared kernel is left out: it
// is used by everyone by definition.
func cyclesOf(cm *CommunityMetrics) [][]string {
	adj := map[string][]string{}
	ids := make([]string, 0, len(cm.Communities))
	for _, c := range cm.Communities {
		if !c.Shared {
			ids = append(ids, c.ID)
		}
	}
	for _, e := range cm.Edges {
		if e.Shared {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	for id := range adj {
		slices.Sort(adj[id])
	}
	// Tarjan
	index := 0
	indices := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	stack := []string{}
	cycles := [][]string{}
	var strong func(v string)
	strong = func(v string) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, seen := indices[w]; !seen {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], indices[w])
			}
		}
		if low[v] == indices[v] {
			component := []string{}
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == v {
					break
				}
			}
			if len(component) > 1 {
				slices.SortFunc(component, compareIDs)
				cycles = append(cycles, component)
			}
		}
	}
	for _, id := range ids {
		if _, seen := indices[id]; !seen {
			strong(id)
		}
	}
	slices.SortFunc(cycles, func(x, y []string) int { return compareIDs(x[0], y[0]) })
	return cycles
}

// compareIDs orders community ids numerically, the shared kernel last.
func compareIDs(x, y string) int {
	if x == y {
		return 0
	}
	if x == SharedID {
		return 1
	}
	if y == SharedID {
		return -1
	}
	if len(x) != len(y) {
		return len(x) - len(y)
	}
	return strings.Compare(x, y)
}

// findingsOf writes down what is worth reading in the communities.
func findingsOf(cm *CommunityMetrics, g *unitGraph, weights map[string]map[string]int) []CommunityFinding {
	findings := []CommunityFinding{}
	byID := map[string]*Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}
	unitWord := "classes"
	if cm.Granularity == GranularityNamespace {
		unitWord = "packages"
	}

	// 1. Cycles: communities depending on each other.
	for i, cycle := range cm.Cycles {
		if i >= maxFindingsPerKind {
			break
		}
		names := make([]string, 0, len(cycle))
		for _, id := range cycle {
			names = append(names, byID[id].ShortName)
		}
		var title, detail string
		if len(cycle) == 2 {
			a, b := cycle[0], cycle[1]
			ab, ba := weights[a][b], weights[b][a]
			if ab < ba {
				a, b, ab, ba = b, a, ba, ab
			}
			title = fmt.Sprintf("%s and %s depend on each other", byID[a].ShortName, byID[b].ShortName)
			detail = fmt.Sprintf("%s uses %s in %d places, and %s uses %s back in %d. Neither can change or be tested without the other; the lighter direction, %s → %s, is the one to cut.",
				byID[a].ShortName, byID[b].ShortName, ab, byID[b].ShortName, byID[a].ShortName, ba, byID[b].ShortName, byID[a].ShortName)
		} else {
			title = fmt.Sprintf("%d communities form a dependency cycle", len(cycle))
			inCycle := map[string]bool{}
			for _, id := range cycle {
				inCycle[id] = true
			}
			cuts := []string{}
			cutWeight, cutCount := 0, 0
			for _, e := range cm.Edges {
				if !e.Back || !inCycle[e.From] || !inCycle[e.To] {
					continue
				}
				cutCount++
				cutWeight += e.Weight
				if len(cuts) < 4 {
					cuts = append(cuts, fmt.Sprintf("%s → %s (%d)", byID[e.From].ShortName, byID[e.To].ShortName, e.Weight))
				}
			}
			shown := names
			if len(shown) > 6 {
				shown = append(append([]string{}, names[:6]...), fmt.Sprintf("%d more", len(names)-6))
			}
			detail = fmt.Sprintf("%s each reach the others: none of them can be understood, changed or extracted alone.", joinNames(shown))
			if cutCount > 0 {
				more := ""
				if cutCount > len(cuts) {
					more = fmt.Sprintf(" and %d more", cutCount-len(cuts))
				}
				detail += fmt.Sprintf(" The lightest way out is to cut %s carrying %d references in all: %s%s. These are the arrows drawn against the flow of the map.",
					plural(cutCount, "dependency", "dependencies"), cutWeight, strings.Join(cuts, ", "), more)
			}
		}
		findings = append(findings, CommunityFinding{Kind: "cycle", Title: title, Detail: detail, Communities: cycle, Category: "coupling"})
	}

	// 2. The shared kernel, and what it depends on.
	if cm.Shared != nil {
		hubs := labelsOf(cm.Shared.Hubs, cm.Labels)
		detail := fmt.Sprintf("%s and the others form the de facto shared kernel: %d communities lean on them and %d%% of all dependencies lead there. This is expected of a kernel; what matters is that it stays small, stable and free of any single feature's rules.",
			joinNames(hubs), len(cm.Shared.UsedBy), int(cm.SharedShare*100+0.5))
		if kernelIsCentreOfGravity(cm) {
			detail = fmt.Sprintf("%s and the others form the de facto shared kernel. That is more than a kernel: %d%% of all dependencies lead to these %d %s, which makes them the centre of gravity of the code. Anything changing there reaches %s.",
				joinNames(hubs), int(cm.SharedShare*100+0.5), cm.Shared.Size, unitWord, plural(len(cm.Shared.UsedBy), "community", "communities"))
		}
		findings = append(findings, CommunityFinding{
			Kind:        "shared",
			Title:       fmt.Sprintf("%s shared by the whole codebase", plural(cm.Shared.Size, unitOne(unitWord)+" is", unitWord+" are")),
			Detail:      detail,
			Communities: []string{SharedID},
			Units:       cm.Shared.Hubs,
			Category:    "boundary",
		})
		if len(cm.Shared.Uses) > 0 {
			names := make([]string, 0, len(cm.Shared.Uses))
			total := 0
			for _, l := range cm.Shared.Uses {
				if len(names) < 5 {
					names = append(names, fmt.Sprintf("%s (%d)", l.Name, l.Weight))
				}
				total += l.Weight
			}
			if len(cm.Shared.Uses) > 5 {
				names = append(names, fmt.Sprintf("%d more", len(cm.Shared.Uses)-5))
			}
			ids := make([]string, 0, len(cm.Shared.Uses))
			for _, l := range cm.Shared.Uses {
				ids = append(ids, l.ID)
			}
			findings = append(findings, CommunityFinding{
				Kind:  "shared-leak",
				Title: "The shared kernel depends on the code that uses it",
				Detail: fmt.Sprintf("Its classes reach back into %s, %d references in all. A kernel that knows its users cannot be changed without them: these references should be inverted or moved out of the kernel.",
					joinNames(names), total),
				Communities: append([]string{SharedID}, ids...),
				Category:    "coupling",
			})
		}
	}

	// 3. Communities without a boundary: most of their members are used
	// from outside, so that no few classes carry the entries.
	exposed := []*Community{}
	for _, c := range cm.Communities {
		if !c.Shared && c.Size >= spreadMinUnits && c.ExposedShare >= exposedMinShare {
			exposed = append(exposed, c)
		}
	}
	slices.SortStableFunc(exposed, func(x, y *Community) int { return y.ExposedCount - x.ExposedCount })
	for i, c := range exposed {
		if i >= maxFindingsPerKind {
			break
		}
		top := c.Exposed
		if len(top) > 3 {
			top = top[:3]
		}
		entries := make([]string, 0, len(top))
		for _, unit := range top {
			entries = append(entries, fmt.Sprintf("%s (reached from %s)", cm.Labels[unit], plural(communitiesReaching(unit, cm, g), "community", "communities")))
		}
		findings = append(findings, CommunityFinding{
			Kind:  "exposed",
			Title: fmt.Sprintf("%s has no boundary: %d of its %d %s are used from outside", c.ShortName, c.ExposedCount, c.Size, unitWord),
			Detail: fmt.Sprintf("A module can be extracted, or given an interface, only when a few %s carry its entries; here %d%% of them do, and the rest of the code reaches anywhere into it. The most used are %s. Gathering the entries behind a few of them would give the module an edge.",
				unitWord, int(c.ExposedShare*100+0.5), joinNames(entries)),
			Communities: []string{c.ID},
			Units:       top,
			Category:    "coupling",
		})
	}

	// 4. Split namespaces: a folder whose classes go their separate ways.
	// The shared kernel does not count as a way: a folder holding the kernel
	// and a feature is the ordinary shape of a Shared namespace.
	if cm.CommunitiesCount >= 2 {
		byNamespace := map[string]map[string]int{}
		for _, c := range cm.Communities {
			if c.Shared {
				continue
			}
			for _, s := range c.Namespaces {
				if byNamespace[s.Namespace] == nil {
					byNamespace[s.Namespace] = map[string]int{}
				}
				byNamespace[s.Namespace][c.ID] = s.Count
			}
		}
		type split struct {
			namespace string
			total     int
			parts     []CommunityLink
		}
		splits := []split{}
		for _, namespace := range slices.Sorted(maps.Keys(byNamespace)) {
			if namespace == "" {
				// code outside any namespace was never filed together
				continue
			}
			parts := byNamespace[namespace]
			total := 0
			for _, n := range parts {
				total += n
			}
			if len(parts) < splitMinCommunities || total < splitMinUnits {
				continue
			}
			links := make([]CommunityLink, 0, len(parts))
			for _, id := range slices.SortedFunc(maps.Keys(parts), compareIDs) {
				links = append(links, CommunityLink{ID: id, Name: byID[id].ShortName, Weight: parts[id]})
			}
			slices.SortStableFunc(links, func(x, y CommunityLink) int { return y.Weight - x.Weight })
			if float64(links[0].Weight)/float64(total) >= 0.5 {
				continue
			}
			splits = append(splits, split{namespace: namespace, total: total, parts: links})
		}
		slices.SortStableFunc(splits, func(x, y split) int {
			if len(x.parts) != len(y.parts) {
				return len(y.parts) - len(x.parts)
			}
			return y.total - x.total
		})
		for i, s := range splits {
			if i >= maxFindingsPerKind {
				break
			}
			shown := s.parts
			if len(shown) > 3 {
				shown = shown[:3]
			}
			parts := make([]string, 0, len(shown))
			ids := make([]string, 0, len(s.parts))
			for _, p := range shown {
				parts = append(parts, fmt.Sprintf("%s (%d)", p.Name, p.Weight))
			}
			for _, p := range s.parts {
				ids = append(ids, p.ID)
			}
			more := ""
			if len(s.parts) > len(shown) {
				more = fmt.Sprintf(" and %d more", len(s.parts)-len(shown))
			}
			findings = append(findings, CommunityFinding{
				Kind:  "split",
				Title: fmt.Sprintf("%s is spread across %d communities", stripRoot(namespaceLabel(s.namespace), cm.Root), len(s.parts)),
				Detail: fmt.Sprintf("Its %d %s follow the code that uses them: %s%s. The folder gathers several concerns rather than one module; each part could move next to the code it serves.",
					s.total, unitWord, strings.Join(parts, ", "), more),
				Communities: ids,
				Units:       []string{s.namespace},
				Category:    "purity",
			})
		}
	}

	// 5. Spread communities: one group of code across several folders. A
	// namespace already reported as split is left out of the count: the
	// finding above tells that story, and every community it lends a few
	// classes to would otherwise repeat it.
	//
	// When most communities are spread, the code is laid out in layers
	// (Controller, Entity, Repository...) and the dependencies run through
	// them feature by feature: that is one observation about the whole
	// project, not one per community.
	spread := cm.CommunitiesCount - cm.CohesiveCount
	if cm.Granularity == GranularityClass && cm.CommunitiesCount >= 3 && spread*2 >= cm.CommunitiesCount {
		layers := map[string]int{}
		for _, c := range cm.Communities {
			if c.Shared {
				continue
			}
			for _, s := range c.Namespaces {
				layers[s.Namespace] += s.Count
			}
		}
		layerNames := slices.SortedFunc(maps.Keys(layers), func(x, y string) int {
			if layers[x] != layers[y] {
				return layers[y] - layers[x]
			}
			return strings.Compare(x, y)
		})
		if len(layerNames) > 4 {
			layerNames = layerNames[:4]
		}
		layerLike := 0
		for i := range layerNames {
			if looksLikeLayer(layerNames[i]) {
				layerLike++
			}
			layerNames[i] = stripRoot(namespaceLabel(layerNames[i]), cm.Root)
		}
		features := []string{}
		for _, c := range cm.Communities {
			if !c.Shared && !c.Cohesive && len(features) < 4 {
				features = append(features, c.ShortName)
			}
		}
		if layerLike >= 2 {
			// the namespaces are named after technical layers: the
			// communities are the features running through them
			findings = append(findings, CommunityFinding{
				Kind:  "layered",
				Title: "The namespaces follow layers, the dependencies follow features",
				Detail: fmt.Sprintf("%d of the %d communities draw from several namespaces (%s...). Each of them is a slice of the application (%s) cutting through those layers. This is the usual layout of a framework application; it stays manageable while the layers are thin, and a layout by feature would file each community together.",
					spread, cm.CommunitiesCount, joinNames(layerNames), joinNames(features)),
				Category: "purity",
			})
		} else {
			findings = append(findings, CommunityFinding{
				Kind:  "layered",
				Title: "The dependencies group the code differently than the namespaces do",
				Detail: fmt.Sprintf("%d of the %d communities draw from several namespaces (%s...): %s each gather classes filed in different places that depend on each other more than on their neighbours. The namespaces say where the code was put; the communities say how it works together.",
					spread, cm.CommunitiesCount, joinNames(layerNames), joinNames(features)),
				Category: "purity",
			})
		}
	} else if cm.CommunitiesCount >= 2 && cm.Granularity == GranularityClass {
		// a community of packages spanning several parent folders says
		// little: packages are filed one level up, not by feature
		splitNamespaces := map[string]bool{}
		for _, f := range findings {
			if f.Kind == "split" && len(f.Units) > 0 {
				splitNamespaces[f.Units[0]] = true
			}
		}
		count := 0
		for _, c := range cm.Communities {
			if c.Shared || c.Cohesive || c.Size < spreadMinUnits || count >= maxFindingsPerKind {
				continue
			}
			own := []NamespaceShare{}
			ownCount := 0
			for _, s := range c.Namespaces {
				if !splitNamespaces[s.Namespace] {
					own = append(own, s)
					ownCount += s.Count
				}
			}
			if len(own) < 2 || ownCount < spreadMinUnits || float64(own[0].Count)/float64(ownCount) >= cohesiveShare {
				continue
			}
			shown := c.Namespaces
			if len(shown) > 3 {
				shown = shown[:3]
			}
			parts := make([]string, 0, len(shown))
			for _, s := range shown {
				parts = append(parts, fmt.Sprintf("%s (%d%%)", stripRoot(namespaceLabel(s.Namespace), cm.Root), int(s.Share*100+0.5)))
			}
			more := ""
			if len(c.Namespaces) > len(shown) {
				more = fmt.Sprintf(" and %d more", len(c.Namespaces)-len(shown))
			}
			findings = append(findings, CommunityFinding{
				Kind:  "spread",
				Title: fmt.Sprintf("%s spans %d namespaces", c.ShortName, len(c.Namespaces)),
				Detail: fmt.Sprintf("Its %d %s come from %s%s, and depend on each other more than on anything else. Either they are one module filed in several places, or one of these namespaces leaks into the others.",
					c.Size, unitWord, strings.Join(parts, ", "), more),
				Communities: []string{c.ID},
				Category:    "purity",
			})
			count++
		}
	}

	// 6. Bridges: units carrying the traffic between communities without
	// being shared by all.
	if cm.CommunitiesCount >= 3 {
		type bridge struct {
			unit    string
			weight  int
			reached map[string]int
		}
		bridges := []bridge{}
		for _, unit := range g.Units {
			own, placed := cm.NodeToCommunity[unit]
			if !placed || own == SharedID {
				continue
			}
			reached := map[string]int{}
			weight := 0
			for b, w := range g.Out[unit] {
				if c, ok := cm.NodeToCommunity[b]; ok && c != own && c != SharedID {
					reached[c] += w
					weight += w
				}
			}
			for a, w := range g.In[unit] {
				if c, ok := cm.NodeToCommunity[a]; ok && c != own && c != SharedID {
					reached[c] += w
					weight += w
				}
			}
			if len(reached) >= bridgeMinCommunities {
				bridges = append(bridges, bridge{unit: unit, weight: weight, reached: reached})
			}
		}
		slices.SortStableFunc(bridges, func(x, y bridge) int {
			if len(x.reached) != len(y.reached) {
				return len(y.reached) - len(x.reached)
			}
			return y.weight - x.weight
		})
		for i, b := range bridges {
			if i >= maxFindingsPerKind {
				break
			}
			ids := slices.SortedFunc(maps.Keys(b.reached), func(x, y string) int {
				if b.reached[x] != b.reached[y] {
					return b.reached[y] - b.reached[x]
				}
				return compareIDs(x, y)
			})
			names := make([]string, 0, len(ids))
			for _, id := range ids {
				if len(names) == 5 {
					names = append(names, fmt.Sprintf("%d more", len(ids)-5))
					break
				}
				names = append(names, byID[id].ShortName)
			}
			findings = append(findings, CommunityFinding{
				Kind:  "bridge",
				Title: fmt.Sprintf("%s links %d communities", bridgeName(b.unit, g, cm), len(b.reached)),
				Detail: fmt.Sprintf("It sits in %s and exchanges %d references with %s. It is a de facto contract between them: a change here reaches all of them, and its interface deserves to be explicit.",
					byID[cm.NodeToCommunity[b.unit]].ShortName, b.weight, joinNames(names)),
				Communities: append([]string{cm.NodeToCommunity[b.unit]}, ids...),
				Units:       []string{b.unit},
				Category:    "boundary",
			})
		}
	}
	return findings
}

// bridgeName names a unit in a finding: a class by its qualified name without
// the root, a file or a namespace by its label.
func bridgeName(unit string, g *unitGraph, cm *CommunityMetrics) string {
	if g.IsClass[unit] {
		return stripRoot(unit, cm.Root)
	}
	return cm.Labels[unit]
}

// layerNames lists the words a namespace is named with when it names a
// technical layer rather than a part of the domain.
var layerNames = map[string]bool{
	"controller": true, "controllers": true, "entity": true, "entities": true, "model": true, "models": true,
	"repository": true, "repositories": true, "service": true, "services": true, "form": true, "forms": true,
	"dto": true, "dtos": true, "domain": true, "application": true, "infrastructure": true, "presentation": true,
	"api": true, "web": true, "ui": true, "persistence": true, "command": true, "commands": true, "query": true,
	"queries": true, "handler": true, "handlers": true, "view": true, "views": true, "util": true, "utils": true,
	"helper": true, "helpers": true, "http": true, "console": true, "event": true, "events": true, "listener": true,
	"listeners": true, "subscriber": true, "subscribers": true, "validator": true, "validators": true, "factory": true,
	"factories": true, "mapper": true, "mappers": true, "provider": true, "providers": true, "middleware": true,
	"exception": true, "exceptions": true, "interface": true, "interfaces": true, "contract": true, "contracts": true,
	"data": true, "core": true, "common": true, "shared": true, "support": true, "lib": true, "libs": true,
	"manager": true, "managers": true, "message": true, "messages": true, "request": true, "requests": true,
	"response": true, "responses": true, "trait": true, "traits": true, "test": true, "tests": true,
}

// looksLikeLayer tells whether a namespace, or a word of a class name, names
// a technical layer.
func looksLikeLayer(namespace string) bool {
	return layerNames[strings.ToLower(lastSegment(namespace))]
}

// kernelIsCentreOfGravity tells whether the shared kernel is too heavy to be
// called a kernel: it draws kernelHeavyShare of the dependencies, or holds
// kernelHeavyUnitShare of the placed units.
func kernelIsCentreOfGravity(cm *CommunityMetrics) bool {
	if cm.Shared == nil || cm.UnitCount == 0 {
		return false
	}
	return cm.SharedShare >= kernelHeavyShare || float64(cm.Shared.Size) >= kernelHeavyUnitShare*float64(cm.UnitCount)
}

// verdictOf writes the sentence the page opens on.
func verdictOf(cm *CommunityMetrics) (string, string) {
	unitWord := "classes"
	if cm.Granularity == GranularityNamespace {
		unitWord = "packages"
	}
	switch {
	case cm.CommunitiesCount == 0:
		return "No community stands out.",
			fmt.Sprintf("The project's own %s barely depend on each other: there is nothing to group.", unitWord)
	case cm.CommunitiesCount == 1:
		return "All the code forms a single community.",
			fmt.Sprintf("The %d %s that depend on each other do so without a seam: no group of them stands apart from the rest.", cm.UnitCount, unitWord)
	}
	verdict := fmt.Sprintf("Your code forms %d communities.", cm.CommunitiesCount)
	if cm.Shared != nil {
		verdict = fmt.Sprintf("Your code forms %d communities around a shared kernel of %s.", cm.CommunitiesCount, plural(cm.Shared.Size, unitOne(unitWord), unitWord))
	}
	spread := cm.CommunitiesCount - cm.CohesiveCount
	switch {
	case len(cm.Cycles) > 0:
		// one cycle holding a good part of the communities is a blob, and is
		// said as such rather than counted among the cycles
		largest := 0
		for _, cycle := range cm.Cycles {
			largest = max(largest, len(cycle))
		}
		if largest >= blobMinCommunities && float64(largest) >= blobMinShare*float64(cm.CommunitiesCount) {
			return verdict, fmt.Sprintf("%d of them form one indissociable block: none can change without the others.", largest)
		}
		caught := 0
		for _, cycle := range cm.Cycles {
			caught += len(cycle)
		}
		return verdict, fmt.Sprintf("%d of them depend on each other in %s.", caught, plural(len(cm.Cycles), "cycle", "cycles"))
	case kernelIsCentreOfGravity(cm):
		return verdict, fmt.Sprintf("The kernel is the centre of gravity: %d%% of the dependencies lead to its %s.", int(cm.SharedShare*100+0.5), plural(cm.Shared.Size, unitOne(unitWord), unitWord))
	case spread == 0:
		return verdict, "Each one stays inside a single namespace: the folders match the dependencies."
	case spread >= (cm.CommunitiesCount+1)/2:
		return verdict, fmt.Sprintf("%d of them cut across your namespaces: the dependencies group the code differently than the folders do.", spread)
	default:
		return verdict, fmt.Sprintf("%d stay within one namespace, %d cross namespace boundaries.", cm.CohesiveCount, spread)
	}
}

// unitOne gives the singular of "classes" or "packages".
func unitOne(unitWord string) string {
	if unitWord == "packages" {
		return "package"
	}
	return "class"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// namespaceSeparator returns the separator a namespace is written with.
func namespaceSeparator(namespace string) string {
	for _, sep := range []string{"::", "\\", "/", "."} {
		if strings.Contains(namespace, sep) {
			return sep
		}
	}
	return ""
}

// lastSegment returns the last segment of a namespace or a qualified name.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[:i]
	}
	if sep := namespaceSeparator(name); sep != "" {
		if i := strings.LastIndex(name, sep); i >= 0 {
			return name[i+len(sep):]
		}
	}
	return name
}

// commonPrefixOfNamespaces returns the segments every namespace begins with,
// joined the way the first one is written, and an empty string when they
// share nothing or are written with different separators.
func commonPrefixOfNamespaces(namespaces []string) string {
	if len(namespaces) == 0 {
		return ""
	}
	sep := namespaceSeparator(namespaces[0])
	if sep == "" {
		return ""
	}
	prefix := strings.Split(namespaces[0], sep)
	for _, namespace := range namespaces[1:] {
		if namespaceSeparator(namespace) != sep {
			return ""
		}
		parts := strings.Split(namespace, sep)
		n := 0
		for n < len(prefix) && n < len(parts) && prefix[n] == parts[n] {
			n++
		}
		prefix = prefix[:n]
		if n == 0 {
			return ""
		}
	}
	return strings.Join(prefix, sep)
}

// The root of the project is the deepest namespace prefix most of its units
// share. The first level needs rootFirstShare of the units: a Laravel
// application keeps App beside Database, a fixture namespace or a lone class
// must not hide the root. Going one level deeper needs rootDeeperShare: once
// a prefix covers the project, a deeper one is only taken when it covers
// nearly all of it, so that Symfony\Component is stripped from the components
// while a repository's internal/ is not stripped from its cmd/.
const (
	rootFirstShare  = 0.8
	rootDeeperShare = 0.95
)

// commonRootOfNamespaces returns the root of the project among the units'
// namespaces, or an empty string when there is none. In a view mixing
// languages, the language holding most of the units decides: the others
// keep their full names.
func commonRootOfNamespaces(g *unitGraph) string {
	if len(g.Units) < 2 {
		return ""
	}
	// the separator most units are written with
	bySep := map[string]int{}
	for _, unit := range g.Units {
		bySep[namespaceSeparator(g.Namespace[unit])]++
	}
	sep, most := "", 0
	for _, candidate := range []string{"\\", ".", "/", "::"} {
		if bySep[candidate] > most {
			sep, most = candidate, bySep[candidate]
		}
	}
	if sep == "" {
		return ""
	}
	// count, for every prefix at every depth, the units it covers
	count := map[string]int{}
	depthOf := map[string]int{}
	total := 0
	for _, unit := range g.Units {
		namespace := g.Namespace[unit]
		if namespace == "" {
			continue
		}
		if own := namespaceSeparator(namespace); own != "" && own != sep {
			// another language
			continue
		}
		total++
		// a unit sitting right in the namespace named by a prefix is under
		// that prefix as much as a deeper one
		parts := strings.Split(namespace, sep)
		for depth := 1; depth <= len(parts); depth++ {
			prefix := strings.Join(parts[:depth], sep)
			count[prefix]++
			depthOf[prefix] = depth
		}
	}
	if total == 0 {
		return ""
	}
	// the best prefix depth by depth
	best := ""
	for depth := 1; ; depth++ {
		candidate, covered := "", 0
		for _, prefix := range slices.Sorted(maps.Keys(count)) {
			if depthOf[prefix] == depth && count[prefix] > covered {
				candidate, covered = prefix, count[prefix]
			}
		}
		if candidate == "" {
			break
		}
		if best != "" && !strings.HasPrefix(candidate, best+sep) {
			break
		}
		share := float64(covered) / float64(total)
		if (depth == 1 && share < rootFirstShare) || (depth > 1 && share < rootDeeperShare) {
			break
		}
		best = candidate
	}
	return best
}

// stripRoot removes the root of the project from a name, wherever it appears:
// a name may join two namespaces.
func stripRoot(name, root string) string {
	if root == "" {
		return name
	}
	// the root alone may carry no separator (a one-level root): the name does
	sep := namespaceSeparator(name)
	if sep == "" {
		return name
	}
	return strings.ReplaceAll(name, root+sep, "")
}
