package cribelo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hunk(added, removed []string) Hunk {
	return Hunk{Start: 10, End: 10 + maxInt(len(added), 1) - 1, OldStart: 10, OldEnd: 10 + maxInt(len(removed), 1) - 1, Added: added, Removed: removed}
}

func natureOf(t *testing.T, language string, h Hunk) HunkReport {
	t.Helper()
	// The identifiers of the file are those of the hunk: a renamed name is
	// gone after, and was absent before. The hunk is given twice, so that
	// the change vouches for its renames.
	idents := identifierSets{base: identifiers([]byte(strings.Join(h.Removed, "\n"))), head: identifiers([]byte(strings.Join(h.Added, "\n")))}
	files := []FileReport{{Hunks: classifyHunks(Change{Path: "x", Hunks: []Hunk{h, h}}, language, nil, idents)}}
	resolveRenames(files)
	require.Len(t, files[0].Hunks, 2)
	return files[0].Hunks[0]
}

func TestClassifyHunksImports(t *testing.T) {
	cases := map[string]Hunk{
		"Golang":     hunk([]string{`import (`, `	"fmt"`, `	log "github.com/sirupsen/logrus"`, `)`}, []string{`import "fmt"`}),
		"PHP":        hunk([]string{`use App\Billing\Invoice;`, `use function strlen;`}, nil),
		"Python":     hunk([]string{`from os import path, sep`, `import json as j`}, []string{`import os`}),
		"Rust":       hunk([]string{`use std::collections::{HashMap, HashSet};`, `pub(crate) use crate::x::Y;`}, nil),
		"TypeScript": hunk([]string{`import { a, b } from './x'`, `import type { T } from "./t";`, `const fs = require('fs')`}, nil),
		"Java":       hunk([]string{`import static org.junit.Assert.*;`, `import java.util.List;`}, nil),
		"C#":         hunk([]string{`using System.Linq;`, `global using Xunit;`}, []string{`using System;`}),
	}
	for language, h := range cases {
		r := natureOf(t, language, h)
		assert.Equal(t, NatureImports, r.Nature, language)
		assert.Equal(t, 0, r.ReadCost(), language)
	}
	r := natureOf(t, "Golang", hunk([]string{`import "fmt"`, `var x = 1`}, nil))
	assert.Equal(t, NatureBehavior, r.Nature, "a declaration among imports is read")
	r = natureOf(t, "Golang", hunk([]string{``, `	`}, nil))
	assert.NotEqual(t, NatureImports, r.Nature, "blank lines alone are not imports")
}

func TestClassifyHunksFormatAndComments(t *testing.T) {
	r := natureOf(t, "Golang", hunk([]string{`if a {`, `	b()`, `}`}, []string{`if a { b() }`}))
	assert.Equal(t, NatureFormat, r.Nature)
	assert.Equal(t, 0, r.ReadCost())

	r = natureOf(t, "Golang", hunk([]string{`// Process runs the thing.`, ``}, nil))
	assert.Equal(t, NatureComment, r.Nature, "comment lines added")

	r = natureOf(t, "PHP", hunk([]string{`$x = 1; // one`}, []string{`$x = 1; /* uno */`}))
	assert.Equal(t, NatureComment, r.Nature, "comment changed next to unchanged code")

	r = natureOf(t, "Golang", hunk([]string{`x := 2`}, []string{`x := 1`}))
	assert.Equal(t, NatureBehavior, r.Nature, "a literal changed")
}

func TestClassifyHunksCommentOnACodeLine(t *testing.T) {
	// A comment added at the end of a line of code is a comment hunk, not
	// a formatting one, in every comment syntax.
	assert.Equal(t, NatureComment, natureOf(t, "Python", hunk([]string{"x = f(1)  # noqa: E501"}, []string{"x = f(1)"})).Nature)
	assert.Equal(t, NatureComment, natureOf(t, "Golang", hunk([]string{"\tx := f(1) // the answer"}, []string{"\tx := f(1) // an answer"})).Nature)
	assert.Equal(t, NatureFormat, natureOf(t, "Golang", hunk([]string{"\tx := f(1)"}, []string{"x :=   f(1)"})).Nature)
	assert.Equal(t, NatureFormat, natureOf(t, "Golang", hunk([]string{"\tx := f(1,", "\t\t2)"}, []string{"\tx := f(1, 2)"})).Nature)
}

