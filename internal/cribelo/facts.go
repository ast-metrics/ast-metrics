package cribelo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/review"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"google.golang.org/protobuf/proto"
)

// A Fact is something about the change that a reader cannot see in the diff
// and can check in a minute: a dependency that did not exist, a cycle, a
// clone left behind, a signature that callers rely on. Facts are not
// predictions; each one is true by construction of the graphs it comes from,
// and says so plainly.
type Fact struct {
	// Kind is one of cycle, signature, history, dependency, library,
	// clone, api.
	Kind string `json:"kind"`
	// Path is the changed file the fact is about.
	Path string `json:"path"`
	// Text is the fact in one sentence.
	Text string `json:"text"`
	// Files are the other files involved, when any.
	Files []string `json:"files,omitempty"`
}

const (
	FactCycle      = "cycle"
	FactDependency = "dependency"
	FactLibrary    = "library"
	FactClone      = "clone"
	FactSignature  = "signature"
	FactAPI        = "api"
	FactHistory    = "history"
)

// factOrder is the order facts are shown in: what breaks the structure
// first, what callers feel next, what was left behind last.
var factOrder = map[string]int{FactCycle: 0, FactSignature: 1, FactHistory: 2, FactDependency: 3, FactLibrary: 4, FactClone: 5, FactAPI: 6}

// graphs holds the dependency graph of the project after the change and
// the graph as it was before it, both keyed by repository-relative path.
type graphs struct {
	root       string
	head, base graph
}

// graph is a dependency graph of the production code, test files left out:
// a cycle through a test, or a test's first import, says nothing about the
// structure.
type graph struct {
	efferent  map[string][]string
	afferent  map[string][]string
	libraries map[string][]string
	// libraryUsers counts the files importing each module.
	libraryUsers map[string]int
	standard     map[string]struct{}
	// modules are the directories that hold a file.
	modules map[string]bool
}

func toGraph(root string, g analyzer.FileDependencyGraph, files []*pb.File) graph {
	out := graph{efferent: map[string][]string{}, afferent: map[string][]string{}, libraries: map[string][]string{}, libraryUsers: map[string]int{}, standard: g.StandardLibraries, modules: map[string]bool{}}
	for _, f := range files {
		if f != nil {
			out.modules[module(relPath(root, f.Path))] = true
		}
	}
	for from, tos := range g.Efferent {
		rel := relPath(root, from)
		if IsTestPath(rel) {
			continue
		}
		for _, to := range tos {
			toRel := relPath(root, to)
			if IsTestPath(toRel) {
				continue
			}
			out.efferent[rel] = append(out.efferent[rel], toRel)
			out.afferent[toRel] = append(out.afferent[toRel], rel)
		}
		sort.Strings(out.efferent[rel])
	}
	for from, libs := range g.Libraries {
		if rel := relPath(root, from); !IsTestPath(rel) {
			out.libraries[rel] = libs
		}
	}
	for lib, users := range g.LibraryUsers {
		out.libraryUsers[lib] = len(users)
	}
	return out
}

// buildGraphs takes the dependencies of the project after the change and
// resolves them as they were before it: the base version of every changed
// file stands in for its head version, under its head path so that imports
// resolve the same way, the deleted files come back and the new ones go.
func buildGraphs(root string, headGraph analyzer.FileDependencyGraph, head, base *Parsed, codeChanges, deletedCode []Change) graphs {
	changed := map[string]bool{}
	for _, c := range codeChanges {
		changed[c.Path] = true
	}
	var baseFiles []*pb.File
	for _, f := range head.All {
		if f == nil || changed[relPath(root, f.Path)] {
			continue
		}
		baseFiles = append(baseFiles, f)
	}
	stand := func(c Change) {
		if base == nil {
			return
		}
		if f := base.Files[c.Path]; f != nil {
			clone := proto.Clone(f).(*pb.File)
			clone.Path = filepath.Join(root, filepath.FromSlash(c.Path))
			baseFiles = append(baseFiles, clone)
		}
	}
	for _, c := range codeChanges {
		if c.Status != 'A' {
			stand(c)
		}
	}
	for _, c := range deletedCode {
		stand(c)
	}
	return graphs{root: root, head: toGraph(root, headGraph, head.All), base: toGraph(root, analyzer.FileDependencies(baseFiles), baseFiles)}
}

