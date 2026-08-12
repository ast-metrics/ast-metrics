// Package conformance also checks that the volume metrics mean the same thing
// in every supported language.
//
// Three numbers describe the size of a scope, and they are checked in three
// different ways, because they do not all have the same kind of truth:
//
//   - LLOC counts statements, which is a property of the program, not of its
//     spelling. Equivalent programs must therefore get the same number in every
//     language, exactly as for cyclomatic complexity. That is the scenario table
//     below.
//
//   - LOC and CLOC count physical lines, so they depend on how the language is
//     written: braces add lines, indentation does not. What can be checked is
//     that the numbers agree with the tools everybody compares against. The
//     expected values here were taken from tokei and scc, which agree with each
//     other on all seven fixtures.
//
//   - a scope is as long as its declaration. That is an invariant every language
//     must satisfy, whatever the code inside, and it is what keeps a function
//     from shrinking when the opening brace moves to a line of its own.
package conformance

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

var allLanguages = []string{langGo, langPHP, langPython, langTS, langJava, langCSharp, langRust}

// parse runs the engine and the analyzers over src, as a real run would.
func parse(t *testing.T, lang, src string) *pb.File {
	t.Helper()
	file, err := engine.CreateTestFileWithCode(runnerFor(lang), src)
	if err != nil {
		t.Fatalf("%s: parse error: %v", lang, err)
	}
	analyzer.AnalyzeFile(file)
	return file
}

// linesOfF returns the line counts measured for the function named "f".
func linesOfF(t *testing.T, lang, src string) *pb.LinesOfCode {
	t.Helper()
	file := parse(t, lang, src)
	fn := findFunction(file.Stmts, "f", map[*pb.Stmts]bool{})
	if fn == nil {
		t.Fatalf("%s: function f not found in the parsed tree", lang)
	}
	if fn.LinesOfCode == nil {
		t.Fatalf("%s: no line counts computed on f", lang)
	}
	return fn.LinesOfCode
}

// ---------------------------------------------------------------------------
// LLOC: the same program has the same number of logical lines everywhere
// ---------------------------------------------------------------------------

type llocScenario struct {
	name string
	// lloc is the number of logical lines every implementation must report.
	lloc int32
	// why explains where the number comes from, so a failure is readable.
	why string
	// impl holds one implementation per language that has the construct.
	impl map[string]string
}

