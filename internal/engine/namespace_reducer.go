package engine

// DefaultNamespaceDepth is the number of levels a namespace keeps when it is
// placed on the architecture graph.
const DefaultNamespaceDepth = 3

// NamespaceReducer cuts namespaces down to the few levels that carry the
// architecture of a project.
//
// A fixed depth is enough as long as the project is rooted near the top:
// App\Service\Mailer and App\Http\Router already differ at their second level.
// It stops being enough once the root is as deep as the depth itself. Three
// levels of Company\Project\SubProject\Artifact\Importer leave nothing but
// Company\Project\SubProject, which every class of the project shares: they all
// land on the same node, and the graph then says that the project depends on
// nothing.
//
// The reducer therefore keeps at least one level below the root shared by every
// namespace of the project, so that the modules below that root stay apart. A
// project rooted higher than the depth keeps the depth untouched: this only
// ever adds detail to the projects that had none.
//
// A namespace from outside the project does not share that root, and keeps the
// plain depth: the map is about how the project is arranged, not about the
// inside of the frameworks it uses.
type NamespaceReducer struct {
	// depth is the number of levels kept for a namespace foreign to the project.
	depth int
	// projectDepth is the number of levels kept for a namespace of the project.
	// It equals depth unless the shared root leaves no level below it.
	projectDepth int
	// root is what every namespace of the project reduces to at rootDepth
	// levels, and is empty when they share no root at all.
	root      string
	rootDepth int
}

// NewNamespaceReducer builds a reducer keeping depth levels of the namespaces
// of a project, given all the namespaces that project owns. A depth below one
// falls back to DefaultNamespaceDepth.
func NewNamespaceReducer(owned []string, depth int) *NamespaceReducer {
	if depth < 1 {
		depth = DefaultNamespaceDepth
	}
	reducer := &NamespaceReducer{depth: depth, projectDepth: depth}

	namespaces := make([]string, 0, len(owned))
	for _, namespace := range owned {
		if namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	if len(namespaces) == 0 {
		return reducer
	}

	reducer.rootDepth = commonDepthOfNamespaces(namespaces)
	if reducer.rootDepth < 1 {
		return reducer
	}
	// Every namespace reduces to the same value at that depth, so any of them
	// spells the root out.
	reducer.root = ReduceDepthOfNamespace(namespaces[0], reducer.rootDepth)
	if reducer.rootDepth >= depth {
		reducer.projectDepth = reducer.rootDepth + 1
	}
	return reducer
}

// Reduce cuts a namespace down to the levels that place it on the graph. The
// nil reducer keeps DefaultNamespaceDepth levels of every namespace, knowing no
// project root.
func (r *NamespaceReducer) Reduce(namespace string) string {
	// An import path already names a package, which is the very thing the graph
	// draws. Cutting it would answer the module it belongs to, merging every
	// package of a repository into a single node, and its levels below the
	// module are directories rather than a hierarchy of names.
	if IsImportPath(namespace) {
		return namespace
	}
	if r == nil {
		return ReduceDepthOfNamespace(namespace, DefaultNamespaceDepth)
	}
	if r.projectDepth == r.depth {
		// The root leaves enough levels below it: everything is cut the same way,
		// and telling the project from the rest would change nothing.
		return ReduceDepthOfNamespace(namespace, r.depth)
	}
	if ReduceDepthOfNamespace(namespace, r.rootDepth) == r.root {
		return ReduceDepthOfNamespace(namespace, r.projectDepth)
	}
	return ReduceDepthOfNamespace(namespace, r.depth)
}

// commonDepthOfNamespaces returns the number of levels every namespace of the
// list begins with, and zero when they share nothing.
//
// The levels are counted by reducing rather than by splitting, so that this
// agrees with ReduceDepthOfNamespace on what a level is, down to the quirks of
// a namespace such as github.com/owner/repo where the host counts as one.
func commonDepthOfNamespaces(namespaces []string) int {
	depth := 0
	for {
		root := ReduceDepthOfNamespace(namespaces[0], depth+1)
		for _, namespace := range namespaces[1:] {
			if ReduceDepthOfNamespace(namespace, depth+1) != root {
				return depth
			}
		}
		depth++
		// Reducing the first namespace no longer cuts anything: it has no level
		// left to share, and the loop would not end.
		if root == namespaces[0] {
			return depth
		}
	}
}

// NamespaceReducers holds one reducer per programming language. The root of a
// project is looked for among the namespaces of one language at a time: a PHP
// project rooted at Company\Project\SubProject shares nothing with the
// TypeScript files beside it, whose modules are named after their bare file
// name, and letting them weigh in would leave the PHP code with no root at all.
type NamespaceReducers map[string]*NamespaceReducer

// NewNamespaceReducers builds one reducer per language, out of the namespaces
// each language owns.
func NewNamespaceReducers(ownedByLanguage map[string][]string, depth int) NamespaceReducers {
	reducers := make(NamespaceReducers, len(ownedByLanguage))
	for language, owned := range ownedByLanguage {
		reducers[language] = NewNamespaceReducer(owned, depth)
	}
	return reducers
}

// Reduce cuts a namespace of the given language. A language no reducer was
// built for, and the nil set alike, keep DefaultNamespaceDepth levels.
func (r NamespaceReducers) Reduce(language string, namespace string) string {
	return r[language].Reduce(namespace)
}
