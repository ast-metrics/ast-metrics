package engine

import (
	"os"
	"regexp"
	"strings"
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func TestCountLinesOfCode_BasicCase(t *testing.T) {
	source := []string{
		"package main",
		"",
		"func main() {",
		"    // comment",
		"    fmt.Println(\"hello\")",
		"}",
	}

	loc := CountLinesOfCode(source, 1, 6, DefaultCommentSyntax())
	if loc.LinesOfCode != 6 {
		t.Errorf("expected 6 lines of code, got %d", loc.LinesOfCode)
	}
	if loc.CommentLinesOfCode != 1 {
		t.Errorf("expected 1 comment line, got %d", loc.CommentLinesOfCode)
	}
	// the blank line is neither code nor comment
	if loc.NonCommentLinesOfCode != 4 {
		t.Errorf("expected 4 lines holding code, got %d", loc.NonCommentLinesOfCode)
	}
}

func TestCountLinesOfCode_InvalidBounds(t *testing.T) {
	source := []string{"line1", "line2"}

	loc := CountLinesOfCode(source, 0, 10, DefaultCommentSyntax())
	if loc.LinesOfCode != 2 {
		t.Errorf("expected 2 lines of code, got %d", loc.LinesOfCode)
	}
}

func TestSplitSourceLines_TrailingNewlineIsATerminator(t *testing.T) {
	if got := SplitSourceLines([]byte("a\nb\nc\n")); len(got) != 3 {
		t.Errorf("expected 3 lines for a file ending with a newline, got %d (%q)", len(got), got)
	}
	// no final newline: the last line is still a line
	if got := SplitSourceLines([]byte("a\nb\nc")); len(got) != 3 {
		t.Errorf("expected 3 lines without a final newline, got %d", len(got))
	}
	// a genuinely empty last line, kept because two newlines end the file
	if got := SplitSourceLines([]byte("a\n\n")); len(got) != 2 {
		t.Errorf("expected 2 lines, got %d (%q)", len(got), got)
	}
	// an empty file holds no line, which is what wc, cloc and scc all report
	if got := SplitSourceLines([]byte("")); len(got) != 0 {
		t.Errorf("expected 0 lines for an empty file, got %d", len(got))
	}
	// a file holding just a newline holds one empty line
	if got := SplitSourceLines([]byte("\n")); len(got) != 1 {
		t.Errorf("expected 1 line for a lone newline, got %d", len(got))
	}
	if got := SplitSourceLines([]byte("a\r\nb\r\n")); len(got) != 2 {
		t.Errorf("expected 2 lines for CRLF input, got %d (%q)", len(got), got)
	}
}

func TestCountLinesOfCode_ACommentAfterCodeIsCode(t *testing.T) {
	// cloc, tokei and scc all agree: a documented line of code is code
	source := []string{"total += 1 // sum it up"}
	loc := CountLinesOfCode(source, 1, 1, DefaultCommentSyntax())
	if loc.CommentLinesOfCode != 0 || loc.NonCommentLinesOfCode != 1 {
		t.Errorf("expected the line to be code, got cloc=%d ncloc=%d",
			loc.CommentLinesOfCode, loc.NonCommentLinesOfCode)
	}
}

func TestCountLinesOfCode_BlockOpenedBeforeTheRange(t *testing.T) {
	// measuring lines 3 to 4 must still know that a block opened on line 1
	source := []string{"/*", "one", "two", "*/", "code()"}
	loc := CountLinesOfCode(source, 3, 4, DefaultCommentSyntax())
	if loc.CommentLinesOfCode != 2 {
		t.Errorf("expected 2 comment lines inside the block, got %d", loc.CommentLinesOfCode)
	}
}

func TestCountLinesOfCode_CommentMarkersInsideStrings(t *testing.T) {
	source := []string{
		`url := "http://example.com"`,
		"path := \"/* not a comment */\"",
		"code()",
	}
	loc := CountLinesOfCode(source, 1, 3, DefaultCommentSyntax())
	if loc.CommentLinesOfCode != 0 {
		t.Errorf("expected no comment line, got %d", loc.CommentLinesOfCode)
	}
	if loc.NonCommentLinesOfCode != 3 {
		t.Errorf("expected 3 lines of code, got %d", loc.NonCommentLinesOfCode)
	}
}

func TestCountLinesOfCode_CodeAfterABlockCloses(t *testing.T) {
	source := []string{"/* doc", "   more */ code()"}
	loc := CountLinesOfCode(source, 1, 2, DefaultCommentSyntax())
	if loc.CommentLinesOfCode != 1 || loc.NonCommentLinesOfCode != 1 {
		t.Errorf("expected one comment line and one code line, got cloc=%d ncloc=%d",
			loc.CommentLinesOfCode, loc.NonCommentLinesOfCode)
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"hello world"`, "             "},
		{`'test'`, "      "},
		{`no quotes`, "no quotes"},
		{`"mixed 'quotes'"`, "                "},
	}

	for _, test := range tests {
		result := stripQuotes(test.input)
		if result != test.expected {
			t.Errorf("stripQuotes(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestFactoryStmts(t *testing.T) {
	stmts := FactoryStmts()

	if stmts == nil {
		t.Error("expected non-nil stmts")
	}
	if stmts.StmtDecisionIf == nil {
		t.Error("expected non-nil StmtDecisionIf")
	}
	if stmts.StmtFunction == nil {
		t.Error("expected non-nil StmtFunction")
	}
}

func TestGetClassesInFile_EmptyFile(t *testing.T) {
	file := &pb.File{}
	classes := GetClassesInFile(file)

	if len(classes) != 0 {
		t.Errorf("expected 0 classes, got %d", len(classes))
	}
}

func TestGetClassesInFile_WithClasses(t *testing.T) {
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "TestClass"}},
			},
		},
	}

	classes := GetClassesInFile(file)
	if len(classes) != 1 {
		t.Errorf("expected 1 class, got %d", len(classes))
	}
}

func TestGetClassesInFile_DeduplicatesNamespaceAndFileEntries(t *testing.T) {
	class := &pb.StmtClass{Name: &pb.Name{Short: "Foo"}}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtClass: []*pb.StmtClass{class},
					},
				},
			},
			StmtClass: []*pb.StmtClass{class},
		},
	}

	classes := GetClassesInFile(file)
	if len(classes) != 1 {
		t.Errorf("expected 1 deduplicated class, got %d", len(classes))
	}
}

func TestGetClassesInFile_DeduplicatesEquivalentClassesByQualifiedName(t *testing.T) {
	classInNamespace := &pb.StmtClass{Name: &pb.Name{Short: "Foo", Qualified: "Acme\\Foo"}}
	classInFile := &pb.StmtClass{Name: &pb.Name{Short: "Foo", Qualified: "Acme\\Foo"}}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtClass: []*pb.StmtClass{classInNamespace},
					},
				},
			},
			StmtClass: []*pb.StmtClass{classInFile},
		},
	}

	classes := GetClassesInFile(file)
	if len(classes) != 1 {
		t.Errorf("expected 1 deduplicated class by qualified name, got %d", len(classes))
	}
}

func TestGetFunctionsInFile_EmptyFile(t *testing.T) {
	file := &pb.File{}
	functions := GetFunctionsInFile(file)

	if len(functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(functions))
	}
}

func TestGetFunctionsInFile_DeduplicatesNamespaceClassAndFileEntries(t *testing.T) {
	method := &pb.StmtFunction{Name: &pb.Name{Short: "method"}}
	topLevel := &pb.StmtFunction{Name: &pb.Name{Short: "top"}}
	class := &pb.StmtClass{
		Stmts: &pb.Stmts{
			StmtFunction: []*pb.StmtFunction{method},
		},
	}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtFunction: []*pb.StmtFunction{method, topLevel},
						StmtClass:    []*pb.StmtClass{class},
					},
				},
			},
			StmtFunction: []*pb.StmtFunction{topLevel},
		},
	}

	functions := GetFunctionsInFile(file)
	if len(functions) != 2 {
		t.Errorf("expected 2 deduplicated functions, got %d", len(functions))
	}
}

func TestGetFunctionsInFile_DeduplicatesEquivalentFunctionsByQualifiedName(t *testing.T) {
	fnInNamespace := &pb.StmtFunction{Name: &pb.Name{Short: "f", Qualified: "Acme\\f"}}
	fnInFile := &pb.StmtFunction{Name: &pb.Name{Short: "f", Qualified: "Acme\\f"}}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtFunction: []*pb.StmtFunction{fnInNamespace},
					},
				},
			},
			StmtFunction: []*pb.StmtFunction{fnInFile},
		},
	}

	functions := GetFunctionsInFile(file)
	if len(functions) != 1 {
		t.Errorf("expected 1 deduplicated function by qualified name, got %d", len(functions))
	}
}

func TestGetFunctionsOutsideClassesInFile_ExcludesFunctionsAttachedToClasses(t *testing.T) {
	method := &pb.StmtFunction{Name: &pb.Name{Short: "M", Qualified: "Acme\\C::M"}}
	outside := &pb.StmtFunction{Name: &pb.Name{Short: "F", Qualified: "Acme\\F"}}

	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtClass: []*pb.StmtClass{
							{
								Name: &pb.Name{Short: "C", Qualified: "Acme\\C"},
								Stmts: &pb.Stmts{
									StmtFunction: []*pb.StmtFunction{
										method,
									},
								},
							},
						},
						StmtFunction: []*pb.StmtFunction{
							method,
							outside,
						},
					},
				},
			},
			StmtFunction: []*pb.StmtFunction{
				outside,
			},
		},
	}

	functions := GetFunctionsOutsideClassesInFile(file)
	if len(functions) != 1 {
		t.Fatalf("expected 1 outside function, got %d", len(functions))
	}
	if functions[0].Name == nil || functions[0].Name.Qualified != "Acme\\F" {
		t.Fatalf("expected outside function Acme\\\\F, got %+v", functions[0].Name)
	}
}

func TestGetFunctionsOutsideClassesInFile_ReturnsFunctionsWhenNoClasses(t *testing.T) {
	fnInNamespace := &pb.StmtFunction{Name: &pb.Name{Short: "f", Qualified: "Acme\\f"}}
	fnInFile := &pb.StmtFunction{Name: &pb.Name{Short: "f", Qualified: "Acme\\f"}}
	fn2 := &pb.StmtFunction{Name: &pb.Name{Short: "g", Qualified: "Acme\\g"}}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtFunction: []*pb.StmtFunction{fnInNamespace, fn2},
					},
				},
			},
			StmtFunction: []*pb.StmtFunction{fnInFile},
		},
	}

	functions := GetFunctionsOutsideClassesInFile(file)
	if len(functions) != 2 {
		t.Fatalf("expected 2 deduplicated outside functions, got %d", len(functions))
	}
}

func TestReduceDepthOfNamespace(t *testing.T) {
	tests := []struct {
		namespace string
		depth     int
		expected  string
	}{
		{"com.example.package", 2, "com.example"},
		{"simple", 1, "simple"},
		{"one.two.three.four", 3, "one.two.three"},
	}

	for _, test := range tests {
		result := ReduceDepthOfNamespace(test.namespace, test.depth)
		if result != test.expected {
			t.Errorf("ReduceDepthOfNamespace(%q, %d) = %q, expected %q",
				test.namespace, test.depth, result, test.expected)
		}
	}

	// Test github.com special case separately
	result := ReduceDepthOfNamespace("github.com/user/repo", 2)
	// github.com case adds 1 to depth, so depth=2 becomes depth=3
	// This should return the full namespace since depth >= parts length
	if !strings.Contains(result, "github.com") {
		t.Errorf("ReduceDepthOfNamespace with github.com should preserve github.com, got %q", result)
	}
}

// splitNamespaceParts has to answer exactly what a "[^A-Za-z0-9]+" regular
// expression answers, separators included, or namespaces would be grouped
// differently. The expression itself is the reference.
func TestSplitNamespaceParts(t *testing.T) {
	reference := regexp.MustCompile("[^A-Za-z0-9]+")

	for _, namespace := range []string{
		"",
		"simple",
		"com.example.package",
		"App\\Component\\Github",
		"github.com/user/repo",
		"/leading/separator",
		"trailing.separator.",
		"double..separator",
		"mixed_-.separators",
		"héllo.wörld",
		"...",
		"a1.b2.c3",
	} {
		separator, parts := splitNamespaceParts(namespace)

		if expected := reference.FindString(namespace); separator != expected {
			t.Errorf("splitNamespaceParts(%q) separator = %q, expected %q", namespace, separator, expected)
		}
		expectedParts := reference.Split(namespace, -1)
		if len(parts) != len(expectedParts) {
			t.Fatalf("splitNamespaceParts(%q) = %q, expected %q", namespace, parts, expectedParts)
		}
		for i := range parts {
			if parts[i] != expectedParts[i] {
				t.Errorf("splitNamespaceParts(%q) part %d = %q, expected %q", namespace, i, parts[i], expectedParts[i])
			}
		}
	}
}

func TestSearchFilesByExtension(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFile := tmpDir + "/test.go"
	err = os.WriteFile(testFile, []byte("package main"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	files, err := SearchFilesByExtension([]string{tmpDir}, ".go")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestGetDependenciesInFile_NilFile(t *testing.T) {
	deps := GetDependenciesInFile(nil)
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies for nil file, got %d", len(deps))
	}
}

func TestNamespacesParsing(t *testing.T) {
	t.Run("Should correctly reduce depth of namespace", func(t *testing.T) {
		assert.Equal(t, "abcd/def", ReduceDepthOfNamespace("abcd/def/ghi/lkm", 2))
		assert.Equal(t, "abcd", ReduceDepthOfNamespace("abcd/def/ghi/lkm", 1))
		assert.Equal(t, "abcd.def.ghi", ReduceDepthOfNamespace("abcd.def.ghi.lkm", 3))
		assert.Equal(t, "abcd.def", ReduceDepthOfNamespace("abcd.def", 3))
		assert.Equal(t, "abcd", ReduceDepthOfNamespace("abcd/def.ghi.lkm", 1))
		assert.Equal(t, "abcd", ReduceDepthOfNamespace("abcd", 2))
	})

	t.Run("Should avoid github.com namespace", func(t *testing.T) {
		assert.Equal(t, "github.com/test/test", ReduceDepthOfNamespace("github.com/test/test/test/test", 2))
		assert.Equal(t, "github.com/test", ReduceDepthOfNamespace("github.com/test/test/test/test", 1))
		assert.Equal(t, "github.com/test/test/test", ReduceDepthOfNamespace("github.com/test.test.test.test", 3))
		assert.Equal(t, "github.com/test.test", ReduceDepthOfNamespace("github.com/test.test", 3))
		assert.Equal(t, "github.com/test", ReduceDepthOfNamespace("github.com/test/test.test.test", 1))
		assert.Equal(t, "github.com/test", ReduceDepthOfNamespace("github.com/test", 2))
	})

}

func TestCountLinesOfCode_PerLanguageSyntax(t *testing.T) {
	// Python: "//" is floor division, "#" is a comment
	pySource := []string{
		"def divide(a, b):",
		"    # a real comment",
		"    return a // b",
	}
	python := CommentSyntax{Line: []string{"#"}, DocString: []string{`"""`, `'''`}, Quote: []rune{'"', '\''}}
	loc := CountLinesOfCode(pySource, 1, 3, python)
	if loc.CommentLinesOfCode != 1 {
		t.Errorf("python: expected 1 comment line, got %d", loc.CommentLinesOfCode)
	}

	// Rust: "#[...]" is an attribute, "//" is a comment, "'a" is a lifetime
	rsSource := []string{
		"fn f<'a>(s: &'a str) {",
		"    // a real comment",
		"    #[allow(unused)]",
		"    let x = 1;",
		"}",
	}
	rust := CommentSyntax{
		Line: []string{"//"}, BlockOpen: "/*", BlockClose: "*/",
		Quote: []rune{'"'}, LifetimeQuote: true,
	}
	loc = CountLinesOfCode(rsSource, 1, 5, rust)
	if loc.CommentLinesOfCode != 1 {
		t.Errorf("rust: expected 1 comment line, got %d", loc.CommentLinesOfCode)
	}
	if loc.NonCommentLinesOfCode != 4 {
		t.Errorf("rust: expected 4 lines of code, got %d", loc.NonCommentLinesOfCode)
	}
}
