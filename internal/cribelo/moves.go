package cribelo

import (
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/review"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Move detection finds the code of a change that is not new: functions that
// left one place of the base and arrived at another place of the head
// unchanged, or with their identifiers renamed, or nearly unchanged; and
// functions cut out of one that shrank. The hunks that hold them are moves
// and extractions, which a reader checks in a glance instead of reading.
//
// Everything rests on the functions the parsers found, their line ranges and
// their token streams; no type information is needed. The approach follows
// RefDiff (Silva, Valente, 2017), which matches functions across a change on
// their token similarity.
const (
	// moveMinTokens is the shortest body matched exactly: shorter functions,
	// accessors and constructors, look alike by construction. moveMinPrints
	// is the shortest body matched on fingerprints, for moves with edits and
	// extractions.
	moveMinTokens = 12
	moveMinPrints = 20
	// moveMinSimilar is the fingerprint similarity from which a function that
	// changed a little is still the same function, moved.
	moveMinSimilar = 0.80
	// extractMinContained is the share of a new function's fingerprints that
	// must come from the function it was cut out of.
	extractMinContained = 0.80
)

// fnEntry is one function of the base or the head, with what it takes to
// compare it with another.
type fnEntry struct {
	path     string
	name     string
	start    int
	end      int
	raw      []string
	rawHash  uint64
	normHash uint64
	prints   []uint64
	// lines are the shapes of the function's lines (identifiers normalized,
	// numbers kept), to tell the lines that moved unchanged from the edits.
	lines map[uint64]bool
}

// moveSpan is a range of lines, in a file of the head or of the base, that
// belongs to a moved or extracted function, and where the code came from or
// went to.
type moveSpan struct {
	start, end int
	nature     Nature
	detail     string
	// counterpart holds the line shapes of the function on the other side
	// of the move: a line of the hunk that is not among them was edited.
	counterpart map[uint64]bool
}

// lineShape hashes a line with its identifiers normalized; "" for a line
// without a token.
func lineShape(line string) (uint64, bool) {
	tokens := shapeTokens(line)
	if len(tokens) == 0 {
		return 0, false
	}
	return hashTokens(tokens), true
}

func lineShapes(lines []string) map[uint64]bool {
	set := map[uint64]bool{}
	for _, line := range lines {
		if h, ok := lineShape(line); ok {
			set[h] = true
		}
	}
	return set
}

// MoveIndex knows, for every changed file, the lines that moved in and the
// lines that moved out.
type MoveIndex struct {
	// arrived holds, per head path, the spans of functions that came from
	// elsewhere; left holds, per base path, the spans that went elsewhere.
	arrived map[string][]moveSpan
	left    map[string][]moveSpan
	// Moves lists what was found, for the plan and for explain.
	Moves []Move
}

// Move is one function that moved or was extracted.
type Move struct {
	Nature   Nature  `json:"nature"`
	From     string  `json:"from"`
	FromName string  `json:"from_name"`
	To       string  `json:"to"`
	ToName   string  `json:"to_name"`
	Lines    int     `json:"lines"`
	Same     float64 `json:"similarity"`
}

func hashTokens(tokens []string) uint64 {
	h := fnv.New64a()
	for _, t := range tokens {
		h.Write([]byte(t))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

// functionEntries lists the functions of a parsed file, with their tokens.
// The source is read from the parsed file's own path, which is the working
// tree for the head and a snapshot directory for the base.
func functionEntries(rel string, file *pb.File) []fnEntry {
	if file == nil {
		return nil
	}
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	functions := review.CollectFunctions(file)
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]fnEntry, 0, len(names))
	for _, name := range names {
		fn := functions[name]
		if fn.Location == nil {
			continue
		}
		start, end := int(fn.Location.StartLine), int(fn.Location.EndLine)
		source := sliceLines(lines, start, end)
		body := functionBody(source)
		raw := rawTokens(body)
		if len(raw) < moveMinTokens {
			continue
		}
		entries = append(entries, fnEntry{
			path: rel, name: name, start: start, end: end, raw: raw,
			rawHash: hashTokens(raw), normHash: hashTokens(shapeTokens(body)), prints: fingerprints(body, moveMinPrints),
			lines: lineShapes(strings.Split(source, "\n")),
		})
	}
	return entries
}

// buildMoveIndex compares the functions of the base and of the head of the
// changed files. deleted lists the code files the change removed: their
// functions may have moved somewhere else.
func buildMoveIndex(head, base *Parsed, changed []Change, deleted []Change) *MoveIndex {
	idx := &MoveIndex{arrived: map[string][]moveSpan{}, left: map[string][]moveSpan{}}
	var arrived, gone []fnEntry
	type shrunk struct {
		before, after fnEntry
	}
	var shrunken []shrunk
	byName := func(entries []fnEntry) map[string]fnEntry {
		m := make(map[string]fnEntry, len(entries))
		for _, e := range entries {
			m[e.name] = e
		}
		return m
	}
	for _, c := range changed {
		var headFile, baseFile *pb.File
		if head != nil {
			headFile = head.Files[c.Path]
		}
		if base != nil {
			baseFile = base.Files[c.Path]
		}
		headFns, baseFns := byName(functionEntries(c.Path, headFile)), byName(functionEntries(c.Path, baseFile))
		for name, e := range headFns {
			before, existed := baseFns[name]
			switch {
			case !existed:
				arrived = append(arrived, e)
			case len(e.raw) < len(before.raw)*4/5:
				shrunken = append(shrunken, shrunk{before, e})
			}
		}
		for name, e := range baseFns {
			if _, stays := headFns[name]; !stays {
				gone = append(gone, e)
			}
		}
	}
	for _, c := range deleted {
		if base == nil {
			break
		}
		gone = append(gone, functionEntries(c.Path, base.Files[c.Path])...)
	}
	sortEntries := func(entries []fnEntry) {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].path != entries[j].path {
				return entries[i].path < entries[j].path
			}
			return entries[i].name < entries[j].name
		})
	}
	sortEntries(arrived)
	sortEntries(gone)

	taken := make([]bool, len(gone))
	record := func(nature Nature, from fnEntry, to fnEntry, same float64) {
		idx.Moves = append(idx.Moves, Move{Nature: nature, From: from.path, FromName: from.name, To: to.path, ToName: to.name, Lines: to.end - to.start + 1, Same: same})
		where := fmt.Sprintf("%s:%s()", from.path, shortName(from.name))
		if from.path == to.path {
			where = shortName(from.name) + "()"
		}
		verb := map[Nature]string{NatureMoved: "moved from", NatureExtracted: "extracted from"}[nature]
		detail := verb + " " + where
		if same < 1 {
			detail = fmt.Sprintf("%s, %d%% identical", detail, int(same*100+0.5))
		}
		idx.arrived[to.path] = append(idx.arrived[to.path], moveSpan{to.start, to.end, nature, detail, from.lines})
		if nature == NatureMoved {
			target := fmt.Sprintf("%s:%s()", to.path, shortName(to.name))
			if from.path == to.path {
				target = shortName(to.name) + "()"
			}
			idx.left[from.path] = append(idx.left[from.path], moveSpan{from.start, from.end, nature, "moved to " + target, to.lines})
		}
	}

	// Exact moves first, then moves with renamed identifiers, then moves
	// with small edits: each pass only looks at what the previous one left.
	match := func(pass func(a, g fnEntry) (float64, bool)) {
		for i, a := range arrived {
			if a.path == "" {
				continue
			}
			bestJ, bestSame := -1, 0.0
			for j, g := range gone {
				if taken[j] {
					continue
				}
				if same, ok := pass(a, g); ok && same > bestSame {
					bestJ, bestSame = j, same
				}
			}
			if bestJ >= 0 {
				taken[bestJ] = true
				record(NatureMoved, gone[bestJ], a, bestSame)
				arrived[i].path = "" // consumed
			}
		}
	}
	match(func(a, g fnEntry) (float64, bool) { return 1, a.rawHash == g.rawHash })
	match(func(a, g fnEntry) (float64, bool) { return 1, a.normHash == g.normHash })
	match(func(a, g fnEntry) (float64, bool) {
		if a.prints == nil || g.prints == nil {
			return 0, false
		}
		same := jaccard(a.prints, g.prints)
		return same, same >= moveMinSimilar
	})

	// Extractions: a new function whose fingerprints were in a function that
	// shrank, and are no longer in what remains of it.
	for i, a := range arrived {
		if a.path == "" || a.prints == nil {
			continue
		}
		bestSame, best := 0.0, -1
		for j, s := range shrunken {
			if s.before.prints == nil {
				continue
			}
			contained := containment(a.prints, s.before.prints)
			if contained < extractMinContained || containment(a.prints, s.after.prints) >= 0.5 {
				continue
			}
			if contained > bestSame || (contained == bestSame && s.before.path == a.path) {
				bestSame, best = contained, j
			}
		}
		if best >= 0 {
			record(NatureExtracted, shrunken[best].before, a, bestSame)
			arrived[i].path = ""
		}
	}
	sort.Slice(idx.Moves, func(i, j int) bool {
		if idx.Moves[i].To != idx.Moves[j].To {
			return idx.Moves[i].To < idx.Moves[j].To
		}
		return idx.Moves[i].ToName < idx.Moves[j].ToName
	})
	return idx
}

