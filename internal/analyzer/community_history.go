package analyzer

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

// The communities are read off the dependencies; the git history says how the
// code is actually worked on. Files changed by the same commits are coupled
// whether or not a reference joins them (Tornhill's temporal coupling), and
// confronting that coupling with the boundaries the structure draws tells
// where the two disagree: two communities that keep changing together are one
// module cut in two, and a community whose members never change together is
// a bag of classes the dependencies happen to link.
//
// Only the last year of history is known (see the git analyzer), and only the
// files behind placed units count.

// CommunityCoChange is another community touched by the same commits as this
// one.
type CommunityCoChange struct {
	ID   string
	Name string
	// Commits is the number of commits touching both communities; Share is
	// that number over the commits touching this community.
	Commits int
	Share   float64
	// Pair is the pair of files most often changed together across the two
	// communities, by their labels, with the number of commits changing both.
	// It is only worked out for the co-changes strong enough to be reported.
	Pair UnitLink
}

// Thresholds of the history analysis.
const (
	// A commit touching more analysed files than this is a bulk change (a
	// reformat, a rename, a mass import) and says nothing about coupling.
	historyMaxFilesPerCommit = 30
	// The cohesion of a community means nothing below this many commits
	// touching several files.
	historyMinCommits = 5
	// Two communities are reported as changing together from this many shared
	// commits, when they make at least historyMinCoChangeShare of the commits
	// of the less active one.
	historyMinCoChange      = 5
	historyMinCoChangeShare = 0.3
	// A community is reported as never changing as a whole from this many
	// commits, when fewer than historyLooseMaxCohesion of its multi-file
	// commits stay inside it.
	historyLooseMinCommits  = 20
	historyLooseMaxCohesion = 0.1
	// At most this many co-changing communities are kept per community.
	maxCoChanges = 5
	// At most this many findings of each history kind: they read alike, and
	// the table lists the rest.
	maxHistoryFindingsPerKind = 3
)

// historyCommit is one commit as the analysis sees it: the files it touched
// that stand behind a placed unit, each with its community.
type historyCommit struct {
	files map[string]string // path -> community id
}

// historyOf crosses the communities with the commits touching their files:
// how many commits touch each community, how many stay inside it, and which
// other communities the same commits reach.
func historyOf(aggregate *Aggregated, cm *CommunityMetrics, g *unitGraph) {
	if aggregate == nil || cm == nil || len(cm.Communities) == 0 {
		return
	}
	byID := map[string]*Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}

	// One pass over the files: which commits touched which files, and the
	// community each file belongs to.
	commits := map[string]*historyCommit{}
	for _, file := range aggregate.ConcernedFiles {
		if file == nil || file.Stmts == nil || file.GetIsTest() || file.Commits == nil || isTestSupportPath(file.Path) {
			continue
		}
		hasCommit := false
		for _, commit := range file.Commits.Commits {
			if commit != nil && commit.Hash != "" {
				hasCommit = true
				break
			}
		}
		if !hasCommit {
			continue
		}
		cm.HistoryAvailable = true
		id := communityOfFile(file, cm, g, aggregate)
		if id == "" {
			continue
		}
		for _, commit := range file.Commits.Commits {
			if commit == nil || commit.Hash == "" {
				continue
			}
			c := commits[commit.Hash]
			if c == nil {
				c = &historyCommit{files: map[string]string{}}
				commits[commit.Hash] = c
			}
			c.files[file.Path] = id
		}
	}
	for hash, c := range commits {
		if len(c.files) > historyMaxFilesPerCommit {
			delete(commits, hash)
		}
	}
	cm.HistoryCommits = len(commits)
	if len(commits) == 0 {
		return
	}

	// Count, per community, its commits, the ones touching several files and
	// the ones among those that stayed inside it; per pair, the shared
	// commits. A commit counts once per community however many of its files
	// it touched. The shared kernel takes no part in the pairs, and a commit
	// touching a community and the kernel still stays "inside".
	perCommunity := map[string]int{}
	multiPerCommunity := map[string]int{}
	insidePerCommunity := map[string]int{}
	pairs := map[string]map[string]int{}
	multi, agreeing := 0, 0
	for _, c := range commits {
		touched := map[string]bool{}
		for _, id := range c.files {
			touched[id] = true
		}
		others := 0
		for id := range touched {
			if id != SharedID {
				others++
			}
		}
		isMulti := len(c.files) >= 2
		if isMulti {
			multi++
			if others <= 1 {
				agreeing++
			}
		}
		for id := range touched {
			perCommunity[id]++
			if !isMulti {
				continue
			}
			multiPerCommunity[id]++
			if others <= 1 {
				insidePerCommunity[id]++
			}
			if id == SharedID {
				continue
			}
			for other := range touched {
				if other == id || other == SharedID {
					continue
				}
				if pairs[id] == nil {
					pairs[id] = map[string]int{}
				}
				pairs[id][other]++
			}
		}
	}
	if multi > 0 {
		cm.HistoryAgreement = float64(agreeing) / float64(multi)
	}
	for id, c := range byID {
		c.HistoryCommits = perCommunity[id]
		c.HistoryMultiFileCommits = multiPerCommunity[id]
		if c.HistoryMultiFileCommits >= historyMinCommits {
			c.HistoryCohesion = float64(insidePerCommunity[id]) / float64(c.HistoryMultiFileCommits)
		}
		if c.Shared || c.HistoryCommits == 0 {
			continue
		}
		coChanges := make([]CommunityCoChange, 0, len(pairs[id]))
		for _, other := range slices.SortedFunc(maps.Keys(pairs[id]), compareIDs) {
			n := pairs[id][other]
			coChanges = append(coChanges, CommunityCoChange{
				ID: other, Name: byID[other].ShortName, Commits: n, Share: float64(n) / float64(c.HistoryCommits),
			})
		}
		slices.SortStableFunc(coChanges, func(x, y CommunityCoChange) int { return y.Commits - x.Commits })
		if len(coChanges) > maxCoChanges {
			coChanges = coChanges[:maxCoChanges]
		}
		c.ChangesWith = coChanges
	}

	// The strongest file pair behind each co-change worth reporting: the
	// finding names it, so that the reader knows where to look first.
	labels := fileLabelsOf(cm, g)
	for _, c := range cm.Communities {
		for i := range c.ChangesWith {
			cc := &c.ChangesWith[i]
			if cc.Commits < historyMinCoChange {
				continue
			}
			cc.Pair = strongestFilePair(commits, c.ID, cc.ID, labels)
		}
	}
}