// module is the unit dependencies are judged between: the directory of a
// file. A package in Go, a namespace folder elsewhere.
func module(rel string) string { return path.Dir(rel) }

// pathBetween finds a dependency path from one file to another in the
// graph, breadth first, and returns it with both ends; nil when there is
// none. Steps are bounded so that a huge graph does not stall the plan.
func (g graph) pathBetween(from, to string, maxSteps int) []string {
	if from == to {
		return []string{from}
	}
	parent := map[string]string{from: ""}
	queue := []string{from}
	for steps := 0; len(queue) > 0 && steps < maxSteps; steps++ {
		var next []string
		for _, u := range queue {
			for _, v := range g.efferent[u] {
				if _, seen := parent[v]; seen {
					continue
				}
				parent[v] = u
				if v == to {
					var chain []string
					for n := to; n != ""; n = parent[n] {
						chain = append([]string{n}, chain...)
					}
					return chain
				}
				next = append(next, v)
			}
		}
		queue = next
	}
	return nil
}

// moduleEdges lists the dependencies between modules, each once.
func (g graph) moduleEdges() map[[2]string]bool {
	edges := map[[2]string]bool{}
	for from, tos := range g.efferent {
		for _, to := range tos {
			if module(from) != module(to) {
				edges[[2]string{module(from), module(to)}] = true
			}
		}
	}
	return edges
}

// graphFacts compares the dependencies of the changed files before and
// after the change: a cycle that did not exist, a module that depends on
// another for the first time, a library nobody used. One fact of each kind
// per file at most.
func graphFacts(gs graphs, codeChanges []Change) []Fact {
	var facts []Fact
	baseModules := gs.base.moduleEdges()
	for _, c := range codeChanges {
		before := map[string]bool{}
		for _, to := range gs.base.efferent[c.Path] {
			before[to] = true
		}
		var cycle []string
		var newModules []string
		seenModule := map[string]bool{}
		for _, to := range gs.head.efferent[c.Path] {
			if before[to] || to == c.Path {
				continue
			}
			if back := gs.head.pathBetween(to, c.Path, 12); back != nil {
				if cycle == nil || len(back) < len(cycle) {
					cycle = back
				}
				continue
			}
			pair := [2]string{module(c.Path), module(to)}
			// A new directory depends on everything for the first time;
			// only a directory that existed is compared with its past.
			if pair[0] != pair[1] && !baseModules[pair] && !seenModule[pair[1]] && gs.base.modules[pair[0]] {
				seenModule[pair[1]] = true
				newModules = append(newModules, pair[1])
			}
		}
		if cycle != nil {
			chain := append([]string{c.Path}, cycle...)
			facts = append(facts, Fact{Kind: FactCycle, Path: c.Path, Files: cycle,
				Text: fmt.Sprintf("introduces a cycle: %s", strings.Join(shortChain(chain), " → "))})
		}
		if len(newModules) > 0 {
			facts = append(facts, Fact{Kind: FactDependency, Path: c.Path, Files: newModules,
				Text: fmt.Sprintf("%s depends on %s for the first time", module(c.Path), joinWords(newModules, 3))})
		}
		beforeLibs := map[string]bool{}
		for _, lib := range gs.base.libraries[c.Path] {
			beforeLibs[lib] = true
		}
		var newLibs []string
		for _, lib := range gs.head.libraries[c.Path] {
			if beforeLibs[lib] || gs.base.libraryUsers[lib] > 0 {
				continue
			}
			if _, standard := gs.head.standard[lib]; standard || looksLikePath(gs.root, c.Path, lib) {
				continue
			}
			newLibs = append(newLibs, lib)
		}
		if len(newLibs) > 0 {
			facts = append(facts, Fact{Kind: FactLibrary, Path: c.Path, Files: newLibs,
				Text: fmt.Sprintf("first use of %s in the repository", joinWords(newLibs, 3))})
		}
	}
	return facts
}

