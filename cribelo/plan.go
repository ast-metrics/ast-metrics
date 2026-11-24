package cribelo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Options drive one plan.
type Options struct {
	// Base is the reference to compare against; empty means the default branch.
	Base string
	// Top is the least number of files to read when Budget is zero; every
	// file at the high level is read as well, unless Exact is set.
	Top int
	// Exact reads exactly Top files, high or not.
	Exact bool
	// Budget is the reading time available; it overrides Top when set.
	Budget time.Duration
	// FailOn lists the kinds of fact that fail the gate when the change
	// states one (cycle, signature...). Empty sets no gate on facts.
	FailOn []string
	// FailOver fails the gate when the reading time of the whole change,
	// every file with something to read, is above it. Zero sets no gate.
	FailOver time.Duration
	// WeightsPath overrides the repository's weights file.
	WeightsPath string
	// Clones enables clone detection against the whole project.
	Clones bool
	// Coverage is a coverage report to read the untested changed lines
	// from, relative to the root; empty looks for one in the usual places,
	// "none" reads none.
	Coverage string
	// Now is the reference time for history windows; zero means time.Now.
	Now time.Time
}

// Skipped is a changed file that is not worth a reader, and why.
type Skipped struct {
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score,omitempty"`
	// Mechanical marks a code file whose every hunk is a move, a rename,
	// formatting, comments or imports: nothing in it changes behavior.
	Mechanical bool `json:"mechanical,omitempty"`
}

// Glanceable tells a skipped file a reviewer can mark as seen without
// opening it: mechanical code, a file renamed and left as it was, a
// generated file.
func (s Skipped) Glanceable() bool {
	return s.Mechanical || s.Kind == KindRenamed.String() || s.Kind == KindGenerated.String()
}

// Plan is the outcome: files to read, files to skip, and how the plan was made.
type Plan struct {
	Methodology string `json:"methodology"`
	Base        string `json:"base"`
	BaseSha     string `json:"base_sha"`
	HeadSha     string `json:"head_sha"`

	// Files are the code files of the change, those to read first and in
	// reading order, with the tests paired with them, then the rest by
	// rank; Read marks the selection.
	Files []FileReport `json:"files"`
	// Groups are the files to read, grouped by dependency.
	Groups []ReadingGroup `json:"groups,omitempty"`
	// Skipped lists every other changed file, grouped later by Kind.
	Skipped []Skipped `json:"skipped"`

	Changed     int `json:"changed"`
	ReadCount   int `json:"read"`
	ReadMinutes int `json:"read_minutes"`
	// LinesChanged counts the changed lines of the code and test files that
	// were analyzed; LinesToRead what is left once the mechanical hunks are
	// set aside. Tests count: a test placed after its file is read too.
	LinesChanged  int                    `json:"lines_changed"`
	LinesToRead   int                    `json:"lines_to_read"`
	LinesToGlance int                    `json:"lines_to_glance"`
	Natures       map[Nature]NatureCount `json:"natures,omitempty"`
	Moves         []Move                 `json:"moves,omitempty"`
	// Renames are the "old → new" pairs of the change with the number of
	// hunks each one touches, most frequent first.
	Renames []Rename `json:"renames,omitempty"`
	// Facts are what the diff does not show about the change: a cycle, a
	// dependency between modules that did not exist, a clone left behind, a
	// signature that callers rely on.
	Facts []Fact `json:"facts,omitempty"`
	// Coverage is the report the coverage facts come from, when one was read.
	Coverage  string `json:"coverage,omitempty"`
	TestFiles int    `json:"test_files"`
	// ChangeMinutes is the reading time of the whole change, every file
	// with something to read or glance at, budget or not.
	ChangeMinutes int `json:"change_minutes"`
	// Gate is empty when no gate is set, else "passed" or "failed";
	// GateReasons says why it failed, one line per breach.
	Gate            string   `json:"gate,omitempty"`
	GateReasons     []string `json:"gate_reasons,omitempty"`
	FailOn          []string `json:"fail_on,omitempty"`
	FailOverMinutes int      `json:"fail_over_minutes,omitempty"`
	Calibrated      bool     `json:"calibrated"`
	TrainedOn       int      `json:"trained_on,omitempty"`
	TrainedAt       string   `json:"trained_at,omitempty"`
	CloneIndexed    int      `json:"clone_indexed"`
}

