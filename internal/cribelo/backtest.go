package cribelo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultWorkers leaves room for the parsers, which already use several
// threads each.
func defaultWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	return n
}

// BacktestOptions drive a replay of the repository's own history.
type BacktestOptions struct {
	// Ref is the main line to replay; empty means the default branch.
	Ref string
	// Max is the number of landings (merges or direct commits) to replay.
	Max int
	// Top is the size of the reading list measured.
	Top int
	// MinFiles is the minimum number of code files for a landing to count:
	// with fewer files than the reading list, there is nothing to rank.
	MinFiles int
	// Now bounds the label window; zero means time.Now.
	Now time.Time
	// Progress is called after each landing.
	Progress func(done, total int)
	// Workers is the number of landings replayed at once; zero picks one
	// from the number of CPUs.
	Workers int
	// Label is LabelInduced (the default) or LabelFixed.
	Label string
}

// Sample is one code file of one landing, with its features and its label.
type Sample struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	// ToRead is what is left of the lines once the mechanical hunks are set
	// aside; Natures counts those hunks.
	ToRead  int                    `json:"to_read"`
	Natures map[Nature]natureCount `json:"natures,omitempty"`
	Values  map[Feature]float64    `json:"features"`
	// Fixed: a fix commit touched the file within the window.
	Fixed bool `json:"fixed"`
	// Induced: a fix within the window repaired lines this change wrote in
	// the file (SZZ).
	Induced bool `json:"induced"`
}

// Labels of the backtest: what counts as a file that needed a reader.
const (
	LabelInduced = "induced"
	LabelFixed   = "fixed"
)

// positive is the label the backtest measures and calibrates on.
func (b *Backtest) positive(s Sample) bool {
	if b.Label == LabelFixed {
		return s.Fixed
	}
	return s.Induced
}

// ChangeEval is one replayed landing.
type ChangeEval struct {
	Sha     string   `json:"sha"`
	Time    int64    `json:"time"`
	Subject string   `json:"subject"`
	Files   []Sample `json:"files"`
}

// Metrics measure a ranking over a set of landings.
type Metrics struct {
	Changes    int     `json:"changes"`
	Files      int     `json:"files"`
	FixedFiles int     `json:"fixed_files"`
	Covered    int     `json:"covered"`
	Coverage   float64 `json:"coverage"`
	LinesRead  int     `json:"lines_read"`
	LinesTotal int     `json:"lines_total"`
	LinesShare float64 `json:"lines_share"`
	// ToRead is what the hunk classification leaves to read of every file,
	// and its share of the changed lines.
	ToRead      int     `json:"to_read"`
	ToReadShare float64 `json:"to_read_share"`
	// ToReadTop is the same for the top files.
	ToReadTop      int     `json:"to_read_top"`
	ToReadTopShare float64 `json:"to_read_top_share"`
	BaselineCover  float64 `json:"baseline_coverage"`
	BaselineShare  float64 `json:"baseline_lines_share"`
	// PathOrder reads the first files by path, the order a pull request
	// shows them in.
	PathOrderCover float64 `json:"path_order_coverage"`
	PathOrderShare float64 `json:"path_order_lines_share"`
	// The rule the plan applies: every high file, at least Top.
	RuleRead     int     `json:"rule_files_read"`
	RuleCovered  int     `json:"rule_covered"`
	RuleCoverage float64 `json:"rule_coverage"`
	RuleShare    float64 `json:"rule_lines_share"`
}

// Backtest is the replay: every eligible landing with its files, plus why
// the other landings were left out.
type Backtest struct {
	Methodology string         `json:"methodology"`
	Label       string         `json:"label"`
	Ref         string         `json:"ref"`
	Top         int            `json:"top"`
	Landings    int            `json:"landings"`
	Changes     []ChangeEval   `json:"changes"`
	LeftOut     map[string]int `json:"left_out"`
	From        int64          `json:"from"`
	To          int64          `json:"to"`
	Generic     Metrics        `json:"generic"`
	// GenericHeldOut and Calibrated compare the two models on the changes
	// the calibration did not train on.
	GenericHeldOut *Metrics `json:"generic_held_out,omitempty"`
	Calibrated     *Metrics `json:"calibrated,omitempty"`
	Weights        string   `json:"weights"`
}

