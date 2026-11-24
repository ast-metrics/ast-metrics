package cribelo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWords(t *testing.T) {
	assert.Equal(t, "file", Plural(1, "file"))
	assert.Equal(t, "files", Plural(2, "file"))
	assert.Equal(t, "45 min", Duration(45))
	assert.Equal(t, "2 h", Duration(120))
	assert.Equal(t, "2 h 05", Duration(125))
	assert.Equal(t, "Put", ShortName("Store::Put"))
	assert.Equal(t, "run", ShortName("run"))
	assert.Equal(t, "3 moves", NatureMoved.Label(3))
	assert.Equal(t, "1 import hunk", NatureImports.Label(1))
	assert.Equal(t, "", NatureBehavior.Label(4), "what is read is not set aside")
	assert.Equal(t, "core depends on api", plain("`core` depends on `api`"))
	assert.Equal(t, []string{"`a`", "`b`"}, codeAll([]string{"a", "b"}))
}