// Run builds the plan of the working tree against the base branch.
func Run(repo *Repo, opts Options) (*Plan, error) {
	config, err := LoadConfig(repo.Root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	if err := SetFixPattern(config.FixPattern); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	if opts.Base == "" {
		opts.Base = config.Base
	}
	if opts.Top == 0 {
		opts.Top = config.Top
	}
	if opts.Top == 0 {
		opts.Top = 3
	}
	if len(opts.FailOn) == 0 {
		opts.FailOn = config.FailOn
	}
	if opts.FailOn, err = gateFacts(opts.FailOn); err != nil {
		return nil, err
	}
	if opts.FailOver == 0 && config.FailOver != "" {
		if opts.FailOver, err = time.ParseDuration(config.FailOver); err != nil {
			return nil, fmt.Errorf("%s: fail_over: %w", ConfigFile, err)
		}
	}
	if opts.Coverage == "" {
		opts.Coverage = config.Coverage
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	baseRef := opts.Base
	if baseRef == "" {
		baseRef, err = repo.DefaultBranch()
		if err != nil {
			return nil, err
		}
	}
	baseSha, err := repo.ResolveRef(baseRef)
	if err != nil {
		return nil, err
	}
	headSha, err := repo.ResolveRef("HEAD")
	if err != nil {
		return nil, err
	}
	mergeBase := repo.MergeBase(baseSha, headSha)

	changes, err := repo.Changes(mergeBase, "")
	if err != nil {
		return nil, err
	}
	changes = filterIgnored(changes, config)

	weightsPath := opts.WeightsPath
	if weightsPath == "" {
		weightsPath = filepath.Join(repo.Root, filepath.FromSlash(WeightsFile))
	} else if _, err := os.Stat(weightsPath); err != nil {
		// Only the default file may be absent: an explicit one is a typo.
		return nil, fmt.Errorf("weights: %w", err)
	}
	weights, err := LoadWeights(weightsPath)
	if err != nil {
		return nil, fmt.Errorf("weights: %w", err)
	}

	plan := &Plan{
		Methodology: MethodologyVersion, Base: baseRef, BaseSha: mergeBase, HeadSha: headSha,
		Changed:    len(changes),
		Calibrated: weights.Calibrated, TrainedOn: weights.TrainedOn, TrainedAt: weights.TrainedAt,
	}
	if len(changes) == 0 {
		plan.gate(opts)
		return plan, nil
	}

	headSource := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(rel)))
		if err != nil {
			return nil
		}
		return data
	}

	// The whole head tree is parsed: hubs and clones are project properties.
	tree, err := repo.TreeFiles()
	if err != nil {
		return nil, err
	}
	kept := tree[:0]
	for _, rel := range tree {
		if !config.Ignored(rel) {
			kept = append(kept, rel)
		}
	}
	// A change that touches no file of a parsed language has nothing to
	// measure: the tree is not parsed for it.
	measurable := false
	for _, c := range changes {
		if !c.Binary && analyzedExtensions[strings.ToLower(path.Ext(c.Path))] {
			measurable = true
			break
		}
	}
	if !measurable {
		kept = nil
	}
	head, err := parseTree(repo.Root, kept)
	if err != nil {
		return nil, err
	}

	kinds := map[string]Kind{}
	var codeChanges, deletedCode, testChanges []Change
	var testPaths []string
	for _, c := range changes {
		var isTest bool
		if f := head.Files[c.Path]; f != nil {
			isTest = f.GetIsTest()
		}
		kind := Classify(c, headSource(c.Path), isTest)
		kinds[c.Path] = kind
		switch kind {
		case KindCode:
			codeChanges = append(codeChanges, c)
		case KindTest:
			testPaths = append(testPaths, c.Path)
			if c.Status != 'D' && !c.Binary && analyzedExtensions[strings.ToLower(path.Ext(c.Path))] {
				testChanges = append(testChanges, c)
			}
		case KindDeleted:
			if isDeletedCode(c) {
				deletedCode = append(deletedCode, c)
			}
		}
	}
	plan.TestFiles = len(testPaths)

	// Only the changed files are needed from the base version. Deleted code
	// files come along: their functions may have moved somewhere else.
	base, cleanup, err := snapshotBase(repo, mergeBase, append(append([]Change{}, codeChanges...), deletedCode...))
	if err != nil {
		return nil, err
	}
	defer cleanup()

	history, err := repo.HistorySince("HEAD", opts.Now.Unix()-Window)
	if err != nil {
		return nil, err
	}

	headGraph := analyzer.FileDependencies(head.All)
	measure := &Measure{
		Head: head, Base: base, Afferent: afferentFromGraph(head, headGraph), History: history,
		At: opts.Now.Unix(), TestPaths: testPaths, HeadSource: headSource,
	}
	if opts.Clones {
		measure.Clones = buildCloneIndex(head)
		plan.CloneIndexed = measure.Clones.Len()
	}
	measure.Moves = buildMoveIndex(head, base, codeChanges, deletedCode)
	plan.Moves = measure.Moves.Moves

	for _, c := range codeChanges {
		report := measure.features(c)
		report.Score, report.Contributions = weights.Score(report.Values)
		report.Level = weights.Level(report.Score)
		report.Reasons = reasons(&report)
		plan.Files = append(plan.Files, report)
	}
	resolveRenames(plan.Files)
	plan.Renames = renamesOf(plan.Files)
	graphs := buildGraphs(repo.Root, headGraph, head, base, codeChanges, deletedCode)
	tests := make([]FileReport, 0, len(testChanges))
	for _, c := range testChanges {
		tests = append(tests, measure.features(c))
	}
	plan.Natures = map[Nature]NatureCount{}
	for _, report := range append(append([]FileReport{}, plan.Files...), tests...) {
		plan.LinesChanged += report.Added + report.Deleted
		plan.LinesToRead += report.ToRead
		plan.LinesToGlance += report.ToGlance
		if report.ToRead+report.ToGlance > 0 {
			plan.ChangeMinutes += report.Minutes
		}
		for n, count := range report.Natures {
			total := plan.Natures[n]
			total.Hunks += count.Hunks
			total.Lines += count.Lines
			plan.Natures[n] = total
		}
	}
	rank(plan.Files)
	selectReads(plan, opts)
	var coverage *Coverage
	if report := opts.Coverage; report != "none" {
		if report == "" {
			report = findCoverage(repo.Root)
		}
		if report != "" {
			if coverage, err = loadCoverage(repo.Root, report); err != nil {
				return nil, err
			}
			plan.Coverage = report
		}
	}
	plan.Facts = collectFacts(graphs, head, base, codeChanges, plan.Files, repo, mergeBase, opts.Now.Unix(), coverage)
	orderReads(plan, graphs.head.efferent, head, tests, budgetMinutes(opts))

	readTests := map[string]bool{}
	for _, f := range plan.Files {
		if f.Test {
			readTests[f.Path] = true
		}
	}
	for _, c := range changes {
		kind := kinds[c.Path]
		if kind == KindCode || readTests[c.Path] {
			continue
		}
		plan.Skipped = append(plan.Skipped, Skipped{Path: c.Path, Kind: kind.String(), Reason: kind.Label()})
	}
	for _, f := range plan.Files {
		if f.Read || f.Test {
			continue
		}
		plan.Skipped = append(plan.Skipped, Skipped{Path: f.Path, Kind: "code", Reason: skipReason(f), Score: f.Score, Mechanical: allMechanical(f)})
	}
	sort.SliceStable(plan.Skipped, func(i, j int) bool {
		if plan.Skipped[i].Kind != plan.Skipped[j].Kind {
			return plan.Skipped[i].Kind < plan.Skipped[j].Kind
		}
		return plan.Skipped[i].Path < plan.Skipped[j].Path
	})

	plan.gate(opts)
	return plan, nil
}