// RunBacktest replays the last landings of the main line. Each landing is
// measured as it was: its files parsed at that commit, its history read up
// to that moment. Two approximations are deliberate and cheap: the hub
// signal uses today's dependency graph, and clone detection is off, since
// both would require parsing the whole project at every commit.
func RunBacktest(repo *Repo, opts BacktestOptions) (*Backtest, error) {
	config, err := LoadConfig(repo.Root)
	if err != nil {
		return nil, err
	}
	if err := SetFixPattern(config.FixPattern); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	if opts.Ref == "" {
		opts.Ref, err = repo.DefaultBranch()
		if err != nil {
			return nil, err
		}
	}
	if opts.Max <= 0 {
		opts.Max = 300
	}
	if opts.Top <= 0 {
		opts.Top = 3
	}
	if opts.MinFiles <= 0 {
		opts.MinFiles = opts.Top + 1
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	switch opts.Label {
	case "":
		opts.Label = LabelInduced
	case LabelInduced, LabelFixed:
	default:
		return nil, fmt.Errorf("unknown label %q: %s or %s", opts.Label, LabelInduced, LabelFixed)
	}

	landings, err := repo.Landings(opts.Ref, opts.Max, opts.Now.Unix()-Window)
	if err != nil {
		return nil, err
	}
	var since int64
	if len(landings) > 0 {
		since = landings[0].Time - Window
	}
	history, err := repo.HistorySince(opts.Ref, since)
	if err != nil {
		return nil, err
	}
	// Today's graph stands in for the graph at every landing.
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
	today, err := parseFiles(repo.Root, kept)
	if err != nil {
		return nil, err
	}
	afferent := afferentCounts(today)

	result := &Backtest{Methodology: MethodologyVersion, Label: opts.Label, Ref: opts.Ref, Top: opts.Top, Landings: len(landings), LeftOut: map[string]int{}}

	// Landings are independent: replay them on a few workers, then gather
	// the outcomes in their original order so the result does not depend on
	// scheduling.
	type outcome struct {
		eval   *ChangeEval
		reason string
		err    error
	}
	outcomes := make([]outcome, len(landings))
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers()
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				landing := landings[i]
				if landing.Time+Window > opts.Now.Unix() {
					outcomes[i] = outcome{reason: "too recent to know if a fix followed"}
				} else {
					eval, reason, err := replay(repo, landing, config, history, afferent, opts)
					outcomes[i] = outcome{eval, reason, err}
				}
				if opts.Progress != nil {
					mu.Lock()
					done++
					opts.Progress(done, len(landings))
					mu.Unlock()
				}
			}
		}()
	}
	for i := range landings {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, o := range outcomes {
		if o.err != nil {
			return nil, fmt.Errorf("replaying %s: %w", shortSha(landings[i].Sha), o.err)
		}
		if o.reason != "" {
			result.LeftOut[o.reason]++
			continue
		}
		result.Changes = append(result.Changes, *o.eval)
	}
	induced, err := repo.InducedLabels(opts.Ref, result.Changes, workers)
	if err != nil {
		return nil, err
	}
	for i := range result.Changes {
		change := &result.Changes[i]
		for j := range change.Files {
			change.Files[j].Induced = induced[change.Sha][change.Files[j].Path]
		}
	}
	if len(result.Changes) > 0 {
		result.From = result.Changes[0].Time
		result.To = result.Changes[len(result.Changes)-1].Time
	}
	result.Generic = result.Evaluate(DefaultWeights(), 0)
	result.Weights = "generic"
	return result, nil
}

