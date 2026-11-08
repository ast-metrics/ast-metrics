package cribelo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Nature says what kind of edit a hunk is. Only a behavior hunk has to be
// read line by line; the others are checked at a glance, and the plan says
// how many of each there are instead of asking for them to be read.
type Nature string

const (
	// NatureBehavior: the code does something different. Read it.
	NatureBehavior Nature = "behavior"
	// NatureFormat: whitespace or line breaks changed, no token did.
	NatureFormat Nature = "format"
	// NatureComment: only comments or blank lines changed.
	NatureComment Nature = "comment"
	// NatureRename: identifiers were renamed, the token structure is the same.
	NatureRename Nature = "rename"
	// NatureImports: import, use or using lines only.
	NatureImports Nature = "imports"
	// NatureMoved: code that left another place of the change, or arrived
	// from one, without changing.
	NatureMoved Nature = "moved"
	// NatureExtracted: a new function cut out of one that shrank.
	NatureExtracted Nature = "extracted"
)

// HunkReport is one hunk of a changed file, with its nature and the detail
// that lets a reader check it in a glance.
type HunkReport struct {
	Start    int `json:"start"`
	End      int `json:"end"`
	OldStart int `json:"old_start,omitempty"`
	OldEnd   int `json:"old_end,omitempty"`
	Added    int `json:"added"`
	Deleted  int `json:"deleted"`
	// Code counts the added and deleted lines that hold code: neither blank,
	// nor a comment, nor an import statement.
	Code   int    `json:"code,omitempty"`
	Nature Nature `json:"nature"`
	Detail string `json:"detail,omitempty"`
	// New counts the lines of a moved or extracted hunk that did not come
	// along: edits made on the way, code around the moved functions. They
	// are read.
	New int `json:"new,omitempty"`
	// Glance counts the lines of the reading cost that a reader glances at
	// rather than reads: lines in a function that gained no decision and
	// stays simple, or outside every function and holding no decision.
	Glance int `json:"glance,omitempty"`
	// counted says Code was measured; decided marks, per added line, whether
	// it holds a decision token (a branch, a loop, a boolean operator).
	counted bool
	decided []bool
	// renames are the "old → new" pairs of a hunk whose two sides have the
	// same shape; the hunk becomes a rename once the change as a whole
	// vouches for them (see resolveRenames).
	renames []string
	// fresh are the identifiers of a moved hunk that the code it came from
	// never named, on freshLines of its lines; those lines are read unless
	// the change renamed them (see resolveRenames).
	fresh      []string
	freshLines int
	// where is the detail of a move before the count of new lines is added.
	where string
}

// Lines is the size of the hunk.
func (h HunkReport) Lines() int { return h.Added + h.Deleted }

// ReadCost is how many lines a reader spends on the hunk: every code line of
// a behavior hunk (blank, comment and import lines left out), a line per
// rename to check the new name, two lines per move or extraction to see where
// the code went plus the lines that did not come along, nothing for
// formatting and imports. Moves are counted after RAID (Brito and Valente,
// 2021), which measured a moved block at two lines of reading instead of
// fifteen.
func (h HunkReport) ReadCost() int {
	switch h.Nature {
	case NatureBehavior:
		if h.counted {
			return h.Code
		}
		return h.Lines()
	case NatureRename:
		return minInt(1, h.Lines())
	case NatureMoved, NatureExtracted:
		return minInt(2, h.Lines()) + h.New
	}
	return 0
}

// Mechanical reports a hunk the reader checks rather than reads: a rename,
// a format or comment change, an import, or a move that brought nothing new.
func (h HunkReport) Mechanical() bool {
	return h.Nature != NatureBehavior && h.New == 0
}

// importShapes are the shapes of import statements, per language. A hunk
// changes imports only when every non-blank line matches one of them and at
// least one line is a statement: the continuation lines of a multi-line
// import block (a name and a comma, a closing brace) look like many other
// lines and never qualify on their own.
type importShapes struct {
	statements, continuations []*regexp.Regexp
}