var llocScenarios = []llocScenario{
	{
		name: "empty_function",
		lloc: 0,
		why:  "a function that does nothing; `pass` and `{}` are both an empty body",
		impl: map[string]string{
			langGo:     "package p\nfunc f() {\n}\n",
			langPHP:    "<?php\nfunction f() {\n}\n",
			langPython: "def f():\n    pass\n",
			langTS:     "function f() {\n}\n",
			langJava:   "class C { void f() {\n\t} }\n",
			langCSharp: "class C { void f() {\n\t} }\n",
			langRust:   "fn f() {\n}\n",
		},
	},
	{
		name: "three_plain_statements",
		lloc: 3,
		why:  "one local declaration and two calls, one per line",
		impl: map[string]string{
			langGo:     "package p\nfunc f() {\n\ta := 1\n\tprintln(a)\n\tprintln(a)\n}\n",
			langPHP:    "<?php\nfunction f() {\n    $a = 1;\n    echo $a;\n    echo $a;\n}\n",
			langPython: "def f():\n    a = 1\n    print(a)\n    print(a)\n",
			langTS:     "function f() {\n\tlet a = 1\n\tconsole.log(a)\n\tconsole.log(a)\n}\n",
			langJava:   "class C { void f() {\n\t\tint a = 1;\n\t\tg(a);\n\t\tg(a);\n\t} }\n",
			langCSharp: "class C { void f() {\n\t\tint a = 1;\n\t\tG(a);\n\t\tG(a);\n\t} }\n",
			langRust:   "fn f() {\n\tlet a = 1;\n\tg(a);\n\tg(a);\n}\n",
		},
	},
	{
		name: "multiline_single_statement",
		lloc: 1,
		why:  "one statement spread over five lines is one logical line",
		impl: map[string]string{
			langGo:     "package p\nfunc f() {\n\tg(\n\t\t1,\n\t\t2,\n\t)\n}\n",
			langPHP:    "<?php\nfunction f() {\n    g(\n        1,\n        2\n    );\n}\n",
			langPython: "def f():\n    g(\n        1,\n        2,\n    )\n",
			langTS:     "function f() {\n\tg(\n\t\t1,\n\t\t2\n\t)\n}\n",
			langJava:   "class C { void f() {\n\t\tg(\n\t\t\t1,\n\t\t\t2\n\t\t);\n\t} }\n",
			langCSharp: "class C { void f() {\n\t\tG(\n\t\t\t1,\n\t\t\t2\n\t\t);\n\t} }\n",
			langRust:   "fn f() {\n\tg(\n\t\t1,\n\t\t2,\n\t);\n}\n",
		},
	},
	{
		name: "two_statements_on_one_line",
		lloc: 1,
		why:  "the metric counts lines carrying a statement, not statements",
		impl: map[string]string{
			langGo:     "package p\nfunc f() {\n\ta := 1; println(a)\n}\n",
			langPHP:    "<?php\nfunction f() {\n    $a = 1; echo $a;\n}\n",
			langPython: "def f():\n    a = 1; print(a)\n",
			langTS:     "function f() {\n\tlet a = 1; console.log(a)\n}\n",
			langJava:   "class C { void f() {\n\t\tint a = 1; g(a);\n\t} }\n",
			langCSharp: "class C { void f() {\n\t\tint a = 1; G(a);\n\t} }\n",
			langRust:   "fn f() {\n\tlet a = 1; g(a);\n}\n",
		},
	},
	{
		name: "if_else",
		lloc: 5,
		why:  "the declaration, the if, the two branch bodies and the return; `else` is a header",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tr := 0\n\tif a > 0 {\n\t\tr = 1\n\t} else {\n\t\tr = 2\n\t}\n\treturn r\n}\n",
			langPHP:    "<?php\nfunction f($a) {\n    $r = 0;\n    if ($a > 0) {\n        $r = 1;\n    } else {\n        $r = 2;\n    }\n    return $r;\n}\n",
			langPython: "def f(a):\n    r = 0\n    if a > 0:\n        r = 1\n    else:\n        r = 2\n    return r\n",
			langTS:     "function f(a: number): number {\n\tlet r = 0\n\tif (a > 0) {\n\t\tr = 1\n\t} else {\n\t\tr = 2\n\t}\n\treturn r\n}\n",
			langJava:   "class C { int f(int a) {\n\t\tint r = 0;\n\t\tif (a > 0) {\n\t\t\tr = 1;\n\t\t} else {\n\t\t\tr = 2;\n\t\t}\n\t\treturn r;\n\t} }\n",
			langCSharp: "class C { int f(int a) {\n\t\tint r = 0;\n\t\tif (a > 0) {\n\t\t\tr = 1;\n\t\t} else {\n\t\t\tr = 2;\n\t\t}\n\t\treturn r;\n\t} }\n",
			langRust:   "fn f(a: i32) -> i32 {\n\tlet mut r = 0;\n\tif a > 0 {\n\t\tr = 1;\n\t} else {\n\t\tr = 2;\n\t}\n\treturn r;\n}\n",
		},
	},
	{
		name: "if_elseif_else",
		lloc: 7,
		why:  "an elseif carries a condition of its own, so it is the nested if that it is",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tr := 0\n\tif a > 0 {\n\t\tr = 1\n\t} else if a < 0 {\n\t\tr = 2\n\t} else {\n\t\tr = 3\n\t}\n\treturn r\n}\n",
			langPHP:    "<?php\nfunction f($a) {\n    $r = 0;\n    if ($a > 0) {\n        $r = 1;\n    } elseif ($a < 0) {\n        $r = 2;\n    } else {\n        $r = 3;\n    }\n    return $r;\n}\n",
			langPython: "def f(a):\n    r = 0\n    if a > 0:\n        r = 1\n    elif a < 0:\n        r = 2\n    else:\n        r = 3\n    return r\n",
			langTS:     "function f(a: number): number {\n\tlet r = 0\n\tif (a > 0) {\n\t\tr = 1\n\t} else if (a < 0) {\n\t\tr = 2\n\t} else {\n\t\tr = 3\n\t}\n\treturn r\n}\n",
			langJava:   "class C { int f(int a) {\n\t\tint r = 0;\n\t\tif (a > 0) {\n\t\t\tr = 1;\n\t\t} else if (a < 0) {\n\t\t\tr = 2;\n\t\t} else {\n\t\t\tr = 3;\n\t\t}\n\t\treturn r;\n\t} }\n",
			langCSharp: "class C { int f(int a) {\n\t\tint r = 0;\n\t\tif (a > 0) {\n\t\t\tr = 1;\n\t\t} else if (a < 0) {\n\t\t\tr = 2;\n\t\t} else {\n\t\t\tr = 3;\n\t\t}\n\t\treturn r;\n\t} }\n",
			langRust:   "fn f(a: i32) -> i32 {\n\tlet mut r = 0;\n\tif a > 0 {\n\t\tr = 1;\n\t} else if a < 0 {\n\t\tr = 2;\n\t} else {\n\t\tr = 3;\n\t}\n\treturn r;\n}\n",
		},
	},
	{
		name: "loop_with_body",
		lloc: 4,
		why:  "the declaration, the loop, its body and the return",
		impl: map[string]string{
			langGo:     "package p\nfunc f() int {\n\tt := 0\n\tfor i := 0; i < 3; i++ {\n\t\tt += i\n\t}\n\treturn t\n}\n",
			langPHP:    "<?php\nfunction f() {\n    $t = 0;\n    for ($i = 0; $i < 3; $i++) {\n        $t += $i;\n    }\n    return $t;\n}\n",
			langPython: "def f():\n    t = 0\n    for i in range(3):\n        t += i\n    return t\n",
			langTS:     "function f(): number {\n\tlet t = 0\n\tfor (let i = 0; i < 3; i++) {\n\t\tt += i\n\t}\n\treturn t\n}\n",
			langJava:   "class C { int f() {\n\t\tint t = 0;\n\t\tfor (int i = 0; i < 3; i++) {\n\t\t\tt += i;\n\t\t}\n\t\treturn t;\n\t} }\n",
			langCSharp: "class C { int f() {\n\t\tint t = 0;\n\t\tfor (int i = 0; i < 3; i++) {\n\t\t\tt += i;\n\t\t}\n\t\treturn t;\n\t} }\n",
			langRust:   "fn f() -> i32 {\n\tlet mut t = 0;\n\tfor i in 0..3 {\n\t\tt += i;\n\t}\n\treturn t;\n}\n",
		},
	},
	{
		// Regression: a `case` label used to count as a statement in PHP, and a
		// `match_arm` in Rust, while counting nowhere else; and the `switch` line
		// itself went uncounted in Java, whose grammar spells it
		// `switch_expression` with no "_statement" suffix.
		name: "switch_two_cases_and_default",
		lloc: 4,
		why:  "the switch and the three returns of its branches; the labels are headers",
		impl: map[string]string{
			langGo:     "package p\nfunc f(a int) int {\n\tswitch a {\n\tcase 1:\n\t\treturn 1\n\tcase 2:\n\t\treturn 2\n\tdefault:\n\t\treturn 3\n\t}\n}\n",
			langPHP:    "<?php\nfunction f($a) {\n    switch ($a) {\n        case 1:\n            return 1;\n        case 2:\n            return 2;\n        default:\n            return 3;\n    }\n}\n",
			langPython: "def f(a):\n    match a:\n        case 1:\n            return 1\n        case 2:\n            return 2\n        case _:\n            return 3\n",
			langTS:     "function f(a: number): number {\n\tswitch (a) {\n\t\tcase 1:\n\t\t\treturn 1\n\t\tcase 2:\n\t\t\treturn 2\n\t\tdefault:\n\t\t\treturn 3\n\t}\n}\n",
			langJava:   "class C { int f(int a) {\n\t\tswitch (a) {\n\t\t\tcase 1:\n\t\t\t\treturn 1;\n\t\t\tcase 2:\n\t\t\t\treturn 2;\n\t\t\tdefault:\n\t\t\t\treturn 3;\n\t\t}\n\t} }\n",
			langCSharp: "class C { int f(int a) {\n\t\tswitch (a) {\n\t\t\tcase 1:\n\t\t\t\treturn 1;\n\t\t\tcase 2:\n\t\t\t\treturn 2;\n\t\t\tdefault:\n\t\t\t\treturn 3;\n\t\t}\n\t} }\n",
			// Rust arms take an expression, so the returns are written out to
			// describe the same program rather than the same syntax
			langRust: "fn f(a: i32) -> i32 {\n\tmatch a {\n\t\t1 => {\n\t\t\treturn 1;\n\t\t}\n\t\t2 => {\n\t\t\treturn 2;\n\t\t}\n\t\t_ => {\n\t\t\treturn 3;\n\t\t}\n\t}\n}\n",
		},
	},
	{
		name: "try_two_handlers",
		lloc: 4,
		why:  "the try, the call it guards and the statement of each handler; the handler headers are not statements",
		impl: map[string]string{
			langPHP:    "<?php\nfunction f() {\n    try {\n        g();\n    } catch (A $e) {\n        h();\n    } catch (B $e) {\n        i();\n    }\n}\n",
			langPython: "def f():\n    try:\n        g()\n    except ValueError:\n        h()\n    except KeyError:\n        i()\n",
			langJava:   "class C { void f() {\n\t\ttry {\n\t\t\tg();\n\t\t} catch (RuntimeException e) {\n\t\t\th();\n\t\t} catch (Error e) {\n\t\t\ti();\n\t\t}\n\t} }\n",
			langCSharp: "class C { void f() {\n\t\ttry {\n\t\t\tG();\n\t\t} catch (System.Exception e) {\n\t\t\tH();\n\t\t} catch {\n\t\t\tI();\n\t\t}\n\t} }\n",
		},
	},
	{
		// Regression: a member declaration is not an instruction, whichever way
		// the language spells it. Go writes a package constant with the very node
		// it uses for a local one, and Python writes a class attribute with the
		// very node it uses for a real statement, so both used to be counted.
		name: "only_the_body_of_f_holds_a_statement",
		lloc: 1,
		why:  "fields, constants, imports and declarations are members, not instructions",
		impl: map[string]string{
			langGo:     "package p\n\nimport \"fmt\"\n\nconst K = 1\n\nvar V = 2\n\ntype T struct {\n\tx int\n}\n\nfunc f() {\n\tfmt.Println(K)\n}\n",
			langPHP:    "<?php\nnamespace N;\n\nuse Other\\Thing;\n\nclass C {\n    const K = 1;\n    private $x;\n    protected int $y = 0;\n\n    public function f() {\n        echo self::K;\n    }\n}\n",
			langPython: "import os\nfrom sys import argv\n\n\nclass C:\n    x = 0\n    y: int = 0\n\n    def f(self):\n        return os.name\n",
			langTS:     "import { a } from './a'\n\ninterface I {\n\tm(): void\n}\n\nclass C {\n\tx: number = 0\n\tprivate y: string = ''\n\n\tf() {\n\t\treturn a\n\t}\n}\n",
			langJava:   "package n;\n\nimport java.util.List;\n\nclass C {\n\tprivate int x;\n\tstatic final int K = 1;\n\n\t@Override\n\tpublic int f() {\n\t\treturn K;\n\t}\n}\n",
			langCSharp: "using System;\n\nnamespace N {\n\tclass C {\n\t\tprivate int x;\n\t\tconst int K = 1;\n\t\tpublic int Prop { get; set; }\n\n\t\t[Obsolete]\n\t\tpublic int f() {\n\t\t\treturn K;\n\t\t}\n\t}\n}\n",
			langRust:   "use std::fmt;\n\nconst K: i32 = 1;\nstatic V: i32 = 2;\n\nstruct T {\n\tx: i32,\n}\n\nimpl T {\n\tfn f(&self) -> i32 {\n\t\treturn K;\n\t}\n}\n",
		},
	},
	{
		name: "documentation_is_not_a_statement",
		lloc: 1,
		why:  "a documentation block of any size holds no instruction",
		impl: map[string]string{
			langGo:     "package p\n\n// One.\n// Two.\n// Three.\nfunc f() int {\n\treturn 1\n}\n",
			langPHP:    "<?php\n/**\n * One.\n * Two.\n * Three.\n */\nfunction f() {\n    return 1;\n}\n",
			langPython: "def f():\n    \"\"\"One.\n\n    Two.\n    Three.\n    \"\"\"\n    return 1\n",
			langTS:     "/**\n * One.\n * Two.\n * Three.\n */\nfunction f(): number {\n\treturn 1\n}\n",
			langJava:   "class C {\n\t/**\n\t * One.\n\t * Two.\n\t * Three.\n\t */\n\tint f() {\n\t\treturn 1;\n\t}\n}\n",
			langCSharp: "class C {\n\t/// One.\n\t/// Two.\n\t/// Three.\n\tint f() {\n\t\treturn 1;\n\t}\n}\n",
			langRust:   "/// One.\n/// Two.\n/// Three.\nfn f() -> i32 {\n\treturn 1;\n}\n",
		},
	},
	{
		name: "nested_function_does_not_inflate_its_parent",
		lloc: 1,
		why:  "the statements of a nested declaration belong to it, and its header is not one",
		impl: map[string]string{
			langGo:     "package p\nfunc g() int {\n\ta := 1\n\tb := 2\n\treturn a + b\n}\nfunc f() int {\n\treturn 1\n}\n",
			langPHP:    "<?php\nfunction g() {\n    $a = 1;\n    $b = 2;\n    return $a + $b;\n}\nfunction f() {\n    return 1;\n}\n",
			langPython: "def g():\n    a = 1\n    b = 2\n    return a + b\n\ndef f():\n    return 1\n",
			langTS:     "function g(): number {\n\tlet a = 1\n\tlet b = 2\n\treturn a + b\n}\nfunction f(): number {\n\treturn 1\n}\n",
			langJava:   "class C {\n\tint g() {\n\t\tint a = 1;\n\t\tint b = 2;\n\t\treturn a + b;\n\t}\n\tint f() {\n\t\treturn 1;\n\t}\n}\n",
			langCSharp: "class C {\n\tint g() {\n\t\tint a = 1;\n\t\tint b = 2;\n\t\treturn a + b;\n\t}\n\tint f() {\n\t\treturn 1;\n\t}\n}\n",
			langRust:   "fn g() -> i32 {\n\tlet a = 1;\n\tlet b = 2;\n\treturn a + b;\n}\nfn f() -> i32 {\n\treturn 1;\n}\n",
		},
	},
}

