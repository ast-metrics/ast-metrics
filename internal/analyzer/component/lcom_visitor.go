package analyzer

import (
	"strings"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

type LackOfCohesionOfMethodsVisitor struct {
	// Language is the programming language of the file being visited. It uses the
	// same values as pb.File.ProgrammingLanguage ("PHP", "Java", ...) and drives the
	// detection of lifecycle methods, which are excluded from the LCOM4 graph.
	// When left empty, a generic policy is applied (see genericLifecycleMethods).
	Language string
}

// lifecycleMethodsByLanguage lists, per language, the methods excluded from the LCOM4
// graph. A constructor or a destructor usually touches every attribute of the class:
// keeping it in the graph connects all the other methods together and hides the real
// lack of cohesion. See https://www.aivosto.com/project/help/pm-oo-cohesion.html
var lifecycleMethodsByLanguage = map[string][]string{
	"PHP":        {"__construct", "__destruct"},
	"Python":     {"__init__", "__new__", "__del__"},
	"TypeScript": {"constructor"},
	"Java":       {"finalize"},
	"C#":         {"Finalize"},
	"Rust":       {"new", "drop"},
	"Golang":     {},
	"C++":        {},
}

// genericLifecycleMethods is used when the language is unknown. It only contains the
// names that cannot be mistaken for a regular method in any supported language.
var genericLifecycleMethods = []string{
	"__construct", "__destruct",
	"__init__", "__new__", "__del__",
	"constructor",
	"finalize", "Finalize",
}

// languagesWithConstructorNamedAfterClass lists the languages where the constructor
// bears the name of the class. It also covers destructors in these languages, since
// parsers expose "~Foo" as "Foo".
var languagesWithConstructorNamedAfterClass = map[string]bool{
	"Java": true,
	"C#":   true,
	"PHP":  true, // PHP 4 style constructors
	"C++":  true,
}

// qualifiedNameSeparators lists the separators used by the parsers to build a
// qualified name, from the outermost to the innermost scope.
var qualifiedNameSeparators = []string{"\\", "::", "."}

// receiverKeywords lists the identifiers designating the current instance. Some
// parsers report them as plain operands ("self" in Python and Rust): they are not
// attributes, and keeping them would connect every method of the class together.
var receiverKeywords = map[string]bool{
	"this":  true,
	"self":  true,
	"Self":  true,
	"cls":   true,
	"super": true,
}

func (v *LackOfCohesionOfMethodsVisitor) Visit(stmts *pb.Stmts, parents *pb.Stmts) {
	// Compute LCOM for the current node when analyzable data is available
	if stmts == nil {
		return
	}
	v.Calculate(stmts)
}

func (v *LackOfCohesionOfMethodsVisitor) LeaveNode(stmts *pb.Stmts) {
	// Ensure LCOM is computed for the current node as well (root/file level included)
	if stmts == nil {
		return
	}
	v.Calculate(stmts)
}

/**
 * Calculates Lack of Cohesion of Methods (LCOM)
 *
 *      According to Wikipedia, "Lack of Cohesion of Methods (LCOM) is a software metric used to measure the cohesion
 *      of methods within a class. It is an indicator of how closely related and focused the methods in a class are."
 */
func (v *LackOfCohesionOfMethodsVisitor) Calculate(stmts *pb.Stmts) {
	if stmts == nil {
		return
	}

	// get vars in namespace
	classes := []*pb.StmtClass{}

	classes = append(classes, stmts.StmtClass...)
	for _, namespace := range stmts.StmtNamespace {
		if namespace == nil || namespace.Stmts == nil {
			continue
		}
		classes = append(classes, namespace.Stmts.StmtClass...)
	}

	for _, class := range classes {
		if class == nil || class.Stmts == nil {
			continue
		}
		if class.Stmts.Analyze == nil {
			class.Stmts.Analyze = &pb.Analyze{}
		}
		if class.Stmts.Analyze.ClassCohesion == nil {
			class.Stmts.Analyze.ClassCohesion = &pb.ClassCohesion{}
		}

		// Only the methods carrying a responsibility take part in the graph
		methods := v.methodsInGraph(class)

		// A class without any of them (no method at all, or only constructors,
		// destructors and empty stubs) has no cohesion to measure: LCOM4 = 0
		if len(methods) == 0 {
			none := int32(0)
			class.Stmts.Analyze.ClassCohesion.Lcom4 = &none
			continue
		}

		// use matrix to count method/field interactions
		// method1: { field1: true, field2: true, method2:true, method3: false }

		// initialize matrix
		matrix := map[string]map[string]bool{}

		for _, method := range methods {

			operandsInMethods := []string{}
			for _, operand := range method.Operands {
				// in methods, names are this.x

				// if name contains more than one dot => skip it. It's probably a dynamic attribute ($this->$foo->$bar item)
				name := strings.TrimPrefix(operand.Name, "this.")
				if strings.Count(name, ".") != 0 {
					continue
				}
				if receiverKeywords[name] {
					continue
				}

				exists := false
				for _, op := range operandsInMethods {
					if op == name {
						exists = true
						continue
					}
				}
				if !exists {
					operandsInMethods = append(operandsInMethods, name)
				}

			}

			// Now we fill in the matrix
			// with operand (attributes) used in method
			matrix[method.Name.Qualified] = map[string]bool{}
			for _, op := range operandsInMethods {
				matrix[method.Name.Qualified][op] = true
			}
			// and with internal methods calls
			for _, call := range method.MethodCalls {
				// if name contains more than one dot => skip it. It's probably a dynamic attribute ($this->$foo->$bar item)
				name := strings.TrimPrefix(call.Name, "this.")
				if strings.Count(name, ".") != 0 {
					continue
				}
				// append "()" to the end of call
				matrix[method.Name.Qualified][name+"()"] = true
			}
		}

		l4 := int32(v.lcom4(matrix, methods))
		class.Stmts.Analyze.ClassCohesion.Lcom4 = &l4
	}
}

// methodsInGraph returns the methods of the class that are relevant for LCOM4.
//
// Two families of methods are left out:
//   - lifecycle methods (constructors, destructors), which access most of the
//     attributes and would therefore connect unrelated responsibilities;
//   - empty methods (stubs, unimplemented interface methods), which have no edge
//     at all and would each be counted as an extra component.
func (v *LackOfCohesionOfMethodsVisitor) methodsInGraph(class *pb.StmtClass) []*pb.StmtFunction {
	methods := make([]*pb.StmtFunction, 0, len(class.Stmts.StmtFunction))
	for _, method := range class.Stmts.StmtFunction {
		if method == nil || method.Name == nil {
			continue
		}
		if v.isLifecycleMethod(class, method) {
			continue
		}
		if isEmptyMethod(method) {
			continue
		}
		methods = append(methods, method)
	}
	return methods
}

// isLifecycleMethod tells whether the method is a constructor or a destructor of the class.
func (v *LackOfCohesionOfMethodsVisitor) isLifecycleMethod(class *pb.StmtClass, method *pb.StmtFunction) bool {
	name := shortName(method.Name)
	if name == "" {
		return false
	}

	lifecycleMethods, isKnownLanguage := lifecycleMethodsByLanguage[v.Language]
	if !isKnownLanguage {
		lifecycleMethods = genericLifecycleMethods
	}
	for _, lifecycleMethod := range lifecycleMethods {
		if name == lifecycleMethod {
			return true
		}
	}

	// A method named after its class is a constructor (or a destructor, whether
	// the parser keeps the leading "~" or drops it) in Java, C#, C++ and legacy
	// PHP.
	if isKnownLanguage && !languagesWithConstructorNamedAfterClass[v.Language] {
		return false
	}
	if class.Name == nil {
		return false
	}
	className := shortName(class.Name)

	return className != "" && (name == className || name == "~"+className)
}

// isEmptyMethod tells whether the method body holds no statement.
func isEmptyMethod(method *pb.StmtFunction) bool {
	return method.LinesOfCode != nil && method.LinesOfCode.LogicalLinesOfCode == 0
}

// shortName returns the name of a symbol, without the scope it belongs to.
func shortName(name *pb.Name) string {
	if name == nil {
		return ""
	}
	if name.Short != "" {
		return name.Short
	}

	short := name.Qualified
	for _, separator := range qualifiedNameSeparators {
		if index := strings.LastIndex(short, separator); index != -1 {
			short = short[index+len(separator):]
		}
	}
	return short
}

// LCOM_HS (Henderson-Sellers) algorithm
// see https://en.wikipedia.org/wiki/Lack_of_cohesion_of_methods
//
// This algo is not used => only for my pleasure, I wanted to code it
func (v *LackOfCohesionOfMethodsVisitor) lcom1(matrix map[string]map[string]bool, class *pb.StmtClass) float64 {

	methods := make([]string, 0, len(matrix))
	for m := range matrix {
		methods = append(methods, m)
	}
	// A = ensemble des attributs (tokens sans "()")
	A := map[string]struct{}{}
	// U(m) = attributs utilisés par m
	U := map[string]map[string]struct{}{}

	for m, tokens := range matrix {
		if _, ok := U[m]; !ok {
			U[m] = map[string]struct{}{}
		}
		for tok, used := range tokens {
			if !used {
				continue
			}
			if strings.HasSuffix(tok, "()") {
				// internal method call: ignored for LCOM_HS
				continue
			}
			// tok = nom d'attribut normalisé (ex: "x")
			U[m][tok] = struct{}{}
			A[tok] = struct{}{}
		}
	}

	m := len(methods)
	a := len(A)

	if m <= 1 || a == 0 {
		// Classe triviale: cohésion considérée parfaite
		// fmt.Printf("%s LCOM_HS=0.00 (m=%d, a=%d)\n", class.Name.Qualified, m, a)
		return 0.0
	}

	//sum(mu(a_i)) = nb total de méthodes qui utilisent chaque attribut
	sumMu := 0
	for attr := range A {
		mu := 0
		for _, meth := range methods {
			if _, ok := U[meth][attr]; ok {
				mu++
			}
		}
		sumMu += mu
	}

	// LCOM_HS borné à [0,1]
	lcom := (float64(m) - (float64(sumMu) / float64(a))) / float64(m-1)
	if lcom < 0 {
		lcom = 0.0
	}
	if lcom > 1 {
		lcom = 1.0
	}

	// fmt.Printf("%s LCOM_HS=%.2f (m=%d, a=%d, Σμ=%d)\n", class.Name.Qualified, lcom, m, a, sumMu)
	return lcom
}

// LCOM4 (Hitz & Montazeri) : nb de composantes connexes
func (v *LackOfCohesionOfMethodsVisitor) lcom4(matrix map[string]map[string]bool, methodsInGraph []*pb.StmtFunction) int {
	// 1) Index simple -> qualifié pour résoudre "foo()" -> "Class::foo"
	//    Seules les méthodes du graphe y figurent: un appel vers un constructeur
	//    ou vers un stub vide ne doit pas créer d'arête.
	nameIndex := map[string]string{}
	for _, m := range methodsInGraph {
		nameIndex[shortName(m.Name)] = m.Name.Qualified
	}

	// 2) Liste des méthodes (nœuds du graphe)
	methods := make([]string, 0, len(matrix))
	for m := range matrix {
		methods = append(methods, m)
	}
	if len(methods) == 0 {
		return 0
	}
	if len(methods) == 1 {
		return 1
	}

	// 3) Prépare: U(m) = attributs utilisés (sans "()")
	U := map[string]map[string]struct{}{}
	for m, toks := range matrix {
		if _, ok := U[m]; !ok {
			U[m] = map[string]struct{}{}
		}
		for tok, used := range toks {
			if !used {
				continue
			}
			if strings.HasSuffix(tok, "()") {
				continue
			}
			U[m][tok] = struct{}{}
		}
	}

	// 4) Graphe non orienté
	adj := map[string]map[string]struct{}{}
	addNode := func(x string) {
		if _, ok := adj[x]; !ok {
			adj[x] = map[string]struct{}{}
		}
	}
	addEdge := func(a, b string) {
		if a == b || a == "" || b == "" {
			return
		}
		addNode(a)
		addNode(b)
		adj[a][b] = struct{}{}
		adj[b][a] = struct{}{}
	}
	for _, m := range methods {
		addNode(m)
	}

	// 5) Arêtes "partage d'attribut"
	intersects := func(a, b map[string]struct{}) bool {
		if len(a) > len(b) {
			a, b = b, a
		}
		for k := range a {
			if _, ok := b[k]; ok {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(methods); i++ {
		for j := i + 1; j < len(methods); j++ {
			if intersects(U[methods[i]], U[methods[j]]) {
				addEdge(methods[i], methods[j])
			}
		}
	}

	// 6) Arêtes "appels internes" via tokens "foo()"
	for m, toks := range matrix {
		for tok, used := range toks {
			if !used || !strings.HasSuffix(tok, "()") {
				continue
			}
			callee := strings.TrimSuffix(tok, "()")
			if q, ok := nameIndex[callee]; ok {
				addEdge(m, q) // intra-classe
			}
		}
	}

	// 7) Comptage des composantes connexes (DFS)
	visited := map[string]bool{}
	var stack []string
	components := 0

	for _, start := range methods {
		if visited[start] {
			continue
		}
		components++
		stack = stack[:0]
		stack = append(stack, start)
		visited[start] = true

		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for nb := range adj[n] {
				if !visited[nb] {
					visited[nb] = true
					stack = append(stack, nb)
				}
			}
		}
	}

	// fmt.Printf("LCOM4=%d\n", components)
	return components
}
