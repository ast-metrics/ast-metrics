package cribelo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeReport(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestLoadCoverageFormats(t *testing.T) {
	root := t.TempDir()
	writeReport(t, root, "go.mod", "module example.com/app\n")
	writeReport(t, root, "coverage.out", "mode: set\nexample.com/app/pkg/a.go:10.2,12.16 2 1\nexample.com/app/pkg/a.go:14.2,15.3 1 0\n")
	writeReport(t, root, "lcov.info", "TN:\nSF:"+filepath.Join(root, "src/b.ts")+"\nDA:3,1\nDA:4,0\nDA:5,0\nend_of_record\nSF:src/c.ts\nDA:1,2\nend_of_record\n")
	writeReport(t, root, "coverage.xml", `<?xml version="1.0"?><coverage><sources><source>`+root+`</source></sources><packages><package><classes><class filename="pkg/d.py"><lines><line number="7" hits="0"/><line number="8" hits="3"/></lines></class></classes></package></packages></coverage>`)
	writeReport(t, root, "clover.xml", `<?xml version="1.0"?><coverage generated="1"><project><file name="`+filepath.Join(root, "src/E.php")+`"><line num="20" count="0" type="method"/><line num="21" count="0" type="stmt"/><line num="22" count="4" type="stmt"/></file></project></coverage>`)

	cov, err := loadCoverage(root, "coverage.out")
	require.NoError(t, err)
	u, n := cov.uncovered("pkg/a.go", []Hunk{{Start: 9, End: 15}})
	assert.Equal(t, [2]int{2, 5}, [2]int{u, n}, "lines 14 and 15 of the five instrumented ones are not run")

	cov, err = loadCoverage(root, "lcov.info")
	require.NoError(t, err)
	u, n = cov.uncovered("src/b.ts", []Hunk{{Start: 3, End: 5}})
	assert.Equal(t, [2]int{2, 3}, [2]int{u, n})
	u, n = cov.uncovered("src/c.ts", []Hunk{{Start: 1, End: 1}})
	assert.Equal(t, [2]int{0, 1}, [2]int{u, n})

	cov, err = loadCoverage(root, filepath.Join(root, "coverage.xml"))
	require.NoError(t, err, "an absolute path is read as it is")
	assert.Equal(t, "coverage.xml", cov.Source)
	cov, err = loadCoverage(root, "coverage.xml")
	require.NoError(t, err)
	u, n = cov.uncovered("pkg/d.py", []Hunk{{Start: 1, End: 10}})
	assert.Equal(t, [2]int{1, 2}, [2]int{u, n})

	cov, err = loadCoverage(root, "clover.xml")
	require.NoError(t, err)
	u, n = cov.uncovered("src/E.php", []Hunk{{Start: 20, End: 22}})
	assert.Equal(t, [2]int{1, 2}, [2]int{u, n}, "the method line is not counted")

	assert.Equal(t, "coverage.out", findCoverage(root))
	writeReport(t, root, "notes.txt", "hello")
	_, err = loadCoverage(root, "notes.txt")
	assert.Error(t, err)
}

func TestCoverageFacts(t *testing.T) {
	cov := &Coverage{Source: "lcov.info", files: map[string]*fileCoverage{"a.go": newFileCoverage(), "b.go": newFileCoverage()}}
	for n := 1; n <= 10; n++ {
		cov.files["a.go"].mark(n, n%2)
		cov.files["b.go"].mark(n, 1)
	}
	files := []FileReport{{Path: "a.go", Read: true}, {Path: "b.go", Read: true}, {Path: "c.go", Read: true}, {Path: "a_test.go", Read: true, Test: true}}
	changes := map[string]Change{"a.go": {Hunks: []Hunk{{Start: 1, End: 6}}}, "b.go": {Hunks: []Hunk{{Start: 1, End: 6}}}}
	facts := coverageFacts(cov, files, changes)
	require.Len(t, facts, 1)
	assert.Equal(t, "a.go", facts[0].Path)
	assert.Equal(t, "3 of 6 changed lines not run by any test, according to lcov.info", facts[0].Text)
	assert.Nil(t, coverageFacts(nil, files, changes))

	// Two uncovered lines out of thirty are not worth a line of the plan; ten are.
	wide := func(uncovered int) *Coverage {
		c := &Coverage{Source: "lcov.info", files: map[string]*fileCoverage{"a.go": newFileCoverage()}}
		for n := 1; n <= 30; n++ {
			hits := 1
			if n <= uncovered {
				hits = 0
			}
			c.files["a.go"].mark(n, hits)
		}
		return c
	}
	changes = map[string]Change{"a.go": {Hunks: []Hunk{{Start: 1, End: 30}}}}
	assert.Empty(t, coverageFacts(wide(2), files, changes))
	require.Len(t, coverageFacts(wide(10), files, changes), 1)
	assert.Equal(t, "10 of 30 changed lines not run by any test, according to lcov.info", coverageFacts(wide(10), files, changes)[0].Text)
}
