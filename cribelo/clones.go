package cribelo

import (
	"hash/fnv"
	"sort"
	"strings"
	"unicode"
)

// Clone detection works on a language-agnostic token stream: identifiers,
// numbers and strings are normalized so that renaming does not hide a copy.
// Two functions are compared through winnowed k-gram fingerprints (Schleimer,
// Wilkerson, Aiken 2003), which keeps the index small and the comparison fast.
const (
	cloneKGram        = 7
	cloneWindow       = 4
	cloneMinTokens    = 60
	cloneMinSimilar   = 0.70
	cloneCandidateMin = 0.35 // share of fingerprints a candidate must have to be compared
)

// CloneMatch is the closest function found elsewhere in the project.
type CloneMatch struct {
	Path       string  `json:"path"`
	Function   string  `json:"function"`
	Line       int     `json:"line"`
	Similarity float64 `json:"similarity"`
}

type cloneEntry struct {
	path     string
	function string
	line     int
	prints   []uint64
}

// CloneIndex holds the fingerprints of every function of a project.
type CloneIndex struct {
	entries  []cloneEntry
	postings map[uint64][]int
}

// NewCloneIndex returns an empty index.
func NewCloneIndex() *CloneIndex {
	return &CloneIndex{postings: map[uint64][]int{}}
}

// Add indexes a function body. Bodies shorter than cloneMinTokens are ignored:
// short functions look alike by construction.
func (idx *CloneIndex) Add(path, function string, line int, source string) {
	prints := Fingerprints(functionBody(source))
	if prints == nil {
		return
	}
	id := len(idx.entries)
	idx.entries = append(idx.entries, cloneEntry{path: path, function: function, line: line, prints: prints})
	for _, p := range prints {
		idx.postings[p] = append(idx.postings[p], id)
	}
}

// Len returns the number of indexed functions.
func (idx *CloneIndex) Len() int {
	if idx == nil {
		return 0
	}
	return len(idx.entries)
}

// Closest returns the most similar indexed function to source, excluding the
// function itself (same path and name). Nil when nothing reaches the
// similarity threshold.
func (idx *CloneIndex) Closest(path, function string, source string) *CloneMatch {
	if idx == nil {
		return nil
	}
	prints := Fingerprints(functionBody(source))
	if prints == nil {
		return nil
	}
	shared := map[int]int{}
	for _, p := range prints {
		for _, id := range idx.postings[p] {
			shared[id]++
		}
	}
	best := (*CloneMatch)(nil)
	ids := make([]int, 0, len(shared))
	for id := range shared {
		ids = append(ids, id)
	}
	sort.Ints(ids) // deterministic tie-breaking
	for _, id := range ids {
		entry := idx.entries[id]
		if entry.path == path && entry.function == function {
			continue
		}
		if float64(shared[id]) < cloneCandidateMin*float64(len(prints)) {
			continue
		}
		union := len(prints) + len(entry.prints) - shared[id]
		if union <= 0 {
			continue
		}
		similarity := float64(shared[id]) / float64(union)
		if similarity < cloneMinSimilar {
			continue
		}
		if best == nil || similarity > best.Similarity {
			best = &CloneMatch{Path: entry.path, Function: entry.function, Line: entry.line, Similarity: similarity}
		}
	}
	return best
}

// Fingerprints tokenizes, normalizes and winnows a piece of source. The result
// is a sorted set of fingerprints, or nil when the source is too short.
func Fingerprints(source string) []uint64 { return fingerprints(source, cloneMinTokens) }