// replay measures one landing. It returns a reason when the landing does
// not qualify.
func replay(repo *Repo, landing Landing, config Config, history *History, afferent map[string]int, opts BacktestOptions) (*ChangeEval, string, error) {
	changes, err := repo.Changes(landing.Parent, landing.Sha)
	if err != nil {
		return nil, "", err
	}
	changes = filterIgnored(changes, config)

	var analyzed, deletedCode []Change
	for _, c := range changes {
		if c.Status != 'D' && !c.Binary && analyzedExtensions[strings.ToLower(path.Ext(c.Path))] {
			analyzed = append(analyzed, c)
		} else if isDeletedCode(c) {
			deletedCode = append(deletedCode, c)
		}
	}
	if len(analyzed) < opts.MinFiles {
		return nil, fmt.Sprintf("fewer than %d code files", opts.MinFiles), nil
	}
	// Tests, generated paths and pure renames are never code, whatever the
	// parser says: when what is left cannot reach MinFiles, the landing is
	// rejected below anyway, so do not pay for reading and parsing it.
	candidates := 0
	for _, c := range analyzed {
		if !IsTestPath(c.Path) && !IsGeneratedPath(c.Path) && !(c.Status == 'R' && c.Added == 0 && c.Deleted == 0) {
			candidates++
		}
	}
	if candidates < opts.MinFiles {
		return nil, fmt.Sprintf("fewer than %d code files", opts.MinFiles), nil
	}

	headPaths := make([]string, 0, len(analyzed))
	basePaths := make([]string, 0, len(analyzed)+len(deletedCode))
	for _, c := range analyzed {
		headPaths = append(headPaths, c.Path)
		if c.Status != 'A' {
			old := c.OldPath
			if old == "" {
				old = c.Path
			}
			basePaths = append(basePaths, old)
		}
	}
	for _, c := range deletedCode {
		basePaths = append(basePaths, c.Path)
	}
	headContents, err := repo.ShowMany(landing.Sha, headPaths)
	if err != nil {
		return nil, "", err
	}
	baseContents, err := repo.ShowMany(landing.Parent, basePaths)
	if err != nil {
		return nil, "", err
	}
	// Base files are stored under their head path so that base and head match.
	baseByHead := map[string][]byte{}
	for _, c := range append(append([]Change{}, analyzed...), deletedCode...) {
		old := c.OldPath
		if old == "" {
			old = c.Path
		}
		if data, ok := baseContents[old]; ok {
			baseByHead[c.Path] = data
		}
	}

	dir, err := os.MkdirTemp("", "cribelo-replay-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	headDir, baseDir := dir+"/head", dir+"/base"
	head, err := parseSnapshot(headDir, headContents)
	if err != nil {
		return nil, "", err
	}
	base := &Parsed{Files: map[string]*pb.File{}}
	if len(baseByHead) > 0 {
		if base, err = parseSnapshot(baseDir, baseByHead); err != nil {
			return nil, "", err
		}
	}

	var codeChanges []Change
	var testPaths []string
	for _, c := range analyzed {
		var isTest bool
		if f := head.Files[c.Path]; f != nil {
			isTest = f.GetIsTest()
		}
		switch Classify(c, headContents[c.Path], isTest) {
		case KindCode:
			codeChanges = append(codeChanges, c)
		case KindTest:
			testPaths = append(testPaths, c.Path)
		}
	}
	if len(codeChanges) < opts.MinFiles {
		return nil, fmt.Sprintf("fewer than %d code files", opts.MinFiles), nil
	}

	measure := &Measure{Head: head, Base: base, Afferent: afferent, History: history, At: landing.Time, TestPaths: testPaths}
	measure.Moves = buildMoveIndex(head, base, codeChanges, deletedCode)
	eval := &ChangeEval{Sha: landing.Sha, Time: landing.Time, Subject: landing.Subject}
	reports := make([]FileReport, 0, len(codeChanges))
	for _, c := range codeChanges {
		reports = append(reports, measure.features(c))
	}
	resolveRenames(reports)
	for i, c := range codeChanges {
		report := reports[i]
		eval.Files = append(eval.Files, Sample{
			Path: c.Path, Added: c.Added, Deleted: c.Deleted, ToRead: report.ToRead, Natures: report.Natures, Values: report.Values,
			Fixed: history.FixedAfter(c.Path, landing.Time, Window),
		})
	}
	return eval, "", nil
}

// Evaluate measures, for the landings from index from on, the reading list
// of b.Top files per landing and the rule the plan applies (every high file,
// at least b.Top): how many of the files later fixed each holds, and which
// share of the changed lines it costs. The baseline reads the largest files
// instead.
func (b *Backtest) Evaluate(weights Weights, from int) Metrics {
	var m Metrics
	baselineCovered, baselineLines, ruleLines := 0, 0, 0
	pathCovered, pathLines := 0, 0
	for _, change := range b.Changes[from:] {
		m.Changes++
		toRead := map[string]int{}
		scored := make([]FileReport, 0, len(change.Files))
		for _, s := range change.Files {
			r := FileReport{Path: s.Path, Added: s.Added, Deleted: s.Deleted, Values: s.Values}
			r.Score, _ = weights.Score(s.Values)
			scored = append(scored, r)
			m.LinesTotal += s.Added + s.Deleted
			m.ToRead += s.ToRead
			toRead[s.Path] = s.ToRead
			m.Files++
			if b.positive(s) {
				m.FixedFiles++
			}
		}
		fixed := map[string]bool{}
		for _, s := range change.Files {
			fixed[s.Path] = b.positive(s)
		}
		rank(scored)
		for i, r := range scored {
			if i < b.Top {
				m.LinesRead += r.Added + r.Deleted
				m.ToReadTop += toRead[r.Path]
				if fixed[r.Path] {
					m.Covered++
				}
			}
			if i < b.Top || weights.Level(r.Score) == LevelHigh {
				m.RuleRead++
				ruleLines += r.Added + r.Deleted
				if fixed[r.Path] {
					m.RuleCovered++
				}
			}
		}
		sort.SliceStable(scored, func(i, j int) bool {
			si, sj := scored[i].Added+scored[i].Deleted, scored[j].Added+scored[j].Deleted
			if si != sj {
				return si > sj
			}
			return scored[i].Path < scored[j].Path
		})
		for _, r := range scored[:minInt(b.Top, len(scored))] {
			baselineLines += r.Added + r.Deleted
			if fixed[r.Path] {
				baselineCovered++
			}
		}
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].Path < scored[j].Path })
		for _, r := range scored[:minInt(b.Top, len(scored))] {
			pathLines += r.Added + r.Deleted
			if fixed[r.Path] {
				pathCovered++
			}
		}
	}
	if m.FixedFiles > 0 {
		m.Coverage = float64(m.Covered) / float64(m.FixedFiles)
		m.BaselineCover = float64(baselineCovered) / float64(m.FixedFiles)
		m.PathOrderCover = float64(pathCovered) / float64(m.FixedFiles)
		m.RuleCoverage = float64(m.RuleCovered) / float64(m.FixedFiles)
	}
	if m.LinesTotal > 0 {
		m.ToReadShare = float64(m.ToRead) / float64(m.LinesTotal)
		m.ToReadTopShare = float64(m.ToReadTop) / float64(m.LinesTotal)
		m.LinesShare = float64(m.LinesRead) / float64(m.LinesTotal)
		m.BaselineShare = float64(baselineLines) / float64(m.LinesTotal)
		m.PathOrderShare = float64(pathLines) / float64(m.LinesTotal)
		m.RuleShare = float64(ruleLines) / float64(m.LinesTotal)
	}
	return m
}

