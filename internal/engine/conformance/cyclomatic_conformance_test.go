// Package conformance checks that the cyclomatic complexity model is the same
// in every supported language.
//
// Each scenario is one construct, written once per language that has it, with
// a single expected number. A language that drifts (a grammar node not mapped,
// a default counted as a branch, a case body left unvisited) fails here rather
// than silently reporting a lower complexity than every other tool.
//
// The expected values follow the extended McCabe measure documented in
// internal/engine/treesitter/decision.go, and were cross-checked against
// lizard, gocyclo and phploc.
package conformance

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

const (
	langGo     = "go"
	langPHP    = "php"
	langPython = "python"
	langTS     = "typescript"
	langJava   = "java"
	langCSharp = "csharp"
	langRust   = "rust"
)

func runnerFor(lang string) engine.Engine {
	switch lang {
	case langGo:
		return &golang.GolangRunner{}
	case langPHP:
		return &php.PhpRunner{}
	case langPython:
		return &python.PythonRunner{}
	case langTS:
		return &typescript.TypeScriptRunner{}
	case langJava:
		return &java.JavaRunner{}
	case langCSharp:
		return &csharp.CSharpRunner{}
	case langRust:
		return &rust.RustRunner{}
	}
	return nil
}

// complexityOfF parses src and returns the cyclomatic complexity of the
// function named "f".
func complexityOfF(t *testing.T, lang, src string) int32 {
	t.Helper()
	file, err := engine.CreateTestFileWithCode(runnerFor(lang), src)
	if err != nil {
		t.Fatalf("%s: parse error: %v", lang, err)
	}
	analyzer.AnalyzeFile(file)

	fn := findFunction(file.Stmts, "f", map[*pb.Stmts]bool{})
	if fn == nil {
		t.Fatalf("%s: function f not found in the parsed tree", lang)
	}
	if fn.Stmts == nil || fn.Stmts.Analyze == nil || fn.Stmts.Analyze.Complexity == nil ||
		fn.Stmts.Analyze.Complexity.Cyclomatic == nil {
		t.Fatalf("%s: no complexity computed on f", lang)
	}
	return *fn.Stmts.Analyze.Complexity.Cyclomatic
}

func findFunction(stmts *pb.Stmts, name string, seen map[*pb.Stmts]bool) *pb.StmtFunction {
	if stmts == nil || seen[stmts] {
		return nil
	}
	seen[stmts] = true
	for _, fn := range stmts.StmtFunction {
		if fn != nil && fn.Name != nil && fn.Name.Short == name {
			return fn
		}
	}
	for _, c := range stmts.StmtClass {
		if got := findFunction(c.GetStmts(), name, seen); got != nil {
			return got
		}
	}
	for _, ns := range stmts.StmtNamespace {
		if got := findFunction(ns.GetStmts(), name, seen); got != nil {
			return got
		}
	}
	return nil
}

type scenario struct {
	name string
	// ccn is the complexity every implementation below must report.
	ccn int32
	// why explains where the number comes from, so a failure is readable.
	why string
	// impl holds one implementation per language that has the construct. A
	// language absent from the map does not have it (Go has no ternary, Rust
	// has no exception).
	impl map[string]string
}

