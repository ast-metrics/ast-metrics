package analyzer

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// projectIndex knows the classes and namespaces the project itself declares,
// so that a dependency can be told to point inside the project or outside it,
// and be brought back to the class or the file it points at.
//
// A dependency names its target the way the language does: PHP writes the
// qualified name of the class, Java and Python the package and the class apart,
// Go and C# the package or namespace alone. The targets are indexed every one
// of those ways, so that a class or a file is found whichever way it is named.
//
// Test files are left out: a test depends on what it tests, and would otherwise
// count as one more user of every class it exercises.
type projectIndex struct {
	files                      []*pb.File
	classes                    map[string]*pb.StmtClass // qualified name -> class
	classesByNamespaceAndShort map[string]*pb.StmtClass // "namespace|Short" -> class
	fileOfClass                map[*pb.StmtClass]*pb.File
	filesByNamespace           map[string][]*pb.File
	namespaceOfFile            map[*pb.File]string
	// The types: classes, traits, enums and interfaces alike. An interface is
	// as much a part of the architecture as a class, and often the part the
	// others depend on.
	types                    map[string]*projectType // qualified name -> type
	typesByNamespaceAndShort map[string]*projectType
	typesOfFile              map[*pb.File][]*projectType
	// scopes caches, per file, what its imports let a simple name stand for.
	scopes map[*pb.File]*importScope
}

// importScope is what a file imported: the classes brought in by name, and
// the namespaces brought in whole (a wildcard import, a using directive).
type importScope struct {
	named      map[string]string // simple name -> namespace it was imported from
	namespaces []string          // namespaces imported whole, in order
	// typeNamespaces holds the namespaces of the types of the project the
	// file uses by name, filled on first use.
	typeNamespaces map[string]bool
}

// projectType is a class or an interface the project declares.
type projectType struct {
	Qualified string
	Short     string
	File      *pb.File
	// Class is nil for an interface.
	Class *pb.StmtClass
}

func indexProject(files []*pb.File) *projectIndex {
	ix := &projectIndex{
		classes:                    make(map[string]*pb.StmtClass),
		classesByNamespaceAndShort: make(map[string]*pb.StmtClass),
		fileOfClass:                make(map[*pb.StmtClass]*pb.File),
		filesByNamespace:           make(map[string][]*pb.File),
		namespaceOfFile:            make(map[*pb.File]string),
		types:                      make(map[string]*projectType),
		typesByNamespaceAndShort:   make(map[string]*projectType),
		typesOfFile:                make(map[*pb.File][]*projectType),
		scopes:                     make(map[*pb.File]*importScope),
	}
	for _, file := range files {
		if file == nil || file.Stmts == nil || file.GetIsTest() {
			continue
		}
		ix.files = append(ix.files, file)
		namespace := namespaceOfFile(file)
		ix.namespaceOfFile[file] = namespace
		if namespace != "" {
			ix.filesByNamespace[namespace] = append(ix.filesByNamespace[namespace], file)
		}
		addType := func(t *projectType) {
			ix.types[t.Qualified] = t
			ix.typesByNamespaceAndShort[namespace+"|"+t.Short] = t
			ix.typesOfFile[file] = append(ix.typesOfFile[file], t)
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class == nil || class.Name == nil || class.Name.Qualified == "" {
				continue
			}
			ix.classes[class.Name.Qualified] = class
			ix.classesByNamespaceAndShort[namespace+"|"+class.Name.Short] = class
			ix.fileOfClass[class] = file
			addType(&projectType{Qualified: class.Name.Qualified, Short: class.Name.Short, File: file, Class: class})
		}
		for _, itf := range file.Stmts.StmtInterface {
			if itf == nil || itf.Name == nil || itf.Name.Qualified == "" {
				continue
			}
			if _, isClass := ix.types[itf.Name.Qualified]; isClass {
				continue
			}
			addType(&projectType{Qualified: itf.Name.Qualified, Short: itf.Name.Short, File: file})
		}
	}
	return ix
}

// targetTypeOf returns the class or interface of the project a dependency
// points at, and nil for a package, a namespace or a foreign type. The file
// is the one the dependency is written in: a simple name, written without its
// namespace the way Java and C# write most types, is resolved the way their
// compilers resolve it, through the imports of the file, then its own
// namespace, then the namespaces imported whole.
func (ix *projectIndex) targetTypeOf(dependency *pb.StmtExternalDependency, file *pb.File) *projectType {
	if dependency == nil {
		return nil
	}
	if dependency.Namespace == "" {
		return ix.resolveSimpleName(dependency.ClassName, file)
	}
	if t := ix.types[dependency.Namespace]; t != nil {
		return t
	}
	if dependency.ClassName == "" {
		return nil
	}
	return ix.typesByNamespaceAndShort[dependency.Namespace+"|"+dependency.ClassName]
}