func TestClassifyHunksRename(t *testing.T) {
	r := natureOf(t, "Golang", hunk([]string{`total := sum(items)`, `return total`}, []string{`t := sum(items)`, `return t`}))
	assert.Equal(t, NatureRename, r.Nature)
	assert.Equal(t, "t → total", r.Detail)
	assert.Equal(t, 1, r.ReadCost())

	r = natureOf(t, "Golang", hunk([]string{`return a + b`}, []string{`return a - b`}))
	assert.Equal(t, NatureBehavior, r.Nature, "an operator changed")

	// The old name still lives elsewhere in the file: another variable is
	// put to use, not a rename.
	h := hunk([]string{`delete(c.series, h)`}, []string{`delete(c.series, e)`})
	idents := identifierSets{base: identifiers([]byte("e h c series")), head: identifiers([]byte("e h c series"))}
	files := []FileReport{{Hunks: classifyHunks(Change{Path: "x", Hunks: []Hunk{h, h}}, "Golang", nil, idents)}}
	resolveRenames(files)
	assert.Equal(t, NatureBehavior, files[0].Hunks[0].Nature)

	// A pair seen once in the whole change may be another method called:
	// it is read.
	h = hunk([]string{`m.removeLink(k)`}, []string{`m.removeSubNode(k)`})
	idents = identifierSets{base: identifiers([]byte(strings.Join(h.Removed, "\n"))), head: identifiers([]byte(strings.Join(h.Added, "\n")))}
	files = []FileReport{{Hunks: classifyHunks(Change{Path: "x", Hunks: []Hunk{h}}, "PHP", nil, idents)}}
	resolveRenames(files)
	assert.Equal(t, NatureBehavior, files[0].Hunks[0].Nature)
	// The same pair in another file of the change vouches for it.
	other := []FileReport{{Hunks: classifyHunks(Change{Path: "x", Hunks: []Hunk{h}}, "PHP", nil, idents)}, {Hunks: classifyHunks(Change{Path: "y", Hunks: []Hunk{h}}, "PHP", nil, idents)}}
	resolveRenames(other)
	assert.Equal(t, NatureRename, other[0].Hunks[0].Nature)
	assert.Equal(t, NatureRename, other[1].Hunks[0].Nature)
	assert.Equal(t, 1, other[1].ToRead)

	// Inconsistent mapping inside the hunk.
	r = natureOf(t, "Golang", hunk([]string{`x := f(a, b)`}, []string{`y := f(a, a)`}))
	assert.Equal(t, NatureBehavior, r.Nature)

	// Without the file's identifiers, no hunk is a rename.
	files = []FileReport{{Hunks: classifyHunks(Change{Path: "x", Hunks: []Hunk{hunk([]string{`return total`}, []string{`return t`}), hunk([]string{`return total`}, []string{`return t`})}}, "Golang", nil, identifierSets{})}}
	resolveRenames(files)
	assert.Equal(t, NatureBehavior, files[0].Hunks[0].Nature)
}

func TestClassifyHunksImportContinuationsNeedAStatement(t *testing.T) {
	r := natureOf(t, "Python", hunk([]string{`    return`}, nil))
	assert.Equal(t, NatureBehavior, r.Nature, "a lone word is not an import")
	r = natureOf(t, "Python", hunk([]string{`from x import (`, `    a,`, `    b,`, `)`}, nil))
	assert.Equal(t, NatureImports, r.Nature)
	r = natureOf(t, "Rust", hunk([]string{`}`}, nil))
	assert.Equal(t, NatureBehavior, r.Nature)
}

