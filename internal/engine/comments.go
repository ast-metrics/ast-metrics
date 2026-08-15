package engine

import (
	"strings"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Line counting model shared by every language.
//
// Three numbers describe the size of a range of source, and every line of that
// range falls in exactly one of them:
//
//	LOC    every physical line, blank ones included
//	CLOC   the lines that hold documentation and nothing else
//	NCLOC  the lines that hold code
//
// so that LOC = CLOC + NCLOC + blank lines. This is the convention of cloc,
// tokei, scc and phploc, and it is what makes the comment ratio of the
// maintainability index mean something.
//
// A line holding code and then a comment is code: `total += 1 // sum it up` is
// a line of code that happens to be documented, and counting it as a comment
// would let a file drift towards 100% comments while being pure code.
//
// A documentation block counts in full, including its delimiters, in whichever
// way the language spells it: a `/* */` block, a run of `//` or `#` lines, or a
// Python docstring. Python's convention is a string literal rather than a
// comment token, but it is the documentation of the function it opens, exactly
// like a PHP docblock, so it is counted the same way. cloc agrees, tokei
// counts it as code.

// CommentSyntax describes how one language spells its comments, so a single
// scanner can serve them all instead of one hand-written copy per grammar.
type CommentSyntax struct {
	// Line lists the tokens that start a comment running to the end of the
	// line ("//", "#").
	Line []string
	// BlockOpen and BlockClose delimit a multi-line comment ("/*", "*/"). Empty
	// when the language has none.
	BlockOpen  string
	BlockClose string
	// DocString lists the delimiters of a documentation string, opened and
	// closed by the same token (Python's `"""` and `'''`). Such a block is
	// documentation, so its content is not scanned as code.
	DocString []string
	// RawString lists the delimiters of a string literal that may span several
	// lines and is never documentation: Go's and TypeScript's backtick, Java's
	// and C#'s `"""` text block. Its lines are code, and a comment marker
	// written inside it is part of the value, not a comment.
	RawString []string
	// IsDocString, when set, decides whether the DocString delimiter opening
	// at the start of the given 1-based line is documentation. A triple-quoted
	// string alone on its line is not always a docstring: black formats a
	// multi-line argument or tuple member as `(` on one line and `"""` on the
	// next, and a test fixture or SQL query written that way is a value the
	// program carries. Only a parser knows which one it is, so the adapter
	// answers from the AST; when nil, the delimiter is taken as documentation.
	IsDocString func(line int) bool
	// LetterPrefixedStrings tells that a string literal may carry a prefix of
	// letters, as Python writes r"", b"", f"" and their combinations. Without
	// it, a raw docstring `r"""..."""` would not be recognised as one.
	LetterPrefixedStrings bool
	// Quote lists the characters that open a string literal, whose content must
	// be ignored: a URL in a string is not a comment, and "#" inside a Python
	// f-string is not one either.
	Quote []rune
	// LifetimeQuote tells that a single quote can open something other than a
	// string, and must not be treated as a delimiter. Rust writes lifetimes
	// this way ('a), with no closing quote to pair it with.
	LifetimeQuote bool
}

// DefaultCommentSyntax honors every marker, for a language that declares none.
func DefaultCommentSyntax() CommentSyntax {
	return CommentSyntax{
		Line:       []string{"//", "#"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
	}
}

// SplitSourceLines cuts a source file into its physical lines.
//
// The newline that ends the last line is a terminator, not a separator: a file
// of three lines ending with "\n" has three lines, and counting the empty
// remainder as a fourth would report one line more than every other tool for
// every well-formed file there is.
func SplitSourceLines(src []byte) []string {
	if len(src) == 0 {
		// an empty file holds no line at all, not one empty one
		return nil
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	// a byte order mark is an encoding marker, not content; left in place it
	// would hide the "//" of the first line, which Visual Studio puts a BOM in
	// front of in every file it generates
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// lineKind is what a single physical line holds.
type lineKind int

const (
	lineBlank lineKind = iota
	lineComment
	lineCode
)

// scanner walks lines in order, remembering what is still open at the end of
// each one. It must start at the top of the file: a block comment opened on
// line 10 makes line 20 a comment line, which cannot be known from line 20
// alone.
//
// Two things can stay open across lines, and they are not the same:
//
//   - a comment block, whose remaining lines are documentation;
//   - a multi-line string literal, whose remaining lines are code. A SQL query
//     or an HTML template held in a Python triple-quoted string is data the
//     program carries, not documentation about it.
//
// What tells them apart is where the delimiter sits. A triple-quoted string
// alone on its line documents what follows it; one opened after code on the same
// line is a value being assigned, returned or passed.
type scanner struct {
	syn CommentSyntax
	// openComment holds the token that closes the comment block in progress.
	openComment string
	// openString holds the token that closes the multi-line string in progress.
	openString string
	// line is the 1-based number of the line being classified.
	line int
}

// classify returns what the line holds, and advances the scanner state.
func (s *scanner) classify(raw string) lineKind {
	line := strings.TrimSpace(raw)

	if s.openComment != "" {
		// every line of a comment block counts, the closing delimiter included
		if idx := strings.Index(line, s.openComment); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len(s.openComment):])
			s.openComment = ""
			if rest != "" && !s.opensComment(rest) {
				// code follows the end of the block on the same line
				s.consumeCode(rest)
				return lineCode
			}
		}
		return lineComment
	}

	if s.openString != "" {
		idx := strings.Index(line, s.openString)
		if idx < 0 {
			return lineCode
		}
		rest := line[idx+len(s.openString):]
		s.openString = ""
		s.consumeCode(rest)
		return lineCode
	}

	if line == "" {
		return lineBlank
	}
	if s.opensComment(line) {
		return lineComment
	}
	// the line starts with code; a comment or a string may still open on it and
	// run over the lines below
	s.consumeCode(line)
	return lineCode
}

// opensComment reports whether the line starts with a comment, and records the
// block it opens when that block stays open past this line.
func (s *scanner) opensComment(line string) bool {
	for _, marker := range s.syn.Line {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	// a documentation string may carry a letter prefix: r"""..."""
	rest := line
	if s.syn.LetterPrefixedStrings {
		rest = strings.TrimLeft(line, "rbufRBUF")
	}
	opener, closer, kind := s.delimiterAt(rest)
	// a raw string standing at the start of a line is a value, not
	// documentation: only a comment block and a documentation string are
	if kind != delimiterComment && kind != delimiterDoc {
		return false
	}
	if kind == delimiterDoc && s.syn.IsDocString != nil && !s.syn.IsDocString(s.line) {
		// the parser says this string is a value, not the documentation of
		// what follows: the line is code, and so is the rest of the string
		return false
	}

	after := rest[len(opener):]
	end := strings.Index(after, closer)
	if end < 0 {
		s.openComment = closer
		return true
	}
	// the block closes on the line it opened on, so what comes after it
	// decides: `/* deprecated */ internal Resources() {` is a line of code
	tail := strings.TrimSpace(after[end+len(closer):])
	if tail == "" {
		return true
	}
	return s.opensComment(tail)
}

// consumeCode walks a line of code to find out whether it leaves a comment
// block or a multi-line string open behind it.
func (s *scanner) consumeCode(line string) {
	for i := 0; i < len(line); {
		if line[i] == '\\' {
			i += 2
			continue
		}

		// a line comment swallows the rest of the line
		if s.startsWith(line[i:], s.syn.Line) {
			return
		}

		// tested before the single quote characters, so that a Python `"""`
		// is not read as an empty string followed by a stray quote
		if opener, closer, kind := s.delimiterAt(line[i:]); opener != "" {
			rest := line[i+len(opener):]
			end := strings.Index(rest, closer)
			if end < 0 {
				if kind == delimiterComment {
					s.openComment = closer
				} else {
					// a string opened after code carries a value, not
					// documentation: what follows is code until it closes
					s.openString = closer
				}
				return
			}
			i += len(opener) + end + len(closer)
			continue
		}

		if c := rune(line[i]); s.isQuote(c) && !(c == '\'' && s.syn.LifetimeQuote) {
			i = skipString(line, i)
			continue
		}
		i++
	}
}

// delimiterKind says what a multi-line delimiter opens.
type delimiterKind int

const (
	delimiterNone delimiterKind = iota
	// delimiterComment opens a comment block, whose lines are documentation.
	delimiterComment
	// delimiterDoc opens a triple-quoted string, documentation when it starts a
	// line and a plain value when it follows code.
	delimiterDoc
	// delimiterRaw opens a string that may span lines and never documents
	// anything.
	delimiterRaw
)

// delimiterAt reports the multi-line delimiter opening at the start of text, as
// the opening token, the token that closes it, and what it opens.
//
// The triple-quoted forms are tested before the single-character quotes, so that
// a Python `"""` is not read as an empty string followed by a stray quote.
func (s *scanner) delimiterAt(text string) (opener, closer string, kind delimiterKind) {
	for _, quote := range s.syn.DocString {
		if strings.HasPrefix(text, quote) {
			return quote, quote, delimiterDoc
		}
	}
	for _, quote := range s.syn.RawString {
		if strings.HasPrefix(text, quote) {
			return quote, quote, delimiterRaw
		}
	}
	if s.syn.BlockOpen != "" && strings.HasPrefix(text, s.syn.BlockOpen) {
		return s.syn.BlockOpen, s.syn.BlockClose, delimiterComment
	}
	return "", "", delimiterNone
}

func (s *scanner) isQuote(c rune) bool {
	for _, q := range s.syn.Quote {
		if q == c {
			return true
		}
	}
	return false
}

func (s *scanner) startsWith(text string, markers []string) bool {
	for _, m := range markers {
		if strings.HasPrefix(text, m) {
			return true
		}
	}
	return false
}

// skipString returns the index just past the string literal opening at i.
func skipString(line string, i int) int {
	quote := line[i]
	for j := i + 1; j < len(line); j++ {
		if line[j] == '\\' {
			j++
			continue
		}
		if line[j] == quote {
			return j + 1
		}
	}
	// unterminated on this line: nothing readable is left
	return len(line)
}

// LineIndex holds what every physical line of one file holds, as running totals.
//
// A file is scanned once, from the top: a comment block opened on line 10 makes
// line 20 documentation, which cannot be decided from line 20 alone. Counting a
// range is then a subtraction. A parser asks for one range per scope, and a
// class of twenty methods asks twenty-two times, so scanning the file for each
// of them would make the cost of a file grow with the number of scopes it holds.
type LineIndex struct {
	// cloc[i] and ncloc[i] hold the totals over the first i lines, so the count
	// over [start, end] is the difference at both ends.
	cloc  []int32
	ncloc []int32
	lines int
}

// NewLineIndex scans a file once and returns the index of its lines.
func NewLineIndex(sourceCode []string, syn CommentSyntax) *LineIndex {
	idx := &LineIndex{
		cloc:  make([]int32, len(sourceCode)+1),
		ncloc: make([]int32, len(sourceCode)+1),
		lines: len(sourceCode),
	}

	s := &scanner{syn: syn}
	for i, line := range sourceCode {
		s.line = i + 1
		cloc, ncloc := idx.cloc[i], idx.ncloc[i]
		switch s.classify(line) {
		case lineComment:
			cloc++
		case lineCode:
			ncloc++
		}
		idx.cloc[i+1], idx.ncloc[i+1] = cloc, ncloc
	}

	return idx
}

// Count measures the 1-based inclusive line range [start, end].
//
// LogicalLinesOfCode is left at zero: only a parser can fill it, following the
// model in internal/engine/treesitter/statement.go.
func (idx *LineIndex) Count(start int, end int) *pb.LinesOfCode {
	if start < 1 {
		start = 1
	}
	if end > idx.lines {
		end = idx.lines
	}
	if end < start {
		// nothing to measure: an empty range is not one line long
		return &pb.LinesOfCode{}
	}

	return &pb.LinesOfCode{
		LinesOfCode:           int32(end - start + 1),
		CommentLinesOfCode:    idx.cloc[end] - idx.cloc[start-1],
		NonCommentLinesOfCode: idx.ncloc[end] - idx.ncloc[start-1],
	}
}

// CountLinesOfCode measures the 1-based inclusive line range [start, end] of a
// file whose lines are given whole, so that a comment block opened before start
// is still known to be open.
//
// Callers that measure several ranges of the same file should build a LineIndex
// instead, and pay the scan once.
func CountLinesOfCode(sourceCode []string, start int, end int, syn CommentSyntax) *pb.LinesOfCode {
	return NewLineIndex(sourceCode, syn).Count(start, end)
}
