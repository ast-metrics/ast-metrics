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
	Before int    `json:"before"`
	After  int    `json:"after"`
	New    bool   `json:"new"`
}

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
	Hunks   []HunkReport           `json:"hunks,omitempty"`
	ToRead  int                    `json:"to_read"`
	Natures map[Nature]natureCount `json:"natures,omitempty"`
}

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
		touched := TouchedFunction{Name: name, Line: start, After: after}
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
	for _, h := range report.Hunks {
		report.ToRead += h.ReadCost()
	}
	report.Minutes = readingMinutes(report.ToRead, 0)
	return report
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
func readingMinutes(added, deleted int) int {
	lines := added + deleted
	minutes := (lines + 19) / 20
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
