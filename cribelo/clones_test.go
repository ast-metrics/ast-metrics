package cribelo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenizeNormalizes(t *testing.T) {
	src := "x := 12 + \"ab\" // trailing\n/* block\ncomment */ y # hash\nf(1.5, 'c', `raw`)"
	got := Tokenize(src)
	want := []string{"$", ":", "=", "0", "+", "\"ab", "$", "$", "(", "0", ",", "\"c", ",", "\"raw", ")"}
	assert.Equal(t, want, got)
}

func TestTokenizeStringEscapes(t *testing.T) {
	assert.Equal(t, []string{"$", "=", "\"q\\\"uote", ";"}, Tokenize(`a = "q\"uote"; // c`))
	assert.Equal(t, []string{"$", "=", "\"never closed"}, Tokenize(`a = "never closed`))
}

func TestTokenizeHashAttributeKept(t *testing.T) {
	// "#[" and "#!" are not comments (Rust attributes)
	assert.Equal(t, []string{"#", "[", "$", "]"}, Tokenize("#[derive]"))
}

func TestTokenizeUnterminated(t *testing.T) {
	assert.NotPanics(t, func() {
		Tokenize("/* never closed")
		Tokenize("\"never closed")
		Tokenize("a /")
	})
	assert.Equal(t, []string{"$", "/"}, Tokenize("a /"))
}

func TestTokenizeIdentifiersAndNumbers(t *testing.T) {
	assert.Equal(t, []string{"$", "$", "$", "0", "0"}, Tokenize("foo _bar $baz 0x1F 1_000"))
	assert.Equal(t, []string{"$"}, Tokenize("héllo"))
}

// synthetic builds a function-like source of n lines whose token shape depends
// on the line index, with identifiers taken from names.
func synthetic(n int, names func(i int) string) string {
	var b strings.Builder
	b.WriteString("func f() {\n")
	for i := 0; i < n; i++ {
		id := names(i)
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "\tif %s > %d { %s = %s + %d }\n", id, i, id, id, i)
		case 1:
			fmt.Fprintf(&b, "\tfor j := 0; j < %d; j++ { %s[j] = call(%s, \"s%d\") }\n", i, id, id, i)
		case 2:
			fmt.Fprintf(&b, "\tswitch %s { case %d: return %s; default: break }\n", id, i, id)
		default:
			fmt.Fprintf(&b, "\t%s, err := run(%s); if err != nil { return err }\n", id, id)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func TestFingerprintsTooShort(t *testing.T) {
	assert.Nil(t, Fingerprints("func f() { return 1 }"))
	assert.Nil(t, Fingerprints(""))
	assert.Nil(t, Fingerprints(strings.Repeat("a ", cloneMinTokens-1)))
	assert.NotNil(t, Fingerprints(strings.Repeat("a + ", cloneMinTokens)))
}

func TestFingerprintsDeterministicAndSorted(t *testing.T) {
	src := synthetic(30, func(i int) string { return fmt.Sprintf("v%d", i) })
	a := Fingerprints(src)
	b := Fingerprints(src)
	require.NotEmpty(t, a)
	assert.Equal(t, a, b)
	for i := 1; i < len(a); i++ {
		assert.Less(t, a[i-1], a[i], "sorted and unique")
	}
}

func TestFingerprintsIgnoreRenaming(t *testing.T) {
	a := Fingerprints(synthetic(30, func(i int) string { return fmt.Sprintf("alpha%d", i) }))
	b := Fingerprints(synthetic(30, func(i int) string { return fmt.Sprintf("zz_%d", i*7) }))
	assert.Equal(t, a, b)
}

func TestCloneIndexClosestFindsRenamedCopy(t *testing.T) {
	original := synthetic(30, func(i int) string { return fmt.Sprintf("alpha%d", i) })
	renamed := synthetic(30, func(i int) string { return fmt.Sprintf("other%d", i) })
	idx := NewCloneIndex()
	idx.Add("a/orig.go", "Original", 10, original)
	idx.Add("a/short.go", "Short", 1, "func s() {}") // ignored
	assert.Equal(t, 1, idx.Len())

	match := idx.Closest("b/copy.go", "Copy", renamed)
	require.NotNil(t, match)
	assert.GreaterOrEqual(t, match.Similarity, 0.9)
	assert.Equal(t, "a/orig.go", match.Path)
	assert.Equal(t, "Original", match.Function)
	assert.Equal(t, 10, match.Line)
}

func TestCloneIndexClosestExcludesItself(t *testing.T) {
	src := synthetic(30, func(i int) string { return fmt.Sprintf("a%d", i) })
	idx := NewCloneIndex()
	idx.Add("a.go", "F", 1, src)
	assert.Nil(t, idx.Closest("a.go", "F", src))
	// same name in another file is a real candidate
	require.NotNil(t, idx.Closest("b.go", "F", src))
	// same path, other function is a real candidate
	require.NotNil(t, idx.Closest("a.go", "G", src))
}

func TestCloneIndexClosestUnrelated(t *testing.T) {
	idx := NewCloneIndex()
	idx.Add("a.go", "F", 1, synthetic(30, func(i int) string { return fmt.Sprintf("a%d", i) }))
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "x[%d] <- y.z(%d) ; w ?? q ;\n", i, i)
	}
	assert.Nil(t, idx.Closest("c.go", "U", b.String()))
	assert.Nil(t, idx.Closest("c.go", "U", "short"))
}

