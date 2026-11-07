package cribelo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goMod = "module example.com/app\n\ngo 1.22\n"

func factTexts(facts []Fact, kind string) []string {
	var out []string
	for _, f := range facts {
		if f.Kind == kind {
			out = append(out, f.Text)
		}
	}
	return out
}

func TestGraphFactsCycleAndFirstDependency(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":    goMod,
		"a/a.go":    "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":    "package b\n\nfunc B() int { return 1 }\n",
		"c/c.go":    "package c\n\nfunc C() int { return 2 }\n",
		"util/u.go": "package util\n\nfunc U() int { return 3 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod":    goMod,
		"a/a.go":    "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":    "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.A() }\n",
		"c/c.go":    "package c\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/util\"\n\t\"github.com/acme/tool\"\n)\n\nfunc C() int { fmt.Println(tool.X); return util.U() }\n",
		"util/u.go": "package util\n\nfunc U() int { return 3 }\n",
	})
	changes := []Change{{Path: "b/b.go", Status: 'M'}, {Path: "c/c.go", Status: 'M'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, nil), changes)
	assert.Equal(t, []string{"introduces a cycle: b.go → a.go → b.go"}, factTexts(facts, FactCycle))
	assert.Equal(t, []string{"c depends on util for the first time"}, factTexts(facts, FactDependency))
	assert.Equal(t, []string{"first use of github.com/acme/tool in the repository"}, factTexts(facts, FactLibrary))

	// The same change against a base where the edges already exist says nothing.
	facts = graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, head, changes, nil), changes)
	assert.Empty(t, facts)
}

func TestGraphFactsDeletedFileBreaksNothing(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":   goMod,
		"a/a.go":   "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":   "package b\n\nfunc B() int { return 1 }\n",
		"b/old.go": "package b\n\nfunc Old() int { return 1 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod": goMod,
		"a/a.go": "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go": "package b\n\nfunc B() int { return 2 }\n",
	})
	changes := []Change{{Path: "b/b.go", Status: 'M'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, []Change{{Path: "b/old.go", Status: 'D'}}), changes)
	assert.Empty(t, facts)
}

func TestCloneFacts(t *testing.T) {
	files := []FileReport{
		{Path: "a/a.go", Clone: &CloneMatch{Path: "b/b.go", Function: "Parse ~ Scan", Similarity: 0.91}},
		{Path: "c/c.go", Clone: &CloneMatch{Path: "a/a.go", Function: "X ~ Y", Similarity: 0.95}},
		{Path: "d/d.go"},
	}
	facts := cloneFacts(files, map[string]bool{"a/a.go": true, "c/c.go": true, "d/d.go": true})
	require.Len(t, facts, 1)
	assert.Equal(t, "Parse() is 91% similar to b/b.go:Scan(), which this change does not touch", facts[0].Text)
	assert.Equal(t, []string{"b/b.go"}, facts[0].Files)
}

func TestSignatureFacts(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":  goMod,
		"a/a.go":  "package a\n\nfunc Sum(a, b int) int { return a + b }\n\nfunc helper(x int) int { return x }\n",
		"b/b.go":  "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.Sum(1, 2) }\n",
		"b/b2.go": "package b\n\nimport \"example.com/app/a\"\n\nfunc B2() int { return a.Sum(3, 4) }\n",
	})
	headA := "package a\n\nfunc Sum(a, b, c int) int { return a + b + c }\n\nfunc helper(x, y int) int { return x }\n\nfunc Fresh() int { return 1 }\n\nfunc Used() int { return 2 }\n\nfunc main() {}\n"
	head := parsedTree(t, map[string]string{
		"go.mod":  goMod,
		"a/a.go":  headA,
		"b/b.go":  "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.Sum(1, 2, 0) + a.Used() }\n",
		"b/b2.go": "package b\n\nimport \"example.com/app/a\"\n\nfunc B2() int { return a.Sum(3, 4, 0) }\n",
	})
	lines := len(strings.Split(headA, "\n"))
	changes := []Change{{Path: "a/a.go", Status: 'M', Hunks: []Hunk{{Start: 1, End: lines}}}, {Path: "b/b.go", Status: 'M'}, {Path: "b/b2.go", Status: 'M'}}
	gs := buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, nil)
	facts := signatureFacts(gs, head, base, changes, func() *nameIndex { return buildNameIndex(head) })
	assert.Equal(t, []string{"Sum() went from 2 to 3 parameters (adds c), called from 2 files"}, factTexts(facts, FactSignature))
	assert.Equal(t, []string{"1 new exported function called nowhere yet: Fresh()"}, factTexts(facts, FactAPI))
}