func (b *Backtest) positiveWording() string {
	if b.Label == LabelFixed {
		return "later touched by a fix"
	}
	return "wrote lines a later fix repaired"
}

// Text renders the backtest for a terminal.
func (b *Backtest) Text() string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s %s\n\n", styleTitle.Render("cribelo backtest"), styleDim.Render(fmt.Sprintf("· %d landings on %s replayed", b.Landings, b.Ref)))
	if len(b.Changes) == 0 {
		s.WriteString("No landing qualified.\n")
	} else {
		share := float64(b.Generic.FixedFiles) / float64(maxInt(b.Generic.Files, 1))
		fmt.Fprintf(&s, "%d changes qualified, %s → %s: %d code files, %d of them (%s) %s.\n\n", len(b.Changes), day(b.From), day(b.To), b.Generic.Files, b.Generic.FixedFiles, pct(share), b.positiveWording())
		s.WriteString(metricsLines(b.Generic, b.Top, "generic weights, all qualified changes"))
		if b.Calibrated != nil && b.GenericHeldOut != nil {
			fmt.Fprintf(&s, "\n%s\n", styleTitle.Render(fmt.Sprintf("held-out check, the %d most recent changes", b.Calibrated.Changes)))
			fmt.Fprintf(&s, "  generic weights     top-%d covers %s of those files for %s of the lines; every high file: %s for %s\n", b.Top, pct(b.GenericHeldOut.Coverage), pct(b.GenericHeldOut.LinesShare), pct(b.GenericHeldOut.RuleCoverage), pct(b.GenericHeldOut.RuleShare))
			fmt.Fprintf(&s, "  calibrated weights  top-%d covers %s of those files for %s of the lines; every high file: %s for %s\n", b.Top, pct(b.Calibrated.Coverage), pct(b.Calibrated.LinesShare), pct(b.Calibrated.RuleCoverage), pct(b.Calibrated.RuleShare))
			fmt.Fprintf(&s, "  %d largest files     cover %s of those files for %s of the lines\n", b.Top, pct(b.Calibrated.BaselineCover), pct(b.Calibrated.BaselineShare))
		}
	}
	if len(b.LeftOut) > 0 {
		s.WriteString("\n" + styleDim.Render("Left out: "+countsLine(b.LeftOut)) + "\n")
	}
	fixWords := "a commit whose message says fix, bug, revert, regression, correction, corrigé... (English, French, Spanish, Portuguese, German; add yours with fix_pattern in .cribelo.yaml)"
	if b.Label == LabelFixed {
		s.WriteString(styleDim.Render("A file counts when "+fixWords+" touches it within 90 days. Hub signal from today's graph; clones not replayed.") + "\n")
	} else {
		s.WriteString(styleDim.Render("A file counts when "+fixWords+", within 90 days, changes or deletes lines that git blame attributes to this change in that file (SZZ). Hub signal from today's graph; clones not replayed.") + "\n")
	}
	return s.String()
}

