package cribelo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNameStatus(t *testing.T) {
	out := "A\x00new.go\x00M\x00mod.go\x00D\x00gone.go\x00R095\x00old/name.go\x00new/name.go\x00T\x00typechg.go\x00"
	got := parseNameStatus(out)
	require.Len(t, got, 5)
	assert.Equal(t, Change{Status: 'A', Path: "new.go"}, got[0])
	assert.Equal(t, Change{Status: 'M', Path: "mod.go"}, got[1])
	assert.Equal(t, Change{Status: 'D', Path: "gone.go"}, got[2])
	assert.Equal(t, Change{Status: 'R', OldPath: "old/name.go", Path: "new/name.go"}, got[3])
	assert.Equal(t, Change{Status: 'M', Path: "typechg.go"}, got[4], "unknown codes collapse to M")
}

func TestParseNameStatusCopyAndTruncated(t *testing.T) {
	got := parseNameStatus("C100\x00a.go\x00b.go\x00")
	require.Len(t, got, 1)
	assert.Equal(t, byte('R'), got[0].Status)

	assert.Empty(t, parseNameStatus(""))
	// truncated input does not panic and keeps what was complete
	got = parseNameStatus("A\x00a.go\x00R100\x00only-old.go\x00")
	require.Len(t, got, 1)
	assert.Equal(t, "a.go", got[0].Path)
	got = parseNameStatus("A\x00a.go\x00M\x00")
	require.Len(t, got, 1)
}

func TestParseNumstat(t *testing.T) {
	out := "3\t1\tplain.go\x00" +
		"-\t-\timage.png\x00" +
		"2\t5\t\x00old.go\x00renamed.go\x00" +
		"0\t0\tlast.go\x00"
	stats := parseNumstat(out)
	assert.Equal(t, numstat{added: 3, deleted: 1}, stats["plain.go"])
	assert.Equal(t, numstat{binary: true}, stats["image.png"])
	assert.Equal(t, numstat{added: 2, deleted: 5}, stats["renamed.go"])
	_, hasOld := stats["old.go"]
	assert.False(t, hasOld)
	assert.Equal(t, numstat{}, stats["last.go"])
	assert.Len(t, stats, 4)
}

func TestParseNumstatIgnoresGarbage(t *testing.T) {
	assert.Empty(t, parseNumstat(""))
	assert.Empty(t, parseNumstat("garbage\x00"))
}

func TestApplyNumstat(t *testing.T) {
	changes := []Change{{Path: "a.go"}, {Path: "b.png"}, {Path: "c.go"}}
	applyNumstat(changes, map[string]numstat{"a.go": {added: 4, deleted: 2}, "b.png": {binary: true}})
	assert.Equal(t, 4, changes[0].Added)
	assert.Equal(t, 2, changes[0].Deleted)
	assert.True(t, changes[1].Binary)
	assert.Equal(t, 0, changes[2].Added)
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		line string
		want Hunk
		ok   bool
	}{
		{"@@ -1,3 +4,6 @@", Hunk{4, 9}, true},
		{"@@ -1,3 +7 @@ func foo() {", Hunk{7, 7}, true},
		{"@@ -10,4 +9,0 @@", Hunk{9, 9}, true},
		{"@@ -0,0 +1,2 @@", Hunk{1, 2}, true},
		{"@@ -1 +1 @@", Hunk{1, 1}, true},
		{"@@ nonsense", Hunk{}, false},
		{"@@ -1,3 @@", Hunk{}, false},
		{"@@ -1,3 -4,6 @@", Hunk{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			h, ok := parseHunkHeader(tt.line)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, h)
			}
		})
	}
}

const realisticDiff = `diff --git a/a.go b/a.go
index 1111111..2222222 100644
--- a/a.go
+++ b/a.go
@@ -3,0 +4,2 @@ func x() {
+	a := 1
+	b := 2
@@ -20 +22,3 @@ func y() {
-	old
+	new1
+	new2
+	new3
diff --git a/removed.go b/removed.go
deleted file mode 100644
index 3333333..0000000
--- a/removed.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package x
diff --git a/dir/b.go b/dir/b.go
new file mode 100644
--- /dev/null
+++ b/dir/b.go
@@ -0,0 +1,10 @@
+package b
@@ -30,2 +29,0 @@ func z() {
`

func TestParseHunks(t *testing.T) {
	hunks := parseHunks(realisticDiff)
	assert.Equal(t, []Hunk{{4, 5}, {22, 24}}, hunks["a.go"])
	_, hasRemoved := hunks["removed.go"]
	assert.False(t, hasRemoved, "/dev/null target is not a file")
	assert.Equal(t, []Hunk{{1, 10}, {29, 29}}, hunks["dir/b.go"])
	assert.Len(t, hunks, 2)
}

func TestParseHunksStripsTrailingTab(t *testing.T) {
	hunks := parseHunks("+++ b/with space.go\t\n@@ -1 +1 @@\n")
	assert.Equal(t, []Hunk{{1, 1}}, hunks["with space.go"])
}

func TestApplyHunks(t *testing.T) {
	changes := []Change{{Path: "a.go"}, {Path: "z.go"}}
	applyHunks(changes, map[string][]Hunk{"a.go": {{1, 2}}})
	assert.Equal(t, []Hunk{{1, 2}}, changes[0].Hunks)
	assert.Nil(t, changes[1].Hunks)
}

func TestTouches(t *testing.T) {
	hunks := []Hunk{{10, 12}, {30, 30}}
	assert.True(t, touches(hunks, 1, 10), "end edge")
	assert.True(t, touches(hunks, 12, 20), "start edge")
	assert.True(t, touches(hunks, 11, 11), "inside")
	assert.True(t, touches(hunks, 25, 35), "contains hunk")
	assert.True(t, touches(hunks, 30, 30))
	assert.False(t, touches(hunks, 1, 9))
	assert.False(t, touches(hunks, 13, 29))
	assert.False(t, touches(hunks, 31, 40))
	assert.False(t, touches(nil, 1, 100))
}

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := dir + "/" + name
		require.NoError(t, writeFile(p, content))
		return p
	}
	assert.Equal(t, 0, countLines(write("empty", "")))
	assert.Equal(t, 2, countLines(write("two", "a\nb\n")))
	assert.Equal(t, 2, countLines(write("noeol", "a\nb")))
	assert.Equal(t, 0, countLines(dir+"/missing"))
}