func TestClassifyHunksAdditionIsBehavior(t *testing.T) {
	r := natureOf(t, "Golang", hunk([]string{`if err != nil {`, `	return err`, `}`}, nil))
	assert.Equal(t, NatureBehavior, r.Nature)
	assert.Equal(t, 3, r.ReadCost())
	assert.Equal(t, 3, r.Lines())
}

func TestCountNaturesAndLabels(t *testing.T) {
	counts := countNatures([]HunkReport{{Nature: NatureMoved, Added: 10}, {Nature: NatureMoved, Added: 5}, {Nature: NatureBehavior, Added: 2, Deleted: 1}})
	assert.Equal(t, natureCount{Hunks: 2, Lines: 15}, counts[NatureMoved])
	assert.Equal(t, natureCount{Hunks: 1, Lines: 3}, counts[NatureBehavior])
	assert.Equal(t, "2 moves", natureLabel(NatureMoved, 2))
	assert.Equal(t, "1 extraction", natureLabel(NatureExtracted, 1))
	assert.Equal(t, "", natureLabel(NatureBehavior, 3))
}

// parsedTree parses a set of Go files written under a fresh directory.
func parsedTree(t *testing.T, files map[string]string) *Parsed {
	t.Helper()
	dir := t.TempDir()
	rels := make([]string, 0, len(files))
	for rel, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644))
		rels = append(rels, rel)
	}
	parsed, err := parseFiles(dir, rels)
	require.NoError(t, err)
	return parsed
}

const movedBody = `
	total := 0
	for _, item := range items {
		if item.Quantity > 0 {
			total += item.Price * item.Quantity
		}
	}
	if total > 1000 {
		total = total - total/10
	}
	return total
}
`

func TestMoveIndexAcrossFiles(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"old/cart.go": "package old\n\ntype Item struct{ Price, Quantity int }\n\nfunc Total(items []Item) int {" + movedBody,
	})
	head := parsedTree(t, map[string]string{
		"old/cart.go":    "package old\n\ntype Item struct{ Price, Quantity int }\n",
		"new/pricing.go": "package pricing\n\ntype Item struct{ Price, Quantity int }\n\n// Total sums the cart.\nfunc Total(items []Item) int {" + movedBody,
	})
	changes := []Change{{Path: "old/cart.go", Status: 'M'}, {Path: "new/pricing.go", Status: 'A'}}
	idx := buildMoveIndex(head, base, changes, nil)
	require.Len(t, idx.Moves, 1)
	assert.Equal(t, Move{Nature: NatureMoved, From: "old/cart.go", FromName: "Total", To: "new/pricing.go", ToName: "Total", Lines: 12, Same: 1}, idx.Moves[0])

	// The hunk that adds the function to the new file, doc comment included.
	added := append([]string{"// Total sums the cart."}, splitLines("func Total(items []Item) int {"+movedBody)...)
	h := Hunk{Start: 5, End: 5 + len(added) - 1, OldStart: 4, OldEnd: 4, Added: added}
	nature, detail, fresh, ok := idx.hunkNature("new/pricing.go", h)
	assert.True(t, ok)
	assert.Equal(t, NatureMoved, nature)
	assert.Equal(t, "moved from old/cart.go:Total()", detail)
	assert.Empty(t, fresh)

	// The hunk that removes it from the old file.
	removed := splitLines("func Total(items []Item) int {" + movedBody)
	h = Hunk{Start: 4, End: 4, OldStart: 5, OldEnd: 5 + len(removed) - 1, Removed: removed}
	nature, detail, _, ok = idx.hunkNature("old/cart.go", h)
	assert.True(t, ok)
	assert.Equal(t, NatureMoved, nature)
	assert.Equal(t, "moved to new/pricing.go:Total()", detail)

	// A hunk that adds a new line on top of the moved code is read.
	h = Hunk{Start: 5, End: 5 + len(added), OldStart: 4, OldEnd: 4, Added: append([]string{"var discount = 10"}, added...)}
	_, _, _, ok = idx.hunkNature("new/pricing.go", h)
	assert.False(t, ok)

	// A line of the moved function that changed on the way is read.
	edited := append([]string{}, added...)
	edited[8] = "\tif total > 2000 {"
	h = Hunk{Start: 5, End: 5 + len(edited) - 1, OldStart: 4, OldEnd: 4, Added: edited}
	_, _, _, ok = idx.hunkNature("new/pricing.go", h)
	assert.False(t, ok)
}

