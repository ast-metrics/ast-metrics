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