func TestGraphFactsSkipNewDirectoriesTestsAndPathAliases(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod": goMod,
		"a/a.go": "package a\n\nfunc A() int { return 1 }\n",
		"b/b.go": "package b\n\nfunc B() int { return 1 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod":      goMod,
		"a/a.go":      "package a\n\nfunc A() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/b\"\n)\n\nfunc TestA(t *testing.T) { _ = b.B() }\n",
		"b/b.go":      "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.A() }\n",
		"fresh/f.go":  "package fresh\n\nimport \"example.com/app/a\"\n\nfunc F() int { return a.A() }\n",
	})
	changes := []Change{{Path: "b/b.go", Status: 'M'}, {Path: "fresh/f.go", Status: 'A'}, {Path: "a/a_test.go", Status: 'A'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, nil), changes)
	// b → a → (a_test → b) is not a cycle: tests are out of the graph. b
	// depends on a for the first time; the new directory fresh says nothing.
	assert.Empty(t, factTexts(facts, FactCycle))
	assert.Equal(t, []string{"b depends on a for the first time"}, factTexts(facts, FactDependency))

	assert.True(t, looksLikePath(head.Root, "fresh/f.go", "a/thing"), "a directory of the repository is a path alias, not a library")
	assert.True(t, looksLikePath(head.Root, "fresh/f.go", "./local"))
	assert.False(t, looksLikePath(head.Root, "fresh/f.go", "github.com/acme/tool"))
	assert.False(t, looksLikePath(head.Root, "fresh/f.go", "react"))
	assert.True(t, looksLikePath(head.Root, "a/pkg/mod.py", "a.other.thing"), "a dotted Python module rooted in a directory of the repository")
	assert.False(t, looksLikePath(head.Root, "a/pkg/mod.py", "numpy.linalg"))
	assert.False(t, looksLikePath(head.Root, "a/Main.java", "a.b.c"), "Java namespaces are not checked against directories")
}

func TestLibraryHelpersForScripts(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "node"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "node", "scan.ts"), []byte("export const x = 1\n"), 0o644))
	assert.True(t, looksLikePath(root, "src/node/index.ts", "scan"), "a bare name that is a file next to the importer")
	assert.False(t, looksLikePath(root, "src/node/index.ts", "rollup"))
	assert.True(t, looksLikePath(root, "src/node/index.ts", "#types"), "a Node subpath import of the package itself")
	assert.True(t, looksLikePath(root, "src/node/index.ts", "package.json"), "a file imported for its content")
	assert.True(t, looksLikePath(root, "src/a.tsx", "@scope/theme/index.css"))
	assert.Equal(t, "@voidzero-dev/vitepress-theme", packageOf("a.ts", "@voidzero-dev/vitepress-theme/index.css"))
	assert.Equal(t, "lodash", packageOf("a.ts", "lodash/fp"))
	assert.Equal(t, "numpy", packageOf("a.py", "numpy.linalg"))
	assert.Equal(t, "github.com/acme/tool", packageOf("a.go", "github.com/acme/tool"))
	assert.True(t, throughBarrel("src/a.ts", []string{"src/index.ts", "src/a.ts"}))
	assert.False(t, throughBarrel("src/a.ts", []string{"src/b.ts", "src/a.ts"}))
	assert.False(t, throughBarrel("a/a.go", []string{"a/index.go", "a/a.go"}))
}

func TestGraphFactsReportACycleOnce(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod": goMod,
		"a/a.go": "package a\n\nfunc A() int { return 1 }\n",
		"b/b.go": "package b\n\nfunc B() int { return 1 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod": goMod,
		"a/a.go": "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go": "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.A() }\n",
	})
	changes := []Change{{Path: "a/a.go", Status: 'M'}, {Path: "b/b.go", Status: 'M'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, nil), changes)
	assert.Equal(t, []string{"introduces a cycle: a.go → b.go → a.go"}, factTexts(facts, FactCycle))
}