var importLines = map[string]importShapes{
	"Golang": {
		statements:    compileAll(`^\s*import\b`, `^\s*(\w+\s+|\.\s+|_\s+)?"[^"]+"\s*(//.*)?$`),
		continuations: compileAll(`^\s*[()]\s*$`),
	},
	"PHP": {
		statements: compileAll(`^\s*use\s+(function\s+|const\s+)?\\?[\w\\]+(\s*\{[^}]*\})?(\s+as\s+\w+)?\s*;`, `^\s*(require|include)(_once)?\b`, `^\s*namespace\s+[\w\\]+\s*;`, `^\s*declare\s*\(strict_types\s*=\s*1\)\s*;`),
	},
	"Python": {
		statements:    compileAll(`^\s*import\s+[\w.]+(\s+as\s+\w+)?(\s*,\s*[\w.]+(\s+as\s+\w+)?)*\s*$`, `^\s*from\s+[\w.]+\s+import\s+`),
		continuations: compileAll(`^\s*[\w]+(\s+as\s+\w+)?\s*,?\s*$`, `^\s*[()]\s*$`),
	},
	"Rust": {
		statements:    compileAll(`^\s*(pub(\([\w\s]+\))?\s+)?use\s+`, `^\s*(pub\s+)?mod\s+\w+\s*;`, `^\s*extern\s+crate\s+\w+(\s+as\s+\w+)?\s*;`),
		continuations: compileAll(`^\s*[\w:]+(\s+as\s+\w+)?\s*,?\s*$`, `^\s*\}\s*;?\s*$`),
	},
	"TypeScript": {
		statements: compileAll(`^\s*import\s`, `^\s*export\s+(\*|\{|type\s+\{)[^=]*from\s+['"]`, `^\s*(const|let|var)\s+[\w{},\s]+\s*=\s*require\(['"][^'"]+['"]\)\s*;?\s*$`,
			`^\s*\}\s*from\s+['"][^'"]+['"]\s*;?\s*$`),
		continuations: compileAll(`^\s*(type\s+)?[\w$]+(\s+as\s+[\w$]+)?\s*,?\s*$`),
	},
	"Java": {statements: compileAll(`^\s*import\s+(static\s+)?[\w.*]+\s*;`, `^\s*package\s+[\w.]+\s*;`)},
	"C#":   {statements: compileAll(`^\s*(global\s+)?using\s+(static\s+)?[\w.]+(\s*=\s*[\w.<>,\s]+)?\s*;`, `^\s*namespace\s+[\w.]+\s*;?`)},
}

func compileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

func matchesAny(line string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
}

// importsOnly reports whether every non-blank line is an import line of the
// language, one of them at least being an import statement.
func importsOnly(language string, lines ...[]string) bool {
	shapes, ok := importLines[language]
	if !ok {
		return false
	}
	statements := 0
	for _, group := range lines {
		for _, line := range group {
			if strings.TrimSpace(line) == "" {
				continue
			}
			switch {
			case matchesAny(line, shapes.statements):
				statements++
			case matchesAny(line, shapes.continuations):
			default:
				return false
			}
		}
	}
	return statements > 0
}

// sameTokens reports whether two token lists are identical.
func sameTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// identifierSets are the identifiers of a file before and after the change,
// what tells a rename from a different variable put to use.
type identifierSets struct {
	base, head map[string]int
}

// identifiers counts the occurrences of every identifier of a source.
func identifiers(source []byte) map[string]int {
	counts := map[string]int{}
	for _, t := range rawTokens(string(source)) {
		if t != "" && (unicode.IsLetter(rune(t[0])) || t[0] == '_' || t[0] == '$') {
			counts[t]++
		}
	}
	return counts
}

// renaming reads a hunk whose two sides have the same shape and tells
// whether the identifiers that differ were renamed: every old name maps to
// one new name and back, the old name is gone from the whole file after the
// change, and the new name did not exist before it. Otherwise the hunk puts
// another variable to use, which is a change of behavior, however small.
// The pairs come back as "old → new", in order of appearance.
func renaming(old, new []string, idents identifierSets) ([]string, bool) {
	forward, backward := map[string]string{}, map[string]string{}
	var pairs []string
	for i := range old {
		if i >= len(new) || old[i] == new[i] {
			continue
		}
		if to, seen := forward[old[i]]; seen && to != new[i] {
			return nil, false
		}
		if from, seen := backward[new[i]]; seen && from != old[i] {
			return nil, false
		}
		if _, seen := forward[old[i]]; !seen {
			if idents.head != nil && idents.head[old[i]] > 0 {
				return nil, false // the old name still lives in the file
			}
			if idents.base != nil && idents.base[new[i]] > 0 {
				return nil, false // the new name was already another thing
			}
			pairs = append(pairs, old[i]+" → "+new[i])
		}
		forward[old[i]], backward[new[i]] = new[i], old[i]
	}
	return pairs, len(pairs) > 0
}