// isDeletedCode tells a removed file whose functions are worth looking for
// elsewhere in the change: a code file in a parsed language, not a test, not
// generated.
func isDeletedCode(c Change) bool {
	return c.Status == 'D' && analyzedExtensions[strings.ToLower(path.Ext(c.Path))] && !IsTestPath(c.Path) && !IsGeneratedPath(c.Path)
}

func filterIgnored(changes []Change, config Config) []Change {
	if len(config.Ignore) == 0 {
		return changes
	}
	kept := changes[:0]
	for _, c := range changes {
		if !config.Ignored(c.Path) {
			kept = append(kept, c)
		}
	}
	return kept
}

// snapshotBase writes the base version of the modified code files into a
// temporary directory and parses it. New files have no base.
func snapshotBase(repo *Repo, baseSha string, changes []Change) (*Parsed, func(), error) {
	contents := map[string][]byte{}
	for _, c := range changes {
		if c.Status == 'A' {
			continue
		}
		old := c.OldPath
		if old == "" {
			old = c.Path
		}
		data, err := repo.Show(baseSha, old)
		if err != nil {
			continue
		}
		contents[c.Path] = data
	}
	noop := func() {}
	if len(contents) == 0 {
		return &Parsed{Files: map[string]*pb.File{}}, noop, nil
	}
	dir, err := os.MkdirTemp("", "cribelo-base-")
	if err != nil {
		return nil, noop, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	parsed, err := parseSnapshot(dir, contents)
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	return parsed, cleanup, nil
}

// rank orders files by score, then by size, then by path: fully deterministic.
func rank(files []FileReport) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Score != files[j].Score {
			return files[i].Score > files[j].Score
		}
		if files[i].Added != files[j].Added {
			return files[i].Added > files[j].Added
		}
		return files[i].Path < files[j].Path
	})
}

