package cribelo

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	// FailOn fails the gate when a file reaches this level: high, medium, never.
	FailOn string
	// WeightsPath overrides the repository's weights file.
	WeightsPath string
	// Clones enables clone detection against the whole project.
	Clones bool
	// Now is the reference time for history windows; zero means time.Now.
	Now time.Time
}

// Skipped is a changed file that is not worth a reader, and why.
type Skipped struct {
	Path   string  `json:"path"`
	Kind   string  `json:"kind"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score,omitempty"`
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
	// LinesChanged counts the changed lines of the ranked code files;
	// LinesToRead what is left once the mechanical hunks are set aside.
	LinesChanged int                    `json:"lines_changed"`
	LinesToRead  int                    `json:"lines_to_read"`
	Natures      map[Nature]natureCount `json:"natures,omitempty"`
	Moves        []Move                 `json:"moves,omitempty"`
	// Renames are the "old → new" pairs of the change with the number of
	// hunks each one touches, most frequent first.
	Renames []Rename `json:"renames,omitempty"`
	// Facts are what the diff does not show about the change: a cycle, a
	// dependency between modules that did not exist, a clone left behind, a
	// signature that callers rely on.
	Facts        []Fact `json:"facts,omitempty"`
	TestFiles    int    `json:"test_files"`
	Gate         string `json:"gate"`
	FailOn       string `json:"fail_on"`
	Calibrated   bool   `json:"calibrated"`
	TrainedOn    int    `json:"trained_on,omitempty"`
	TrainedAt    string `json:"trained_at,omitempty"`
	CloneIndexed int    `json:"clone_indexed"`
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
	if opts.FailOn == "" {
		opts.FailOn = config.FailOn
	}
	if opts.FailOn == "" {
		opts.FailOn = "never"
	}
	switch opts.FailOn {
	case "high", "medium", "any", "never":
	default:
		return nil, fmt.Errorf("fail-on: unknown level %q (high, medium or never)", opts.FailOn)
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
		Changed: len(changes), FailOn: opts.FailOn,
		Calibrated: weights.Calibrated, TrainedOn: weights.TrainedOn, TrainedAt: weights.TrainedAt,
	}
	if len(changes) == 0 {
		plan.Gate = "passed"
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
	head, err := parseFiles(repo.Root, kept)
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

	measure := &Measure{
		Head: head, Base: base, Afferent: afferentCounts(head), History: history,
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
	graphs := buildGraphs(repo.Root, head, base, codeChanges, deletedCode)
	plan.Facts = collectFacts(graphs, head, base, codeChanges, plan.Files)
	plan.Natures = map[Nature]natureCount{}
	for _, report := range plan.Files {
		plan.LinesChanged += report.Added + report.Deleted
		plan.LinesToRead += report.ToRead
		for n, count := range report.Natures {
			total := plan.Natures[n]
			total.Hunks += count.Hunks
			total.Lines += count.Lines
			plan.Natures[n] = total
		}
	}
	rank(plan.Files)
	selectReads(plan, opts)
	tests := make([]FileReport, 0, len(testChanges))
	for _, c := range testChanges {
		tests = append(tests, measure.features(c))
	}
	orderReads(plan, graphs.head.efferent, head, tests)

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
		plan.Skipped = append(plan.Skipped, Skipped{Path: f.Path, Kind: "code", Reason: skipReason(f), Score: f.Score})
	}
	sort.SliceStable(plan.Skipped, func(i, j int) bool {
		if plan.Skipped[i].Kind != plan.Skipped[j].Kind {
			return plan.Skipped[i].Kind < plan.Skipped[j].Kind
		}
		return plan.Skipped[i].Path < plan.Skipped[j].Path
	})

	plan.Gate = gate(plan.Files, opts.FailOn)
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
	budget := 0
	if opts.Budget > 0 {
		budget = int(math.Ceil(opts.Budget.Minutes()))
	}
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

// unchanged reports a code file whose change is too small to measure, or
// whose every hunk is mechanical.
func unchanged(f FileReport) bool {
	return allMechanical(f) || (f.Values[FeatureComplexityAdded] == 0 && f.Added <= 10 && f.Clone == nil)
}

// allMechanical reports a classified file without a behavior hunk.
func allMechanical(f FileReport) bool {
	return f.Hunks != nil && f.Natures[NatureBehavior].Hunks == 0
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

// gate is "failed" when a ranked file reaches the fail-on level.
func gate(files []FileReport, failOn string) string {
	var threshold Level
	switch failOn {
	case "high":
		threshold = LevelHigh
	case "medium", "any":
		threshold = LevelMedium
	default:
		return "passed"
	}
	for _, f := range files {
		if f.Level == LevelHigh || (threshold == LevelMedium && f.Level == LevelMedium) {
			return "failed"
		}
	}
	return "passed"
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
