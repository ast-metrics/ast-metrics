package conformance

// The cognitive complexity model, documented in
// internal/engine/treesitter/cognitive.go, checked the same way as the
// cyclomatic one: one expected number per construct, in every language that
// has it. Expected values follow SonarSource's specification.

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// cognitiveOfF parses src and returns the cognitive complexity of the
// function named "f".
func cognitiveOfF(t *testing.T, lang, src string) int32 {
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
	cognitive := fn.GetStmts().GetAnalyze().GetComplexity()
	if cognitive == nil || cognitive.Cognitive == nil {
		t.Fatalf("%s: no cognitive complexity computed on f", lang)
	}
	return cognitive.GetCognitive()
}

// cognitiveOfCyclomaticScenarios gives the cognitive complexity of the
// programs written for the cyclomatic scenarios.
var cognitiveOfCyclomaticScenarios = map[string]int32{
	"baseline":                     0,
	"if_else":                      2, // if + else
	"if_elseif_else":               3, // if + else if + else
	"while_loop":                   1,
	"foreach_loop":                 1,
	"switch_two_cases_and_default": 1, // the switch; its cases add nothing
	// switch 1, then at nesting 1: if 2 and if 2, then the loop at nesting 2: 3
	"decisions_inside_case_bodies":                8,
	"logical_operators_in_condition":              3, // if + a run of && + a run of ||
	"ternary":                                     1,
	"two_exception_handlers":                      2, // one per handler
	"nested_function_does_not_inflate_its_parent": 1,
}

// notReused names the cyclomatic scenarios whose programs do not share one
// cognitive complexity, with where they are checked instead.
var notReused = map[string]string{
	"early_return_on_error": "Rust's ? is shorthand: error_check_is_an_if, shorthand_adds_nothing",
}