// looksLikePath tells an import that names a directory of the repository
// (a path alias, "src/parser/type", or a relative module) from a package of
// a registry: its first segment is a directory next to the importing file
// or above it.
func looksLikePath(root, from, module string) bool {
	if strings.HasPrefix(module, ".") || strings.HasPrefix(module, "/") || strings.HasPrefix(module, "~") || strings.HasPrefix(module, "@/") {
		return true
	}
	first := module
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	if first == "" || strings.Contains(first, ".") {
		return false
	}
	for dir := path.Dir(from); ; dir = path.Dir(dir) {
		candidate := filepath.Join(root, filepath.FromSlash(dir), first)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return true
		}
		if dir == "." || dir == "/" || dir == "" {
			return false
		}
	}
}

// joinWords lists words with commas and "and", the tail counted past max.
func joinWords(words []string, max int) string {
	if len(words) > max {
		return fmt.Sprintf("%s and %d more", strings.Join(words[:max], ", "), len(words)-max)
	}
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// shortChain shortens the paths of a cycle to their base names, keeping the
// directory when two names collide.
func shortChain(chain []string) []string {
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, p := range chain {
		if !seen[p] {
			seen[p] = true
			counts[path.Base(p)]++
		}
	}
	out := make([]string, 0, len(chain))
	for _, p := range chain {
		if counts[path.Base(p)] > 1 {
			out = append(out, p)
		} else {
			out = append(out, path.Base(p))
		}
	}
	return out
}

// cloneFacts points at the twin of a touched function when the change does
// not touch it: half of the inconsistent changes to clones are faults
// (Juergens et al., 2009).
func cloneFacts(files []FileReport, changed map[string]bool) []Fact {
	var facts []Fact
	for _, f := range files {
		if f.Clone == nil || changed[f.Clone.Path] {
			continue
		}
		names := strings.SplitN(f.Clone.Function, " ~ ", 2)
		from, to := names[0], names[len(names)-1]
		facts = append(facts, Fact{Kind: FactClone, Path: f.Path, Files: []string{f.Clone.Path},
			Text: fmt.Sprintf("%s() is %d%% similar to %s:%s(), which this change does not touch", shortName(from), int(f.Clone.Similarity*100+0.5), f.Clone.Path, shortName(to))})
	}
	return facts
}

var (
	exportKeyword = regexp.MustCompile(`(^|\s)(public|export|pub(\([^)]*\))?)\s`)
	hiddenKeyword = regexp.MustCompile(`\b(private|protected)\b`)
)

// isExported tells whether a function is part of what the rest of the code
// may call: by its name in Go and Python, by the keyword on its declaration
// line elsewhere. A free function in PHP or TypeScript without a keyword
// is exported in PHP and local in TypeScript.
func isExported(language, name, declaration string) bool {
	short := shortName(name)
	switch language {
	case "Golang":
		r := []rune(short)
		return len(r) > 0 && unicode.IsUpper(r[0])
	case "Python":
		return !strings.HasPrefix(short, "_")
	case "PHP":
		if !strings.Contains(name, "::") {
			return true
		}
		return exportKeyword.MatchString(declaration) || !hiddenKeyword.MatchString(declaration)
	}
	return exportKeyword.MatchString(declaration)
}

// nameIndex knows, for every identifier called somewhere in the project,
// the files that call it. It is built on demand and once.
type nameIndex struct {
	callers map[string]map[string]bool
}

func buildNameIndex(p *Parsed) *nameIndex {
	idx := &nameIndex{callers: map[string]map[string]bool{}}
	if p == nil {
		return idx
	}
	for rel, file := range p.Files {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			continue
		}
		tokens := rawTokens(string(data))
		for i := 0; i+1 < len(tokens); i++ {
			if tokens[i+1] != "(" {
				continue
			}
			t := tokens[i]
			if t == "" || !(unicode.IsLetter(rune(t[0])) || t[0] == '_' || t[0] == '$') {
				continue
			}
			if idx.callers[t] == nil {
				idx.callers[t] = map[string]bool{}
			}
			idx.callers[t][rel] = true
		}
	}
	return idx
}