// selectReads marks the files to read: every file at the high level and at
// least the top N, or exactly N with Exact, or as many as the budget allows.
// A budget is not spent on a file with no measurable change. A file whose
// every hunk is mechanical is never read, only listed with its reason.
// Files are ranked, so the high ones come first.
func selectReads(plan *Plan, opts Options) {
	budget := budgetMinutes(opts)
	for i := range plan.Files {
		f := &plan.Files[i]
		if allMechanical(*f) {
			continue
		}
		if budget > 0 {
			if plan.ReadCount > 0 && (plan.ReadMinutes+f.Minutes > budget || unchanged(*f)) {
				continue
			}
		} else if plan.ReadCount >= opts.Top && (opts.Exact || f.Level != LevelHigh) {
			break
		}
		f.Read = true
		plan.ReadCount++
		plan.ReadMinutes += f.Minutes
	}
}

// budgetMinutes is the reading time allowed, in whole minutes, 0 for none.
func budgetMinutes(opts Options) int {
	if opts.Budget <= 0 {
		return 0
	}
	return int(math.Ceil(opts.Budget.Minutes()))
}

// unchanged reports a code file whose change is too small to measure, or
// whose every hunk is mechanical.
func unchanged(f FileReport) bool {
	return allMechanical(f) || (f.Values[FeatureComplexityAdded] == 0 && f.Added <= 10 && f.Clone == nil)
}