var cognitiveScenarios = []scenario{
	{
		name: "error_check_is_an_if",
		ccn:  2,
		why:  "one per if",
		impl: map[string]string{
			langGo: "package p\nfunc f(a int) (int, error) {\n\tx, err := g(a)\n\tif err != nil { return 0, err }\n\ty, err := h(x)\n\tif err != nil { return 0, err }\n\treturn y, nil\n}\n",
		},
	},
	{
		name: "shorthand_adds_nothing",
		ccn:  0,
		why:  "Rust's ? and a comprehension's clauses read as one expression",
		impl: map[string]string{
			langRust:   "fn f(a: i32) -> Option<i32> {\n\tlet x = g(h(a)?)?;\n\tSome(x)\n}\n",
			langPython: "def f(xs):\n    return [y for x in xs for y in x if y > 0]\n",
		},
	},
	{
		name: "nesting_adds_its_depth",
		ccn:  6,
		why:  "loop 1, if at nesting 1: 2, if at nesting 2: 3",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tfor a > 0 {\n\t\tif a > 1 {\n\t\t\tif a > 2 { a-- }\n\t\t}\n\t\ta--\n\t}\n\treturn a\n}\n",
			langPHP:    "<?php\nfunction f($a) { while ($a > 0) { if ($a > 1) { if ($a > 2) { $a--; } } $a--; } return $a; }\n",
			langPython: "def f(a):\n    while a > 0:\n        if a > 1:\n            if a > 2:\n                a -= 1\n        a -= 1\n    return a\n",
			langTS:     "function f(a: number): number { while (a > 0) { if (a > 1) { if (a > 2) { a-- } } a-- } return a }\n",
			langJava:   "class C { int f(int a) { while (a > 0) { if (a > 1) { if (a > 2) { a--; } } a--; } return a; } }\n",
			langCSharp: "class C { int f(int a) { while (a > 0) { if (a > 1) { if (a > 2) { a--; } } a--; } return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { let mut a = a; while a > 0 { if a > 1 { if a > 2 { a -= 1 } } a -= 1 } a }\n",
		},
	},
	{
		name: "else_if_stays_at_the_depth_of_its_chain",
		ccn:  5,
		why:  "loop 1, if at nesting 1: 2, else if 1, else 1",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tfor a > 0 {\n\t\tif a > 2 { a -= 2 } else if a > 1 { a-- } else { a = 0 }\n\t}\n\treturn a\n}\n",
			langPHP:    "<?php\nfunction f($a) { while ($a > 0) { if ($a > 2) { $a -= 2; } elseif ($a > 1) { $a--; } else { $a = 0; } } return $a; }\n",
			langPython: "def f(a):\n    while a > 0:\n        if a > 2:\n            a -= 2\n        elif a > 1:\n            a -= 1\n        else:\n            a = 0\n    return a\n",
			langTS:     "function f(a: number): number { while (a > 0) { if (a > 2) { a -= 2 } else if (a > 1) { a-- } else { a = 0 } } return a }\n",
			langJava:   "class C { int f(int a) { while (a > 0) { if (a > 2) { a -= 2; } else if (a > 1) { a--; } else { a = 0; } } return a; } }\n",
			langCSharp: "class C { int f(int a) { while (a > 0) { if (a > 2) { a -= 2; } else if (a > 1) { a--; } else { a = 0; } } return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { let mut a = a; while a > 0 { if a > 2 { a -= 2 } else if a > 1 { a -= 1 } else { a = 0 } } a }\n",
		},
	},
	{
		name: "like_operators_are_one_run",
		ccn:  2,
		why:  "if + one run of &&",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int, b int) int { if a > 0 && b > 0 && a < b { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b) { if ($a > 0 && $b > 0 && $a < $b) { return 1; } return 0; }\n",
			langPython: "def f(a, b):\n    if a > 0 and b > 0 and a < b:\n        return 1\n    return 0\n",
			langTS:     "function f(a: number, b: number): number { if (a > 0 && b > 0 && a < b) { return 1 } return 0 }\n",
			langJava:   "class C { int f(int a, int b) { if (a > 0 && b > 0 && a < b) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(int a, int b) { if (a > 0 && b > 0 && a < b) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: i32, b: i32) -> i32 { if a > 0 && b > 0 && a < b { return 1 } 0 }\n",
		},
	},
	{
		name: "else_block_holding_an_if",
		ccn:  4,
		why:  "if 1, else 1, the if inside the else at nesting 1: 2",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tif a > 2 { a = 1 } else {\n\t\tif a > 1 { a = 2 }\n\t}\n\treturn a\n}\n",
			langPHP:    "<?php\nfunction f($a) { if ($a > 2) { $a = 1; } else { if ($a > 1) { $a = 2; } } return $a; }\n",
			langPython: "def f(a):\n    if a > 2:\n        a = 1\n    else:\n        if a > 1:\n            a = 2\n    return a\n",
			langTS:     "function f(a: number): number { if (a > 2) { a = 1 } else { if (a > 1) { a = 2 } } return a }\n",
			langJava:   "class C { int f(int a) { if (a > 2) { a = 1; } else { if (a > 1) { a = 2; } } return a; } }\n",
			langCSharp: "class C { int f(int a) { if (a > 2) { a = 1; } else { if (a > 1) { a = 2; } } return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { let mut a = a; if a > 2 { a = 1 } else { if a > 1 { a = 2 } } a }\n",
		},
	},
	{
		name: "else_block_holding_statements_and_ifs",
		ccn:  10,
		why:  "loop 1, if at nesting 1: 2, else 1, two ifs at nesting 2: 3 each",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tfor a > 0 {\n\t\tif a > 2 { a = 1 } else {\n\t\t\ta--\n\t\t\tif a > 1 { a = 2 }\n\t\t\tif a > 3 { a = 3 }\n\t\t}\n\t}\n\treturn a\n}\n",
			langPHP:    "<?php\nfunction f($a) { while ($a > 0) { if ($a > 2) { $a = 1; } else { $a--; if ($a > 1) { $a = 2; } if ($a > 3) { $a = 3; } } } return $a; }\n",
			langPython: "def f(a):\n    while a > 0:\n        if a > 2:\n            a = 1\n        else:\n            a -= 1\n            if a > 1:\n                a = 2\n            if a > 3:\n                a = 3\n    return a\n",
			langTS:     "function f(a: number): number { while (a > 0) { if (a > 2) { a = 1 } else { a--; if (a > 1) { a = 2 } if (a > 3) { a = 3 } } } return a }\n",
			langJava:   "class C { int f(int a) { while (a > 0) { if (a > 2) { a = 1; } else { a--; if (a > 1) { a = 2; } if (a > 3) { a = 3; } } } return a; } }\n",
			langCSharp: "class C { int f(int a) { while (a > 0) { if (a > 2) { a = 1; } else { a--; if (a > 1) { a = 2; } if (a > 3) { a = 3; } } } return a; } }\n",
			langRust:   "fn f(a: i32) -> i32 { let mut a = a; while a > 0 { if a > 2 { a = 1 } else { a -= 1; if a > 1 { a = 2 } if a > 3 { a = 3 } } } a }\n",
		},
	},
	{
		name: "ternary_in_an_if_condition",
		ccn:  2,
		why:  "if 1, the ternary in its condition stays at nesting 0: 1",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f($a) { if ($a > 0 ? $a > 1 : $a > 2) { return 1; } return 0; }\n",
			langPython: "def f(a):\n    if a > 1 if a > 0 else a > 2:\n        return 1\n    return 0\n",
			langTS:     "function f(a: number): number { if (a > 0 ? a > 1 : a > 2) { return 1 } return 0 }\n",
			langJava:   "class C { int f(int a) { if (a > 0 ? a > 1 : a > 2) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(int a) { if (a > 0 ? a > 1 : a > 2) { return 1; } return 0; } }\n",
		},
	},
	{
		name: "ternary_in_an_else_if_condition",
		ccn:  4,
		why:  "if 1, else if 1, else 1, the ternary in the condition stays at nesting 0: 1",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f($a) { if ($a > 5) { return 1; } elseif ($a > 0 ? $a > 1 : $a > 2) { return 2; } else { return 3; } }\n",
			langPython: "def f(a):\n    if a > 5:\n        return 1\n    elif a > 1 if a > 0 else a > 2:\n        return 2\n    else:\n        return 3\n",
			langTS:     "function f(a: number): number { if (a > 5) { return 1 } else if (a > 0 ? a > 1 : a > 2) { return 2 } else { return 3 } }\n",
			langJava:   "class C { int f(int a) { if (a > 5) { return 1; } else if (a > 0 ? a > 1 : a > 2) { return 2; } else { return 3; } } }\n",
			langCSharp: "class C { int f(int a) { if (a > 5) { return 1; } else if (a > 0 ? a > 1 : a > 2) { return 2; } else { return 3; } } }\n",
		},
	},
	{
		name: "if_in_the_condition_of_an_if",
		ccn:  3,
		why:  "if 1, inner if at nesting 0: 1, its else 1",
		impl: map[string]string{
			langRust: "fn f(a: i32) -> i32 { if if a > 0 { a > 1 } else { a > 2 } { return 1 } 0 }\n",
		},
	},
	{
		name: "if_in_the_let_condition_of_an_if",
		ccn:  3,
		why:  "if let 1, inner if at nesting 0: 1, its else 1",
		impl: map[string]string{
			langRust: "fn f(a: i32) -> i32 { if let Some(x) = if a > 0 { Some(1) } else { None } { return x } 0 }\n",
		},
	},
	{
		name: "if_in_the_initializer_of_an_if",
		ccn:  2,
		why:  "if 1, the if of the closure in its initializer stays at nesting 0: 1",
		impl: map[string]string{
			langGo: "package p\nfunc f(a int) int {\n\tif x := func() bool { if a > 1 { return true }; return false }(); x { return 1 }\n\treturn 0\n}\n",
		},
	},
	{
		name: "ternary_in_a_loop_condition_is_nested",
		ccn:  3,
		why:  "loop 1, the ternary at nesting 1: 2, only the if keeps its condition out",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f($a) { while ($a > 0 ? $a > 1 : $a > 2) { $a--; } return $a; }\n",
			langPython: "def f(a):\n    while a > 1 if a > 0 else a > 2:\n        a -= 1\n    return a\n",
			langTS:     "function f(a: number): number { while (a > 0 ? a > 1 : a > 2) { a-- } return a }\n",
			langJava:   "class C { int f(int a) { while (a > 0 ? a > 1 : a > 2) { a--; } return a; } }\n",
			langCSharp: "class C { int f(int a) { while (a > 0 ? a > 1 : a > 2) { a--; } return a; } }\n",
		},
	},
	{
		name: "operator_runs_follow_the_source_order",
		ccn:  4,
		why:  "if + the runs of a || b && c || d read left to right: ||, &&, ||",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a, b, c, d bool) int { if a || b && c || d { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b, $c, $d) { if ($a || $b && $c || $d) { return 1; } return 0; }\n",
			langPython: "def f(a, b, c, d):\n    if a or b and c or d:\n        return 1\n    return 0\n",
			langTS:     "function f(a: boolean, b: boolean, c: boolean, d: boolean): number { if (a || b && c || d) { return 1 } return 0 }\n",
			langJava:   "class C { int f(boolean a, boolean b, boolean c, boolean d) { if (a || b && c || d) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(bool a, bool b, bool c, bool d) { if (a || b && c || d) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: bool, b: bool, c: bool, d: bool) -> i32 { if a || b && c || d { return 1 } 0 }\n",
		},
	},
	{
		name: "parentheses_on_the_left_do_not_split_a_run",
		ccn:  2,
		why:  "if + one run of &&",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a, b, c, d bool) int { if (a && b) && c { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b, $c, $d) { if (($a && $b) && $c) { return 1; } return 0; }\n",
			langPython: "def f(a, b, c, d):\n    if (a and b) and c:\n        return 1\n    return 0\n",
			langTS:     "function f(a: boolean, b: boolean, c: boolean, d: boolean): number { if ((a && b) && c) { return 1 } return 0 }\n",
			langJava:   "class C { int f(boolean a, boolean b, boolean c, boolean d) { if ((a && b) && c) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(bool a, bool b, bool c, bool d) { if ((a && b) && c) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: bool, b: bool, c: bool, d: bool) -> i32 { if (a && b) && c { return 1 } 0 }\n",
		},
	},
	{
		name: "parentheses_on_the_right_do_not_split_a_run",
		ccn:  2,
		why:  "if + one run of &&",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a, b, c, d bool) int { if a && (b && c) { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b, $c, $d) { if ($a && ($b && $c)) { return 1; } return 0; }\n",
			langPython: "def f(a, b, c, d):\n    if a and (b and c):\n        return 1\n    return 0\n",
			langTS:     "function f(a: boolean, b: boolean, c: boolean, d: boolean): number { if (a && (b && c)) { return 1 } return 0 }\n",
			langJava:   "class C { int f(boolean a, boolean b, boolean c, boolean d) { if (a && (b && c)) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(bool a, bool b, bool c, bool d) { if (a && (b && c)) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: bool, b: bool, c: bool, d: bool) -> i32 { if a && (b && c) { return 1 } 0 }\n",
		},
	},
	{
		name: "parentheses_keep_different_operators_apart",
		ccn:  3,
		why:  "if + a run of && + a run of ||",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a, b, c, d bool) int { if a && (b || c) { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b, $c, $d) { if ($a && ($b || $c)) { return 1; } return 0; }\n",
			langPython: "def f(a, b, c, d):\n    if a and (b or c):\n        return 1\n    return 0\n",
			langTS:     "function f(a: boolean, b: boolean, c: boolean, d: boolean): number { if (a && (b || c)) { return 1 } return 0 }\n",
			langJava:   "class C { int f(boolean a, boolean b, boolean c, boolean d) { if (a && (b || c)) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(bool a, bool b, bool c, bool d) { if (a && (b || c)) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: bool, b: bool, c: bool, d: bool) -> i32 { if a && (b || c) { return 1 } 0 }\n",
		},
	},
	{
		name: "a_call_argument_starts_its_own_run",
		ccn:  3,
		why:  "if + the run of && outside + the run of && in the argument",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a, b, c, d bool) int { if a && g(b && c) { return 1 }; return 0 }\n",
			langPHP:    "<?php\nfunction f($a, $b, $c, $d) { if ($a && g($b && $c)) { return 1; } return 0; }\n",
			langPython: "def f(a, b, c, d):\n    if a and g(b and c):\n        return 1\n    return 0\n",
			langTS:     "function f(a: boolean, b: boolean, c: boolean, d: boolean): number { if (a && g(b && c)) { return 1 } return 0 }\n",
			langJava:   "class C { int f(boolean a, boolean b, boolean c, boolean d) { if (a && g(b && c)) { return 1; } return 0; } }\n",
			langCSharp: "class C { int f(bool a, bool b, bool c, bool d) { if (a && g(b && c)) { return 1; } return 0; } }\n",
			langRust:   "fn f(a: bool, b: bool, c: bool, d: bool) -> i32 { if a && g(b && c) { return 1 } 0 }\n",
		},
	},
}

func TestCognitiveComplexityIsTheSameInEveryLanguage(t *testing.T) {
	check := func(name string, want int32, why string, impl map[string]string) {
		for lang, src := range impl {
			t.Run(name+"/"+lang, func(t *testing.T) {
				if got := cognitiveOfF(t, lang, src); got != want {
					t.Errorf("cognitive complexity of f = %d, want %d (%s)", got, want, why)
				}
			})
		}
	}
	for _, sc := range scenarios {
		if _, skip := notReused[sc.name]; skip {
			continue
		}
		want, ok := cognitiveOfCyclomaticScenarios[sc.name]
		if !ok {
			t.Errorf("cyclomatic scenario %q has no expected cognitive complexity", sc.name)
			continue
		}
		check(sc.name, want, "the program of the cyclomatic scenario", sc.impl)
	}
	for _, sc := range cognitiveScenarios {
		check(sc.name, sc.ccn, sc.why, sc.impl)
	}
}