// jaccard is the similarity of two sorted fingerprint sets.
func jaccard(a, b []uint64) float64 {
	shared := sharedCount(a, b)
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// containment is the share of a found in b, both sorted.
func containment(a, b []uint64) float64 {
	if len(a) == 0 {
		return 0
	}
	return float64(sharedCount(a, b)) / float64(len(a))
}

func sharedCount(a, b []uint64) int {
	i, j, shared := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			shared++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return shared
}

// hunkNature tells whether a hunk is made of moved or extracted code: every
// added line sits in a function that arrived and exists, identifiers aside,
// in the function it came from; every removed line sits in a function that
// left and exists in the one it went to. A line that is in a moved function
// but not in its counterpart was edited on the way, and the hunk is read.
// Lines without a token (blank, comments) are never in the way.
func (idx *MoveIndex) hunkNature(path string, h Hunk) (Nature, string, bool) {
	if idx == nil {
		return "", "", false
	}
	nature, detail := Nature(""), ""
	carried := func(lines []string, first int, spans []moveSpan) bool {
		if len(lines) == 0 {
			return true
		}
		if len(spans) == 0 {
			return false
		}
		for i, line := range lines {
			shape, hasTokens := lineShape(line)
			if !hasTokens {
				continue
			}
			n := first + i
			found := false
			for _, s := range spans {
				if s.start <= n && n <= s.end && s.counterpart[shape] {
					found = true
					if nature == "" || s.nature == NatureExtracted {
						nature, detail = s.nature, s.detail
					}
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if !carried(h.Added, h.Start, idx.arrived[path]) || !carried(h.Removed, h.OldStart, idx.left[path]) || nature == "" {
		return "", "", false
	}
	return nature, detail, true
}