// strongestFilePair returns the pair of files, one in each community, that
// the most commits changed together.
func strongestFilePair(commits map[string]*historyCommit, a, b string, labels map[string]string) UnitLink {
	counts := map[[2]string]int{}
	for _, c := range commits {
		for pathA, idA := range c.files {
			if idA != a {
				continue
			}
			for pathB, idB := range c.files {
				if idB == b {
					counts[[2]string{pathA, pathB}]++
				}
			}
		}
	}
	best, bestCount := [2]string{}, 0
	for _, pair := range slices.SortedFunc(maps.Keys(counts), func(x, y [2]string) int {
		if x[0] != y[0] {
			return compareIDs(x[0], y[0])
		}
		return compareIDs(x[1], y[1])
	}) {
		if counts[pair] > bestCount {
			best, bestCount = pair, counts[pair]
		}
	}
	if bestCount == 0 {
		return UnitLink{}
	}
	label := func(path string) string {
		if l, ok := labels[path]; ok {
			return l
		}
		return filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)))
	}
	return UnitLink{From: label(best[0]), To: label(best[1]), Weight: bestCount}
}

// fileLabelsOf names each file after the unit it declares: the class name
// when a class is what the file holds, since the reader knows the classes
// better than the paths. A file declaring several units takes the first one
// in order.
func fileLabelsOf(cm *CommunityMetrics, g *unitGraph) map[string]string {
	labels := map[string]string{}
	for _, unit := range g.Units {
		file := g.FileOf[unit]
		if file == nil {
			continue
		}
		if _, seen := labels[file.Path]; seen {
			continue
		}
		if l, ok := cm.Labels[unit]; ok && l != "" {
			labels[file.Path] = l
		}
	}
	return labels
}

