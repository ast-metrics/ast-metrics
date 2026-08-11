package treesitter

import (
	"path/filepath"
	"strings"

	engine "github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	sitter "github.com/smacker/go-tree-sitter"
)

type Visitor struct {
	ad    LangAdapter
	file  *pb.File
	ns    *pb.StmtNamespace
	lines []string

	classStk []*pb.StmtClass
	funcStk  []*pb.StmtFunction

	// receiverMethods holds the methods declared outside of the class they
	// belong to (Go receivers). They are attached to their class once the whole
	// file has been visited, because a method may be declared before its type.
	receiverMethods []receiverMethod

	// logicalLines holds the 1-based line numbers on which a statement starts.
	// LLOC at every level (file, class, function) is the number of such lines
	// in the scope's range.
	logicalLines map[int]bool
}

// receiverMethod is a method waiting to be attached to the class of its
// receiver.
type receiverMethod struct {
	fn       *pb.StmtFunction
	receiver string
}

// IsDefaultLogicalNode reports whether a tree-sitter node type is a statement
// for LLOC purposes. Statements and local declarations count; pure structure
// (blocks), member declarations (classes, functions, fields) and imports do
// not. Adapters can refine this per grammar by implementing
// IsLogicalNode(*sitter.Node) bool.
func IsDefaultLogicalNode(nodeType string) bool {
	switch nodeType {
	case "compound_statement", "statement_block", "empty_statement",
		"import_statement", "import_from_statement", "future_import_statement",
		"export_statement":
		return false
	}
	return strings.HasSuffix(nodeType, "_statement")
}

