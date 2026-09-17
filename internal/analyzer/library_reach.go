package analyzer

import (
	"sort"
	"strings"
)

// Reach is the part of the scope standing on a library: the files importing
// it, then the files depending on those, level after level. The files of
// Levels[0] import the library themselves; a file of Levels[d] is d
// dependencies away from one that does.
//
// An import is a possible path, not a data flow: the reach is the upper bound
// of what a change or a flaw in the library can touch, and the level is how
// far the file stands from it.
type Reach struct {
	Query string `json:"query"`
	// Modules are the imported modules the query matched, sorted.
	Modules []string `json:"modules"`
	// Levels holds the reached files, sorted, by distance to the library.
	Levels [][]string `json:"levels"`
	// Scope is the number of files the reach is measured against.
	Scope int `json:"scope"`
	// Communities are the communities the reached files belong to, the most
	// affected first; nil when no community was detected.
	Communities []CommunityTouch `json:"communities,omitempty"`
}

// Files is the number of files reached, at every level.
func (r Reach) Files() int {
	files := 0
	for _, level := range r.Levels {
		files += len(level)
	}
	return files
}

// CommunityTouch is how much of a community a reach covers.
type CommunityTouch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Reached is the number of files of the community that are reached, out
	// of Files.
	Reached int `json:"reached"`
	Files   int `json:"files"`
}

// LibraryUse is how much of the scope stands on one imported module.
type LibraryUse struct {
	Module string `json:"module"`
	// Importers is the number of files importing the module.
	Importers int `json:"importers"`
	// Reach is the number of files depending on it at any distance, the
	// importers included.
	Reach int `json:"reach"`
	// Standard is true for a module shipping with the language.
	Standard bool `json:"standard,omitempty"`
}

// WhoUses finds the files of the scope depending on the modules matching a
// query, directly or through other files, and the communities they belong
// to. The query is matched case-insensitively anywhere in the module:
// "log4j" finds "org.apache.logging.log4j" and "org.apache.logging.log4j.core".
// maxDepth bounds the number of levels past the importers; zero or less
// lifts the bound.
func (a *Aggregated) WhoUses(query string, maxDepth int) Reach {
	reach := a.FileDependencies.Reach(query, maxDepth)
	reach.Scope = len(a.ConcernedFiles)
	if a.Community != nil {
		reached := make([]string, 0, reach.Files())
		for _, level := range reach.Levels {
			reached = append(reached, level...)
		}
		reach.Communities = a.Community.Touched(reached)
	}
	return reach
}

// Reach finds the files depending on the modules matching a query, directly
// or through other files. See Aggregated.WhoUses for the matching and the
// bound.
func (g FileDependencyGraph) Reach(query string, maxDepth int) Reach {
	reach := Reach{Query: query, Modules: []string{}, Levels: [][]string{}}
	needle := strings.ToLower(query)
	if needle == "" {
		return reach
	}
	importers := make(map[string]struct{})
	for module, users := range g.LibraryUsers {
		if !strings.Contains(strings.ToLower(module), needle) {
			continue
		}
		reach.Modules = append(reach.Modules, module)
		for _, user := range users {
			importers[user] = struct{}{}
		}
	}
	sort.Strings(reach.Modules)
	if len(importers) == 0 {
		return reach
	}

	frontier := make([]string, 0, len(importers))
	for file := range importers {
		frontier = append(frontier, file)
	}
	sort.Strings(frontier)
	reach.Levels = append(reach.Levels, frontier)
	visited := importers
	for depth := 1; maxDepth <= 0 || depth <= maxDepth; depth++ {
		next := []string{}
		for _, file := range frontier {
			for _, user := range g.Afferent[file] {
				if _, seen := visited[user]; seen {
					continue
				}
				visited[user] = struct{}{}
				next = append(next, user)
			}
		}
		if len(next) == 0 {
			break
		}
		sort.Strings(next)
		reach.Levels = append(reach.Levels, next)
		frontier = next
	}
	return reach
}