func TestMoveIndexFromDeletedFileWithRenames(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"legacy/total.go": "package legacy\n\ntype Item struct{ Price, Quantity int }\n\nfunc computeTotal(items []Item) int {" + movedBody,
	})
	renamed := "package cart\n\ntype Item struct{ Price, Quantity int }\n\nfunc Total(lines []Item) int {\n\tsum := 0\n\tfor _, line := range lines {\n\t\tif line.Quantity > 0 {\n\t\t\tsum += line.Price * line.Quantity\n\t\t}\n\t}\n\tif sum > 1000 {\n\t\tsum = sum - sum/10\n\t}\n\treturn sum\n}\n"
	head := parsedTree(t, map[string]string{"cart/total.go": renamed})
	idx := buildMoveIndex(head, base, []Change{{Path: "cart/total.go", Status: 'A'}}, []Change{{Path: "legacy/total.go", Status: 'D'}})
	require.Len(t, idx.Moves, 1)
	assert.Equal(t, "legacy/total.go", idx.Moves[0].From)
	assert.Equal(t, "computeTotal", idx.Moves[0].FromName)
	assert.Equal(t, "Total", idx.Moves[0].ToName)
	assert.Equal(t, 1.0, idx.Moves[0].Same, "renamed identifiers still make the same function")
}

func TestMoveIndexExtraction(t *testing.T) {
	long := `package p

func Handle(r Request) Response {
	if r.User == nil {
		return Response{Code: 401}
	}
	if !r.User.Active {
		return Response{Code: 403}
	}
	total := 0
	for _, item := range r.Items {
		if item.Quantity > 0 {
			total += item.Price * item.Quantity
		}
	}
	if total > 1000 {
		total = total - total/10
	}
	if total > r.User.Credit {
		return Response{Code: 402}
	}
	return Response{Code: 200, Total: total}
}
`
	split := `package p

func Handle(r Request) Response {
	if r.User == nil {
		return Response{Code: 401}
	}
	if !r.User.Active {
		return Response{Code: 403}
	}
	total := price(r.Items)
	if total > r.User.Credit {
		return Response{Code: 402}
	}
	return Response{Code: 200, Total: total}
}

func price(items []Item) int {
	total := 0
	for _, item := range items {
		if item.Quantity > 0 {
			total += item.Price * item.Quantity
		}
	}
	if total > 1000 {
		total = total - total/10
	}
	return total
}
`
	base := parsedTree(t, map[string]string{"p/h.go": long})
	head := parsedTree(t, map[string]string{"p/h.go": split})
	idx := buildMoveIndex(head, base, []Change{{Path: "p/h.go", Status: 'M'}}, nil)
	require.Len(t, idx.Moves, 1)
	assert.Equal(t, NatureExtracted, idx.Moves[0].Nature)
	assert.Equal(t, "Handle", idx.Moves[0].FromName)
	assert.Equal(t, "price", idx.Moves[0].ToName)
	assert.GreaterOrEqual(t, idx.Moves[0].Same, extractMinContained)
	spans := idx.arrived["p/h.go"]
	require.Len(t, spans, 1)
	assert.Contains(t, spans[0].detail, "extracted from Handle()")
}

