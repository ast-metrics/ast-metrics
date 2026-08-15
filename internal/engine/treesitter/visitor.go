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
	// joined is the source rebuilt from lines. The adapters that extract
	// operators, operands or method calls from the source text are called once
	// per scope and all need it, so it is built once: building it per scope
	// would copy the whole file for every function it holds.
	joined []byte

	classStk []*pb.StmtClass
	funcStk  []*pb.StmtFunction

	// receiverMethods holds the methods declared outside of the class they
	// belong to (Go receivers). They are attached to their class once the whole
	// file has been visited, because a method may be declared before its type.
	receiverMethods []receiverMethod

	// logicalLines tells, for each line of the file, whether a statement starts
	// on it. LLOC at every level (file, class, function) is the number of such
	// lines in the scope's range, read from llocTotals.
	logicalLines []bool
	// llocTotals holds the running total of logical lines, so that the count
	// over a scope is a subtraction instead of a walk over the whole file.
	llocTotals []int32
	// lineIndex holds LOC, CLOC and NCLOC per line, scanned once for the file.
	lineIndex *engine.LineIndex
	// collected tells that the file-wide passes have run: they belong to the
	// first Visit call, the one receiving the root node.
	collected bool
}

// receiverMethod is a method waiting to be attached to the class of its
// receiver.
type receiverMethod struct {
	fn       *pb.StmtFunction
	receiver string
}

// collectLogicalLines walks the whole tree once and records the lines on which
// a statement starts, following the model documented in statement.go.
//
// inClass and inFunction track the nearest enclosing scope, which is what
// tells a local declaration from a member one and a real statement from a
// field initializer.
func (v *Visitor) collectLogicalLines(node *sitter.Node) {
	var walk func(n *sitter.Node, inClass, inFunction bool)
	walk = func(n *sitter.Node, inClass, inFunction bool) {
		if n == nil {
			return
		}
		counts := false
		switch v.ad.Statement(n) {
		case IsStatement:
			// a statement written directly in a class body initializes a
			// member; it is not an instruction of the program
			counts = !inClass
		case IsLocalDeclaration:
			counts = inFunction
		}
		if counts {
			if line := int(n.StartPoint().Row); line < len(v.logicalLines) {
				v.logicalLines[line] = true
			}
		}

		childInClass, childInFunction := inClass, inFunction
		switch {
		case v.ad.IsFunction(n):
			childInClass, childInFunction = false, true
		case v.ad.IsClass(n):
			childInClass, childInFunction = true, false
		default:
			if ia, ok := v.ad.(InterfaceAware); ok && ia.IsInterface(n) {
				childInClass, childInFunction = true, false
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), childInClass, childInFunction)
		}
	}
	walk(node, false, false)
}

// countLogicalLines returns the number of logical lines within the 1-based
// inclusive line range.
func (v *Visitor) countLogicalLines(start, end int) int {
	if len(v.llocTotals) == 0 {
		return 0
	}
	if start < 1 {
		start = 1
	}
	if end > len(v.logicalLines) {
		end = len(v.logicalLines)
	}
	if end < start {
		return 0
	}
	return int(v.llocTotals[end] - v.llocTotals[start-1])
}

// indexLogicalLines turns the lines carrying a statement into running totals,
// once the whole tree has been walked.
func (v *Visitor) indexLogicalLines() {
	v.llocTotals = make([]int32, len(v.logicalLines)+1)
	for i, carries := range v.logicalLines {
		total := v.llocTotals[i]
		if carries {
			total++
		}
		v.llocTotals[i+1] = total
	}
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
	lines := engine.SplitSourceLines(src)
	mod := ad.ModuleNameFromPath(filepath.Base(path))

	v := &Visitor{
		ad:     ad,
		file:   &pb.File{Path: path, ProgrammingLanguage: "", Stmts: engine.FactoryStmts(), LinesOfCode: &pb.LinesOfCode{LinesOfCode: int32(len(lines))}},
		ns:     &pb.StmtNamespace{Name: &pb.Name{Short: mod, Qualified: mod}, Stmts: engine.FactoryStmts(), LinesOfCode: &pb.LinesOfCode{}},
		lines:  lines,
		joined: []byte(strings.Join(lines, "\n")),
	}
	v.lineIndex = engine.NewLineIndex(lines, v.commentSyntax())

	return v
}

// commentSyntax returns the comment syntax declared by the adapter, or a
// permissive default when it declares none.
func (v *Visitor) commentSyntax() engine.CommentSyntax {
	if cs, ok := v.ad.(interface{ CommentSyntax() engine.CommentSyntax }); ok {
		return cs.CommentSyntax()
	}
	return engine.DefaultCommentSyntax()
}

// linesOfCodeIn measures the 1-based inclusive line range of a scope: its
// physical size, how many of those lines are comments, how many hold code, and
// how many carry a statement.
func (v *Visitor) linesOfCodeIn(start, end int) *pb.LinesOfCode {
	loc := v.lineIndex.Count(start, end)
	loc.LogicalLinesOfCode = int32(v.countLogicalLines(start, end))
	return loc
}

func (v *Visitor) Result() *pb.File {
	// methods declared outside of their class (Go receivers) are attached now
	// that every class of the file is known
	v.bindReceiverMethods()

	if len(v.file.Stmts.StmtNamespace) == 0 {
		v.file.Stmts.StmtNamespace = append(v.file.Stmts.StmtNamespace, v.ns)
	}

	v.file.LinesOfCode = v.linesOfCodeIn(1, len(v.lines))

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
		addLinesOfCode(class.LinesOfCode, rm.fn.LinesOfCode)
	}
	v.receiverMethods = nil
}

// addLinesOfCode adds the size of a method declared outside of the body of its
// type to the size of that type.
//
// Go and Rust declare methods next to the type rather than inside it, so the
// declaration span of the type covers its fields only. Without this, a struct
// with two hundred lines of methods would report the three lines of its field
// list, and would look like the smallest type of the file while being the
// largest.
func addLinesOfCode(dst, src *pb.LinesOfCode) {
	if dst == nil || src == nil {
		return
	}
	dst.LinesOfCode += src.LinesOfCode
	dst.LogicalLinesOfCode += src.LogicalLinesOfCode
	dst.CommentLinesOfCode += src.CommentLinesOfCode
	dst.NonCommentLinesOfCode += src.NonCommentLinesOfCode
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
	if !v.collected {
		v.collected = true
		v.logicalLines = make([]bool, len(v.lines))
		v.collectLogicalLines(node)
		v.indexLogicalLines()
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
		// A scope is as long as its declaration: from the line the class opens
		// on to the line it closes on, inclusive. Measuring the body instead
		// would drop the signature line whenever the opening brace sits on a
		// line of its own, so the very same class would shrink by one line when
		// reformatted.
		start, end := int(node.StartPoint().Row)+1, int(node.EndPoint().Row)+1
		c.LinesOfCode = v.linesOfCodeIn(start, end)

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
		// as for a class: the declaration span, so that the signature line
		// counts in every language and under every brace style
		nodeStart := int(node.StartPoint().Row) + 1
		nodeEnd := int(node.EndPoint().Row) + 1
		fn.LinesOfCode = v.linesOfCodeIn(nodeStart, nodeEnd)

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
			ops, opr := va.ExtractOperatorsOperands(v.joined, nodeStart, nodeEnd)
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
			calls := mc.ExtractMethodCalls(v.joined, nodeStart, nodeEnd)
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