// resolveSimpleName finds the type of the project a simple name stands for in
// a file, and nil when it stands for none: a type of the standard library, of
// a framework, or a type parameter.
func (ix *projectIndex) resolveSimpleName(name string, file *pb.File) *projectType {
	if name == "" || file == nil {
		return nil
	}
	scope := ix.scopeOf(file)
	if namespace, imported := scope.named[name]; imported {
		if t := ix.typesByNamespaceAndShort[namespace+"|"+name]; t != nil {
			return t
		}
		// an alias for a type named in full: using Builder = A.B.StringBuilder
		if t := ix.types[namespace]; t != nil {
			return t
		}
	}
	// the namespace of the file, then its parents: C# sees the enclosing
	// namespaces, Java the package alone, and a parent match in Java is
	// harmless since nothing else would resolve the name
	for namespace := ix.namespaceOfFile[file]; ; namespace = parentNamespace(namespace) {
		if t := ix.typesByNamespaceAndShort[namespace+"|"+name]; t != nil {
			return t
		}
		if namespace == "" {
			break
		}
	}
	for _, namespace := range scope.namespaces {
		if t := ix.typesByNamespaceAndShort[namespace+"|"+name]; t != nil {
			return t
		}
	}
	return nil
}

// dependencyKey names what a dependency points at, so that two dependencies
// written differently for the same thing count once: an import and the simple
// name it resolves, a qualified name and its import. The second value is false
// for a simple name that resolves to nothing, neither a type of the project
// nor an import of the file: a type of the standard library written by its
// bare name, or a type parameter, which the analysis has nothing to say about.
func (ix *projectIndex) dependencyKey(dependency *pb.StmtExternalDependency, file *pb.File) (string, bool) {
	if dependency == nil {
		return "", false
	}
	if t := ix.targetTypeOf(dependency, file); t != nil {
		return "type:" + t.Qualified, true
	}
	if dependency.Namespace != "" {
		if dependency.ClassName == "" && namespaceImportIsNotAUse(file.GetProgrammingLanguage()) && ix.usesTypesOf(file, dependency.Namespace) {
			// a using directive on a namespace the file uses types of: the
			// types count, the directive that let them be written does not
			return "", false
		}
		return dependency.Namespace + "|" + dependency.ClassName, true
	}
	if namespace, imported := ix.scopeOf(file).named[dependency.ClassName]; imported {
		return namespace + "|" + dependency.ClassName, true
	}
	return "", false
}

// usesTypesOf tells whether a file uses, by name, at least one type of the
// project declared in the given namespace.
func (ix *projectIndex) usesTypesOf(file *pb.File, namespace string) bool {
	if file == nil {
		return false
	}
	scope := ix.scopeOf(file)
	if scope.typeNamespaces == nil {
		scope.typeNamespaces = map[string]bool{}
		for _, dependency := range engine.GetDependenciesInFile(file) {
			if dependency == nil || dependency.ClassName == "" {
				continue
			}
			if t := ix.targetTypeOf(dependency, file); t != nil {
				scope.typeNamespaces[ix.namespaceOfFile[t.File]] = true
			}
		}
	}
	return scope.typeNamespaces[namespace]
}

// scopeOf reads the imports of a file once.
func (ix *projectIndex) scopeOf(file *pb.File) *importScope {
	if scope, ok := ix.scopes[file]; ok {
		return scope
	}
	scope := &importScope{named: map[string]string{}}
	seen := map[string]bool{}
	record := func(dependency *pb.StmtExternalDependency) {
		if dependency == nil || dependency.Namespace == "" {
			return
		}
		if dependency.ClassName == "" {
			if !seen[dependency.Namespace] {
				seen[dependency.Namespace] = true
				scope.namespaces = append(scope.namespaces, dependency.Namespace)
			}
			return
		}
		if _, known := scope.named[dependency.ClassName]; !known {
			scope.named[dependency.ClassName] = dependency.Namespace
		}
	}
	if file != nil && file.Stmts != nil {
		for _, dependency := range file.Stmts.StmtExternalDependencies {
			record(dependency)
		}
		for _, namespace := range file.Stmts.StmtNamespace {
			if namespace != nil && namespace.Stmts != nil {
				for _, dependency := range namespace.Stmts.StmtExternalDependencies {
					record(dependency)
				}
			}
		}
	}
	ix.scopes[file] = scope
	return scope
}

