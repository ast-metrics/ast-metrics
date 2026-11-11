package treesitter_test

import (
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/cpp"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Locations anchor SARIF findings (and review annotations) to real lines.
// Every language goes through the shared visitor, so each engine is checked
// against a sample whose class and function start lines are known.
func TestVisitor_PopulatesLocations(t *testing.T) {
	cases := []struct {
		lang      string
		runner    enginePkg.Engine
		code      string
		funcName  string
		funcLine  int32
		className string
		classLine int32
	}{
		{
			lang:   "golang",
			runner: &golang.GolangRunner{},
			code: "package main\n" + // 1
				"\n" + // 2
				"type C struct{}\n" + // 3
				"\n" + // 4
				"func (c C) M(x int) int {\n" + // 5
				"\treturn x\n" + // 6
				"}\n", // 7
			funcName: "M", funcLine: 5,
			className: "C", classLine: 3,
		},
		{
			lang:   "php",
			runner: &php.PhpRunner{},
			code: "<?php\n" + // 1
				"\n" + // 2
				"class Foo\n" + // 3
				"{\n" + // 4
				"    public function bar(): int\n" + // 5
				"    {\n" + // 6
				"        return 1;\n" + // 7
				"    }\n" + // 8
				"}\n", // 9
			funcName: "bar", funcLine: 5,
			className: "Foo", classLine: 3,
		},
		{
			lang:   "python",
			runner: &python.PythonRunner{},
			code: "class Foo:\n" + // 1
				"    def bar(self):\n" + // 2
				"        return 1\n", // 3
			funcName: "bar", funcLine: 2,
			className: "Foo", classLine: 1,
		},
		{
			lang:   "rust",
			runner: &rust.RustRunner{},
			code: "struct Foo {}\n" + // 1
				"\n" + // 2
				"impl Foo {\n" + // 3
				"    fn bar(&self) -> i32 {\n" + // 4
				"        1\n" + // 5
				"    }\n" + // 6
				"}\n", // 7
			funcName: "bar", funcLine: 4,
		},
		{
			lang:   "java",
			runner: &java.JavaRunner{},
			code: "class Foo {\n" + // 1
				"    int bar() {\n" + // 2
				"        return 1;\n" + // 3
				"    }\n" + // 4
				"}\n", // 5
			funcName: "bar", funcLine: 2,
			className: "Foo", classLine: 1,
		},
		{
			lang:   "csharp",
			runner: &csharp.CSharpRunner{},
			code: "class Foo {\n" + // 1
				"    int Bar() {\n" + // 2
				"        return 1;\n" + // 3
				"    }\n" + // 4
				"}\n", // 5
			funcName: "Bar", funcLine: 2,
			className: "Foo", classLine: 1,
		},
		{
			lang:   "cpp",
			runner: &cpp.CppRunner{},
			code: "namespace app {\n" + // 1
				"\n" + // 2
				"class Foo {\n" + // 3
				"    int bar() {\n" + // 4
				"        return 1;\n" + // 5
				"    }\n" + // 6
				"};\n" + // 7
				"}\n", // 8
			funcName: "bar", funcLine: 4,
			className: "Foo", classLine: 3,
		},
		{
			lang:   "typescript",
			runner: &typescript.TypeScriptRunner{},
			code: "class Foo {\n" + // 1
				"    bar(): number {\n" + // 2
				"        return 1;\n" + // 3
				"    }\n" + // 4
				"}\n", // 5
			funcName: "bar", funcLine: 2,
			className: "Foo", classLine: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			file, err := enginePkg.CreateTestFileWithCode(tc.runner, tc.code)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			fn := findFunction(file.Stmts, tc.funcName)
			if fn == nil {
				t.Fatalf("function %q not found", tc.funcName)
			}
			if fn.Location == nil {
				t.Fatalf("function %q has no location", tc.funcName)
			}
			if fn.Location.StartLine != tc.funcLine {
				t.Errorf("function %q: expected start line %d, got %d", tc.funcName, tc.funcLine, fn.Location.StartLine)
			}
			if fn.Location.EndLine < fn.Location.StartLine {
				t.Errorf("function %q: end line %d before start line %d", tc.funcName, fn.Location.EndLine, fn.Location.StartLine)
			}

			if tc.className != "" {
				cls := findClass(file.Stmts, tc.className)
				if cls == nil {
					t.Fatalf("class %q not found", tc.className)
				}
				if cls.Location == nil {
					t.Fatalf("class %q has no location", tc.className)
				}
				if cls.Location.StartLine != tc.classLine {
					t.Errorf("class %q: expected start line %d, got %d", tc.className, tc.classLine, cls.Location.StartLine)
				}
			}
		})
	}
}

func findFunction(stmts *pb.Stmts, name string) *pb.StmtFunction {
	if stmts == nil {
		return nil
	}
	for _, fn := range stmts.StmtFunction {
		if fn != nil && fn.Name != nil && fn.Name.Short == name {
			return fn
		}
	}
	for _, cls := range stmts.StmtClass {
		if cls != nil {
			if fn := findFunction(cls.Stmts, name); fn != nil {
				return fn
			}
		}
	}
	for _, ns := range stmts.StmtNamespace {
		if ns != nil {
			if fn := findFunction(ns.Stmts, name); fn != nil {
				return fn
			}
		}
	}
	return nil
}

func findClass(stmts *pb.Stmts, name string) *pb.StmtClass {
	if stmts == nil {
		return nil
	}
	for _, cls := range stmts.StmtClass {
		if cls != nil && cls.Name != nil && cls.Name.Short == name {
			return cls
		}
	}
	for _, ns := range stmts.StmtNamespace {
		if ns != nil {
			if cls := findClass(ns.Stmts, name); cls != nil {
				return cls
			}
		}
	}
	return nil
}