// allMechanical reports a classified file whose hunks are all mechanical:
// no behavior hunk, no move that brought new lines along.
func allMechanical(f FileReport) bool {
	if f.Hunks == nil {
		return false
	}
	for _, h := range f.Hunks {
		if !h.Mechanical() {
			return false
		}
	}
	return true
}

// skipReason explains why a ranked code file is not in the reading list.
func skipReason(f FileReport) string {
	if allMechanical(f) {
		return mechanicalReason(f)
	}
	if unchanged(f) {
		return "no measurable change"
	}
	return "ranked lower"
}

// mechanicalReason names what a file made only of mechanical hunks holds.
func mechanicalReason(f FileReport) string {
	for _, n := range []Nature{NatureMoved, NatureExtracted, NatureRename, NatureFormat, NatureComment, NatureImports} {
		if c, ok := f.Natures[n]; ok && c.Hunks == len(f.Hunks) {
			return map[Nature]string{NatureMoved: "moved code only", NatureExtracted: "extracted code only", NatureRename: "renames only", NatureFormat: "formatting only", NatureComment: "comments only", NatureImports: "imports only"}[n]
		}
	}
	return "mechanical changes only"
}

// gateFacts checks the kinds of fact a gate may be set on. The levels of
// the old gate are refused with a hint; "never" sets no gate.
func gateFacts(kinds []string) ([]string, error) {
	var out []string
	for _, kind := range kinds {
		switch kind {
		case "never", "":
		case "high", "medium", "any", "low":
			return nil, fmt.Errorf("fail-on: %q is a score level, not a gate any more; name a fact (%s) or set --fail-over 1h", kind, strings.Join(factKinds, ", "))
		default:
			if _, ok := factOrder[kind]; !ok {
				return nil, fmt.Errorf("fail-on: unknown fact %q (%s)", kind, strings.Join(factKinds, ", "))
			}
			out = append(out, kind)
		}
	}
	return out, nil
}

// factKinds lists the kinds a gate may be set on, in display order.
var factKinds = []string{FactCycle, FactSignature, FactHistory, FactDependency, FactLibrary, FactClone, FactAPI, FactCoverage}

// gate applies the gate the options set: nothing when none is set, else
// "failed" with one reason per breach, or "passed". The reading time is
// the one of the whole change, not of the files that fit the budget: the
// gate says whether the change is readable, not whether the plan is.
func (p *Plan) gate(opts Options) {
	p.FailOn = opts.FailOn
	p.FailOverMinutes = budgetMinutes(Options{Budget: opts.FailOver})
	if len(p.FailOn) == 0 && p.FailOverMinutes == 0 {
		return
	}
	p.Gate = "passed"
	if p.FailOverMinutes > 0 && p.ChangeMinutes > p.FailOverMinutes {
		p.GateReasons = append(p.GateReasons, fmt.Sprintf("reading time %s, over the %s allowed", Duration(p.ChangeMinutes), Duration(p.FailOverMinutes)))
	}
	for _, kind := range p.FailOn {
		for _, f := range p.Facts {
			if f.Kind == kind {
				p.GateReasons = append(p.GateReasons, fmt.Sprintf("%s: %s", f.Path, f.Text))
			}
		}
	}
	if len(p.GateReasons) > 0 {
		p.Gate = "failed"
	}
}

// JSON renders the whole plan.
func (p *Plan) JSON() (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// File finds the report of a code file by path, nil when the path is not
// one of the analyzed files.
func (p *Plan) File(path string) *FileReport {
	for i := range p.Files {
		if p.Files[i].Path == path {
			return &p.Files[i]
		}
	}
	return nil
}

// Highest returns the level of the top ranked file, low when none.
func (p *Plan) Highest() Level {
	if len(p.Files) == 0 {
		return LevelLow
	}
	return p.Files[0].Level
}

// Count returns how many ranked files reach a level.
func (p *Plan) Count(level Level) int {
	n := 0
	for _, f := range p.Files {
		if f.Level == level {
			n++
		}
	}
	return n
}
