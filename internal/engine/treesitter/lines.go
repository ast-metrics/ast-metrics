package treesitter

import (
	"strings"
	"sync"
)

// LineCache splits a source into its lines once.
//
// The extractors that read the text of a scope (method calls, operators) are
// called once per function, and each of them received the whole file. Splitting
// it again for every function copied the file as many times as it holds
// functions, which is what made a large file cost far more than its size: a
// 194 KB stub took three times longer per byte than the rest of a project.
//
// An adapter serves one file, so caching on the source it is handed is enough.
// The identity of the slice is what is compared, not its content: the callers
// pass the same slice every time.
type LineCache struct {
	mutex sync.Mutex
	src   []byte
	lines []string
}

// Lines returns src split on newlines, reusing the previous split when src is
// the very same slice.
func (c *LineCache) Lines(src []byte) []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.sameSource(src) {
		return c.lines
	}

	c.src = src
	c.lines = strings.Split(string(src), "\n")
	return c.lines
}

func (c *LineCache) sameSource(src []byte) bool {
	if c.lines == nil || len(c.src) != len(src) {
		return false
	}
	if len(src) == 0 {
		return true
	}
	return &c.src[0] == &src[0]
}