// collectLogicalLines walks the whole tree once and records the lines on
// which a statement starts.
func (v *Visitor) collectLogicalLines(node *sitter.Node) {
	isLogical := func(n *sitter.Node) bool { return IsDefaultLogicalNode(n.Type()) }
	if la, ok := v.ad.(interface{ IsLogicalNode(*sitter.Node) bool }); ok {
		isLogical = la.IsLogicalNode
	}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if isLogical(n) {
			v.logicalLines[int(n.StartPoint().Row)+1] = true
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(node)
}

// countLogicalLines returns the number of logical lines within the 1-based
// inclusive line range.
func (v *Visitor) countLogicalLines(start, end int) int {
	cnt := 0
	for line := range v.logicalLines {
		if line >= start && line <= end {
			cnt++
		}
	}
	return cnt
}

// locationOf converts a tree-sitter node position into a 1-based file
// location. Downstream consumers (rules, review, SARIF) rely on it to anchor
// findings to the exact line.
func locationOf(node *sitter.Node) *pb.StmtLocationInFile {
	if node == nil {
		return nil
	}
	return &pb.StmtLocationInFile{
		StartLine:    int32(node.StartPoint().Row) + 1,
		EndLine:      int32(node.EndPoint().Row) + 1,
		StartFilePos: int32(node.StartByte()),
		EndFilePos:   int32(node.EndByte()),
	}
}

func (v *Visitor) curStmts() *pb.Stmts {
	if f := v.curFunc(); f != nil {
		return f.Stmts
	}
	if c := v.curClass(); c != nil {
		return c.Stmts
	}
	return v.file.Stmts
}

func NewVisitor(ad LangAdapter, path string, src []byte) *Visitor {
	lines := strings.Split(string(src), "\n")
	mod := ad.ModuleNameFromPath(filepath.Base(path))

	return &Visitor{
		ad:    ad,
		file:  &pb.File{Path: path, ProgrammingLanguage: "", Stmts: engine.FactoryStmts(), LinesOfCode: &pb.LinesOfCode{LinesOfCode: int32(len(lines))}},
		ns:    &pb.StmtNamespace{Name: &pb.Name{Short: mod, Qualified: mod}, Stmts: engine.FactoryStmts(), LinesOfCode: &pb.LinesOfCode{}},
		lines: lines,
	}
}

// commentMarkers returns the comment tokens declared by the adapter, or every
// marker when the adapter does not declare any.
func (v *Visitor) commentMarkers() engine.CommentMarkers {
	if cm, ok := v.ad.(interface{ CommentMarkers() engine.CommentMarkers }); ok {
		return cm.CommentMarkers()
	}
	return engine.AllCommentMarkers()
}

func (v *Visitor) Result() *pb.File {
	// methods declared outside of their class (Go receivers) are attached now
	// that every class of the file is known
	v.bindReceiverMethods()

	if len(v.file.Stmts.StmtNamespace) == 0 {
		v.file.Stmts.StmtNamespace = append(v.file.Stmts.StmtNamespace, v.ns)
	}

	// allow adapter to provide a better comment count
	if cc, ok := v.ad.(interface{ CountComments([]string, int, int) int }); ok {
		newC := int32(cc.CountComments(v.lines, 1, len(v.lines)))
		v.file.LinesOfCode.CommentLinesOfCode = newC
		v.file.LinesOfCode.NonCommentLinesOfCode = v.file.LinesOfCode.LinesOfCode - newC
	}

	// LLOC counts the lines on which a statement starts
	v.file.LinesOfCode.LogicalLinesOfCode = int32(len(v.logicalLines))

	return v.file
}

func (v *Visitor) pushClass(c *pb.StmtClass) {
	v.classStk = append(v.classStk, c)
}

func (v *Visitor) popClass() {
	v.classStk = v.classStk[:len(v.classStk)-1]
}

func (v *Visitor) curClass() *pb.StmtClass {
	if len(v.classStk) == 0 {
		return nil
	}
	return v.classStk[len(v.classStk)-1]
}

func (v *Visitor) pushFunc(f *pb.StmtFunction) {
	v.funcStk = append(v.funcStk, f)
}

func (v *Visitor) popFunc() {
	v.funcStk = v.funcStk[:len(v.funcStk)-1]
}

func (v *Visitor) curFunc() *pb.StmtFunction {
	if len(v.funcStk) == 0 {
		return nil
	}
	return v.funcStk[len(v.funcStk)-1]
}

func (v *Visitor) attachClass(c *pb.StmtClass) {
	v.ns.Stmts.StmtClass = append(v.ns.Stmts.StmtClass, c)
	if f := v.curFunc(); f != nil {
		f.Stmts.StmtClass = append(f.Stmts.StmtClass, c)
		return
	}
	if pc := v.curClass(); pc != nil {
		pc.Stmts.StmtClass = append(pc.Stmts.StmtClass, c)
		return
	}
	v.file.Stmts.StmtClass = append(v.file.Stmts.StmtClass, c)
}

func (v *Visitor) attachFunction(fn *pb.StmtFunction) {
	v.ns.Stmts.StmtFunction = append(v.ns.Stmts.StmtFunction, fn)
	if f := v.curFunc(); f != nil {
		f.Stmts.StmtFunction = append(f.Stmts.StmtFunction, fn)
		return
	}
	if pc := v.curClass(); pc != nil {
		pc.Stmts.StmtFunction = append(pc.Stmts.StmtFunction, fn)
		return
	}
	v.file.Stmts.StmtFunction = append(v.file.Stmts.StmtFunction, fn)
}

// Optional interface support
// An adapter can implement this to let Visitor create StmtInterface nodes.
type InterfaceAware interface {
	IsInterface(*sitter.Node) bool
}

// ReceiverAware lets an adapter tell that a function node is a method bound to a
// type declared elsewhere in the file. Go declares its methods at the top level,
// outside of the struct they belong to: without this, a struct would hold no
// method at all and every class-level metric (cohesion, number of methods)
// would ignore them.
type ReceiverAware interface {
	// ReceiverTypeName returns the short name of the type the method is bound
	// to, or an empty string when the node is a plain function.
	ReceiverTypeName(*sitter.Node) string
}

// bindReceiverMethods moves the methods declared with a receiver into the class
// of that receiver. The method is moved and not copied, so that it stays
// reachable exactly once from the file.
func (v *Visitor) bindReceiverMethods() {
	if len(v.receiverMethods) == 0 {
		return
	}

	classes := map[string]*pb.StmtClass{}
	for _, c := range v.ns.Stmts.StmtClass {
		if c != nil && c.Name != nil {
			classes[c.Name.Short] = c
		}
	}

	for _, rm := range v.receiverMethods {
		class, ok := classes[rm.receiver]
		if !ok {
			// the receiver type is declared in another file of the package: the
			// method stays where it is
			continue
		}
		if class.Stmts == nil {
			class.Stmts = engine.FactoryStmts()
		}
		class.Stmts.StmtFunction = append(class.Stmts.StmtFunction, rm.fn)
		v.file.Stmts.StmtFunction = removeFunction(v.file.Stmts.StmtFunction, rm.fn)
		if rm.fn.Name != nil {
			// qualify with the receiver: two structs of the same file may
			// declare a method with the same name
			rm.fn.Name.Qualified = v.ad.AttachQualified(class.Name.Qualified, rm.fn.Name.Short)
		}
	}
	v.receiverMethods = nil
}

func removeFunction(list []*pb.StmtFunction, fn *pb.StmtFunction) []*pb.StmtFunction {
	for i, item := range list {
		if item == fn {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// namespaceSeparator returns the separator used between a namespace and a
// class name in qualified names. Defaults to "\\" (PHP-style); adapters can
// implement NamespaceSeparator() to override (e.g. "." for Java/C#).
func (v *Visitor) namespaceSeparator() string {
	if s, ok := v.ad.(interface{ NamespaceSeparator() string }); ok {
		return s.NamespaceSeparator()
	}
	return "\\"
}

func (v *Visitor) Visit(node *sitter.Node) {
	// The first call receives the root node: collect logical lines and the
	// file-level decisions (a script can branch outside of any function) for
	// the whole file before descending.
	if v.logicalLines == nil {
		v.logicalLines = map[int]bool{}
		v.collectLogicalLines(node)
		v.collectDecisions(node, v.file.Stmts)
	}

	switch {
	case v.ad.IsModule(node):
		for i := 0; i < int(node.ChildCount()); i++ {
			v.Visit(node.Child(i))
		}
		return
	case func() bool {
		if ia, ok := v.ad.(InterfaceAware); ok {
			return ia.IsInterface(node)
		}
		return false
	}():
		name := v.ad.NodeName(node)
		qualified := name
		if v.ns != nil && v.ns.Name != nil {
			ns := v.ns.Name.Qualified
			if ns != "" {
				qualified = ns + v.namespaceSeparator() + name
			}
		}
		itf := &pb.StmtInterface{
			Name:     &pb.Name{Short: name, Qualified: qualified},
			Stmts:    engine.FactoryStmts(),
			Location: locationOf(node),
		}
		body := v.ad.NodeBody(node)
		// attach to namespace and file
		v.ns.Stmts.StmtInterface = append(v.ns.Stmts.StmtInterface, itf)
		v.file.Stmts.StmtInterface = append(v.file.Stmts.StmtInterface, itf)
		// visit body
		v.ad.EachChildBody(body, func(ch *sitter.Node) { v.Visit(ch) })
		return

	case v.ad.IsClass(node):
		name := v.ad.NodeName(node)
		qualified := name
		// qualify with namespace if provided (PHP namespaces, even single segment)
		if v.ns != nil && v.ns.Name != nil {
			ns := v.ns.Name.Qualified
			if ns != "" {
				qualified = ns + v.namespaceSeparator() + name
			}
		}
		c := &pb.StmtClass{
			Name:        &pb.Name{Short: name, Qualified: qualified},
			Stmts:       engine.FactoryStmts(),
			LinesOfCode: &pb.LinesOfCode{},
			Location:    locationOf(node),
		}
		body := v.ad.NodeBody(node)
		start := int(node.StartPoint().Row) + 1
		end := start
		if body != nil {
			// For class LOC, count from the class declaration line up to the closing brace line inclusively.
			// body.EndPoint().Row points at the '}' line; do not add +1 here to avoid counting the next line.
			end = max(start, int(body.EndPoint().Row))
		}
		c.LinesOfCode = engine.GetLocPositionFromSourceWithMarkers(v.lines, start, end, v.commentMarkers())
		// If adapter can count comments precisely (e.g., PHP docblocks), override class CLOC using adapter for class span
		if cc, ok := v.ad.(interface{ CountComments([]string, int, int) int }); ok {
			newC := int32(cc.CountComments(v.lines, start, end))
			c.LinesOfCode.CommentLinesOfCode = newC
		}
		c.LinesOfCode.LogicalLinesOfCode = int32(v.countLogicalLines(start, end))

		// Pre-initialize class-level CLOC from class body to preserve expected semantics in tests
		if c.Stmts == nil {
			c.Stmts = engine.FactoryStmts()
		}
		if c.Stmts.Analyze == nil {
			c.Stmts.Analyze = &pb.Analyze{}
		}
		if c.Stmts.Analyze.Volume == nil {
			c.Stmts.Analyze.Volume = &pb.Volume{}
		}
		cl := c.LinesOfCode.CommentLinesOfCode
		c.Stmts.Analyze.Volume.Cloc = &cl

		v.attachClass(c)
		// Attach any class-level externals provided by adapter
		if items := v.ad.Imports(node); len(items) > 0 {
			for _, it := range items {
				name := it.Name // leave empty for plain module imports (Python expectation)
				from := ""
				if f := v.curFunc(); f != nil && f.Name != nil {
					from = f.Name.Qualified
					if from == "" {
						from = f.Name.Short
					}
				} else if c != nil && c.Name != nil {
					from = c.Name.Qualified
					if from == "" {
						from = c.Name.Short
					}
				} else if v.ns != nil && v.ns.Name != nil {
					from = v.ns.Name.Qualified
					if from == "" {
						from = v.ns.Name.Short
					}
				}
				dep := &pb.StmtExternalDependency{ClassName: name, Namespace: it.Module, From: from}
				c.Stmts.StmtExternalDependencies = append(c.Stmts.StmtExternalDependencies, dep)
			}
		}
		// If adapter can list direct class operands (e.g., PHP properties), attach them
		if va, ok := v.ad.(interface{ ClassDirectOperands(*sitter.Node) []string }); ok {
			for _, p := range va.ClassDirectOperands(node) {
				c.Operands = append(c.Operands, &pb.StmtOperand{Name: p})
			}
		}

		// decisions written directly in the class body (field initializers,
		// static blocks) belong to the class, not to any of its methods
		v.collectDecisions(node, c.Stmts)

		v.pushClass(c)
		v.ad.EachChildBody(body, func(ch *sitter.Node) { v.Visit(ch) })
		v.popClass()
		return

	case v.ad.IsFunction(node):
		name := v.ad.NodeName(node)
		qualified := name
		if cls := v.curClass(); cls != nil {
			qualified = v.ad.AttachQualified(cls.Name.Qualified, name)
		}

		fn := &pb.StmtFunction{
			Name:        &pb.Name{Short: name, Qualified: qualified},
			Stmts:       engine.FactoryStmts(),
			LinesOfCode: &pb.LinesOfCode{},
			Location:    locationOf(node),
		}
		if params := v.ad.NodeParams(node); params != nil {
			v.ad.EachParamIdent(params, func(id string) {
				fn.Parameters = append(fn.Parameters, &pb.StmtParameter{Name: id})
			})
		}
		body := v.ad.NodeBody(node)
		nodeStart := int(node.StartPoint().Row) + 1
		nodeEnd := int(node.EndPoint().Row) + 1
		locStart := nodeStart
		locEnd := nodeEnd
		if body != nil {
			locStart = int(body.StartPoint().Row) + 1
			locEnd = int(body.EndPoint().Row) + 1
		}
		fn.LinesOfCode = engine.GetLocPositionFromSourceWithMarkers(v.lines, locStart, locEnd, v.commentMarkers())

		// allow adapter to provide a better comment count
		if cc, ok := v.ad.(interface{ CountComments([]string, int, int) int }); ok {
			cs := int(node.StartPoint().Row) + 1
			ce := int(node.EndPoint().Row) + 1
			newC := int32(cc.CountComments(v.lines, cs, ce))
			fn.LinesOfCode.CommentLinesOfCode = newC
		}
		fn.LinesOfCode.LogicalLinesOfCode = int32(v.countLogicalLines(nodeStart, nodeEnd))

		v.attachFunction(fn)
		if ra, ok := v.ad.(ReceiverAware); ok && v.curClass() == nil {
			if receiver := ra.ReceiverTypeName(node); receiver != "" {
				v.receiverMethods = append(v.receiverMethods, receiverMethod{fn: fn, receiver: receiver})
			}
		}
		// the whole declaration is scanned, not only the body: a default
		// argument value can hold a ternary or a boolean operator
		v.collectDecisions(node, fn.Stmts)

		v.pushFunc(fn)
		v.ad.EachChildBody(body, func(ch *sitter.Node) { v.Visit(ch) })
		// optional: extract operators/operands from source per adapter
		if va, ok := v.ad.(interface {
			ExtractOperatorsOperands(src []byte, startLine, endLine int) (ops []string, operands []string)
		}); ok {
			ops, opr := va.ExtractOperatorsOperands([]byte(strings.Join(v.lines, "\n")), nodeStart, nodeEnd)
			for _, o := range ops {
				fn.Operators = append(fn.Operators, &pb.StmtOperator{Name: o})
			}
			for _, p := range opr {
				fn.Operands = append(fn.Operands, &pb.StmtOperand{Name: p})
			}
		}
		// optional: extract method calls (e.g., this.foo, parent.bar) per adapter
		if mc, ok := v.ad.(interface {
			ExtractMethodCalls(src []byte, startLine, endLine int) []string
		}); ok {
			calls := mc.ExtractMethodCalls([]byte(strings.Join(v.lines, "\n")), nodeStart, nodeEnd)
			for _, m := range calls {
				fn.MethodCalls = append(fn.MethodCalls, &pb.StmtMethodCall{Name: m})
			}
		}
		v.popFunc()
		return
	}

	// Imports and externals
	if items := v.ad.Imports(node); len(items) > 0 {
		st := v.curStmts()
		for _, it := range items {
			name := it.Name // keep empty for plain imports
			from := ""
			if f := v.curFunc(); f != nil && f.Name != nil {
				from = f.Name.Qualified
				if from == "" {
					from = f.Name.Short
				}
			} else if c := v.curClass(); c != nil && c.Name != nil {
				from = c.Name.Qualified
				if from == "" {
					from = c.Name.Short
				}
			} else if v.ns != nil && v.ns.Name != nil {
				from = v.ns.Name.Qualified
				if from == "" {
					from = v.ns.Name.Short
				}
			}
			dep := &pb.StmtExternalDependency{
				ClassName:    name,
				FunctionName: "",
				Namespace:    it.Module,
				From:         from,
			}
			// attach to class scope when inside a class to satisfy PHP tests
			if c := v.curClass(); c != nil {
				c.Stmts.StmtExternalDependencies = append(c.Stmts.StmtExternalDependencies, dep)
			}
			st.StmtExternalDependencies = append(st.StmtExternalDependencies, dep)
			v.ns.Stmts.StmtExternalDependencies = append(v.ns.Stmts.StmtExternalDependencies, dep)
		}
	}

	// Fallback
	for i := 0; i < int(node.ChildCount()); i++ {
		v.Visit(node.Child(i))
	}
}

// isScopeBoundary reports whether a node opens a scope that owns its own
// decisions. collectDecisions stops there, so a method never inflates the
// class that declares it and a closure never inflates its enclosing function.
func (v *Visitor) isScopeBoundary(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if v.ad.IsClass(n) || v.ad.IsFunction(n) {
		return true
	}
	if ia, ok := v.ad.(InterfaceAware); ok && ia.IsInterface(n) {
		return true
	}
	return false
}

// collectDecisions records, on target, every decision point of the subtree
// rooted at scope, excluding the scopes nested inside it.
//
// It walks the whole subtree rather than following the structural traversal:
// a decision can hide anywhere, including in the condition of another decision
// (`if a && b`), in a case branch or in a default argument. Nodes are recorded
// flat, each with its location, because what a metric needs is how many
// branches a scope has and where they are, not how they nest.
func (v *Visitor) collectDecisions(scope *sitter.Node, target *pb.Stmts) {
	if scope == nil || target == nil {
		return
	}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		v.recordDecision(n, target)
		for i := 0; i < int(n.ChildCount()); i++ {
			if ch := n.Child(i); !v.isScopeBoundary(ch) {
				walk(ch)
			}
		}
	}
	// the scope node itself is not a decision: start at its children
	for i := 0; i < int(scope.ChildCount()); i++ {
		if ch := scope.Child(i); !v.isScopeBoundary(ch) {
			walk(ch)
		}
	}
}

// decisionLocation returns the source range a decision covers.
//
// For an if, the else branch is cut out: `if a {} else if b {}` is a chain of
// sibling branches, not two nested ones, and a metric reading these ranges as
// nesting must not see a depth of two there.
func (v *Visitor) decisionLocation(n *sitter.Node, kind DecisionKind) *pb.StmtLocationInFile {
	loc := locationOf(n)
	if loc == nil || kind != DecIf {
		return loc
	}
	// grammars without an else node (Go, Java, C#) expose it as a field
	alt := n.ChildByFieldName("alternative")
	if alt == nil {
		// the others (PHP, TypeScript, Python, Rust) make it a child clause
		for i := 0; i < int(n.ChildCount()) && alt == nil; i++ {
			switch v.ad.Decision(n.Child(i)) {
			case DecElse, DecElif:
				alt = n.Child(i)
			}
		}
	}
	if alt == nil {
		return loc
	}
	loc.EndLine = int32(alt.StartPoint().Row) + 1
	loc.EndFilePos = int32(alt.StartByte())
	return loc
}

func (v *Visitor) recordDecision(n *sitter.Node, target *pb.Stmts) {
	kind := v.ad.Decision(n)
	loc := v.decisionLocation(n, kind)
	switch kind {
	case DecIf:
		target.StmtDecisionIf = append(target.StmtDecisionIf,
			&pb.StmtDecisionIf{Stmts: engine.FactoryStmts(), Location: loc})
	case DecElif:
		target.StmtDecisionElseIf = append(target.StmtDecisionElseIf,
			&pb.StmtDecisionElseIf{Stmts: engine.FactoryStmts(), Location: loc})
	case DecElse:
		target.StmtDecisionElse = append(target.StmtDecisionElse,
			&pb.StmtDecisionElse{Stmts: engine.FactoryStmts(), Location: loc})
	case DecLoop:
		target.StmtLoop = append(target.StmtLoop,
			&pb.StmtLoop{Stmts: engine.FactoryStmts(), Location: loc})
	case DecSwitch:
		target.StmtDecisionSwitch = append(target.StmtDecisionSwitch,
			&pb.StmtDecisionSwitch{Stmts: engine.FactoryStmts(), Location: loc})
	case DecCase:
		target.StmtDecisionCase = append(target.StmtDecisionCase,
			&pb.StmtDecisionCase{Stmts: engine.FactoryStmts(), Location: loc})
	case DecCatch:
		target.StmtDecisionCatch = append(target.StmtDecisionCatch,
			&pb.StmtDecisionCatch{Stmts: engine.FactoryStmts(), Location: loc})
	case DecTernary:
		target.StmtDecisionTernary = append(target.StmtDecisionTernary,
			&pb.StmtDecisionTernary{Stmts: engine.FactoryStmts(), Location: loc})
	case DecLogical:
		op := ""
		if lo, ok := v.ad.(interface {
			LogicalOperator(*sitter.Node) string
		}); ok {
			op = lo.LogicalOperator(n)
		}
		target.StmtDecisionLogical = append(target.StmtDecisionLogical,
			&pb.StmtDecisionLogical{Stmts: engine.FactoryStmts(), Location: loc, Operator: op})
	case DecDefault:
		// the fallback branch of a switch opens no new path
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
