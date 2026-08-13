package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
)

// Bench tools for checking the volume metrics against the reference counters on
// real projects. Neither asserts anything; both skip unless pointed at a target,
// so they cost nothing in a normal run.
//
// What the numbers were validated against, and where each tool is unreliable:
//
//   - scc agrees with us on LOC for every file of the corpus, and on CLOC for Go,
//     Rust and C#. It loses track of PHP heredocs, of TypeScript regular
//     expression literals and of long Java javadoc blocks, and it stops counting
//     a Python docstring partway through.
//   - tokei splits the Markdown embedded in doc comments into a language of its
//     own, so its per-file report cannot be compared directly for Rust or Java.
//     Its totals agree with ours.
//   - radon is the reference for Python: our NCLOC equals its SLOC exactly. Our
//     CLOC is its Comments plus Multi plus the blank lines inside a docstring,
//     which we count as part of the block, as we do for every `/* */` block.

// commentSyntaxOf returns the comment syntax a language declares.
func commentSyntaxOf(lang string) engine.CommentSyntax {
	switch lang {
	case langGo:
		return (&golang.TreeSitterAdapter{}).CommentSyntax()
	case langPHP:
		return (&php.TreeSitterAdapter{}).CommentSyntax()
	case langPython:
		return (&python.TreeSitterAdapter{}).CommentSyntax()
	case langTS:
		return (&typescript.TreeSitterAdapter{}).CommentSyntax()
	case langJava:
		return (&java.TreeSitterAdapter{}).CommentSyntax()
	case langCSharp:
		return (&csharp.TreeSitterAdapter{}).CommentSyntax()
	case langRust:
		return (&rust.TreeSitterAdapter{}).CommentSyntax()
	}
	return engine.DefaultCommentSyntax()
}

var sourceExtension = map[string]string{
	langGo: ".go", langPHP: ".php", langPython: ".py", langTS: ".ts",
	langJava: ".java", langCSharp: ".cs", langRust: ".rs",
}

// TestDumpCorpusLineCounts walks a real project and writes, for every file, the
// line counts the engine measures, as "loc cloc ncloc lloc path". Compare the
// result with `scc --by-file`, `tokei --output json` or `radon raw`:
//
//	AST_METRICS_CORPUS=~/src/cobra AST_METRICS_CORPUS_LANG=go \
//	  AST_METRICS_CORPUS_OUT=/tmp/ours.tsv \
//	  go test ./internal/engine/conformance/ -run TestDumpCorpusLineCounts
func TestDumpCorpusLineCounts(t *testing.T) {
	root := os.Getenv("AST_METRICS_CORPUS")
	lang := os.Getenv("AST_METRICS_CORPUS_LANG")
	if root == "" || lang == "" {
		t.Skip("set AST_METRICS_CORPUS and AST_METRICS_CORPUS_LANG to run the bench")
	}
	ext, ok := sourceExtension[lang]
	if !ok {
		t.Fatalf("unknown language %q", lang)
	}

	runner := runnerFor(lang)
	var out strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ext) {
			return nil
		}
		file, perr := runner.Parse(path)
		if perr != nil || file == nil || file.LinesOfCode == nil {
			fmt.Fprintf(&out, "ERR\t%s\n", path)
			return nil
		}
		l := file.LinesOfCode
		fmt.Fprintf(&out, "%d\t%d\t%d\t%d\t%s\n",
			l.LinesOfCode, l.CommentLinesOfCode, l.NonCommentLinesOfCode,
			l.LogicalLinesOfCode, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	dest := os.Getenv("AST_METRICS_CORPUS_OUT")
	if dest == "" {
		t.Log("\n" + out.String())
		return
	}
	if err := os.WriteFile(dest, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", dest)
}

// TestDumpFileLineKinds prints how each line of one file is measured, which is
// what tells a disagreement with another counter from a bug:
//
//	AST_METRICS_CLASSIFY=~/src/gson/JsonReader.java AST_METRICS_CLASSIFY_LANG=java \
//	  go test ./internal/engine/conformance/ -run TestDumpFileLineKinds -v
func TestDumpFileLineKinds(t *testing.T) {
	path := os.Getenv("AST_METRICS_CLASSIFY")
	lang := os.Getenv("AST_METRICS_CLASSIFY_LANG")
	if path == "" || lang == "" {
		t.Skip("set AST_METRICS_CLASSIFY and AST_METRICS_CLASSIFY_LANG to run the bench")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	syn := commentSyntaxOf(lang)
	lines := engine.SplitSourceLines(src)
	var out strings.Builder
	for i := range lines {
		// each line is measured on a range of its own, scanned from the top of
		// the file, which is how the engine sees it
		one := engine.CountLinesOfCode(lines, i+1, i+1, syn)
		kind := "blank"
		switch {
		case one.CommentLinesOfCode > 0:
			kind = "COMMENT"
		case one.NonCommentLinesOfCode > 0:
			kind = "code"
		}
		fmt.Fprintf(&out, "%5d %-7s %s\n", i+1, kind, lines[i])
	}
	fmt.Print(out.String())
}