// sourceTypeOf returns the class or interface of the project a dependency is
// used from. A dependency used from a class or one of its methods names that
// class; one used from the top of a file declaring a single type, the way an
// interface or a class following PSR-4 does, is that type's. Nil otherwise.
func (ix *projectIndex) sourceTypeOf(dependency *pb.StmtExternalDependency, file *pb.File) *projectType {
	if dependency == nil {
		return nil
	}
	// the scope itself, or the class a method is named after: PHP writes
	// Class::method, the other languages Class.method
	for name := dependency.From; name != ""; name = parentOfScope(name) {
		if t := ix.types[name]; t != nil {
			return t
		}
	}
	// A file declaring a single type, the way PSR-4 and most conventions
	// want it: whatever is used in it is used by that type, be it from a
	// method the engine did not name after it or from a line at the top. A
	// file declaring several (a class and its nested classes) gives what is
	// written at its top, its imports first, to the first of them.
	types := ix.typesOfFile[file]
	if len(types) == 1 {
		return types[0]
	}
	if len(types) > 1 && (dependency.From == "" || dependency.From == ix.namespaceOfFile[file]) {
		return types[0]
	}
	return nil
}

// targetClassOf returns the class of the project a dependency points at, and
// nil for a package, a namespace or a foreign class.
func (ix *projectIndex) targetClassOf(dependency *pb.StmtExternalDependency, file *pb.File) *pb.StmtClass {
	if dependency == nil {
		return nil
	}
	if dependency.Namespace == "" {
		if t := ix.resolveSimpleName(dependency.ClassName, file); t != nil {
			return t.Class
		}
		return nil
	}
	if class := ix.classes[dependency.Namespace]; class != nil {
		return class
	}
	if dependency.ClassName == "" {
		return nil
	}
	return ix.classesByNamespaceAndShort[dependency.Namespace+"|"+dependency.ClassName]
}

// targetNamespaceOf returns the namespace of the project a dependency points
// at: the namespace of the file declaring the class, or the namespace itself
// when the dependency names one. The second value is false when the dependency
// points outside the project.
func (ix *projectIndex) targetNamespaceOf(dependency *pb.StmtExternalDependency, file *pb.File) (string, bool) {
	if t := ix.targetTypeOf(dependency, file); t != nil {
		return ix.namespaceOfFile[t.File], true
	}
	if dependency == nil || dependency.Namespace == "" {
		// a simple name that resolved to nothing is foreign, or a type
		// parameter: it names no namespace of the project
		return "", false
	}
	if _, declared := ix.filesByNamespace[dependency.Namespace]; declared {
		return dependency.Namespace, true
	}
	// The name may be a type of the project the analysis did not see declared,
	// written below one of its namespaces: the namespace is then the target.
	// Only a name written like a type is taken that way; a lowercase leaf is a
	// function, a constant or a name the parser mistook for one, and does not
	// stand for a part of the architecture.
	if !looksLikeType(lastSegmentOfName(dependency.Namespace)) {
		return "", false
	}
	for name := parentNamespace(dependency.Namespace); name != ""; name = parentNamespace(name) {
		if _, declared := ix.filesByNamespace[name]; declared {
			return name, true
		}
	}
	return "", false
}

// parentOfScope returns the scope a qualified scope name is written under:
// the class of Class::method or of pkg.Class.method, and an empty string for a
// name with nothing before its last segment.
func parentOfScope(name string) string {
	return parentNamespace(name)
}

// looksLikeType tells whether a name is written the way a class is: with an
// uppercase first letter.
func looksLikeType(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// lastSegmentOfName returns the last segment of a qualified name.
func lastSegmentOfName(name string) string {
	if parent := parentNamespace(name); parent != "" {
		return strings.TrimLeft(name[len(parent):], `\./:`)
	}
	return name
}

// parentNamespace returns the namespace a qualified name is declared in, and
// an empty string for a top-level name.
func parentNamespace(name string) string {
	// Rust writes crate::module::item
	if i := strings.LastIndex(name, "::"); i > 0 {
		return name[:i]
	}
	last := -1
	for i := 0; i < len(name); i++ {
		if name[i] == '\\' || name[i] == '.' || name[i] == '/' {
			last = i
		}
	}
	if last <= 0 {
		return ""
	}
	return name[:last]
}

// targetFilesOf returns the files of the project a dependency points at: the
// one declaring the class, or every file of the package or namespace.
func (ix *projectIndex) targetFilesOf(dependency *pb.StmtExternalDependency, file *pb.File) []*pb.File {
	if class := ix.targetClassOf(dependency, file); class != nil {
		return []*pb.File{ix.fileOfClass[class]}
	}
	if dependency == nil || dependency.Namespace == "" {
		return nil
	}
	return ix.filesByNamespace[dependency.Namespace]
}

// sourceClassOf returns the class of the project a dependency is used from,
// and nil when it is used from a namespace, a plain function or a foreign
// scope. A method is named "Class::method" by every engine, so its class is
// read off its name.
func (ix *projectIndex) sourceClassOf(dependency *pb.StmtExternalDependency) *pb.StmtClass {
	if dependency == nil || dependency.From == "" {
		return nil
	}
	if class := ix.classes[dependency.From]; class != nil {
		return class
	}
	if i := strings.LastIndex(dependency.From, "::"); i > 0 {
		return ix.classes[dependency.From[:i]]
	}
	return nil
}