// LibraryUses lists every imported module with how far it reaches, the
// farthest-reaching first, then the most imported, then by name.
func (g FileDependencyGraph) LibraryUses() []LibraryUse {
	if len(g.LibraryUsers) == 0 {
		return nil
	}
	// The files are numbered so that a traversal works on slices rather
	// than on maps of paths: one module usually reaches a good part of the
	// project, and there are as many traversals as modules.
	index := make(map[string]int)
	numberOf := func(path string) int {
		id, known := index[path]
		if !known {
			id = len(index)
			index[path] = id
		}
		return id
	}
	for target, sources := range g.Afferent {
		numberOf(target)
		for _, source := range sources {
			numberOf(source)
		}
	}
	for _, users := range g.LibraryUsers {
		for _, user := range users {
			numberOf(user)
		}
	}
	afferent := make([][]int, len(index))
	for target, sources := range g.Afferent {
		id := index[target]
		afferent[id] = make([]int, len(sources))
		for i, source := range sources {
			afferent[id][i] = index[source]
		}
	}

	// A traversal marks the files it visits with its own stamp, which
	// spares clearing the marks between two of them. Modules imported by
	// the very same files reach the very same files.
	visited := make([]int, len(index))
	queue := make([]int, 0, len(index))
	stamp := 0
	reachOf := make(map[string]int)
	uses := make([]LibraryUse, 0, len(g.LibraryUsers))
	for module, users := range g.LibraryUsers {
		key := strings.Join(users, "\x00")
		reach, known := reachOf[key]
		if !known {
			stamp++
			queue = queue[:0]
			for _, user := range users {
				id := index[user]
				visited[id] = stamp
				queue = append(queue, id)
			}
			for head := 0; head < len(queue); head++ {
				for _, source := range afferent[queue[head]] {
					if visited[source] != stamp {
						visited[source] = stamp
						queue = append(queue, source)
					}
				}
			}
			reach = len(queue)
			reachOf[key] = reach
		}
		_, standard := g.StandardLibraries[module]
		uses = append(uses, LibraryUse{Module: module, Importers: len(users), Reach: reach, Standard: standard})
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].Reach != uses[j].Reach {
			return uses[i].Reach > uses[j].Reach
		}
		if uses[i].Importers != uses[j].Importers {
			return uses[i].Importers > uses[j].Importers
		}
		return uses[i].Module < uses[j].Module
	})
	return uses
}

// Touched tells which communities a set of files belongs to, and how much of
// each is covered, the most covered first. A file holding units of several
// communities counts for each of them.
func (m *CommunityMetrics) Touched(files []string) []CommunityTouch {
	if m == nil || len(m.FileCommunities) == 0 {
		return nil
	}
	total := make(map[string]int)
	for _, ids := range m.FileCommunities {
		for _, id := range ids {
			total[id]++
		}
	}
	reached := make(map[string]int)
	for _, file := range files {
		for _, id := range m.FileCommunities[file] {
			reached[id]++
		}
	}
	if len(reached) == 0 {
		return nil
	}
	nameOf := func(id string) string {
		for _, community := range m.Communities {
			if community.ID == id {
				if community.ShortName != "" {
					return community.ShortName
				}
				return community.Name
			}
		}
		if m.Shared != nil && m.Shared.ID == id {
			return "shared kernel"
		}
		return id
	}
	touched := make([]CommunityTouch, 0, len(reached))
	for id, count := range reached {
		touched = append(touched, CommunityTouch{ID: id, Name: nameOf(id), Reached: count, Files: total[id]})
	}
	sort.Slice(touched, func(i, j int) bool {
		ri := float64(touched[i].Reached) / float64(touched[i].Files)
		rj := float64(touched[j].Reached) / float64(touched[j].Files)
		if ri != rj {
			return ri > rj
		}
		if touched[i].Reached != touched[j].Reached {
			return touched[i].Reached > touched[j].Reached
		}
		return touched[i].ID < touched[j].ID
	})
	return touched
}
