package cribelo

import (
	"os"
	"path"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/review"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Window is the span of history a file's past is judged on, and the span in
// which a later fix counts against a change.
const Window int64 = 90 * 24 * 3600

// TouchedFunction is a function whose lines intersect the change.
type TouchedFunction struct {
	Name   string `json:"name"`
	Line   int    `json:"line"`
	End    int    `json:"end,omitempty"`
	Before int    `json:"before"`
	After  int    `json:"after"`
	New    bool   `json:"new"`
	// ToRead is the reading cost of the hunk lines inside the function.
	ToRead int `json:"to_read,omitempty"`
	// Glance marks a function that gained no decision and stays simple
	// (cyclomatic complexity at most GlanceComplexity): its lines are glanced
	// at rather than read.
	Glance bool `json:"glance,omitempty"`
}

// LineRange is a span of lines of the new version of a file, both ends
// included.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// rangeGap is the number of lines two spans to read may be apart and still
// be told as one: the closing brace, the blank line and the comment between
// two functions are not worth a second span.
const rangeGap = 3

// GlanceComplexity is the cyclomatic complexity up to which a function that
// gained no decision is glanced at rather than read. Measured on the bench:
// such functions are repaired half as often as the others.
const GlanceComplexity = 3

// FileReport is everything known about one ranked file.
type FileReport struct {
	Path          string              `json:"path"`
	Status        string              `json:"status"`
	Added         int                 `json:"added"`
	Deleted       int                 `json:"deleted"`
	Score         float64             `json:"score"`
	Level         Level               `json:"level"`
	Minutes       int                 `json:"minutes"`
	Read          bool                `json:"read"`
	Reasons       []string            `json:"reasons"`
	Values        map[Feature]float64 `json:"features"`
	Contributions []Contribution      `json:"contributions"`
	Functions     []TouchedFunction   `json:"functions,omitempty"`
	Clone         *CloneMatch         `json:"clone,omitempty"`
	// Hunks are the blocks of the diff with their nature; ToRead is the
	// lines a reader spends on them once moves, renames, formatting and
	// imports are set aside.
	Hunks []HunkReport `json:"hunks,omitempty"`
	// ToRead counts the lines to read closely; ToGlance the lines to glance
	// at: in a function that gained no decision and stays simple, or outside
	// every function (declarations, fields, data) with no decision.
	ToRead   int `json:"to_read"`
	ToGlance int `json:"to_glance"`
	// Ranges are the line spans of the new version that hold the code to
	// read closely, in order; GlanceRanges those that hold the code to
	// glance at. The two never overlap.
	Ranges       []LineRange            `json:"read_ranges,omitempty"`
	GlanceRanges []LineRange            `json:"glance_ranges,omitempty"`
	Natures      map[Nature]NatureCount `json:"natures,omitempty"`
	// Test marks a test file read along with the file it exercises.
	Test bool `json:"test,omitempty"`
}

// ReadLines are the lines of the file to read closely and to glance at. A
// file whose hunks were not classified is read whole.
func (f *FileReport) ReadLines() (closely, glance int) {
	if f.Hunks == nil {
		return f.Added + f.Deleted, 0
	}
	return f.ToRead, f.ToGlance
}

// IsNew says whether the file did not exist on the base.
func (f *FileReport) IsNew() bool { return f.Status == "A" || f.Values[FeatureNewFile] > 0 }

// Measure holds what the features are computed from.
type Measure struct {
	Head     *Parsed
	Base     *Parsed
	Afferent map[string]int
	History  *History
	At       int64
	// TestPaths are the test files of the change, used to decide whether a
	// file's change came with a test change.
	TestPaths []string
	Clones    *CloneIndex
	// HeadSource reads the head content of a file for clone detection.
	HeadSource func(rel string) []byte
	// Moves knows the functions that moved within the change; nil when
	// move detection is off.
	Moves *MoveIndex
}

// features computes the raw feature values of one code change.
func (m *Measure) features(c Change) FileReport {
	report := FileReport{
		Path: c.Path, Status: string(c.Status), Added: c.Added, Deleted: c.Deleted,
		Values: map[Feature]float64{},
	}
	report.Values[FeatureLinesAdded] = float64(c.Added)
	report.Values[FeatureLinesDeleted] = float64(c.Deleted)
	if c.Status == 'A' {
		report.Values[FeatureNewFile] = 1
	}

	var head, base *pb.File
	if m.Head != nil {
		head = m.Head.Files[c.Path]
	}
	if m.Base != nil {
		base = m.Base.Files[c.Path]
	}

	headFunctions := review.CollectFunctions(head)
	baseFunctions := review.CollectFunctions(base)
	complexityAdded, maxComplexity := 0, 0
	for name, fn := range headFunctions {
		if fn.Location == nil {
			continue
		}
		start, end := int(fn.Location.StartLine), int(fn.Location.EndLine)
		if c.Status != 'A' && !touches(c.Hunks, start, end) {
			continue
		}
		after := cyclomatic(fn)
		touched := TouchedFunction{Name: name, Line: start, End: end, After: after}
		if baseFn, existed := baseFunctions[name]; existed {
			touched.Before = cyclomatic(baseFn)
			if after > touched.Before {
				complexityAdded += after - touched.Before
			}
		} else {
			touched.New = true
			// A trivial new function (one path) adds nothing to read.
			complexityAdded += after - 1
		}
		if after > maxComplexity {
			maxComplexity = after
		}
		report.Functions = append(report.Functions, touched)
	}
	sort.Slice(report.Functions, func(i, j int) bool {
		gi, gj := report.Functions[i].growth(), report.Functions[j].growth()
		if gi != gj {
			return gi > gj
		}
		return report.Functions[i].Line < report.Functions[j].Line
	})
	report.Values[FeatureComplexityAdded] = float64(complexityAdded)
	report.Values[FeatureMaxComplexity] = float64(maxComplexity)
	report.Values[FeatureAfferent] = float64(m.Afferent[c.Path])

	commits, fixes := m.History.Before(c.Path, m.At, Window)
	report.Values[FeatureChurn90d] = float64(commits)
	report.Values[FeatureFixes90d] = float64(fixes)

	if !hasRelatedTest(c.Path, m.TestPaths) {
		report.Values[FeatureUntested] = 1
	}

	if m.Clones != nil && m.Clones.Len() > 0 && m.HeadSource != nil {
		if source := m.HeadSource(c.Path); source != nil {
			lines := strings.Split(string(source), "\n")
			for _, fn := range report.Functions {
				headFn := headFunctions[fn.Name]
				if headFn == nil || headFn.Location == nil {
					continue
				}
				body := sliceLines(lines, int(headFn.Location.StartLine), int(headFn.Location.EndLine))
				if match := m.Clones.Closest(c.Path, fn.Name, body); match != nil {
					if report.Clone == nil || match.Similarity > report.Clone.Similarity {
						report.Clone = match
						report.Values[FeatureClone] = match.Similarity
						report.Clone.Function = fn.Name + " ~ " + match.Function
					}
				}
			}
		}
	}

	language := languageOf(head, c.Path)
	var idents identifierSets
	if head != nil {
		if source, err := os.ReadFile(head.Path); err == nil {
			idents.head = identifiers(source)
		}
	}
	if base != nil {
		if source, err := os.ReadFile(base.Path); err == nil {
			idents.base = identifiers(source)
		}
	}
	report.Hunks = classifyHunks(c, language, m.Moves, idents)
	report.Natures = countNatures(report.Hunks)
	report.tally()
	return report
}

// tally counts, from the hunks and the touched functions, the lines to read
// closely and the lines to glance at, and the reading time.
func (f *FileReport) tally() {
	for i := range f.Functions {
		fn := &f.Functions[i]
		fn.ToRead = 0
		fn.Glance = fn.growth() <= 0 && fn.After <= GlanceComplexity
	}
	for j := range f.Hunks {
		f.Hunks[j].Glance = 0
	}
	f.Ranges, f.GlanceRanges = spansOf(attributeReads(f.Functions, f.Hunks))
	f.ToRead, f.ToGlance = 0, 0
	for _, h := range f.Hunks {
		f.ToRead += h.ReadCost() - h.Glance
		f.ToGlance += h.Glance
	}
	f.Minutes = readingMinutes(f.ToRead, f.ToGlance)
}

// attributeReads charges each hunk's reading cost to the touched functions
// it spans, line by line, the innermost function taking a nested line, and
// counts the lines of the hunk that are glanced at: those in a glance
// function, and those outside every function that hold no decision
// (declarations, fields, constants, data). A hunk with no new-side span (a
// pure deletion) goes to the function that holds its position. It returns
// where the reading happens: the lines of code read closely, those glanced
// at, and the filler, changed lines a reader is not pointed to (blank lines,
// comments, delimiters). A move that was edited on the way is read closely
// over every line it has in a function to read, since the edited ones are
// not located.
func attributeReads(functions []TouchedFunction, hunks []HunkReport) (closely, glance []int, filler map[int]bool) {
	filler = map[int]bool{}
	innermost := func(line int) int {
		best := -1
		for i, fn := range functions {
			if line < fn.Line || line > fn.End {
				continue
			}
			if best < 0 || fn.End-fn.Line < functions[best].End-functions[best].Line {
				best = i
			}
		}
		return best
	}
	for j := range hunks {
		h := &hunks[j]
		cost := h.ReadCost()
		if cost == 0 {
			continue
		}
		// The check of a rename and the look at where a block went are
		// glances by construction; the lines a move edited on the way
		// follow the rule of the function that holds them.
		base := 0
		switch h.Nature {
		case NatureRename:
			base = cost
		case NatureMoved, NatureExtracted:
			base = cost - h.New
		}
		h.Glance = base
		cost -= base
		span := h.End - h.Start + 1
		if span < 1 {
			span = 1
		}
		counts := map[int]int{}
		glanced := 0
		for line := h.Start; line < h.Start+span; line++ {
			read := false
			if cost > 0 {
				if i := innermost(line); i >= 0 {
					counts[i]++
					read = !functions[i].Glance
				} else {
					read = h.decides(line)
				}
			}
			if !read {
				glanced++
			}
			switch {
			case !h.locates(line):
				filler[line] = true
			case read:
				closely = append(closely, line)
			default:
				glance = append(glance, line)
			}
		}
		if cost == 0 {
			continue
		}
		for i, n := range counts {
			functions[i].ToRead += cost * n / span
		}
		h.Glance += cost * glanced / span
	}
	return closely, glance, filler
}

// lineRanges folds lines into spans. Two lines fall in the same span when
// they are at most rangeGap apart, or when only filler separates them: the
// comment above a function does not cut a span in two. A span of apart
// between two lines always does.
func lineRanges(lines []int, filler map[int]bool, apart ...LineRange) []LineRange {
	sort.Ints(lines)
	joins := func(from, to int) bool {
		for _, r := range apart {
			if r.Start > from && r.Start < to {
				return false
			}
		}
		if to-from <= rangeGap+1 {
			return true
		}
		for line := from + 1; line < to; line++ {
			if !filler[line] {
				return false
			}
		}
		return true
	}
	var out []LineRange
	for _, line := range lines {
		if n := len(out); n > 0 && joins(out[n-1].End, line) {
			out[n-1].End = max(out[n-1].End, line)
			continue
		}
		out = append(out, LineRange{Start: line, End: line})
	}
	return out
}

// spansOf turns the lines read closely and the lines glanced at into
// spans. A line to glance at that a span to read closely already covers is
// read with it, and no span to glance at runs over one to read closely.
func spansOf(closely, glance []int, filler map[int]bool) (read, glanced []LineRange) {
	read = lineRanges(closely, filler)
	var left []int
next:
	for _, line := range glance {
		for _, r := range read {
			if line >= r.Start && line <= r.End {
				continue next
			}
		}
		left = append(left, line)
	}
	return read, lineRanges(left, filler, read...)
}

// languageOf names the language of a file, from the parser when it saw the
// file, from the extension otherwise.
func languageOf(file *pb.File, rel string) string {
	if file != nil && file.ProgrammingLanguage != "" {
		return file.ProgrammingLanguage
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".go":
		return "Golang"
	case ".php":
		return "PHP"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".java":
		return "Java"
	case ".cs":
		return "C#"
	}
	return ""
}

func (f TouchedFunction) growth() int {
	if f.New {
		return f.After - 1
	}
	return f.After - f.Before
}

func cyclomatic(fn *pb.StmtFunction) int {
	if fn == nil || fn.Stmts == nil || fn.Stmts.Analyze == nil || fn.Stmts.Analyze.Complexity == nil || fn.Stmts.Analyze.Complexity.Cyclomatic == nil {
		return 0
	}
	return int(*fn.Stmts.Analyze.Complexity.Cyclomatic)
}

// readingMinutes estimates careful reading at about twenty lines a minute,
// never less than a minute per file.
func readingMinutes(read, glance int) int {
	minutes := (read*3 + glance + 59) / 60
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}

// hasRelatedTest reports whether one of the test files of the change looks
// like it exercises the given file: same directory, or a name built on the
// file's stem (invoice.go and invoice_test.go, Session.php and SessionTest.php,
// token.py and test_token.py).
func hasRelatedTest(file string, testPaths []string) bool {
	if len(testPaths) == 0 {
		return false
	}
	dir := path.Dir(file)
	stem := strings.ToLower(stemOf(file))
	for _, test := range testPaths {
		if path.Dir(test) == dir {
			return true
		}
		testStem := strings.ToLower(stemOf(test))
		if testStem == stem {
			return true
		}
		// A stem of one or two letters would be found in almost any name.
		if len(stem) >= 3 && strings.Contains(testStem, stem) {
			return true
		}
	}
	return false
}

func stemOf(file string) string {
	base := path.Base(file)
	if dot := strings.IndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	return base
}