func TestCloneIndexNilSafe(t *testing.T) {
	var idx *CloneIndex
	assert.Equal(t, 0, idx.Len())
	assert.Nil(t, idx.Closest("a", "b", synthetic(30, func(i int) string { return "v" })))
}

func TestCloneIndexPicksMostSimilar(t *testing.T) {
	base := synthetic(40, func(i int) string { return fmt.Sprintf("v%d", i) })
	// a variant with its last third replaced by a different shape
	lines := strings.Split(base, "\n")
	variant := strings.Join(lines[:27], "\n") + "\n" + strings.Repeat("q ?? w <- e ; r ;\n", 14)
	idx := NewCloneIndex()
	idx.Add("v.go", "Variant", 1, variant)
	idx.Add("e.go", "Exact", 1, base)
	match := idx.Closest("n.go", "New", base)
	require.NotNil(t, match)
	assert.Equal(t, "Exact", match.Function)
	assert.InDelta(t, 1.0, match.Similarity, 1e-9)
}

func TestSliceLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	assert.Equal(t, "b\nc", sliceLines(lines, 2, 3))
	assert.Equal(t, "a\nb", sliceLines(lines, -5, 2))
	assert.Equal(t, "c\nd", sliceLines(lines, 3, 99))
	assert.Equal(t, "", sliceLines(lines, 3, 2))
}

func TestFunctionBody(t *testing.T) {
	assert.Equal(t, "{ return a }", functionBody("func f(a int) int { return a }"))
	assert.Equal(t, "{\n  return 1;\n}", functionBody("public function __construct(\n  private int $a,\n  private int $b,\n)\n{\n  return 1;\n}"))
	assert.Equal(t, "    return a\n", functionBody("def f(a: int) -> int:\n    return a\n"))
	assert.Equal(t, "    return a\n", functionBody("@decorated\ndef f(\n    a,\n    b,\n):\n    return a\n"))
	assert.Equal(t, "{}) -> None:\n    return a\n", functionBody("def f(a={}) -> None:\n    return a\n"), "a brace in the signature costs a few tokens, nothing more")
	assert.Equal(t, "no body here", functionBody("no body here"))
}

func TestPromotedConstructorsAreNotClones(t *testing.T) {
	a := "public function __construct(\n" + strings.Repeat("    public readonly int $x,\n", 12) + ") {}"
	b := "public function __construct(\n" + strings.Repeat("    public readonly string $y,\n", 12) + ") {}"
	require.Greater(t, len(Tokenize(a)), cloneMinTokens, "the whole declaration is long enough to be indexed")
	idx := NewCloneIndex()
	idx.Add("a.php", "__construct", 1, a)
	assert.Equal(t, 0, idx.Len(), "an empty body is not indexed")
	assert.Nil(t, idx.Closest("b.php", "__construct", b))
}