// fingerprints is Fingerprints with its own minimum length: a move or an
// extraction is matched on shorter functions than a clone, since the source
// is known to have disappeared.
func fingerprints(source string, minTokens int) []uint64 {
	tokens := Tokenize(source)
	if len(tokens) < minTokens || len(tokens) < cloneKGram+cloneWindow {
		return nil
	}
	grams := make([]uint64, 0, len(tokens)-cloneKGram+1)
	for i := 0; i+cloneKGram <= len(tokens); i++ {
		h := fnv.New64a()
		for _, t := range tokens[i : i+cloneKGram] {
			h.Write([]byte(t))
			h.Write([]byte{0})
		}
		grams = append(grams, h.Sum64())
	}
	// Winnowing: keep the minimum of every window; equal minima are recorded once.
	set := map[uint64]struct{}{}
	for i := 0; i+cloneWindow <= len(grams); i++ {
		min := grams[i]
		for _, g := range grams[i+1 : i+cloneWindow] {
			if g < min {
				min = g
			}
		}
		set[min] = struct{}{}
	}
	prints := make([]uint64, 0, len(set))
	for p := range set {
		prints = append(prints, p)
	}
	sort.Slice(prints, func(i, j int) bool { return prints[i] < prints[j] })
	return prints
}

// Tokenize splits source into normalized tokens: every identifier becomes
// "$", every number "0", and comments are dropped. A string literal keeps its
// content, prefixed with a quote: a function copied and adapted keeps its
// strings, while the mapping boilerplate of two data holders (toArray,
// fromArray, a configuration block) differs by its keys and must not pass
// for a clone. Punctuation is kept as is, one rune per token.
func Tokenize(source string) []string { return tokenize(source, true, true) }

// rawTokens is Tokenize without the normalization: identifiers and numbers
// are kept as written. Two pieces of code with the same raw tokens differ
// only by whitespace and comments.
func rawTokens(source string) []string { return tokenize(source, false, false) }

// shapeTokens normalizes identifiers but keeps numbers: two pieces of code
// with the same shape tokens differ only by the names they use.
func shapeTokens(source string) []string { return tokenize(source, true, false) }

func tokenize(source string, normalizeIdentifiers, normalizeNumbers bool) []string {
	var tokens []string
	runes := []rune(source)
	n := len(runes)
	for i := 0; i < n; {
		c := runes[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '/' && i+1 < n && runes[i+1] == '/', c == '#' && !(i+1 < n && (runes[i+1] == '[' || runes[i+1] == '!')):
			for i < n && runes[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && runes[i+1] == '*':
			i += 2
			for i+1 < n && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i += 2
		case c == '"' || c == '\'' || c == '`':
			quote := c
			i++
			start := i
			for i < n && runes[i] != quote {
				if runes[i] == '\\' {
					i++
				}
				i++
			}
			end := minInt(i, n)
			i++
			tokens = append(tokens, "\""+string(runes[start:end]))
		case unicode.IsLetter(c) || c == '_' || c == '$':
			start := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_' || runes[i] == '$') {
				i++
			}
			if normalizeIdentifiers {
				tokens = append(tokens, "$")
			} else {
				tokens = append(tokens, string(runes[start:i]))
			}
		case unicode.IsDigit(c):
			start := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '.' || runes[i] == '_') {
				i++
			}
			if normalizeNumbers {
				tokens = append(tokens, "0")
			} else {
				tokens = append(tokens, string(runes[start:i]))
			}
		default:
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}

// functionBody drops the declaration of a function and keeps its body. Once
// identifiers are normalized, every parameter list looks like every other
// one: a constructor with promoted properties, or a data holder, would pass
// for a clone of any other. The body starts at the first brace, or, for
// Python, after the first line that ends with a colon, whichever comes first.
func functionBody(source string) string {
	for offset := 0; offset < len(source); {
		next := strings.IndexByte(source[offset:], '\n')
		var line string
		if next < 0 {
			line, next = source[offset:], len(source)
		} else {
			line, next = source[offset:offset+next], offset+next+1
		}
		if i := strings.IndexByte(line, '{'); i >= 0 {
			return source[offset+i:]
		}
		if strings.HasSuffix(strings.TrimRightFunc(line, unicode.IsSpace), ":") {
			return source[next:]
		}
		offset = next
	}
	return source
}

// sliceLines returns lines [start, end] (1-based, inclusive) joined back.
func sliceLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}
