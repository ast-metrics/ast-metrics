package cribelo

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func capture(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestRunVersion(t *testing.T) {
	out := capture(t, func() {
		assert.Equal(t, 0, Run([]string{"cribelo", "--version"}, "1.2.3"))
	})
	assert.Contains(t, out, "1.2.3")
}

func TestRunOutsideRepositoryIsAnError(t *testing.T) {
	code := Run([]string{"cribelo", "-C", t.TempDir()}, "")
	assert.Equal(t, 2, code)
}

func TestRunUnknownFormat(t *testing.T) {
	code := Run([]string{"cribelo", "--format", "pdf", "-C", t.TempDir()}, "")
	assert.Equal(t, 2, code)
}

func TestPullRequestAddress(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	github := env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "o/r", "GITHUB_REF": "refs/pull/12/merge"})
	assert.Equal(t, "https://github.com/o/r/pull/12", pullRequestAddress("", github))
	assert.Equal(t, "https://example.com/x/y/pull/3", pullRequestAddress("https://example.com/x/y/pull/3", github), "the address given wins")
	assert.Equal(t, "", pullRequestAddress("none", github))

	push := env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "o/r", "GITHUB_REF": "refs/heads/main"})
	assert.Equal(t, "", pullRequestAddress("", push), "a push is not a pull request")

	gitlab := env(map[string]string{"CI_MERGE_REQUEST_PROJECT_URL": "https://gitlab.com/g/p", "CI_MERGE_REQUEST_IID": "7"})
	assert.Equal(t, "https://gitlab.com/g/p/-/merge_requests/7", pullRequestAddress("", gitlab))
	assert.Equal(t, "", pullRequestAddress("", env(nil)))
}