func TestLogicalLinesAreTheSameInEveryLanguage(t *testing.T) {
	for _, sc := range llocScenarios {
		for lang, src := range sc.impl {
			t.Run(sc.name+"/"+lang, func(t *testing.T) {
				if got := linesOfF(t, lang, src).LogicalLinesOfCode; got != sc.lloc {
					t.Errorf("lloc of f = %d, want %d (%s)", got, sc.lloc, sc.why)
				}
			})
		}
	}
}

// TestEveryLanguageCoversEveryUniversalLlocConstruct guards the table itself: a
// construct that every language has must be exercised everywhere, so that
// adding a language cannot quietly skip half the model.
func TestEveryLanguageCoversEveryUniversalLlocConstruct(t *testing.T) {
	universal := map[string]bool{
		"empty_function": true, "three_plain_statements": true,
		"multiline_single_statement": true, "two_statements_on_one_line": true,
		"if_else": true, "if_elseif_else": true, "loop_with_body": true,
		"switch_two_cases_and_default":                true,
		"only_the_body_of_f_holds_a_statement":        true,
		"documentation_is_not_a_statement":            true,
		"nested_function_does_not_inflate_its_parent": true,
	}
	for _, sc := range llocScenarios {
		if !universal[sc.name] {
			continue
		}
		for _, lang := range allLanguages {
			if _, ok := sc.impl[lang]; !ok {
				t.Errorf("scenario %q has no %s implementation", sc.name, lang)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// LOC, CLOC and NCLOC: the numbers the reference tools report
// ---------------------------------------------------------------------------

// documentedFixture is the same twelve-to-fifteen line program in each
// language, holding two documentation lines above the function, a block comment
// of two lines inside it, a blank line, and a line of code carrying a trailing
// comment.
//
// The expected values are what tokei and scc both report for these exact files.
// The interesting one is the trailing comment: `b = a + 1 // trailing note` is a
// line of code, not a comment line, for both tools and for cloc.
var documentedFixture = map[string]struct {
	src                    string
	loc, cloc, ncloc, lloc int32
}{
	langGo: {
		src: "package p\n\n// doc line one\n// doc line two\nfunc f(a int) int {\n\tb := a + 1 // trailing note\n\n\t/* block one\n\t   block two */\n\tif b > 0 {\n\t\tb = b - 1\n\t}\n\treturn b\n}\n",
		loc: 14, cloc: 4, ncloc: 8, lloc: 4,
	},
	langPHP: {
		src: "<?php\nfunction f($a) {\n    // doc line one\n    // doc line two\n    $b = $a + 1; // trailing note\n\n    /* block one\n       block two */\n    if ($b > 0) {\n        $b = $b - 1;\n    }\n    return $b;\n}\n",
		loc: 13, cloc: 4, ncloc: 8, lloc: 4,
	},
	langPython: {
		src: "# doc line one\n# doc line two\ndef f(a):\n    b = a + 1  # trailing note\n\n    # block one\n    # block two\n    if b > 0:\n        b = b - 1\n    return b\n",
		loc: 10, cloc: 4, ncloc: 5, lloc: 4,
	},
	langTS: {
		src: "// doc line one\n// doc line two\nfunction f(a: number): number {\n\tlet b = a + 1 // trailing note\n\n\t/* block one\n\t   block two */\n\tif (b > 0) {\n\t\tb = b - 1\n\t}\n\treturn b\n}\n",
		loc: 12, cloc: 4, ncloc: 7, lloc: 4,
	},
	langJava: {
		src: "class A {\n\t// doc line one\n\t// doc line two\n\tint f(int a) {\n\t\tint b = a + 1; // trailing note\n\n\t\t/* block one\n\t\t   block two */\n\t\tif (b > 0) {\n\t\t\tb = b - 1;\n\t\t}\n\t\treturn b;\n\t}\n}\n",
		loc: 14, cloc: 4, ncloc: 9, lloc: 4,
	},
	langCSharp: {
		src: "class A {\n\t// doc line one\n\t// doc line two\n\tint f(int a) {\n\t\tint b = a + 1; // trailing note\n\n\t\t/* block one\n\t\t   block two */\n\t\tif (b > 0) {\n\t\t\tb = b - 1;\n\t\t}\n\t\treturn b;\n\t}\n}\n",
		loc: 14, cloc: 4, ncloc: 9, lloc: 4,
	},
	langRust: {
		src: "// doc line one\n// doc line two\nfn f(a: i32) -> i32 {\n\tlet mut b = a + 1; // trailing note\n\n\t/* block one\n\t   block two */\n\tif b > 0 {\n\t\tb = b - 1;\n\t}\n\treturn b;\n}\n",
		loc: 12, cloc: 4, ncloc: 7, lloc: 4,
	},
}

func TestFileLineCountsMatchTheReferenceTools(t *testing.T) {
	for _, lang := range allLanguages {
		want, ok := documentedFixture[lang]
		if !ok {
			t.Fatalf("%s has no fixture", lang)
		}
		t.Run(lang, func(t *testing.T) {
			got := parse(t, lang, want.src).GetLinesOfCode()
			if got == nil {
				t.Fatal("no line counts computed on the file")
			}
			if got.LinesOfCode != want.loc {
				t.Errorf("loc = %d, want %d (tokei and scc agree on this fixture)", got.LinesOfCode, want.loc)
			}
			if got.CommentLinesOfCode != want.cloc {
				t.Errorf("cloc = %d, want %d (the trailing comment sits on a line of code)",
					got.CommentLinesOfCode, want.cloc)
			}
			if got.NonCommentLinesOfCode != want.ncloc {
				t.Errorf("ncloc = %d, want %d (blank lines are neither code nor comment)",
					got.NonCommentLinesOfCode, want.ncloc)
			}
			if got.LogicalLinesOfCode != want.lloc {
				t.Errorf("lloc = %d, want %d", got.LogicalLinesOfCode, want.lloc)
			}
			// the three buckets partition the file, blank lines apart
			if got.CommentLinesOfCode+got.NonCommentLinesOfCode > got.LinesOfCode {
				t.Errorf("cloc (%d) + ncloc (%d) exceeds loc (%d)",
					got.CommentLinesOfCode, got.NonCommentLinesOfCode, got.LinesOfCode)
			}
		})
	}
}

// TestFileLocCountsThePhysicalLines pins down the one number no tool disagrees
// about: the newline that ends the last line is a terminator, so a file of ten
// lines has ten, not eleven.
func TestFileLocCountsThePhysicalLines(t *testing.T) {
	for _, lang := range allLanguages {
		src := documentedFixture[lang].src
		t.Run(lang, func(t *testing.T) {
			want := int32(len(engine.SplitSourceLines([]byte(src))))
			if got := parse(t, lang, src).GetLinesOfCode().GetLinesOfCode(); got != want {
				t.Errorf("file loc = %d, want %d physical lines", got, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// a scope is as long as its declaration
// ---------------------------------------------------------------------------

// TestScopeSizeIsItsDeclarationSpan checks the invariant that ties LOC to the
// source: a function or a class measures the lines from the one it opens on to
// the one it closes on, inclusive.
//
// Measuring the body instead would drop the signature line whenever the opening
// brace sits on a line of its own, and always in Python, where the body starts
// on the line below the `def`.
func TestScopeSizeIsItsDeclarationSpan(t *testing.T) {
	for _, lang := range allLanguages {
		src := documentedFixture[lang].src
		t.Run(lang, func(t *testing.T) {
			file := parse(t, lang, src)
			for _, fn := range engine.GetFunctionsInFile(file) {
				loc, span := fn.GetLinesOfCode(), fn.GetLocation()
				if loc == nil || span == nil {
					t.Fatalf("function %s has no measure", fn.GetName().GetShort())
				}
				want := span.EndLine - span.StartLine + 1
				if loc.LinesOfCode != want {
					t.Errorf("function %s: loc = %d, want %d (span %d-%d)",
						fn.GetName().GetShort(), loc.LinesOfCode, want, span.StartLine, span.EndLine)
				}
			}
			for _, class := range engine.GetClassesInFile(file) {
				loc, span := class.GetLinesOfCode(), class.GetLocation()
				if loc == nil || span == nil {
					t.Fatalf("class %s has no measure", class.GetName().GetShort())
				}
				want := span.EndLine - span.StartLine + 1
				if loc.LinesOfCode != want {
					t.Errorf("class %s: loc = %d, want %d (span %d-%d)",
						class.GetName().GetShort(), loc.LinesOfCode, want, span.StartLine, span.EndLine)
				}
			}
		})
	}
}

// TestMovingTheOpeningBraceDoesNotShrinkTheFunction is the regression that
// TestScopeSizeIsItsDeclarationSpan exists to prevent, written out on the code
// style it used to break on. PSR-12 mandates it for PHP, and it is the default
// in C#.
func TestMovingTheOpeningBraceDoesNotShrinkTheFunction(t *testing.T) {
	styles := map[string]struct{ kr, allman string }{
		langPHP: {
			kr:     "<?php\nfunction f($a) {\n    return $a;\n}\n",
			allman: "<?php\nfunction f($a)\n{\n    return $a;\n}\n",
		},
		langJava: {
			kr:     "class C {\n\tint f(int a) {\n\t\treturn a;\n\t}\n}\n",
			allman: "class C\n{\n\tint f(int a)\n\t{\n\t\treturn a;\n\t}\n}\n",
		},
		langCSharp: {
			kr:     "class C {\n\tint f(int a) {\n\t\treturn a;\n\t}\n}\n",
			allman: "class C\n{\n\tint f(int a)\n\t{\n\t\treturn a;\n\t}\n}\n",
		},
		langTS: {
			kr:     "function f(a: number): number {\n\treturn a\n}\n",
			allman: "function f(a: number): number\n{\n\treturn a\n}\n",
		},
	}
	for lang, style := range styles {
		t.Run(lang, func(t *testing.T) {
			kr := linesOfF(t, lang, style.kr)
			allman := linesOfF(t, lang, style.allman)
			if allman.LinesOfCode != kr.LinesOfCode+1 {
				t.Errorf("loc = %d with the brace on its own line, %d with it on the signature: "+
					"the function is exactly one physical line longer",
					allman.LinesOfCode, kr.LinesOfCode)
			}
			// the program did not change, so its logical size did not either
			if allman.LogicalLinesOfCode != kr.LogicalLinesOfCode {
				t.Errorf("lloc changed with the brace style: %d then %d",
					kr.LogicalLinesOfCode, allman.LogicalLinesOfCode)
			}
		})
	}
}

// TestATypeIncludesTheMethodsDeclaredOutsideOfIt covers Go and Rust, the two
// languages here that declare methods next to the type rather than inside it. A
// struct whose methods are ignored reports the size of its field list, and would
// look like the smallest type of a file while being the largest.
func TestATypeIncludesTheMethodsDeclaredOutsideOfIt(t *testing.T) {
	srcs := map[string]string{
		langGo:   "package p\n\ntype C struct {\n\tx int\n}\n\nfunc (c *C) f() int {\n\treturn c.x\n}\n\nfunc (c *C) g() int {\n\treturn c.x + 1\n}\n",
		langRust: "struct C {\n\tx: i32,\n}\n\nimpl C {\n\tfn f(&self) -> i32 {\n\t\treturn self.x;\n\t}\n\n\tfn g(&self) -> i32 {\n\t\treturn self.x + 1;\n\t}\n}\n",
	}
	for lang, src := range srcs {
		t.Run(lang, func(t *testing.T) {
			file := parse(t, lang, src)
			classes := engine.GetClassesInFile(file)
			if len(classes) != 1 {
				t.Fatalf("expected the single type C, got %d classes", len(classes))
			}
			c := classes[0]
			if got := len(c.GetStmts().GetStmtFunction()); got != 2 {
				t.Fatalf("type C holds %d methods, want its 2", got)
			}
			// three lines of type declaration, plus three per method
			if got := c.GetLinesOfCode().GetLinesOfCode(); got != 9 {
				t.Errorf("loc of C = %d, want 9 (3 for the type, 3 per method)", got)
			}
			if got := c.GetLinesOfCode().GetLogicalLinesOfCode(); got != 2 {
				t.Errorf("lloc of C = %d, want 2 (one return per method)", got)
			}
		})
	}
}
