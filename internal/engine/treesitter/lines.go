package treesitter

import (
	"strings"
	"sync"
)

// LineCache splits a source into its lines once.
//
// The extractors that read the text of a scope are called once per function and
// each of them receives the whole file. Splitting it again for every function
// copied the file as many times as it holds functions, which is what made a
// large file cost far more than its size.
//
// An adapter serves one file, so remembering the last source is enough. What is
// compared is the identity of the slice, not its content: the callers hand over
// the same slice every time, and a different one must not be answered with the
// lines of the previous file.
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

	same := c.lines != nil && len(c.src) == len(src) &&
		(len(src) == 0 || &c.src[0] == &src[0])
	if !same {
		c.src = src
		c.lines = strings.Split(string(src), "\n")
	}

	return c.lines
}