func metricsLines(m Metrics, top int, label string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s\n", styleTitle.Render(label))
	fmt.Fprintf(&s, "  Setting aside moves, extractions, renames, formatting and imports leaves %s of the changed lines to read.\n", pct(m.ToReadShare))
	fmt.Fprintf(&s, "  Reading the top-%d files covered %s of those files, for %s of the changed lines (%s once classified).\n", top, pct(m.Coverage), pct(m.LinesShare), pct(m.ToReadTopShare))
	fmt.Fprintf(&s, "  Reading the first %d in path order, as a pull request lists them: %s, for %s of the lines.\n", top, pct(m.PathOrderCover), pct(m.PathOrderShare))
	fmt.Fprintf(&s, "  Reading the %d largest files instead: %s, for %s of the lines.\n", top, pct(m.BaselineCover), pct(m.BaselineShare))
	perChange := float64(m.RuleRead) / float64(maxInt(m.Changes, 1))
	fmt.Fprintf(&s, "  Reading every high file, at least %d (%.1f files a change): %s, for %s of the lines.\n", top, perChange, pct(m.RuleCoverage), pct(m.RuleShare))
	return s.String()
}

// JSON renders the backtest.
func (b *Backtest) JSON() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func pct(x float64) string { return fmt.Sprintf("%d%%", int(x*100+0.5)) }

func day(t int64) string { return time.Unix(t, 0).UTC().Format("2006-01-02") }

func countsLine(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return strings.Join(parts, " · ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ShowMany reads several files at one commit through a single git process.
// Missing paths are absent from the result.
func (r *Repo) ShowMany(sha string, paths []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(paths) == 0 {
		return out, nil
	}
	cmd := exec.Command("git", "-c", "core.quotepath=false", "cat-file", "--batch")
	cmd.Dir = r.Root
	var requests bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&requests, "%s:%s\n", sha, p)
	}
	cmd.Stdin = &requests
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(stdout, 1<<20)
	for _, p := range paths {
		header, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		fields := strings.Fields(header)
		if len(fields) < 3 || fields[1] != "blob" {
			continue // "missing": nothing follows
		}
		size, _ := strconv.Atoi(fields[2])
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			break
		}
		reader.ReadByte() // trailing newline
		out[p] = data
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	return out, nil
}