// callersOf lists the files calling a name, the file declaring it aside.
func (idx *nameIndex) callersOf(name, except string) []string {
	var out []string
	for rel := range idx.callers[shortName(name)] {
		if rel != except {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// signatureFacts reports the exported functions whose parameters changed,
// with the files that call them, and the exported functions the change
// adds that nothing calls yet. A caller is a file that holds the name
// followed by a parenthesis and depends on the file, or sits in its
// directory; when the graph knows no dependent, every file holding the
// name counts, and the fact says so.
func signatureFacts(gs graphs, head, base *Parsed, codeChanges []Change, names func() *nameIndex) []Fact {
	var facts []Fact
	for _, c := range codeChanges {
		var headFile, baseFile *pb.File
		if head != nil {
			headFile = head.Files[c.Path]
		}
		if base != nil {
			baseFile = base.Files[c.Path]
		}
		if headFile == nil {
			continue
		}
		language := languageOf(headFile, c.Path)
		source, err := os.ReadFile(headFile.Path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(source), "\n")
		related := relatedFiles(gs.head, head, c.Path)
		callers := func(name string) ([]string, bool) {
			byName := names().callersOf(name, c.Path)
			if len(related) == 0 {
				return byName, false
			}
			var out []string
			for _, rel := range byName {
				if related[rel] {
					out = append(out, rel)
				}
			}
			return out, true
		}
		headFns, baseFns := review.CollectFunctions(headFile), review.CollectFunctions(baseFile)
		fnNames := make([]string, 0, len(headFns))
		for name := range headFns {
			fnNames = append(fnNames, name)
		}
		sort.Strings(fnNames)
		var unused []string
		for _, name := range fnNames {
			fn := headFns[name]
			if fn.Location == nil {
				continue
			}
			decl := sliceLines(lines, int(fn.Location.StartLine), int(fn.Location.StartLine))
			if !isExported(language, name, decl) {
				continue
			}
			before, existed := baseFns[name]
			switch {
			case !existed:
				if isEntryPoint(name) || !touches(c.Hunks, int(fn.Location.StartLine), int(fn.Location.EndLine)) && c.Status != 'A' {
					continue
				}
				if len(names().callersOf(name, c.Path)) == 0 {
					unused = append(unused, shortName(name)+"()")
				}
			case !sameParameters(before, fn):
				files, resolved := callers(name)
				text := fmt.Sprintf("%s() %s", shortName(name), parameterChange(before, fn))
				switch {
				case len(files) > 0 && resolved:
					text += fmt.Sprintf(", called from %d %s", len(files), plural(len(files), "file"))
				case len(files) > 0:
					text += fmt.Sprintf(", called by name from %d %s", len(files), plural(len(files), "file"))
				default:
					text += ", no caller in the repository"
				}
				facts = append(facts, Fact{Kind: FactSignature, Path: c.Path, Files: files, Text: text})
			}
		}
		if len(unused) > 0 {
			shown := unused
			if len(shown) > 3 {
				shown = append(append([]string{}, shown[:3]...), fmt.Sprintf("+%d more", len(unused)-3))
			}
			facts = append(facts, Fact{Kind: FactAPI, Path: c.Path, Text: fmt.Sprintf("%d new exported %s called nowhere yet: %s", len(unused), plural(len(unused), "function"), strings.Join(shown, ", "))})
		}
	}
	return facts
}

// isEntryPoint names the functions a runtime calls, which no code needs to.
func isEntryPoint(name string) bool {
	short := strings.ToLower(shortName(name))
	switch short {
	case "main", "init", "__construct", "__destruct", "__invoke", "__tostring", "__get", "__set", "constructor", "new", "default", "drop", "fmt", "dispose", "finalize", "tostring", "equals", "hashcode", "gethashcode", "setup", "teardown", "run":
		return true
	}
	return strings.HasPrefix(short, "test")
}

// relatedFiles are the files that may call a file's functions: those the
// graph says depend on it, and those of its directory.
func relatedFiles(g graph, head *Parsed, rel string) map[string]bool {
	related := map[string]bool{}
	for _, dependent := range g.afferent[rel] {
		related[dependent] = true
	}
	if head != nil {
		dir := path.Dir(rel)
		for other := range head.Files {
			if other != rel && path.Dir(other) == dir {
				related[other] = true
			}
		}
	}
	return related
}

// sameParameters compares two parameter lists by count and, when the
// parser gives them, by type. Names are left out: renaming a parameter
// changes nothing for a caller.
func sameParameters(a, b *pb.StmtFunction) bool {
	if len(a.Parameters) != len(b.Parameters) {
		return false
	}
	for i := range a.Parameters {
		ta, tb := a.Parameters[i].GetType(), b.Parameters[i].GetType()
		if ta != "" && tb != "" && ta != tb {
			return false
		}
	}
	return true
}

// parameterChange words what happened to a parameter list: the counts, and
// the names that appeared or went when the lists tell them apart.
func parameterChange(before, after *pb.StmtFunction) string {
	text := fmt.Sprintf("went from %d to %d %s", len(before.Parameters), len(after.Parameters), plural(len(after.Parameters), "parameter"))
	was := map[string]bool{}
	for _, p := range before.Parameters {
		was[p.GetName()] = true
	}
	is := map[string]bool{}
	for _, p := range after.Parameters {
		is[p.GetName()] = true
	}
	var added, dropped []string
	for _, p := range after.Parameters {
		if n := p.GetName(); n != "" && n != "_" && !was[n] {
			added = append(added, n)
		}
	}
	for _, p := range before.Parameters {
		if n := p.GetName(); n != "" && n != "_" && !is[n] {
			dropped = append(dropped, n)
		}
	}
	var notes []string
	if len(added) > 0 {
		notes = append(notes, "adds "+joinWords(added, 3))
	}
	if len(dropped) > 0 {
		notes = append(notes, "drops "+joinWords(dropped, 3))
	}
	if len(notes) == 0 && len(before.Parameters) == len(after.Parameters) {
		notes = append(notes, "types changed")
	}
	if len(notes) > 0 {
		text += " (" + strings.Join(notes, ", ") + ")"
	}
	return text
}

// sortFacts orders facts by kind, then by path.
func sortFacts(facts []Fact) {
	sort.SliceStable(facts, func(i, j int) bool {
		if factOrder[facts[i].Kind] != factOrder[facts[j].Kind] {
			return factOrder[facts[i].Kind] < factOrder[facts[j].Kind]
		}
		if facts[i].Path != facts[j].Path {
			return facts[i].Path < facts[j].Path
		}
		return facts[i].Text < facts[j].Text
	})
}

// collectFacts gathers every fact about a change and orders them. repo may
// be nil, in which case the history of functions is not asked.
func collectFacts(gs graphs, head, base *Parsed, codeChanges []Change, files []FileReport, repo *Repo, baseSha string, now int64) []Fact {
	changed := map[string]bool{}
	byPath := map[string]Change{}
	for _, c := range codeChanges {
		changed[c.Path] = true
		byPath[c.Path] = c
	}
	var names *nameIndex
	lazyNames := func() *nameIndex {
		if names == nil {
			names = buildNameIndex(head)
		}
		return names
	}
	facts := graphFacts(gs, codeChanges)
	facts = append(facts, cloneFacts(files, changed)...)
	facts = append(facts, signatureFacts(gs, head, base, codeChanges, lazyNames)...)
	facts = append(facts, historyFacts(repo, baseSha, base, files, byPath, now)...)
	sortFacts(facts)
	return facts
}

// factsOf lists the facts about one file.
func factsOf(facts []Fact, path string) []Fact {
	var out []Fact
	for _, f := range facts {
		if f.Path == path {
			out = append(out, f)
		}
	}
	return out
}
