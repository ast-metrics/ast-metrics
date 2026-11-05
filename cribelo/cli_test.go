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