func TestParameterChange(t *testing.T) {
	params := func(names ...string) *pb.StmtFunction {
		fn := &pb.StmtFunction{}
		for _, n := range names {
			fn.Parameters = append(fn.Parameters, &pb.StmtParameter{Name: n})
		}
		return fn
	}
	assert.True(t, sameParameters(params("a", "b"), params("x", "y")), "a rename is not a signature change")
	assert.False(t, sameParameters(params("a"), params("a", "b")))
	assert.Equal(t, "went from 1 to 2 parameters (adds b)", parameterChange(params("a"), params("a", "b")))
	assert.Equal(t, "went from 2 to 1 parameter (drops b)", parameterChange(params("a", "b"), params("a")))
	assert.Equal(t, "went from 0 to 1 parameter (adds ctx)", parameterChange(params(), params("ctx")))
	assert.Equal(t, "a, b and c", joinWords([]string{"a", "b", "c"}, 3))
	assert.Equal(t, "a, b, c and 2 more", joinWords([]string{"a", "b", "c", "d", "e"}, 3))
}

func TestIsExported(t *testing.T) {
	assert.True(t, isExported("Golang", "Parse", "func Parse() {}"))
	assert.False(t, isExported("Golang", "parse", "func parse() {}"))
	assert.False(t, isExported("Python", "Foo::_hidden", "    def _hidden(self):"))
	assert.True(t, isExported("Python", "Foo::run", "    def run(self):"))
	assert.True(t, isExported("PHP", "Foo::bar", "    public function bar() {"))
	assert.False(t, isExported("PHP", "Foo::bar", "    private function bar() {"))
	assert.True(t, isExported("PHP", "Foo::bar", "    function bar() {"))
	assert.True(t, isExported("PHP", "helper", "function helper() {"))
	assert.True(t, isExported("TypeScript", "helper", "export function helper() {"))
	assert.False(t, isExported("TypeScript", "helper", "function helper() {"))
	assert.True(t, isExported("Rust", "run", "pub(crate) fn run() {"))
	assert.True(t, isExported("Java", "Foo::run", "    public static void run() {"))
	assert.False(t, isExported("Java", "Foo::run", "    void run() {"))
}

func TestSortFacts(t *testing.T) {
	facts := []Fact{{Kind: FactClone, Path: "z"}, {Kind: FactCycle, Path: "b"}, {Kind: FactSignature, Path: "a"}, {Kind: FactCycle, Path: "a"}}
	sortFacts(facts)
	assert.Equal(t, []Fact{{Kind: FactCycle, Path: "a"}, {Kind: FactCycle, Path: "b"}, {Kind: FactSignature, Path: "a"}, {Kind: FactClone, Path: "z"}}, facts)
}

func TestHistoryFactsCountFixesOfAFunction(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "go.mod", goMod)
	body := func(n int) string {
		return fmt.Sprintf("package p\n\nfunc Parse(s string) int {\n\tif s == \"\" {\n\t\treturn %d\n\t}\n\treturn len(s)\n}\n\nfunc Other() int { return 1 }\n", n)
	}
	put(t, dir, "p/p.go", body(0))
	git(t, dir, "2026-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2026-01-01T10:00:00Z", "commit", "-q", "-m", "initial")
	put(t, dir, "p/p.go", body(1))
	git(t, dir, "2026-02-01T10:00:00Z", "commit", "-q", "-am", "fix: empty input")
	put(t, dir, "p/p.go", body(2))
	git(t, dir, "2026-03-01T10:00:00Z", "commit", "-q", "-am", "Revert \"empty input\"")
	put(t, dir, "p/p.go", body(3))
	git(t, dir, "2026-04-01T10:00:00Z", "commit", "-q", "-am", "feat: something else")
	// A fix elsewhere in the file does not count for Parse.
	put(t, dir, "p/p.go", body(3)+"\nfunc Later() int { return 2 }\n")
	git(t, dir, "2026-05-01T10:00:00Z", "commit", "-q", "-am", "fix: add Later")

	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	baseSha, err := repo.ResolveRef("HEAD")
	require.NoError(t, err)
	base, err := parseFiles(dir, []string{"p/p.go"})
	require.NoError(t, err)
	files := []FileReport{{Path: "p/p.go", Read: true, Functions: []TouchedFunction{{Name: "Parse", After: 3}, {Name: "Other", After: 1}}}}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Unix()
	facts := historyFacts(repo, baseSha, base, files, map[string]Change{}, now)
	require.Len(t, facts, 1)
	assert.Equal(t, FactHistory, facts[0].Kind)
	assert.Equal(t, "Parse() was fixed 2 times in the past year, once by a revert", facts[0].Text)

	// Outside the window, nothing is said.
	later := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	assert.Empty(t, historyFacts(repo, baseSha, base, files, map[string]Change{}, later))
}

