package analyzer

import (
	"maps"
	"slices"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// ownersOfCommunities reads the git history of the files of each community
// into its top committers and bus factor: the number of people who, together,
// made half of the commits touching the community.
//
// A file belongs to the community holding most of its units: its classes when
// the units are classes, its namespace otherwise.
func ownersOfCommunities(aggregate *Aggregated, cm *CommunityMetrics, g *unitGraph) {
	if aggregate == nil || cm == nil || len(cm.Communities) == 0 {
		return
	}
	byID := map[string]*Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}
	commits := map[string]map[string]int{} // community -> committer -> commits
	for _, file := range aggregate.ConcernedFiles {
		if file == nil || file.Stmts == nil || file.GetIsTest() || file.Commits == nil {
			continue
		}
		id := communityOfFile(file, cm, g, aggregate)
		if id == "" {
			continue
		}
		if commits[id] == nil {
			commits[id] = map[string]int{}
		}
		for _, commit := range file.Commits.Commits {
			if commit == nil || commit.Author == "" {
				continue
			}
			commits[id][commit.Author]++
		}
	}
	for id, committers := range commits {
		c := byID[id]
		total := 0
		for _, n := range committers {
			total += n
		}
		c.CommitCount = total
		if total == 0 {
			continue
		}
		sorted := make([]CommitterShare, 0, len(committers))
		for _, name := range slices.Sorted(maps.Keys(committers)) {
			sorted = append(sorted, CommitterShare{Name: name, Commits: committers[name]})
		}
		slices.SortStableFunc(sorted, func(x, y CommitterShare) int { return y.Commits - x.Commits })
		sum := 0
		for _, share := range sorted {
			sum += share.Commits
			c.BusFactor++
			if 2*sum >= total {
				break
			}
		}
		if len(sorted) > 3 {
			sorted = sorted[:3]
		}
		c.TopCommitters = sorted
	}
}

// communityOfFile returns the id of the community a file belongs to, and an
// empty string when none of its units was placed.
func communityOfFile(file *pb.File, cm *CommunityMetrics, g *unitGraph, aggregate *Aggregated) string {
	votes := map[string]int{}
	if g.Language[file.GetProgrammingLanguage()] == GranularityClass {
		for _, class := range engine.GetClassesInFile(file) {
			if class == nil || class.Name == nil {
				continue
			}
			if id, ok := cm.NodeToCommunity[class.Name.Qualified]; ok {
				votes[id]++
			}
		}
	}
	if len(votes) == 0 {
		namespace := namespaceOfFile(file)
		if namespace == "" {
			namespace = engine.ReduceDepthOfNamespace(file.Path, 2)
		}
		node := aggregate.NamespaceReducers.Reduce(file.GetProgrammingLanguage(), namespace)
		if id, ok := cm.NodeToCommunity[node]; ok {
			return id
		}
		return ""
	}
	best, bestVotes := "", 0
	for _, id := range slices.SortedFunc(maps.Keys(votes), compareIDs) {
		if votes[id] > bestVotes {
			best, bestVotes = id, votes[id]
		}
	}
	return best
}

// unitLabel is the short, human name of a unit: the class name, or the last
// segment of a namespace.
func unitLabel(unit string) string {
	if unit == "" {
		return ""
	}
	if i := strings.LastIndex(unit, "::"); i >= 0 {
		unit = unit[:i]
	}
	return lastSegment(unit)
}
