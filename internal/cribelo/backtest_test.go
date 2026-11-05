package cribelo

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBacktestEndToEnd(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "go.mod", "module example.com/bt\n\ngo 1.21\n")
	files := []string{"pkg/f1.go", "pkg/f2.go", "pkg/f3.go", "pkg/f4.go", "pkg/f5.go"}
	for i, f := range files {
		put(t, dir, f, fmt.Sprintf("package pkg\n\nfunc Base%d() int { return %d }\n", i+1, i))
	}
	git(t, dir, "2024-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-01-01T10:00:00Z", "commit", "-q", "-m", "initial")

	base := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
	for c := 1; c <= 8; c++ {
		date := base.AddDate(0, 0, 30*(c-1)).Format(time.RFC3339)
		for i, f := range files {
			body := fmt.Sprintf("package pkg\n\nfunc Base%d() int { return %d }\n", i+1, i)
			for k := 1; k <= c; k++ {
				body += fmt.Sprintf("\nfunc Step%d_%d(x int) int {\n\tif x > %d {\n\t\treturn x\n\t}\n\treturn %d\n}\n", i+1, k, k, k)
			}
			put(t, dir, f, body)
		}
		git(t, dir, date, "add", "-A")
		git(t, dir, date, "commit", "-q", "-m", fmt.Sprintf("feat: step %d", c))
	}
	// last feature commit is on 2024-08-29; a fix lands ten days later on f1
	fixDate := base.AddDate(0, 0, 30*7+10).Format(time.RFC3339)
	put(t, dir, "pkg/f1.go", "package pkg\n\nfunc Base1() int { return 42 }\n")
	git(t, dir, fixDate, "add", "-A")
	git(t, dir, fixDate, "commit", "-q", "-m", "fix: wrong value in Base1")

	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var progress []int
	bt, err := RunBacktest(repo, BacktestOptions{
		Ref: "main", Max: 50, Top: 3, MinFiles: 4, Now: now,
		Progress: func(done, total int) { progress = append(progress, done) },
	})
	require.NoError(t, err)

	assert.Equal(t, 9, bt.Landings, "9 commits after the root")
	assert.Len(t, bt.Changes, 8)
	assert.Equal(t, 1, bt.LeftOut["fewer than 4 code files"], "the fix touches one file")
	total := len(bt.Changes)
	for _, n := range bt.LeftOut {
		total += n
	}
	assert.Equal(t, bt.Landings, total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, progress)
	assert.Equal(t, MethodologyVersion, bt.Methodology)
	assert.Equal(t, "generic", bt.Weights)
	assert.Equal(t, bt.Changes[0].Time, bt.From)
	assert.Equal(t, bt.Changes[len(bt.Changes)-1].Time, bt.To)
	assert.Less(t, bt.From, bt.To)

	fixedSeen := false
	for _, ch := range bt.Changes {
		assert.Len(t, ch.Files, 5)
		for _, s := range ch.Files {
			if s.Path == "pkg/f1.go" && s.Fixed {
				fixedSeen = true
			}
			if s.Path != "pkg/f1.go" {
				assert.False(t, s.Fixed, s.Path)
			}
		}
	}
	assert.True(t, fixedSeen, "f1.go is marked fixed in a landing within 90 days of the fix")
	// the most recent feature landing precedes the fix by ten days
	last := bt.Changes[len(bt.Changes)-1]
	for _, s := range last.Files {
		assert.Equal(t, s.Path == "pkg/f1.go", s.Fixed)
	}
	// the first landing (Feb 1) is more than 90 days before the fix
	for _, s := range bt.Changes[0].Files {
		assert.False(t, s.Fixed)
	}
	assert.GreaterOrEqual(t, bt.Generic.FixedFiles, 1)
	assert.Equal(t, 8, bt.Generic.Changes)
	assert.Equal(t, 40, bt.Generic.Files)

	js, err := bt.JSON()
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(js)))
	assert.Contains(t, bt.Text(), "cribelo backtest")
}

func TestRunBacktestTooRecent(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "a.go", "package a\n")
	git(t, dir, "2024-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-01-01T10:00:00Z", "commit", "-q", "-m", "init")
	put(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	git(t, dir, "2024-01-20T10:00:00Z", "add", "-A")
	git(t, dir, "2024-01-20T10:00:00Z", "commit", "-q", "-m", "feat: a")
	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	bt, err := RunBacktest(repo, BacktestOptions{Ref: "main", Now: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	assert.Empty(t, bt.Changes)
	assert.Equal(t, 0, bt.Landings, "commits younger than the window are not even listed")
	assert.Empty(t, bt.LeftOut)
	assert.Contains(t, bt.Text(), "No landing qualified.")
}