func TestMoveIndexIgnoresShortAndCopiedFunctions(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"a.go": "package a\n\nfunc Get(x int) int { return x }\n\nfunc Total(items []Item) int {" + movedBody,
	})
	head := parsedTree(t, map[string]string{
		"a.go": "package a\n\nfunc Get(x int) int { return x }\n\nfunc Total(items []Item) int {" + movedBody,
		"b.go": "package a\n\nfunc Get2(x int) int { return x }\n\nfunc Total2(items []Item) int {" + movedBody,
	})
	idx := buildMoveIndex(head, base, []Change{{Path: "a.go", Status: 'M'}, {Path: "b.go", Status: 'A'}}, nil)
	assert.Empty(t, idx.Moves, "a copy whose source stays is a clone, not a move; a one-line accessor is never matched")
}

func splitLines(s string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// A moved function whose lines keep their shape but call something the
// original never named was edited on the way: the hunk is read, and the
// new name is told, unless the change renamed that thing everywhere.
func TestMovedHunkWithNewNameIsRead(t *testing.T) {
	body := func(call string) string {
		return "\n\ttotal := 0\n\tfor _, item := range items {\n\t\tif item.Quantity > 0 {\n\t\t\ttotal += item.Price * item.Quantity\n\t\t}\n\t}\n\tif total > 1000 {\n\t\ttotal = " + call + "(total)\n\t}\n\treturn total\n}\n"
	}
	base := parsedTree(t, map[string]string{
		"old/cart.go": "package old\n\ntype Item struct{ Price, Quantity int }\n\nfunc rebate(n int) int { return n - n/10 }\n\nfunc Total(items []Item) int {" + body("rebate"),
	})
	head := parsedTree(t, map[string]string{
		"old/cart.go":    "package old\n\ntype Item struct{ Price, Quantity int }\n",
		"new/pricing.go": "package pricing\n\ntype Item struct{ Price, Quantity int }\n\nfunc discount(n int) int { return n - n/5 }\n\nfunc Total(items []Item) int {" + body("discount"),
	})
	changes := []Change{{Path: "old/cart.go", Status: 'M'}, {Path: "new/pricing.go", Status: 'A'}}
	idx := buildMoveIndex(head, base, changes, nil)
	require.Len(t, idx.Moves, 1)

	added := splitLines("func Total(items []Item) int {" + body("discount"))
	h := Hunk{Start: 7, End: 7 + len(added) - 1, OldStart: 4, OldEnd: 4, Added: added}
	nature, _, fresh, ok := idx.hunkNature("new/pricing.go", h)
	require.True(t, ok)
	assert.Equal(t, NatureMoved, nature)
	assert.Equal(t, []string{"discount"}, fresh)

	files := []FileReport{{Path: "new/pricing.go", Hunks: []HunkReport{{Start: 7, End: 7 + len(added) - 1, Added: len(added), Nature: NatureMoved, Detail: "moved from old/cart.go:Total()", fresh: fresh}}}}
	resolveRenames(files)
	assert.Equal(t, NatureBehavior, files[0].Hunks[0].Nature)
	assert.Equal(t, "moved from old/cart.go:Total(); new: discount", files[0].Hunks[0].Detail)
	assert.Equal(t, len(added), files[0].ToRead)

	// The same hunk stays a move when the change renamed rebate to discount.
	files = []FileReport{
		{Path: "new/pricing.go", Hunks: []HunkReport{{Start: 7, End: 7 + len(added) - 1, Added: len(added), Nature: NatureMoved, Detail: "moved from old/cart.go:Total()", fresh: fresh}}},
		{Path: "a.go", Hunks: []HunkReport{{Added: 1, Deleted: 1, renames: []string{"rebate → discount"}}, {Added: 1, Deleted: 1, renames: []string{"rebate → discount"}}}},
	}
	resolveRenames(files)
	assert.Equal(t, NatureMoved, files[0].Hunks[0].Nature)
	assert.Equal(t, NatureRename, files[1].Hunks[0].Nature)
}
