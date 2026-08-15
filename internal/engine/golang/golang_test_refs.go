package golang

import (
	pb "github.com/ast-metrics/ast-metrics/pb"
	sitter "github.com/smacker/go-tree-sitter"
)

// A Go test lives in the same package as the code it tests: it references the
// tested symbols directly, without any import. The import list of a test file
// therefore says nothing about what the test exercises. To trace tests back to
// production code, walk the test file and record the symbols it references:
// type usages (Counter{}, var c Counter) and function calls (NewCounter()).
// Types are qualified with the package name ("counter\Counter") to match the
// qualified names produced by the visitor; functions keep their short name,
// which is how top-level functions are indexed.

// goBuiltinTypes lists the predeclared Go types: referencing one says nothing
// about production code.
var goBuiltinTypes = map[string]bool{
	"bool": true, "byte": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "any": true, "comparable": true,
}

// goBuiltinFuncs lists the predeclared Go functions.
var goBuiltinFuncs = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
}

// attachTestSymbolRefs appends the symbols referenced by a test file to its
// external dependencies, so the test quality aggregator can match them against
// production structs and functions.
func attachTestSymbolRefs(file *pb.File, adapter *TreeSitterAdapter, root *sitter.Node, src []byte) {
	if file == nil || file.Stmts == nil || root == nil {
		return
	}

	// pkg is the bare name of the package, the one a production struct is
	// qualified with, so that a reference to it reads the same as the struct.
	// The package itself is named by its import path, as every dependency of
	// the file is: the graph links the two ends of an edge by that name.
	pkg, importPath := "", ""
	if len(file.Stmts.StmtNamespace) > 0 && file.Stmts.StmtNamespace[0].Name != nil {
		pkg = file.Stmts.StmtNamespace[0].Name.GetShort()
		importPath = file.Stmts.StmtNamespace[0].Name.GetQualified()
	}
	if importPath == "" {
		importPath = pkg
	}

	// imports maps the local name of an import ("assert") to its module path.
	// A selector whose operand is not in this map is a call on a variable, not
	// a package reference.
	imports := map[string]string{}
	// declared holds the names declared by the test file itself (helpers,
	// mocks): a reference to them is not a reference to production code.
	declared := map[string]bool{}
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "import_declaration":
			for _, it := range adapter.Imports(child) {
				imports[it.Name] = it.Module
			}
		case "function_declaration":
			if id := firstChildOfType(child, "identifier"); id != nil {
				declared[text(src, id)] = true
			}
		case "type_declaration":
			eachDescendantOfType(child, "type_spec", func(spec *sitter.Node) {
				if name := spec.ChildByFieldName("name"); name != nil {
					declared[text(src, name)] = true
				}
			})
		}
	}

	seen := map[string]bool{}
	addRef := func(className, namespace string) {
		key := className + "|" + namespace
		if className == "" || seen[key] {
			return
		}
		seen[key] = true
		file.Stmts.StmtExternalDependencies = append(file.Stmts.StmtExternalDependencies, &pb.StmtExternalDependency{
			ClassName: className,
			Namespace: namespace,
			From:      importPath,
		})
	}

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "package_clause", "import_declaration":
			return
		case "qualified_type":
			// pkg.Type: a type from another package (external test packages,
			// "package counter_test", reference the tested package this way)
			pkgID := firstChildOfType(n, "package_identifier")
			typeID := firstChildOfType(n, "type_identifier")
			if pkgID != nil && typeID != nil {
				p := text(src, pkgID)
				if module, ok := imports[p]; ok {
					addRef(p+"\\"+text(src, typeID), module)
				}
			}
			return
		case "type_identifier":
			name := text(src, n)
			if pkg != "" && !goBuiltinTypes[name] && !declared[name] {
				addRef(pkg+"\\"+name, importPath)
			}
			return
		case "call_expression":
			if fn := n.ChildByFieldName("function"); fn != nil {
				switch fn.Type() {
				case "identifier":
					name := text(src, fn)
					if !goBuiltinFuncs[name] && !goBuiltinTypes[name] && !declared[name] {
						addRef(name, importPath)
					}
				case "selector_expression":
					op := fn.ChildByFieldName("operand")
					field := fn.ChildByFieldName("field")
					if op != nil && op.Type() == "identifier" && field != nil {
						p := text(src, op)
						if module, ok := imports[p]; ok {
							addRef(p+"\\"+text(src, field), module)
						}
					}
				}
			}
			// keep walking: arguments may hold composite literals or nested calls
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
}