var scenarios = []scenario{
	{
		name: "baseline",
		ccn:  1,
		why:  "a function with no branch",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int { return a }\n",
			langPHP:    "<?php\nfunction f($a) { return $a; }\n",
			langPython: "def f(a):\n    return a\n",
			langTS:     "function f(a: number): number { return a }\n",
			langJava:   "class C { int f(int a) { return a; } }\n",
			langCSharp: "class C { int f(int a) { return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { a }\n",
		},
	},
	{
		name: "if_else",
		ccn:  2,
		why:  "1 + if; the else is the other outcome of the same decision",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int { if a > 0 { return 1 } else { return 2 } }\n",
			langPHP:    "<?php\nfunction f($a) { if ($a > 0) { return 1; } else { return 2; } }\n",
			langPython: "def f(a):\n    if a > 0:\n        return 1\n    else:\n        return 2\n",
			langTS:     "function f(a: number): number { if (a > 0) { return 1 } else { return 2 } }\n",
			langJava:   "class C { int f(int a) { if (a > 0) { return 1; } else { return 2; } } }\n",
			langCSharp: "class C { int f(int a) { if (a > 0) { return 1; } else { return 2; } } }\n",
			langRust:   "fn f(a: i32) -> i32 { if a > 0 { 1 } else { 2 } }\n",
		},
	},
	{
		name: "if_elseif_else",
		ccn:  3,
		why:  "1 + if + else-if; the trailing else is free",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int { if a > 0 { return 1 } else if a < 0 { return 2 } else { return 3 } }\n",
			langPHP:    "<?php\nfunction f($a) { if ($a > 0) { return 1; } elseif ($a < 0) { return 2; } else { return 3; } }\n",
			langPython: "def f(a):\n    if a > 0:\n        return 1\n    elif a < 0:\n        return 2\n    else:\n        return 3\n",
			langTS:     "function f(a: number): number { if (a > 0) { return 1 } else if (a < 0) { return 2 } else { return 3 } }\n",
			langJava:   "class C { int f(int a) { if (a > 0) { return 1; } else if (a < 0) { return 2; } else { return 3; } } }\n",
			langCSharp: "class C { int f(int a) { if (a > 0) { return 1; } else if (a < 0) { return 2; } else { return 3; } } }\n",
			langRust:   "fn f(a: i32) -> i32 { if a > 0 { 1 } else if a < 0 { 2 } else { 3 } }\n",
		},
	},
	{
		name: "while_loop",
		ccn:  2,
		why:  "1 + loop",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int { for a < 10 { a++ }; return a }\n",
			langPHP:    "<?php\nfunction f($a) { while ($a < 10) { $a++; } return $a; }\n",
			langPython: "def f(a):\n    while a < 10:\n        a += 1\n    return a\n",
			langTS:     "function f(a: number): number { while (a < 10) { a++ } return a }\n",
			langJava:   "class C { int f(int a) { while (a < 10) { a++; } return a; } }\n",
			langCSharp: "class C { int f(int a) { while (a < 10) { a++; } return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { let mut a = a; while a < 10 { a += 1 } a }\n",
		},
	},
	{
		name: "foreach_loop",
		ccn:  2,
		why:  "1 + loop; iterating a collection is a loop like any other",
		impl: map[string]string{
			langGo:     "package p\nfunc f(xs []int) int { t := 0; for _, x := range xs { t += x }; return t }\n",
			langPHP:    "<?php\nfunction f($xs) { $t = 0; foreach ($xs as $x) { $t += $x; } return $t; }\n",
			langPython: "def f(xs):\n    t = 0\n    for x in xs:\n        t += x\n    return t\n",
			langTS:     "function f(xs: number[]): number { let t = 0; for (const x of xs) { t += x } return t }\n",
			langJava:   "class C { int f(int[] xs) { int t = 0; for (int x : xs) { t += x; } return t; } }\n",
			langCSharp: "class C { int f(int[] xs) { int t = 0; foreach (var x in xs) { t += x; } return t; } }\n",
			langRust:   "fn f(xs: Vec<i32>) -> i32 { let mut t = 0; for x in xs { t += x } t }\n",
		},
	},
	{
		name: "switch_two_cases_and_default",
		ccn:  3,
		why:  "1 + 2 cases; the switch only holds them and the default is the fallback path",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int { switch a { case 1: return 1; case 2: return 2; default: return 0 } }\n",
			langPHP:    "<?php\nfunction f($a) { switch ($a) { case 1: return 1; case 2: return 2; default: return 0; } }\n",
			langPython: "def f(a):\n    match a:\n        case 1:\n            return 1\n        case 2:\n            return 2\n        case _:\n            return 0\n",
			langTS:     "function f(a: number): number { switch (a) { case 1: return 1; case 2: return 2; default: return 0 } }\n",
			langJava:   "class C { int f(int a) { switch (a) { case 1: return 1; case 2: return 2; default: return 0; } } }\n",
			langCSharp: "class C { int f(int a) { switch (a) { case 1: return 1; case 2: return 2; default: return 0; } } }\n",
			langRust:   "fn f(a: i32) -> i32 { match a { 1 => 1, 2 => 2, _ => 0 } }\n",
		},
	},
	{
		// Regression: the branches of a switch used to be a blind spot. Go never
		// saw its cases at all, and PHP saw the labels but not their bodies, so
		// everything written inside a case was invisible to every metric.
		name: "decisions_inside_case_bodies",
		ccn:  6,
		why:  "1 + 2 cases + 2 ifs + 1 loop nested inside the branches",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tswitch a {\n\tcase 1:\n\t\tif a > 0 {\n\t\t\tfor i := 0; i < 3; i++ { a++ }\n\t\t}\n\tcase 2:\n\t\tif a < 0 { a-- }\n\t}\n\treturn a\n}\n",
			langPHP:    "<?php\nfunction f($a) {\n    switch ($a) {\n        case 1:\n            if ($a > 0) {\n                for ($i = 0; $i < 3; $i++) { $a++; }\n            }\n            break;\n        case 2:\n            if ($a < 0) { $a--; }\n            break;\n    }\n    return $a;\n}\n",
			langPython: "def f(a):\n    match a:\n        case 1:\n            if a > 0:\n                while a < 3:\n                    a += 1\n        case 2:\n            if a < 0:\n                a -= 1\n    return a\n",
			langTS:     "function f(a: number): number {\n\tswitch (a) {\n\t\tcase 1:\n\t\t\tif (a > 0) {\n\t\t\t\tfor (let i = 0; i < 3; i++) { a++ }\n\t\t\t}\n\t\t\tbreak\n\t\tcase 2:\n\t\t\tif (a < 0) { a-- }\n\t\t\tbreak\n\t}\n\treturn a\n}\n",
			langJava:   "class C {\n\tint f(int a) {\n\t\tswitch (a) {\n\t\t\tcase 1:\n\t\t\t\tif (a > 0) {\n\t\t\t\t\tfor (int i = 0; i < 3; i++) { a++; }\n\t\t\t\t}\n\t\t\t\tbreak;\n\t\t\tcase 2:\n\t\t\t\tif (a < 0) { a--; }\n\t\t\t\tbreak;\n\t\t}\n\t\treturn a;\n\t}\n}\n",
			langCSharp: "class C {\n\tint f(int a) {\n\t\tswitch (a) {\n\t\t\tcase 1:\n\t\t\t\tif (a > 0) {\n\t\t\t\t\tfor (int i = 0; i < 3; i++) { a++; }\n\t\t\t\t}\n\t\t\t\tbreak;\n\t\t\tcase 2:\n\t\t\t\tif (a < 0) { a--; }\n\t\t\t\tbreak;\n\t\t}\n\t\treturn a;\n\t}\n}\n",
			langRust:   "fn f(a: i32) -> i32 {\n\tlet mut a = a;\n\tmatch a {\n\t\t1 => { if a > 0 { for _i in 0..3 { a += 1 } } }\n\t\t2 => { if a < 0 { a -= 1 } }\n\t\t_ => {}\n\t}\n\ta\n}\n",
		},
	},
	{
		// Regression: a decision written in the condition of another decision
		// used to be unreachable, because only the body of a decision was
		// visited.
		name: "logical_operators_in_condition",
		ccn:  4,
		why:  "1 + if + the two short-circuit operators of its condition",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int, b int) int { if a > 0 && b > 0 || a < 0 { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b) { if ($a > 0 && $b > 0 || $a < 0) { return 1; } return 0; }\n",
			langPython: "def f(a, b):\n    if a > 0 and b > 0 or a < 0:\n        return 1\n    return 0\n",
			langTS:     "function f(a: number, b: number): number { if (a > 0 && b > 0 || a < 0) { return 1 } return 0 }\n",
			langJava:   "class C { int f(int a, int b) { if (a > 0 && b > 0 || a < 0) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(int a, int b) { if (a > 0 && b > 0 || a < 0) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: i32, b: i32) -> i32 { if a > 0 && b > 0 || a < 0 { return 1 } 0 }\n",
		},
	},
	{
		name: "ternary",
		ccn:  2,
		why:  "1 + the conditional expression; Go and Rust have none",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f($a) { return $a > 0 ? 1 : 2; }\n",
			langPython: "def f(a):\n    return 1 if a > 0 else 2\n",
			langTS:     "function f(a: number): number { return a > 0 ? 1 : 2 }\n",
			langJava:   "class C { int f(int a) { return a > 0 ? 1 : 2; } }\n",
			langCSharp: "class C { int f(int a) { return a > 0 ? 1 : 2; } }\n",
		},
	},
	{
		name: "two_exception_handlers",
		ccn:  3,
		why:  "1 + one branch per handler; Go and Rust have no exception",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f($a) { try { g(); } catch (A $e) { return 1; } catch (B $e) { return 2; } return 0; }\n",
			langPython: "def f(a):\n    try:\n        g()\n    except ValueError:\n        return 1\n    except KeyError:\n        return 2\n    return 0\n",
			// TypeScript allows a single catch per try, so two are needed
			langTS:     "function f(a: number): number { try { g() } catch (e) { return 1 } try { h() } catch (e) { return 2 } return 0 }\n",
			langJava:   "class C { int f(int a) { try { g(); } catch (RuntimeException e) { return 1; } catch (Error e) { return 2; } return 0; } }\n",
			langCSharp: "class C { int f(int a) { try { G(); } catch (System.Exception e) { return 1; } catch { return 2; } return 0; } }\n",
		},
	},
	{
		// Returning early on failure is a branch, whether the language spells
		// it out or hides it in an operator. Rust `?` returns from the function
		// exactly like the `if err != nil` it replaces in Go, and must cost the
		// same, or idiomatic Rust would look simpler than the same code
		// written anywhere else.
		name: "early_return_on_error",
		ccn:  3,
		why:  "1 + one branch per early return on failure",
		impl: map[string]string{
			langRust: "fn f(a: i32) -> Option<i32> {\n\tlet x = g(a)?;\n\tlet y = h(x)?;\n\tSome(y)\n}\n",
			langGo:   "package p\nfunc f(a int) (int, error) {\n\tx, err := g(a)\n\tif err != nil { return 0, err }\n\ty, err := h(x)\n\tif err != nil { return 0, err }\n\treturn y, nil\n}\n",
		},
	},
	{
		name: "nested_function_does_not_inflate_its_parent",
		ccn:  2,
		why:  "1 + the if of f; the branches of the nested declaration belong to it",
		impl: map[string]string{
			langGo:     "package p\nfunc g(a int) int { if a > 0 { return 1 }; if a < 0 { return 2 }; return 0 }\nfunc f(a int) int { if a > 0 { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction g($a) { if ($a > 0) { return 1; } if ($a < 0) { return 2; } return 0; }\nfunction f($a) { if ($a > 0) { return 1; } return 0; }\n",
			langPython: "def g(a):\n    if a > 0:\n        return 1\n    if a < 0:\n        return 2\n    return 0\n\ndef f(a):\n    if a > 0:\n        return 1\n    return 0\n",
			langTS:     "function g(a: number): number { if (a > 0) { return 1 } if (a < 0) { return 2 } return 0 }\nfunction f(a: number): number { if (a > 0) { return 1 } return 0 }\n",
			langJava:   "class C { int g(int a) { if (a > 0) { return 1; } if (a < 0) { return 2; } return 0; } int f(int a) { if (a > 0) { return 1; } return 0; } }\n",
			langCSharp: "class C { int g(int a) { if (a > 0) { return 1; } if (a < 0) { return 2; } return 0; } int f(int a) { if (a > 0) { return 1; } return 0; } }\n",
			langRust:   "fn g(a: i32) -> i32 { if a > 0 { return 1 } if a < 0 { return 2 } 0 }\nfn f(a: i32) -> i32 { if a > 0 { return 1 } 0 }\n",
		},
	},
}

