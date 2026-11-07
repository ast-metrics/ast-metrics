package cribelo

import (
	"os"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/ast-metrics/ast-metrics/internal/review"
)

// The reading list is not a ranking but a route. Files that depend on each
// other are read together, the one the others use first: a reader who knows
// what a function does before seeing its callers finds more (Baum et al.,
// 2017, 85% of reviewers prefer callee first). A test is read right after
// the file it exercises. The group that carries structural facts comes
// first; within a group, when the dependencies leave a choice, the file
// with the highest score. Mechanical files are not in the list at all:
// they are listed apart with their reason, to check in a glance.

// ReadingGroup is a set of files of the reading list read together.
type ReadingGroup struct {
	// Files are the paths in reading order, tests right after their file.
	Files []string `json:"files"`
	// Facts counts the structural facts about the files of the group:
	// cycles, signatures, dependencies and clones.
	Facts int `json:"facts,omitempty"`
	// Linked tells whether the files depend on each other; a group of
	// one file and its tests is not linked.
	Linked bool `json:"linked,omitempty"`
}

// orderReads arranges the files marked Read into groups, pairs the tests of
// the change with them, and rewrites plan.Files in reading order: the
// groups one after the other, then the files left out in rank order.
// efferent is the dependency graph of the project, by repository path;
// head gives the sources, for the dependencies that imports do not show.
func orderReads(plan *Plan, efferent map[string][]string, head *Parsed, tests []FileReport) {
	read := map[string]*FileReport{}
	var readPaths, allPaths []string
	for i := range plan.Files {
		allPaths = append(allPaths, plan.Files[i].Path)
		if plan.Files[i].Read {
			read[plan.Files[i].Path] = &plan.Files[i]
			readPaths = append(readPaths, plan.Files[i].Path)
		}
	}
	if len(read) == 0 {
		plan.Groups = nil
		return
	}

	uses, usedBy := map[string][]string{}, map[string][]string{}
	link := func(u, v string) {
		for _, known := range uses[u] {
			if known == v {
				return
			}
		}
		uses[u] = append(uses[u], v)
		usedBy[v] = append(usedBy[v], u)
	}
	for _, u := range readPaths {
		for _, v := range efferent[u] {
			if v != u && read[v] != nil {
				link(u, v)
			}
		}
	}
	for u, targets := range siblingUses(head, readPaths) {
		for _, v := range targets {
			link(u, v)
		}
	}

	// Connected components of the read files, in the undirected graph.
	component := map[string]int{}
	var components [][]string
	for _, p := range readPaths {
		if _, seen := component[p]; seen {
			continue
		}
		id := len(components)
		component[p] = id
		stack, members := []string{p}, []string{}
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			members = append(members, x)
			for _, y := range append(append([]string{}, uses[x]...), usedBy[x]...) {
				if _, seen := component[y]; !seen {
					component[y] = id
					stack = append(stack, y)
				}
			}
		}
		components = append(components, members)
	}

	better := func(a, b string) bool {
		fa, fb := read[a], read[b]
		if fa.Score != fb.Score {
			return fa.Score > fb.Score
		}
		if fa.ToRead != fb.ToRead {
			return fa.ToRead > fb.ToRead
		}
		return a < b
	}
	// Callee first: a file is ready once every read file it uses is placed.
	// A cycle is broken on the best file left.
	orderComponent := func(members []string) []string {
		remaining := map[string]bool{}
		pending := map[string]int{}
		for _, m := range members {
			remaining[m] = true
			pending[m] = len(uses[m])
		}
		out := make([]string, 0, len(members))
		for len(remaining) > 0 {
			best := ""
			for _, m := range members {
				if remaining[m] && pending[m] == 0 && (best == "" || better(m, best)) {
					best = m
				}
			}
			if best == "" {
				for _, m := range members {
					if remaining[m] && (best == "" || better(m, best)) {
						best = m
					}
				}
			}
			out = append(out, best)
			delete(remaining, best)
			for _, d := range usedBy[best] {
				pending[d]--
			}
		}
		return out
	}

	// Only the facts that bear on the structure pull a group forward; a
	// new library or an export nobody calls yet does not.
	factsOn := map[string]int{}
	for _, f := range plan.Facts {
		switch f.Kind {
		case FactCycle, FactSignature, FactDependency, FactClone:
			factsOn[f.Path]++
		}
	}
	testsOf := pairTests(readPaths, allPaths, tests, better)
	groups := make([]ReadingGroup, 0, len(components))
	maxScore := make([]float64, 0, len(components))
	for _, members := range components {
		g := ReadingGroup{Linked: len(members) > 1}
		top := 0.0
		for _, p := range orderComponent(members) {
			g.Files = append(g.Files, p)
			g.Facts += factsOn[p]
			top = max(top, read[p].Score)
			for _, t := range testsOf[p] {
				g.Files = append(g.Files, t.Path)
			}
		}
		groups = append(groups, g)
		maxScore = append(maxScore, top)
	}
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if a.Facts != b.Facts {
			return a.Facts > b.Facts
		}
		if maxScore[order[i]] != maxScore[order[j]] {
			return maxScore[order[i]] > maxScore[order[j]]
		}
		if len(a.Files) != len(b.Files) {
			return len(a.Files) > len(b.Files)
		}
		return a.Files[0] < b.Files[0]
	})

	var files []FileReport
	plan.Groups = make([]ReadingGroup, 0, len(groups))
	for _, i := range order {
		plan.Groups = append(plan.Groups, groups[i])
		for _, p := range groups[i].Files {
			if f := read[p]; f != nil {
				files = append(files, *f)
				continue
			}
			for _, t := range tests {
				if t.Path == p {
					t.Read, t.Test = true, true
					files = append(files, t)
					plan.ReadCount++
					plan.ReadMinutes += t.Minutes
				}
			}
		}
	}
	for _, f := range plan.Files {
		if !f.Read {
			files = append(files, f)
		}
	}
	plan.Files = files
}