// historyFindingsOf writes down where the history disagrees with the
// boundaries the dependencies draw.
func historyFindingsOf(cm *CommunityMetrics) []CommunityFinding {
	findings := []CommunityFinding{}
	if cm == nil || !cm.HistoryAvailable || cm.HistoryCommits == 0 {
		return findings
	}
	byID := map[string]*Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}
	unitWord := "classes"
	if cm.Granularity == GranularityNamespace {
		unitWord = "packages"
	}

	// 1. Communities changing together. The share is judged on the less
	// active of the two: it is the one the commits of the other swallow.
	type crossed struct {
		a, b    *Community // a is the less active one
		commits int
		share   float64
		pair    UnitLink
	}
	crossings := []crossed{}
	seen := map[[2]string]bool{}
	for _, c := range cm.Communities {
		if c.Shared {
			continue
		}
		for _, cc := range c.ChangesWith {
			other := byID[cc.ID]
			if other == nil || other.Shared || cc.Commits < historyMinCoChange {
				continue
			}
			key := [2]string{c.ID, cc.ID}
			if compareIDs(key[0], key[1]) > 0 {
				key[0], key[1] = key[1], key[0]
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			a, b := c, other
			if b.HistoryCommits < a.HistoryCommits || (b.HistoryCommits == a.HistoryCommits && compareIDs(b.ID, a.ID) < 0) {
				a, b = b, a
			}
			share := float64(cc.Commits) / float64(a.HistoryCommits)
			if share < historyMinCoChangeShare {
				continue
			}
			pair := cc.Pair
			if a != c {
				// The pair was read from c's side; name it from a's side.
				pair = UnitLink{From: pair.To, To: pair.From, Weight: pair.Weight}
			}
			crossings = append(crossings, crossed{a: a, b: b, commits: cc.Commits, share: share, pair: pair})
		}
	}
	slices.SortStableFunc(crossings, func(x, y crossed) int {
		if x.commits != y.commits {
			return y.commits - x.commits
		}
		if x.share != y.share {
			if y.share > x.share {
				return 1
			}
			return -1
		}
		if c := compareIDs(x.a.ID, y.a.ID); c != 0 {
			return c
		}
		return compareIDs(x.b.ID, y.b.ID)
	})
	for i, x := range crossings {
		if i >= maxHistoryFindingsPerKind {
			break
		}
		where := ""
		if x.pair.Weight > 0 {
			where = fmt.Sprintf(", most often %s with %s (%d times)", x.pair.From, x.pair.To, x.pair.Weight)
		}
		// the lesson is the same for every pair: it is spelled out once
		lesson := ""
		if i == 0 {
			lesson = " Either the boundary sits in the wrong place, or an abstraction is missing between them: what changes together belongs together."
		}
		findings = append(findings, CommunityFinding{
			Kind:  "history-crossed",
			Title: fmt.Sprintf("%s and %s change together", x.a.ShortName, x.b.ShortName),
			Detail: fmt.Sprintf("%d of the %d commits touching %s also touch %s%s.%s",
				x.commits, x.a.HistoryCommits, x.a.ShortName, x.b.ShortName, where, lesson),
			Communities: []string{x.a.ID, x.b.ID},
			Category:    "cohesion",
		})
	}

	// 2. Communities that never change as a whole.
	loose := []*Community{}
	for _, c := range cm.Communities {
		if c.Shared || c.Size < spreadMinUnits || c.HistoryCommits < historyLooseMinCommits {
			continue
		}
		if c.HistoryMultiFileCommits < historyMinCommits || c.HistoryCohesion >= historyLooseMaxCohesion {
			continue
		}
		loose = append(loose, c)
	}
	slices.SortStableFunc(loose, func(x, y *Community) int {
		if x.HistoryCommits != y.HistoryCommits {
			return y.HistoryCommits - x.HistoryCommits
		}
		return compareIDs(x.ID, y.ID)
	})
	for i, c := range loose {
		if i >= maxHistoryFindingsPerKind {
			break
		}
		findings = append(findings, CommunityFinding{
			Kind:  "history-loose",
			Title: fmt.Sprintf("%s never changes as a whole", c.ShortName),
			Detail: fmt.Sprintf("Its %d %s were touched by %d commits this year, but only %d%% of the commits touching several files stayed inside it: the dependencies group them, the work does not. It is probably a utility bag rather than a module.",
				c.Size, unitWord, c.HistoryCommits, int(c.HistoryCohesion*100+0.5)),
			Communities: []string{c.ID},
			Category:    "cohesion",
		})
	}
	return findings
}

// withHistoryFindings inserts the history findings right after the cycles,
// so that what the history says sits near the top of the page, before the
// observations drawn from the structure alone.
func withHistoryFindings(findings, history []CommunityFinding) []CommunityFinding {
	if len(history) == 0 {
		return findings
	}
	at := 0
	for i, f := range findings {
		if f.Kind == "cycle" {
			at = i + 1
		}
	}
	return slices.Insert(findings, at, history...)
}