func TestCyclomaticComplexityIsTheSameInEveryLanguage(t *testing.T) {
	for _, sc := range scenarios {
		for lang, src := range sc.impl {
			t.Run(sc.name+"/"+lang, func(t *testing.T) {
				if got := complexityOfF(t, lang, src); got != sc.ccn {
					t.Errorf("complexity of f = %d, want %d (%s)", got, sc.ccn, sc.why)
				}
			})
		}
	}
}

// TestEveryLanguageCoversEveryUniversalConstruct guards the scenario table
// itself: a construct that every language has must be exercised everywhere, so
// that adding a language cannot quietly skip half the model.
func TestEveryLanguageCoversEveryUniversalConstruct(t *testing.T) {
	universal := map[string]bool{
		"baseline": true, "if_else": true, "if_elseif_else": true,
		"while_loop": true, "foreach_loop": true,
		"switch_two_cases_and_default":                true,
		"decisions_inside_case_bodies":                true,
		"logical_operators_in_condition":              true,
		"nested_function_does_not_inflate_its_parent": true,
	}
	all := []string{langGo, langPHP, langPython, langTS, langJava, langCSharp, langRust}
	for _, sc := range scenarios {
		if !universal[sc.name] {
			continue
		}
		for _, lang := range all {
			if _, ok := sc.impl[lang]; !ok {
				t.Errorf("scenario %q has no %s implementation", sc.name, lang)
			}
		}
	}
}
