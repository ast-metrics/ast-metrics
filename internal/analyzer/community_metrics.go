package analyzer

import (
	"github.com/ast-metrics/ast-metrics/internal/engine"
)

type CommunitySubMetricsCalculator struct {
}

func NewCommunitySubMetricsCalculator() *CommunitySubMetricsCalculator {
	return &CommunitySubMetricsCalculator{}
}

func (c *CommunitySubMetricsCalculator) Calculate(aggregate *Aggregated) {

	files := aggregate.ConcernedFiles

	aggregate.Community.TopCommittersPerCommunity = make(map[string]map[string]int)
	aggregate.Community.BusFactorPerCommunity = make(map[string]int)

	for _, file := range files {

		// Find the graph node this file belongs to, by reducing its namespace the
		// way the graph reduced the namespaces it was built from.
		var namespace string
		if file.Stmts != nil && len(file.Stmts.StmtNamespace) > 0 && file.Stmts.StmtNamespace[0].Name != nil {
			namespace = file.Stmts.StmtNamespace[0].Name.Qualified
			if namespace == "" {
				namespace = file.Stmts.StmtNamespace[0].Name.Short
			}
			namespace = aggregate.NamespaceReducer.Reduce(namespace)
		}

		// If no namespace found, try using the file path
		if namespace == "" {
			namespace = engine.ReduceDepthOfNamespace(file.Path, 2)
		}

		// Find the community for this file
		communityID, exists := aggregate.Community.NodeToCommunity[namespace]
		if !exists {
			continue
		}

		if _, ok := aggregate.Community.TopCommittersPerCommunity[communityID]; !ok {
			aggregate.Community.TopCommittersPerCommunity[communityID] = make(map[string]int)
		}

		if file.Commits == nil {
			continue
		}

		for _, commit := range file.Commits.Commits {

			// Exclude commits with no author or from
			if commit.Author == "" {
				continue
			}

			if _, ok := aggregate.Community.TopCommittersPerCommunity[communityID][commit.Author]; !ok {
				aggregate.Community.TopCommittersPerCommunity[communityID][commit.Author] = 0
			}

			aggregate.Community.TopCommittersPerCommunity[communityID][commit.Author]++
		}
	}

	// Calculate bus factor per community
	for communityID, committers := range aggregate.Community.TopCommittersPerCommunity {
		totalCommits := 0
		for _, count := range committers {
			totalCommits += count
		}

		if totalCommits == 0 {
			aggregate.Community.BusFactorPerCommunity[communityID] = 0
			continue
		}

		// Sort committers by count desc
		type committer struct {
			Name  string
			Count int
		}
		sortedCommitters := make([]committer, 0, len(committers))
		for name, count := range committers {
			sortedCommitters = append(sortedCommitters, committer{Name: name, Count: count})
		}
		// Sort manually to avoid importing sort if not needed, but sort package is standard
		// Let's use a simple bubble sort or similar since lists are likely small, or just import sort.
		// Actually, I should check if sort is imported. It is not in the original file.
		// I'll implement a simple sort to avoid adding imports if possible, or just add the import.
		// Adding import is better. I'll check imports first.
		// Wait, I can't check imports in the middle of a replace.
		// I'll implement a simple selection sort here.
		for i := 0; i < len(sortedCommitters); i++ {
			for j := i + 1; j < len(sortedCommitters); j++ {
				if sortedCommitters[j].Count > sortedCommitters[i].Count {
					sortedCommitters[i], sortedCommitters[j] = sortedCommitters[j], sortedCommitters[i]
				}
			}
		}

		busFactor := 0
		currentSum := 0
		threshold := int(float64(totalCommits) * 0.5)

		for _, c := range sortedCommitters {
			currentSum += c.Count
			busFactor++
			if currentSum >= threshold {
				break
			}
		}
		aggregate.Community.BusFactorPerCommunity[communityID] = busFactor
	}
}