// pairTests gives every test of the change to the read file it exercises
// best: the file whose name the test is built on, or failing that a file of
// its directory, unless the test is named after a changed file that is not
// read. A test that matches no read file stays out of the list.
func pairTests(readPaths, allPaths []string, tests []FileReport, better func(a, b string) bool) map[string][]FileReport {
	paired := map[string][]FileReport{}
	for _, t := range tests {
		best, strength := "", 0
		for _, p := range readPaths {
			s := testStrength(p, t.Path)
			if s > strength || (s == strength && s > 0 && better(p, best)) {
				best, strength = p, s
			}
		}
		if strength == 1 {
			for _, p := range allPaths {
				if testStrength(p, t.Path) > 1 {
					strength = 0
					break
				}
			}
		}
		if strength > 0 {
			paired[best] = append(paired[best], t)
		}
	}
	for p := range paired {
		sort.Slice(paired[p], func(i, j int) bool { return paired[p][i].Path < paired[p][j].Path })
	}
	return paired
}

// testStrength says how surely a test file exercises a code file: 3 when
// its name is the file's name with a test marker (invoice_test.go,
// test_token.py, SessionTest.php, button.test.tsx), 2 when the file's name
// is part of it, 1 when they share a directory, 0 otherwise.
func testStrength(file, test string) int {
	stem := strings.ToLower(stemOf(file))
	testStem := strings.ToLower(stemOf(test))
	for _, marker := range []string{"_test", "test", "_tests", "tests", "_spec", "spec"} {
		if testStem == stem+marker || testStem == marker+"_"+stem || testStem == marker+stem {
			return 3
		}
	}
	if testStem == stem {
		return 3
	}
	if len(stem) >= 3 && strings.Contains(testStem, stem) {
		return 2
	}
	if path.Dir(file) == path.Dir(test) {
		return 1
	}
	return 0
}

// siblingUses finds the dependencies between read files of one directory
// that imports do not show: files of a Go package, or of a folder in any
// language, use each other by calling the functions they declare. A file
// uses another when it calls, by name, a function the other declares and
// it does not. Names of three letters or less are too common to count.
func siblingUses(head *Parsed, readPaths []string) map[string][]string {
	uses := map[string][]string{}
	if head == nil {
		return uses
	}
	byDir := map[string][]string{}
	for _, p := range readPaths {
		byDir[path.Dir(p)] = append(byDir[path.Dir(p)], p)
	}
	for _, siblings := range byDir {
		if len(siblings) < 2 {
			continue
		}
		declared, called := map[string]map[string]bool{}, map[string]map[string]bool{}
		for _, p := range siblings {
			file := head.Files[p]
			if file == nil {
				continue
			}
			declared[p] = map[string]bool{}
			for name := range review.CollectFunctions(file) {
				if short := shortName(name); len(short) > 3 {
					declared[p][short] = true
				}
			}
			called[p] = calledNames(file.Path)
		}
		for _, u := range siblings {
			for _, v := range siblings {
				if u == v {
					continue
				}
				for name := range declared[v] {
					if called[u][name] && !declared[u][name] {
						uses[u] = append(uses[u], v)
						break
					}
				}
			}
		}
	}
	return uses
}

// calledNames lists the identifiers a source calls: every name followed by
// an opening parenthesis.
func calledNames(sourcePath string) map[string]bool {
	names := map[string]bool{}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return names
	}
	tokens := rawTokens(string(data))
	for i := 0; i+1 < len(tokens); i++ {
		t := tokens[i]
		if tokens[i+1] == "(" && t != "" && (unicode.IsLetter(rune(t[0])) || t[0] == '_') {
			names[t] = true
		}
	}
	return names
}

// groupTitle words why a group's files are read together; empty for a
// single file.
func groupTitle(g ReadingGroup) string {
	if len(g.Files) < 2 {
		return ""
	}
	lead := path.Base(g.Files[0])
	if g.Linked {
		return lead + ", then what uses it"
	}
	return lead + " with its tests"
}