func TestDeclarationEdgeIsRustOnly(t *testing.T) {
	assert.True(t, declarationEdge("src/lib.rs", "src/sbom.rs"))
	assert.True(t, declarationEdge("src/ops/mod.rs", "src/ops/registry/mod.rs"))
	assert.True(t, declarationEdge("src/ops/registry.rs", "src/ops/registry/search.rs"))
	assert.False(t, declarationEdge("src/ops/registry/search.rs", "src/ops/registry/mod.rs"), "the child reading its parent is a dependency")
	assert.False(t, declarationEdge("src/ops/mod.rs", "src/core/workspace.rs"))
	assert.False(t, declarationEdge("a/index.ts", "a/b.ts"))
}

func TestDependencyFactsAreToldOncePerModuleAndPerSharedTarget(t *testing.T) {
	facts := dependencyFacts([]Fact{
		{Kind: FactDependency, Path: "rules/a.go", Files: []string{"model/metadata"}},
		{Kind: FactDependency, Path: "rules/b.go", Files: []string{"model/metadata", "util/x"}},
		{Kind: FactDependency, Path: "storage/remote/c.go", Files: []string{"model/metadata"}},
		{Kind: FactDependency, Path: "cmd/d.go", Files: []string{"model/metadata"}},
	})
	assert.Equal(t, []string{
		"model/metadata is depended on by rules, storage/remote and cmd for the first time",
		"rules depends on util/x for the first time",
	}, factTexts(facts, FactDependency))
	assert.Equal(t, []string{"rules", "storage/remote", "cmd"}, facts[0].Files)
}

func TestShortChainNamesGenericFilesByDirectory(t *testing.T) {
	assert.Equal(t, []string{"validators/mod.rs", "named_tuple.rs", "validators/mod.rs"},
		shortChain([]string{"src/validators/mod.rs", "src/validators/named_tuple.rs", "src/validators/mod.rs"}))
	assert.Equal(t, []string{"b.go", "a.go", "b.go"}, shortChain([]string{"b/b.go", "a/a.go", "b/b.go"}))
}

func TestThroughBarrelKnowsPythonPackages(t *testing.T) {
	assert.True(t, throughBarrel("pkg/a.py", []string{"pkg/__init__.py", "pkg/a.py"}))
	assert.False(t, throughBarrel("pkg/a.py", []string{"pkg/b.py", "pkg/a.py"}))
	assert.False(t, throughBarrel("pkg/a.go", []string{"pkg/__init__.py", "pkg/a.go"}))
}

func TestWorkspacePackagesAreNotLibraries(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644))
	}
	write("package.json", `{"name": "vite-monorepo", "private": true}`)
	write("playground/dep/package.json", "{\n  \"name\": \"@vitejs/test-dep\",\n  \"version\": \"1.0.0\"\n}\n")
	write("crates/cargo-util-schemas/Cargo.toml", "[package]\nname = \"cargo-util-schemas\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = \"1\"\n")
	write("tools/go.mod", "module github.com/acme/app/tools\n\ngo 1.25\n")
	write("node_modules/react/package.json", `{"name": "react"}`)
	gs := graphs{workspace: workspacePackages(root)}
	assert.True(t, gs.inWorkspace("@vitejs/test-dep"))
	assert.True(t, gs.inWorkspace("cargo_util_schemas::core"))
	assert.True(t, gs.inWorkspace("github.com/acme/app/tools/cmd/gen"))
	assert.False(t, gs.inWorkspace("react"), "dependency directories are not walked")
	assert.False(t, gs.inWorkspace("serde"))
}

func TestIsConfigFile(t *testing.T) {
	assert.True(t, isConfigFile("playground/ssr/vite.config.ts"))
	assert.True(t, isConfigFile(".eslintrc.js"))
	assert.False(t, isConfigFile("src/config.ts"))
	assert.False(t, isConfigFile("src/node/config/index.ts"))
}
