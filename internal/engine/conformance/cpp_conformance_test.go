package conformance

import "testing"

// C++ is installed into the shared scenario tables here to keep the tables in
// their original, language-comparison form while making the conformance guard
// require C++ for every universal construct.
func init() {
	complexity := map[string]string{
		"baseline":                       "int f(int a) { return a; }\n",
		"if_else":                        "int f(int a) { if (a > 0) return 1; else return 2; }\n",
		"if_elseif_else":                 "int f(int a) { if (a > 0) return 1; else if (a < 0) return 2; else return 3; }\n",
		"while_loop":                     "int f(int a) { while (a < 10) ++a; return a; }\n",
		"foreach_loop":                   "int f(int* xs) { int t = 0; for (int x : xs) t += x; return t; }\n",
		"switch_two_cases_and_default":   "int f(int a) { switch (a) { case 1: return 1; case 2: return 2; default: return 0; } }\n",
		"decisions_inside_case_bodies":   "int f(int a) { switch (a) { case 1: if (a > 0) for (int i=0; i<3; ++i) ++a; break; case 2: if (a < 0) --a; } return a; }\n",
		"logical_operators_in_condition": "int f(int a, int b) { if (a > 0 && b > 0 || a < 0) return 1; return 0; }\n",
		"nested_function_does_not_inflate_its_parent": "int g(int a) { if (a > 0) return 1; if (a < 0) return 2; return 0; } int f(int a) { if (a > 0) return 1; return 0; }\n",
		"ternary":                "int f(int a) { return a > 0 ? 1 : 2; }\n",
		"two_exception_handlers": "int f(int a) { try { g(); } catch (A& e) { return 1; } catch (B& e) { return 2; } return 0; }\n",
	}
	for i := range scenarios {
		if src, ok := complexity[scenarios[i].name]; ok {
			scenarios[i].impl[langCpp] = src
		}
	}

	lloc := map[string]string{
		"empty_function":                              "void f() {\n}\n",
		"three_plain_statements":                      "void f() {\n\tint a = 1;\n\tg(a);\n\tg(a);\n}\n",
		"multiline_single_statement":                  "void f() {\n\tg(\n\t\t1,\n\t\t2\n\t);\n}\n",
		"two_statements_on_one_line":                  "void f() {\n\tint a = 1; g(a);\n}\n",
		"if_else":                                     "int f(int a) {\n\tint r = 0;\n\tif (a > 0) {\n\t\tr = 1;\n\t} else {\n\t\tr = 2;\n\t}\n\treturn r;\n}\n",
		"if_elseif_else":                              "int f(int a) {\n\tint r = 0;\n\tif (a > 0) {\n\t\tr = 1;\n\t} else if (a < 0) {\n\t\tr = 2;\n\t} else {\n\t\tr = 3;\n\t}\n\treturn r;\n}\n",
		"loop_with_body":                              "int f() {\n\tint t = 0;\n\tfor (int i = 0; i < 3; ++i) {\n\t\tt += i;\n\t}\n\treturn t;\n}\n",
		"switch_two_cases_and_default":                "int f(int a) {\n\tswitch (a) {\n\tcase 1:\n\t\treturn 1;\n\tcase 2:\n\t\treturn 2;\n\tdefault:\n\t\treturn 3;\n\t}\n}\n",
		"try_two_handlers":                            "void f() {\n\ttry {\n\t\tg();\n\t} catch (A& e) {\n\t\th();\n\t} catch (B& e) {\n\t\ti();\n\t}\n}\n",
		"only_the_body_of_f_holds_a_statement":        "#include <vector>\nclass C { int x; static const int K = 1; void f() { g(); } };\n",
		"documentation_is_not_a_statement":            "/// One.\n/// Two.\n/// Three.\nint f() {\n\treturn 1;\n}\n",
		"nested_function_does_not_inflate_its_parent": "int g() { int a=1; int b=2; return a+b; }\nint f() {\n\treturn 1;\n}\n",
	}
	for i := range llocScenarios {
		if src, ok := lloc[llocScenarios[i].name]; ok {
			llocScenarios[i].impl[langCpp] = src
		}
	}
}

func TestCppPhysicalVolumeConformance(t *testing.T) {
	got := linesOfF(t, langCpp, "// file comment\nint f() {\n\t// body comment\n\treturn 1;\n}\n")
	if got.LinesOfCode != 4 || got.CommentLinesOfCode != 1 || got.LogicalLinesOfCode != 1 {
		t.Fatalf("C++ f volume = LOC %d, CLOC %d, LLOC %d; want 4, 1, 1",
			got.LinesOfCode, got.CommentLinesOfCode, got.LogicalLinesOfCode)
	}
}