// allBlank reports lines that hold nothing but whitespace.
func allBlank(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}

// differsInComments tells lines that keep their tokens but change a
// comment from lines merely laid out differently: as many lines on both
// sides, each pair identical once whitespace is collapsed, or differing
// only where no token differs, which leaves a comment.
func differsInComments(added, removed []string) bool {
	if len(added) != len(removed) {
		return false
	}
	comment := false
	for i := range added {
		a, r := strings.Join(strings.Fields(added[i]), " "), strings.Join(strings.Fields(removed[i]), " ")
		if a == r {
			continue
		}
		if !sameTokens(rawTokens(added[i]), rawTokens(removed[i])) {
			return false
		}
		comment = true
	}
	return comment
}

// classifyHunks tells the nature of every hunk of a changed file. moves may
// be nil when move detection is off; idents may be empty when the sources
// are unavailable, in which case no hunk is a rename.
func classifyHunks(c Change, language string, moves *MoveIndex, idents identifierSets) []HunkReport {
	out := make([]HunkReport, 0, len(c.Hunks))
	for _, h := range c.Hunks {
		r := HunkReport{Start: h.Start, End: h.End, OldStart: h.OldStart, OldEnd: h.OldEnd, Added: len(h.Added), Deleted: len(h.Removed), Nature: NatureBehavior}
		if r.Added == 0 && r.Deleted == 0 {
			// An untracked file, or a diff without content: nothing to tell.
			r.Added = h.End - h.Start + 1
			out = append(out, r)
			continue
		}
		r.Code = codeLines(language, h.Added) + codeLines(language, h.Removed)
		r.decided = decided(h.Added)
		r.counted = true
		added, removed := strings.Join(h.Added, "\n"), strings.Join(h.Removed, "\n")
		rawAdded, rawRemoved := rawTokens(added), rawTokens(removed)
		switch {
		case importsOnly(language, h.Added, h.Removed):
			r.Nature = NatureImports
		case len(rawAdded) == 0 && len(rawRemoved) == 0:
			// Lines without a token: blank lines are layout, the rest comments.
			if allBlank(h.Added) && allBlank(h.Removed) {
				r.Nature, r.Detail = NatureFormat, "blank lines"
			} else {
				r.Nature = NatureComment
			}
		case len(rawAdded) > 0 && len(rawRemoved) > 0 && sameTokens(rawAdded, rawRemoved):
			if differsInComments(h.Added, h.Removed) {
				r.Nature = NatureComment
			} else {
				r.Nature, r.Detail = NatureFormat, "same tokens, laid out differently"
			}
		default:
			if len(rawAdded) > 0 && len(rawRemoved) > 0 && (idents.head != nil || idents.base != nil) && sameTokens(shapeTokens(added), shapeTokens(removed)) {
				if pairs, ok := renaming(rawRemoved, rawAdded, idents); ok {
					// A candidate until the change vouches for it.
					r.renames = pairs
					break
				}
			}
			if moves != nil {
				if carry, ok := moves.hunkNature(c.Path, h); ok {
					r.Nature, r.where, r.fresh, r.freshLines = carry.nature, carry.detail, carry.fresh, carry.freshLines
					r.New = carry.edited + carry.freshLines
					r.Detail = moveDetail(r.where, r.New, nil)
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// codeLines counts the lines that hold code: neither blank, nor a comment
// (a line starting with a comment marker), nor an import statement of the
// language.
func codeLines(language string, lines []string) int {
	shapes := importLines[language]
	n := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCommentLine(trimmed) || matchesAny(line, shapes.statements) {
			continue
		}
		n++
	}
	return n
}

// decisionTokens are the tokens that open a branch or a loop in the
// supported languages, and the boolean operators.
var decisionTokens = regexp.MustCompile(`\b(if|elif|else|for|foreach|while|do|switch|select|match|case|when|catch|except|rescue)\b|&&|\|\||\?\?`)

// decided marks the lines that hold a decision token, comments left out.
func decided(lines []string) []bool {
	out := make([]bool, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCommentLine(trimmed) {
			continue
		}
		out[i] = decisionTokens.MatchString(line)
	}
	return out
}

// decides reports whether the added line at the given new-side position
// holds a decision. A hunk that was not measured decides everywhere.
func (h HunkReport) decides(line int) bool {
	if !h.counted {
		return true
	}
	i := line - h.Start
	return i >= 0 && i < len(h.decided) && h.decided[i]
}

// moveDetail words a move: where the code came from, how many of its lines
// did not come along, and what new names they use.
func moveDetail(where string, newLines int, fresh []string) string {
	detail := where
	if newLines > 0 {
		detail = fmt.Sprintf("%s, %d %s new", where, newLines, plural(newLines, "line"))
	}
	if len(fresh) > 3 {
		fresh = append(fresh[:3], fmt.Sprintf("+%d more", len(fresh)-3))
	}
	if len(fresh) > 0 {
		detail += " (" + strings.Join(fresh, ", ") + ")"
	}
	return detail
}

// resolveRenames turns the candidate renames into renames when the change
// vouches for them: a pair "old → new" seen in two hunks at least, in one
// file or across files, is a name changed everywhere. A pair seen once may
// be a different method put to use on one line, which is a change of
// behavior however small, and stays one. The lines of a moved hunk that
// name something its origin never named are edits, and are read, unless
// those names are the new side of vouched renames. Totals are recomputed.
func resolveRenames(files []FileReport) {
	seen := map[string]int{}
	for _, f := range files {
		for _, h := range f.Hunks {
			for _, pair := range h.renames {
				seen[pair]++
			}
		}
	}
	renamed := map[string]bool{}
	for pair, n := range seen {
		if n >= 2 {
			renamed[pair[strings.LastIndex(pair, " ")+1:]] = true
		}
	}
	for i := range files {
		f := &files[i]
		changed := false
		for j := range f.Hunks {
			h := &f.Hunks[j]
			if h.Nature == NatureMoved && len(h.fresh) > 0 {
				var fresh []string
				for _, name := range h.fresh {
					if !renamed[name] {
						fresh = append(fresh, name)
					}
				}
				if len(fresh) == 0 {
					h.New -= h.freshLines
				}
				h.Detail = moveDetail(h.where, h.New, fresh)
				h.fresh = nil
				changed = true
				continue
			}
			if len(h.renames) == 0 {
				continue
			}
			vouched := true
			for _, pair := range h.renames {
				if seen[pair] < 2 {
					vouched = false
					break
				}
			}
			if !vouched {
				continue
			}
			h.Nature = NatureRename
			pairs := h.renames
			if len(pairs) > 3 {
				pairs = append(append([]string{}, pairs[:3]...), fmt.Sprintf("+%d more", len(pairs)-3))
			}
			h.Detail = strings.Join(pairs, ", ")
			changed = true
		}
		if changed {
			f.Natures = countNatures(f.Hunks)
			f.tally()
		}
	}
}

// Rename is one "old → new" pair of the change and the hunks it touches.
type Rename struct {
	Pair  string `json:"pair"`
	Hunks int    `json:"hunks"`
}

// renamesOf lists the rename pairs of the files, most frequent first.
func renamesOf(files []FileReport) []Rename {
	counts := map[string]int{}
	for _, f := range files {
		for _, h := range f.Hunks {
			if h.Nature == NatureRename {
				for _, pair := range h.renames {
					counts[pair]++
				}
			}
		}
	}
	out := make([]Rename, 0, len(counts))
	for pair, n := range counts {
		out = append(out, Rename{Pair: pair, Hunks: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hunks != out[j].Hunks {
			return out[i].Hunks > out[j].Hunks
		}
		return out[i].Pair < out[j].Pair
	})
	return out
}

// natureCounts counts the hunks of a file by nature, lines included.
type natureCount struct {
	Hunks int `json:"hunks"`
	Lines int `json:"lines"`
}

func countNatures(hunks []HunkReport) map[Nature]natureCount {
	counts := map[Nature]natureCount{}
	for _, h := range hunks {
		c := counts[h.Nature]
		c.Hunks++
		c.Lines += h.Lines()
		counts[h.Nature] = c
	}
	return counts
}

// natureLabel is the wording of a nature in the plan, with its count.
func natureLabel(n Nature, count int) string {
	word := map[Nature]string{
		NatureMoved: "move", NatureExtracted: "extraction", NatureRename: "rename",
		NatureFormat: "formatting hunk", NatureComment: "comment hunk", NatureImports: "import hunk",
	}[n]
	if word == "" {
		return ""
	}
	return fmt.Sprintf("%d %s", count, plural(count, word))
}
