package analyzer

import (
	"maps"
	"math"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/classifier"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	engine "github.com/ast-metrics/ast-metrics/internal/engine"
	csharpdeps "github.com/ast-metrics/ast-metrics/internal/engine/csharp/deps"
	golangdeps "github.com/ast-metrics/ast-metrics/internal/engine/golang/deps"
	javadeps "github.com/ast-metrics/ast-metrics/internal/engine/java/deps"
	pythondeps "github.com/ast-metrics/ast-metrics/internal/engine/python/deps"
	rustdeps "github.com/ast-metrics/ast-metrics/internal/engine/rust/deps"
	typescriptdeps "github.com/ast-metrics/ast-metrics/internal/engine/typescript/deps"
	Scm "github.com/ast-metrics/ast-metrics/internal/scm"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

type ProjectAggregated struct {
	ByFile                Aggregated
	ByClass               Aggregated
	Combined              Aggregated
	ByProgrammingLanguage map[string]Aggregated
	// ByDirectory holds one aggregate per analyzed path (the arguments given to
	// the CLI, e.g. `ast-metrics analyze ./src ./lib`). It stays empty when a
	// single path is analyzed, since that would duplicate the global view.
	ByDirectory  map[string]Aggregated
	ErroredFiles []*pb.File
	Evaluation   *requirement.EvaluationResult
	Comparaison  *ProjectComparaison
	Predictions  []classifier.ClassPrediction
}

type AggregateResult struct {
	Sum     float64
	Min     float64
	Max     float64
	Avg     float64
	Counter int
}

func NewAggregateResult() AggregateResult {
	return AggregateResult{
		Sum:     0,
		Min:     0,
		Max:     0,
		Avg:     0,
		Counter: 0,
	}
}

type Aggregated struct {
	ProgrammingLanguages map[string]int
	ConcernedFiles       []*pb.File
	ErroredFiles         []*pb.File
	Comparaison          *Comparaison
	// hashmap of classes, just with the qualified name, used for afferent coupling calculation
	ClassesAfferentCoupling map[string]int
	NbFiles                 int
	// Number of test files. Test files are counted in NbFiles, but their metrics
	// are excluded from all the aggregates below (they are not production code).
	NbTestFiles int
	// TestLoc holds the physical lines of the test files, the one measure kept
	// about them. It exists so a report can account for the whole tree: without
	// it, Loc covers a part of NbFiles and nothing on screen says which part.
	TestLoc                                 AggregateResult
	NbFunctions                             int
	NbClasses                               int
	NbClassesWithCode                       int
	NbMethods                               int
	Loc                                     AggregateResult
	Cloc                                    AggregateResult
	Lloc                                    AggregateResult
	MethodsPerClass                         AggregateResult
	LocPerClass                             AggregateResult
	LocPerMethod                            AggregateResult
	LlocPerMethod                           AggregateResult
	ClocPerMethod                           AggregateResult
	CyclomaticComplexity                    AggregateResult
	CyclomaticComplexityPerMethod           AggregateResult
	CyclomaticComplexityPerClass            AggregateResult
	HalsteadDifficulty                      AggregateResult
	HalsteadEffort                          AggregateResult
	HalsteadVolume                          AggregateResult
	HalsteadTime                            AggregateResult
	HalsteadBugs                            AggregateResult
	Lcom4PerClass                           AggregateResult
	MaintainabilityIndex                    AggregateResult
	MaintainabilityIndexWithoutComments     AggregateResult
	MaintainabilityCommentWeight            AggregateResult
	Instability                             AggregateResult
	EfferentCoupling                        AggregateResult
	AfferentCoupling                        AggregateResult
	MaintainabilityPerMethod                AggregateResult
	MaintainabilityPerMethodWithoutComments AggregateResult
	MaintainabilityCommentWeightPerMethod   AggregateResult
	CommitCountForPeriod                    int
	CommittedFilesCountForPeriod            int
	BusFactor                               int
	TopCommitters                           []TopCommitter
	ResultOfGitAnalysis                     []ResultOfGitAnalysis
	PackageRelations                        map[string]map[string]int // counter of dependencies. Ex: A -> B -> 2
	FileDependencies                        FileDependencyGraph
	Graph                                   *pb.Graph
	// NamespaceReducers map a namespace to the node of Graph it belongs to. They
	// are built along with the graph, and are what anything looking a node up
	// has to go through: the depth a namespace is cut at depends on the project,
	// so reducing it by hand somewhere else gives a node that does not exist.
	NamespaceReducers engine.NamespaceReducers
	ExternalNodes     map[string]bool // node IDs that are external (not from project source)
	Community         *CommunityMetrics
	TestQuality       *TestQualityMetrics
	Suggestions       []Suggestion
	Architecture      *ArchitectureMetrics
}

type ProjectComparaison struct {
	ByFile                Comparaison
	ByClass               Comparaison
	Combined              Comparaison
	ByProgrammingLanguage map[string]Comparaison
}

type Aggregator struct {
	files             []*pb.File
	projectAggregated ProjectAggregated
	analyzers         []AggregateAnalyzer
	gitSummaries      []ResultOfGitAnalysis
	ComparedFiles     []*pb.File
	ComparedBranch    string
	// AnalyzedPaths are the paths given by the user on the command line. They
	// drive the per-directory aggregation (ProjectAggregated.ByDirectory).
	AnalyzedPaths []string
}

type TopCommitter struct {
	Name string
	// Email is only used to resolve an avatar. It stays empty when the git log
	// does not expose it.
	Email      string
	Count      int
	Percentage float64
}

type ResultOfGitAnalysis struct {
	ProgrammingLanguage     string
	ReportRootDir           string
	CountCommits            int
	CountCommiters          int
	CountCommitsForLanguage int
	CountCommitsIgnored     int
	// AuthorEmails maps an author display name to one of its commit addresses,
	// so the report can resolve an avatar for the top contributors.
	AuthorEmails  map[string]string
	GitRepository Scm.GitRepository
}

func NewAggregator(files []*pb.File, gitSummaries []ResultOfGitAnalysis) *Aggregator {
	a := &Aggregator{
		files:        files,
		gitSummaries: gitSummaries,
	}
	// Register default analyzers
	// One resolver per language: an import means something different in each,
	// and each is consulted only for the files it owns. They live in leaf
	// packages beside their engine rather than inside it, so that an engine
	// test may keep importing this package.
	a.WithAggregateAnalyzer(NewFileDependencyAnalyzer(
		typescriptdeps.NewFileDependencyResolver(),
		golangdeps.NewFileDependencyResolver(),
		javadeps.NewFileDependencyResolver(),
		csharpdeps.NewFileDependencyResolver(),
		rustdeps.NewFileDependencyResolver(),
		pythondeps.NewFileDependencyResolver(),
	))
	a.WithAggregateAnalyzer(NewGraphAggregator())
	// Run community detection after graph is built
	a.WithAggregateAnalyzer(NewCommunityAggregator())
	// Run test quality analysis
	a.WithAggregateAnalyzer(NewTestQualityAggregator())
	return a
}

type AggregateAnalyzer interface {
	Calculate(aggregate *Aggregated)
}

func newAggregated() Aggregated {
	return Aggregated{
		ProgrammingLanguages:                    make(map[string]int),
		ConcernedFiles:                          make([]*pb.File, 0),
		ClassesAfferentCoupling:                 make(map[string]int),
		ErroredFiles:                            make([]*pb.File, 0),
		NbTestFiles:                             0,
		NbClasses:                               0,
		NbClassesWithCode:                       0,
		NbMethods:                               0,
		NbFunctions:                             0,
		Loc:                                     NewAggregateResult(),
		TestLoc:                                 NewAggregateResult(),
		MethodsPerClass:                         NewAggregateResult(),
		LocPerClass:                             NewAggregateResult(),
		LocPerMethod:                            NewAggregateResult(),
		ClocPerMethod:                           NewAggregateResult(),
		CyclomaticComplexity:                    NewAggregateResult(),
		CyclomaticComplexityPerMethod:           NewAggregateResult(),
		CyclomaticComplexityPerClass:            NewAggregateResult(),
		HalsteadEffort:                          NewAggregateResult(),
		HalsteadVolume:                          NewAggregateResult(),
		HalsteadTime:                            NewAggregateResult(),
		HalsteadBugs:                            NewAggregateResult(),
		Lcom4PerClass:                           NewAggregateResult(),
		MaintainabilityIndex:                    NewAggregateResult(),
		MaintainabilityIndexWithoutComments:     NewAggregateResult(),
		MaintainabilityCommentWeight:            NewAggregateResult(),
		Instability:                             NewAggregateResult(),
		EfferentCoupling:                        NewAggregateResult(),
		AfferentCoupling:                        NewAggregateResult(),
		MaintainabilityPerMethod:                NewAggregateResult(),
		MaintainabilityPerMethodWithoutComments: NewAggregateResult(),
		MaintainabilityCommentWeightPerMethod:   NewAggregateResult(),
		CommitCountForPeriod:                    0,
		CommittedFilesCountForPeriod:            0,
		BusFactor:                               0,
		TopCommitters:                           make([]TopCommitter, 0),
		ResultOfGitAnalysis:                     nil,
		PackageRelations:                        make(map[string]map[string]int),
		Graph:                                   &pb.Graph{Nodes: make(map[string]*pb.Node)},
		ExternalNodes:                           make(map[string]bool),
		Community:                               nil,
		TestQuality:                             nil,
		Suggestions:                             make([]Suggestion, 0),
	}
}

// This method is the main entry point to get the aggregated data
// It will:
// - chunk the files by number of processors, to speed up the process
// - map the files to the aggregated object with sums
// - reduce the sums to get the averages
// - map the coupling
// - run the risk analysis
//
// it also computes the comparaison if the compared files are set
func (r *Aggregator) Aggregates() ProjectAggregated {

	// We create a new aggregated object for each type of aggregation
	r.projectAggregated = r.executeAggregationOnFiles(r.files)

	// Do the same for the comparaison files (if needed)
	if r.ComparedFiles != nil {
		comparaidAggregated := r.executeAggregationOnFiles(r.ComparedFiles)

		// Compare
		comparaison := ProjectComparaison{}
		comparator := NewComparator(r.ComparedBranch)
		comparaison.Combined = comparator.Compare(r.projectAggregated.Combined, comparaidAggregated.Combined)
		r.projectAggregated.Combined.Comparaison = &comparaison.Combined

		comparaison.ByClass = comparator.Compare(r.projectAggregated.ByClass, comparaidAggregated.ByClass)
		r.projectAggregated.ByClass.Comparaison = &comparaison.ByClass

		comparaison.ByFile = comparator.Compare(r.projectAggregated.ByFile, comparaidAggregated.ByFile)
		r.projectAggregated.ByFile.Comparaison = &comparaison.ByFile

		// By language
		comparaison.ByProgrammingLanguage = make(map[string]Comparaison)
		for lng, byLanguage := range r.projectAggregated.ByProgrammingLanguage {
			if _, ok := comparaidAggregated.ByProgrammingLanguage[lng]; !ok {
				continue
			}
			c := comparator.Compare(byLanguage, comparaidAggregated.ByProgrammingLanguage[lng])
			comparaison.ByProgrammingLanguage[lng] = c

			// assign to the original object (slow, but otherwise we need to change the whole structure ByProgrammingLanguage map)
			// @see https://stackoverflow.com/questions/42605337/cannot-assign-to-struct-field-in-a-map
			// Feel free to change this
			entry := r.projectAggregated.ByProgrammingLanguage[lng]
			entry.Comparaison = &c
			r.projectAggregated.ByProgrammingLanguage[lng] = entry
		}

		// By analyzed directory, so that the per-folder pages of the report also
		// show the comparaison instead of an empty one
		for dir, byDirectory := range r.projectAggregated.ByDirectory {
			if _, ok := comparaidAggregated.ByDirectory[dir]; !ok {
				continue
			}
			c := comparator.Compare(byDirectory, comparaidAggregated.ByDirectory[dir])
			entry := r.projectAggregated.ByDirectory[dir]
			entry.Comparaison = &c
			r.projectAggregated.ByDirectory[dir] = entry
		}

		r.projectAggregated.Comparaison = &comparaison
	}

	return r.projectAggregated
}

func (r *Aggregator) executeAggregationOnFiles(files []*pb.File) ProjectAggregated {

	projectAggregated := ProjectAggregated{
		ByFile:                newAggregated(),
		ByClass:               newAggregated(),
		Combined:              newAggregated(),
		ByProgrammingLanguage: make(map[string]Aggregated),
		ByDirectory:           make(map[string]Aggregated),
		ErroredFiles:          make([]*pb.File, 0),
		Evaluation:            nil,
		Comparaison:           nil,
	}

	// do the sums. Group files by number of processors
	var wg sync.WaitGroup
	numberOfProcessors := runtime.NumCPU()

	// Split the files into chunks
	chunkSize := len(files) / numberOfProcessors
	chunks := make([][]*pb.File, numberOfProcessors)
	for i := 0; i < numberOfProcessors; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if i == numberOfProcessors-1 {
			end = len(files)
		}
		chunks[i] = files[start:end]
	}

	// for each analyzed path (CLI argument), we create a separated result. Only
	// when several paths are analyzed: with a single one, the directory view
	// would be a duplicate of the global view.
	directoryOfFile := make(map[*pb.File]string)
	if len(r.AnalyzedPaths) > 1 {
		scopes := buildAnalyzedPathScopes(r.AnalyzedPaths)
		if len(scopes) > 1 {
			for _, file := range files {
				key, ok := scopeOfFile(scopes, file.Path)
				if !ok {
					continue
				}
				directoryOfFile[file] = key
			}
		}
	}

	// Each worker owns its aggregates and writes them into its own slot: nothing
	// is shared, so we get to choose the order in which they are merged back.
	// That choice is what makes the analysis reproducible, since summing floats
	// in the order the workers happen to finish would make the project totals
	// wobble in their last bits from one run to the next.
	partials := make([]chunkAggregates, numberOfProcessors)

	for i := 0; i < numberOfProcessors; i++ {

		wg.Add(1)

		// Reduce results : we want to get sums, and to count calculated values into a AggregateResult
		go func(slot int, files []*pb.File) {
			defer wg.Done()

			partial := newChunkAggregates()

			// the process deal with its own chunk
			for _, file := range files {
				// by file
				byFile := r.mapSums(file, partial.byFile)
				byFile.ConcernedFiles = append(byFile.ConcernedFiles, file)
				partial.byFile = byFile

				// by class
				byClass := r.mapSums(file, partial.byClass)
				byClass.ConcernedFiles = append(byClass.ConcernedFiles, file)
				partial.byClass = byClass

				// by language, and by analyzed directory
				r.addFileToBucket(partial.byLanguage, file.ProgrammingLanguage, file)
				if directory, ok := directoryOfFile[file]; ok {
					r.addFileToBucket(partial.byDirectory, directory, file)
				}
			}

			partials[slot] = partial

		}(i, chunks[i])
	}

	wg.Wait()

	// Now we have chunk of sums. We want to reduce its into a single object,
	// walking the chunks in index order rather than in completion order.
	for i := range partials {
		partial := partials[i]
		projectAggregated.ByFile = r.mergeChunks(projectAggregated.ByFile, &partial.byFile)
		projectAggregated.ByClass = r.mergeChunks(projectAggregated.ByClass, &partial.byClass)
		r.mergeBuckets(projectAggregated.ByProgrammingLanguage, partial.byLanguage)
		r.mergeBuckets(projectAggregated.ByDirectory, partial.byDirectory)
	}

	// Files were appended chunk by chunk. Canonicalize every aggregate before
	// the coupling and test-quality analyzers consume these lists, since both
	// index by a key several files can share and keep the last writer.
	sortFilesByPath(projectAggregated.ByClass.ConcernedFiles)
	sortFilesByPath(projectAggregated.ByFile.ConcernedFiles)

	// Now  we have sums. We want to reduce metrics and get the averages.
	// mapCoupling writes back into the *pb.File it visits, so the buckets are
	// walked by sorted key: with Go's map order, the values one bucket reads
	// would depend on which buckets ran before it.
	projectAggregated.ByClass = r.reduceMetrics(projectAggregated.ByClass)
	projectAggregated.ByFile = r.reduceMetrics(projectAggregated.ByFile)
	for _, k := range slices.Sorted(maps.Keys(projectAggregated.ByProgrammingLanguage)) {
		v := projectAggregated.ByProgrammingLanguage[k]
		sortFilesByPath(v.ConcernedFiles)
		v = r.reduceMetrics(v)
		f := r.mapCoupling(&v)
		projectAggregated.ByProgrammingLanguage[k] = f
	}
	for _, k := range slices.Sorted(maps.Keys(projectAggregated.ByDirectory)) {
		v := projectAggregated.ByDirectory[k]
		sortFilesByPath(v.ConcernedFiles)
		v = r.reduceMetrics(v)
		f := r.mapCoupling(&v)
		projectAggregated.ByDirectory[k] = f
	}

	// Coupling (should be done separately, to avoid race condition)
	projectAggregated.ByClass = r.mapCoupling(&projectAggregated.ByClass)
	projectAggregated.ByFile = r.mapCoupling(&projectAggregated.ByFile)

	// For all languages (set Combined before running analyzers that rely on it)
	projectAggregated.Combined = projectAggregated.ByFile
	projectAggregated.ErroredFiles = projectAggregated.ByFile.ErroredFiles

	// Risks
	riskAnalyzer := NewRiskAnalyzer()
	riskAnalyzer.Analyze(projectAggregated)

	return projectAggregated
}

// chunkAggregates holds the sums computed by a single worker over its own chunk
// of files. Workers never share these, which is what lets the caller merge them
// back in a fixed order.
type chunkAggregates struct {
	byFile      Aggregated
	byClass     Aggregated
	byLanguage  map[string]Aggregated
	byDirectory map[string]Aggregated
}

func newChunkAggregates() chunkAggregates {
	return chunkAggregates{
		byFile:      newAggregated(),
		byClass:     newAggregated(),
		byLanguage:  make(map[string]Aggregated),
		byDirectory: make(map[string]Aggregated),
	}
}

// addFileToBucket sums a file into the named bucket, creating it on first use.
func (r *Aggregator) addFileToBucket(buckets map[string]Aggregated, key string, file *pb.File) {
	bucket, ok := buckets[key]
	if !ok {
		bucket = newAggregated()
	}
	bucket = r.mapSums(file, bucket)
	bucket.ConcernedFiles = append(bucket.ConcernedFiles, file)
	buckets[key] = bucket
}

// mergeBuckets folds one worker's named aggregates into the project ones.
func (r *Aggregator) mergeBuckets(into map[string]Aggregated, from map[string]Aggregated) {
	for key, bucket := range from {
		base, ok := into[key]
		if !ok {
			base = newAggregated()
		}
		into[key] = r.mergeChunks(base, &bucket)
	}
}

func sortFilesByPath(files []*pb.File) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i] == nil {
			return false
		}
		if files[j] == nil {
			return true
		}
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].ProgrammingLanguage < files[j].ProgrammingLanguage
	})
}

// Add an analyzer to the aggregator
// You can add multiple analyzers. See the example of RiskAnalyzer
func (r *Aggregator) WithAggregateAnalyzer(analyzer AggregateAnalyzer) {
	r.analyzers = append(r.analyzers, analyzer)
}

// WithAnalyzedPaths declares the paths given by the user on the command line.
// They are used to build ProjectAggregated.ByDirectory: one aggregate per
// analyzed path. When fewer than two paths are given, no per-directory
// aggregate is produced (it would be a copy of the global view).
func (r *Aggregator) WithAnalyzedPaths(paths []string) {
	r.AnalyzedPaths = paths
}

// analyzedPathScope links an analyzed path, as typed by the user, to the
// absolute prefix used to decide whether a file belongs to it.
type analyzedPathScope struct {
	// Key is the path as given by the user: it is the ByDirectory map key.
	Key string
	// Abs is the cleaned, absolute form of Key.
	Abs string
}

// buildAnalyzedPathScopes resolves the analyzed paths to absolute prefixes,
// sorted from the longest to the shortest one so that nested paths are matched
// by their most specific scope. Duplicates are dropped.
func buildAnalyzedPathScopes(paths []string) []analyzedPathScope {
	scopes := make([]analyzedPathScope, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs := p
		if !filepath.IsAbs(abs) {
			resolved, err := filepath.Abs(abs)
			if err != nil {
				continue
			}
			abs = resolved
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		scopes = append(scopes, analyzedPathScope{Key: p, Abs: abs})
	}

	// Longest prefix first: a file under ./src/vendor belongs to ./src/vendor,
	// not to ./src, when both are analyzed.
	sort.SliceStable(scopes, func(i, j int) bool {
		return len(scopes[i].Abs) > len(scopes[j].Abs)
	})

	return scopes
}

// scopeOfFile returns the key of the analyzed path a file belongs to. A file
// belongs to exactly one scope (the most specific one).
func scopeOfFile(scopes []analyzedPathScope, filePath string) (string, bool) {
	if filePath == "" {
		return "", false
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		resolved, err := filepath.Abs(abs)
		if err != nil {
			return "", false
		}
		abs = resolved
	}
	abs = filepath.Clean(abs)

	for _, scope := range scopes {
		if abs == scope.Abs {
			return scope.Key, true
		}
		if strings.HasPrefix(abs, scope.Abs+string(filepath.Separator)) {
			return scope.Key, true
		}
	}

	return "", false
}

// Set the files and branch to compare with
func (r *Aggregator) WithComparaison(allResultsCloned []*pb.File, comparedBranch string) {
	r.ComparedFiles = allResultsCloned
	r.ComparedBranch = comparedBranch
}

// Map the sums of a file to the aggregated object
func (r *Aggregator) mapSums(file *pb.File, specificAggregation Aggregated) Aggregated {
	// copy the specific aggregation to new object to avoid side effects
	result := specificAggregation
	result.NbFiles++
	if file.GetIsTest() {
		result.NbTestFiles++
	}

	// deal with errors
	if len(file.Errors) > 0 {
		result.ErroredFiles = append(result.ErroredFiles, file)
		return result
	}

	// Test files are not production code: they are kept in ConcernedFiles (the
	// TestQuality and Graph analyzers need them), but their metrics must not
	// pollute the averages of the project. Their size is kept aside, so a report
	// can say how much of the tree it left out.
	if file.GetIsTest() {
		if file.LinesOfCode != nil {
			result.TestLoc.Sum += float64(file.LinesOfCode.LinesOfCode)
			result.TestLoc.Counter++
		}
		return result
	}

	if file.Stmts == nil {
		return result
	}

	classes := engine.GetClassesInFile(file)
	functions := engine.GetFunctionsInFile(file)

	// Number of classes
	result.NbClasses += len(classes)

	// Number of standalone functions (declared outside of any class);
	// class methods are counted in NbMethods
	if file.Stmts != nil {
		result.NbFunctions += len(file.Stmts.StmtFunction)
	}

	// Ensure LOC is set
	if file.LinesOfCode == nil {
		if file.Stmts != nil && file.Stmts.Analyze != nil && file.Stmts.Analyze.Volume != nil {
			file.LinesOfCode = &pb.LinesOfCode{
				LinesOfCode:        *file.Stmts.Analyze.Volume.Loc,
				CommentLinesOfCode: *file.Stmts.Analyze.Volume.Cloc,
				LogicalLinesOfCode: *file.Stmts.Analyze.Volume.Lloc,
			}
		} else {
			file.LinesOfCode = &pb.LinesOfCode{
				LinesOfCode:        0,
				CommentLinesOfCode: 0,
				LogicalLinesOfCode: 0,
			}
		}
	}

	result.Loc.Sum += float64(file.LinesOfCode.LinesOfCode)
	result.Loc.Counter++
	result.Cloc.Sum += float64(file.LinesOfCode.CommentLinesOfCode)
	result.Cloc.Counter++
	result.Lloc.Sum += float64(file.LinesOfCode.LogicalLinesOfCode)
	result.Lloc.Counter++

	// Functions
	for _, function := range functions {

		if function == nil || function.Stmts == nil {
			continue
		}

		result.NbMethods++

		// Average cyclomatic complexity per method
		if function.Stmts.Analyze != nil && function.Stmts.Analyze.Complexity != nil {
			if function.Stmts.Analyze.Complexity.Cyclomatic != nil {

				// @todo: only for functions and methods of classes (not interfaces)
				// otherwise, average may be lower than 1
				ccn := float64(*function.Stmts.Analyze.Complexity.Cyclomatic)
				result.CyclomaticComplexityPerMethod.Sum += ccn
				result.CyclomaticComplexityPerMethod.Counter++
				if specificAggregation.CyclomaticComplexityPerMethod.Min == 0 || ccn < specificAggregation.CyclomaticComplexityPerMethod.Min {
					result.CyclomaticComplexityPerMethod.Min = ccn
				}
				if specificAggregation.CyclomaticComplexityPerMethod.Max == 0 || ccn > specificAggregation.CyclomaticComplexityPerMethod.Max {
					result.CyclomaticComplexityPerMethod.Max = ccn
				}

				result.CyclomaticComplexity.Sum += ccn
				result.CyclomaticComplexity.Counter++
				if specificAggregation.CyclomaticComplexity.Min == 0 || ccn < specificAggregation.CyclomaticComplexity.Min {
					result.CyclomaticComplexity.Min = ccn
				}
				if specificAggregation.CyclomaticComplexity.Max == 0 || ccn > specificAggregation.CyclomaticComplexity.Max {
					result.CyclomaticComplexity.Max = ccn
				}
			}
		}

		// Average maintainability index per method
		if function.Stmts.Analyze != nil && function.Stmts.Analyze.Maintainability != nil {
			if function.Stmts.Analyze.Maintainability.MaintainabilityIndex != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.MaintainabilityIndex)) {
				result.MaintainabilityIndex.Sum += *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				result.MaintainabilityIndex.Counter++
				if specificAggregation.MaintainabilityIndex.Min == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndex < specificAggregation.MaintainabilityIndex.Min {
					result.MaintainabilityIndex.Min = *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
				if specificAggregation.MaintainabilityIndex.Max == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndex > specificAggregation.MaintainabilityIndex.Max {
					result.MaintainabilityIndex.Max = *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
			}

			// Maintainability index without comments
			if function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments)) {
				result.MaintainabilityIndexWithoutComments.Sum += *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				result.MaintainabilityIndexWithoutComments.Counter++
				if specificAggregation.MaintainabilityIndexWithoutComments.Min == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments < specificAggregation.MaintainabilityIndexWithoutComments.Min {
					result.MaintainabilityIndexWithoutComments.Min = *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
				if specificAggregation.MaintainabilityIndexWithoutComments.Max == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments > specificAggregation.MaintainabilityIndexWithoutComments.Max {
					result.MaintainabilityIndexWithoutComments.Max = *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
			}

			// Comment weight
			if function.Stmts.Analyze.Maintainability.CommentWeight != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.CommentWeight)) {
				result.MaintainabilityCommentWeight.Sum += *function.Stmts.Analyze.Maintainability.CommentWeight
				result.MaintainabilityCommentWeight.Counter++
				if specificAggregation.MaintainabilityCommentWeight.Min == 0 || *function.Stmts.Analyze.Maintainability.CommentWeight < specificAggregation.MaintainabilityCommentWeight.Min {
					result.MaintainabilityCommentWeight.Min = *function.Stmts.Analyze.Maintainability.CommentWeight
				}
				if specificAggregation.MaintainabilityCommentWeight.Max == 0 || *function.Stmts.Analyze.Maintainability.CommentWeight > specificAggregation.MaintainabilityCommentWeight.Max {
					result.MaintainabilityCommentWeight.Max = *function.Stmts.Analyze.Maintainability.CommentWeight
				}
			}

			// Maintainability index per method
			if function.Stmts.Analyze.Maintainability.MaintainabilityIndex != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.MaintainabilityIndex)) {
				result.MaintainabilityPerMethod.Sum += *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				result.MaintainabilityPerMethod.Counter++
				if specificAggregation.MaintainabilityPerMethod.Min == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndex < specificAggregation.MaintainabilityPerMethod.Min {
					result.MaintainabilityPerMethod.Min = *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
				if specificAggregation.MaintainabilityPerMethod.Max == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndex > specificAggregation.MaintainabilityPerMethod.Max {
					result.MaintainabilityPerMethod.Max = *function.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
			}

			// Maintainability index per method without comments
			if function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments)) {
				result.MaintainabilityPerMethodWithoutComments.Sum += *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				result.MaintainabilityPerMethodWithoutComments.Counter++
				if specificAggregation.MaintainabilityPerMethodWithoutComments.Min == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments < specificAggregation.MaintainabilityPerMethodWithoutComments.Min {
					result.MaintainabilityPerMethodWithoutComments.Min = *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
				if specificAggregation.MaintainabilityPerMethodWithoutComments.Max == 0 || *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments > specificAggregation.MaintainabilityPerMethodWithoutComments.Max {
					result.MaintainabilityPerMethodWithoutComments.Max = *function.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
			}

			// Comment weight per method
			if function.Stmts.Analyze.Maintainability.CommentWeight != nil && !math.IsNaN(float64(*function.Stmts.Analyze.Maintainability.CommentWeight)) {
				result.MaintainabilityCommentWeightPerMethod.Sum += *function.Stmts.Analyze.Maintainability.CommentWeight
				result.MaintainabilityCommentWeightPerMethod.Counter++
				if specificAggregation.MaintainabilityCommentWeightPerMethod.Min == 0 || *function.Stmts.Analyze.Maintainability.CommentWeight < specificAggregation.MaintainabilityCommentWeightPerMethod.Min {
					result.MaintainabilityCommentWeightPerMethod.Min = *function.Stmts.Analyze.Maintainability.CommentWeight
				}
				if specificAggregation.MaintainabilityCommentWeightPerMethod.Max == 0 || *function.Stmts.Analyze.Maintainability.CommentWeight > specificAggregation.MaintainabilityCommentWeightPerMethod.Max {
					result.MaintainabilityCommentWeightPerMethod.Max = *function.Stmts.Analyze.Maintainability.CommentWeight
				}
			}
		}
		// average lines of code per method
		if function.Stmts.Analyze != nil && function.Stmts.Analyze.Volume != nil {
			if function.Stmts.Analyze.Volume.Loc != nil {
				result.LocPerMethod.Sum += float64(*function.Stmts.Analyze.Volume.Loc)
				result.LocPerMethod.Counter++
			}
			if function.Stmts.Analyze.Volume.Cloc != nil {
				result.ClocPerMethod.Sum += float64(*function.Stmts.Analyze.Volume.Cloc)
				result.ClocPerMethod.Counter++
			}
			if function.Stmts.Analyze.Volume.Lloc != nil {
				result.LlocPerMethod.Sum += float64(*function.Stmts.Analyze.Volume.Lloc)
				result.LlocPerMethod.Counter++
			}
		}
	}

	for _, class := range classes {

		if class == nil || class.Stmts == nil {
			continue
		}

		// Number of classes with code
		//if class.LinesOfCode != nil && class.LinesOfCode.LinesOfCode > 0 {
		result.NbClassesWithCode++
		//}

		// Maintainability Index
		if class.Stmts.Analyze.Maintainability != nil {
			if class.Stmts.Analyze.Maintainability.MaintainabilityIndex != nil && !math.IsNaN(float64(*class.Stmts.Analyze.Maintainability.MaintainabilityIndex)) {
				result.MaintainabilityIndex.Sum += *class.Stmts.Analyze.Maintainability.MaintainabilityIndex
				result.MaintainabilityIndex.Counter++
				if specificAggregation.MaintainabilityIndex.Min == 0 || *class.Stmts.Analyze.Maintainability.MaintainabilityIndex < specificAggregation.MaintainabilityIndex.Min {
					result.MaintainabilityIndex.Min = *class.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
				if specificAggregation.MaintainabilityIndex.Max == 0 || *class.Stmts.Analyze.Maintainability.MaintainabilityIndex > specificAggregation.MaintainabilityIndex.Max {
					result.MaintainabilityIndex.Max = *class.Stmts.Analyze.Maintainability.MaintainabilityIndex
				}
			}
			if class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments != nil && !math.IsNaN(float64(*class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments)) {
				result.MaintainabilityIndexWithoutComments.Sum += *class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				result.MaintainabilityIndexWithoutComments.Counter++
				if specificAggregation.MaintainabilityIndexWithoutComments.Min == 0 || *class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments < specificAggregation.MaintainabilityIndexWithoutComments.Min {
					result.MaintainabilityIndexWithoutComments.Min = *class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
				if specificAggregation.MaintainabilityIndexWithoutComments.Max == 0 || *class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments > specificAggregation.MaintainabilityIndexWithoutComments.Max {
					result.MaintainabilityIndexWithoutComments.Max = *class.Stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments
				}
			}
		}

		// Coupling
		if class.Stmts.Analyze.Coupling != nil {
			result.EfferentCoupling.Sum += float64(class.Stmts.Analyze.Coupling.Efferent)
			result.EfferentCoupling.Counter++
			result.AfferentCoupling.Sum += float64(class.Stmts.Analyze.Coupling.Afferent)
			result.AfferentCoupling.Counter++
			// Instability for class
			if class.Stmts.Analyze.Coupling.Efferent > 0 {
				class.Stmts.Analyze.Coupling.Instability = float64(class.Stmts.Analyze.Coupling.Efferent) / float64(class.Stmts.Analyze.Coupling.Efferent+class.Stmts.Analyze.Coupling.Afferent)
			}
		}

		// cyclomatic complexity per class
		if class.Stmts.Analyze.Complexity != nil && class.Stmts.Analyze.Complexity.Cyclomatic != nil {

			result.CyclomaticComplexityPerClass.Sum += float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			result.CyclomaticComplexityPerClass.Counter++
			if specificAggregation.CyclomaticComplexityPerClass.Min == 0 || float64(*class.Stmts.Analyze.Complexity.Cyclomatic) < specificAggregation.CyclomaticComplexityPerClass.Min {
				result.CyclomaticComplexityPerClass.Min = float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			}
			if specificAggregation.CyclomaticComplexityPerClass.Max == 0 || float64(*class.Stmts.Analyze.Complexity.Cyclomatic) > specificAggregation.CyclomaticComplexityPerClass.Max {
				result.CyclomaticComplexityPerClass.Max = float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			}

			result.CyclomaticComplexity.Sum += float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			result.CyclomaticComplexity.Counter++
			if specificAggregation.CyclomaticComplexity.Min == 0 || float64(*class.Stmts.Analyze.Complexity.Cyclomatic) < specificAggregation.CyclomaticComplexity.Min {
				result.CyclomaticComplexity.Min = float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			}
			if specificAggregation.CyclomaticComplexity.Max == 0 || float64(*class.Stmts.Analyze.Complexity.Cyclomatic) > specificAggregation.CyclomaticComplexity.Max {
				result.CyclomaticComplexity.Max = float64(*class.Stmts.Analyze.Complexity.Cyclomatic)
			}
		}

		// Halstead
		if class.Stmts.Analyze.Volume != nil {
			if class.Stmts.Analyze.Volume.HalsteadDifficulty != nil && !math.IsNaN(*class.Stmts.Analyze.Volume.HalsteadDifficulty) {
				result.HalsteadDifficulty.Sum += *class.Stmts.Analyze.Volume.HalsteadDifficulty
				result.HalsteadDifficulty.Counter++
			}
			if class.Stmts.Analyze.Volume.HalsteadEffort != nil && !math.IsNaN(*class.Stmts.Analyze.Volume.HalsteadEffort) {
				result.HalsteadEffort.Sum += *class.Stmts.Analyze.Volume.HalsteadEffort
				result.HalsteadEffort.Counter++
			}
			if class.Stmts.Analyze.Volume.HalsteadVolume != nil && !math.IsNaN(*class.Stmts.Analyze.Volume.HalsteadVolume) {
				result.HalsteadVolume.Sum += *class.Stmts.Analyze.Volume.HalsteadVolume
				result.HalsteadVolume.Counter++
			}
			if class.Stmts.Analyze.Volume.HalsteadTime != nil && !math.IsNaN(*class.Stmts.Analyze.Volume.HalsteadTime) {
				result.HalsteadTime.Sum += *class.Stmts.Analyze.Volume.HalsteadTime
				result.HalsteadTime.Counter++
			}
			// The estimated number of delivered bugs was declared and merged
			// across chunks, but never collected here: it stayed at zero for
			// every project.
			if class.Stmts.Analyze.Volume.HalsteadBugs != nil && !math.IsNaN(*class.Stmts.Analyze.Volume.HalsteadBugs) {
				bugs := *class.Stmts.Analyze.Volume.HalsteadBugs
				result.HalsteadBugs.Sum += bugs
				result.HalsteadBugs.Counter++
				if bugs > result.HalsteadBugs.Max {
					result.HalsteadBugs.Max = bugs
				}
			}
		}

		// LCOM
		// LCOM4 = 0 means the class has no method to measure cohesion on: it is not aggregated
		if class.Stmts.Analyze.ClassCohesion != nil && class.Stmts.Analyze.ClassCohesion.Lcom4 != nil && *class.Stmts.Analyze.ClassCohesion.Lcom4 > 0 {
			// want a float64, got a int32
			lcom4 := float64(*class.Stmts.Analyze.ClassCohesion.Lcom4)
			result.Lcom4PerClass.Sum += lcom4
			result.Lcom4PerClass.Counter++
			if specificAggregation.Lcom4PerClass.Min == 0 || lcom4 < specificAggregation.Lcom4PerClass.Min {
				result.Lcom4PerClass.Min = lcom4
			}
			if specificAggregation.Lcom4PerClass.Max == 0 || lcom4 > specificAggregation.Lcom4PerClass.Max {
				result.Lcom4PerClass.Max = lcom4
			}
		}

		// Coupling
		if class.Stmts.Analyze.Coupling == nil {
			class.Stmts.Analyze.Coupling = &pb.Coupling{
				Efferent: 0,
				Afferent: 0,
			}
		}

		// Add dependencies to file. The coupling of the file itself is left to
		// mapCoupling, which runs after this: nothing is coupled yet.
		if file.Stmts.Analyze.Coupling == nil {
			file.Stmts.Analyze.Coupling = &pb.Coupling{
				Efferent: 0,
				Afferent: 0,
			}
		}
		if file.Stmts.StmtExternalDependencies == nil {
			file.Stmts.StmtExternalDependencies = make([]*pb.StmtExternalDependency, 0)
		}
		file.Stmts.StmtExternalDependencies = append(file.Stmts.StmtExternalDependencies, class.Stmts.StmtExternalDependencies...)
	}

	return result
}

// mergeAggregateResults folds a chunk result into an accumulator. Min is stored
// with 0 meaning "not set yet", which is the convention mapSums uses.
func mergeAggregateResults(into *AggregateResult, chunk AggregateResult) {
	into.Sum += chunk.Sum
	into.Counter += chunk.Counter
	if into.Min == 0 || (chunk.Min > 0 && chunk.Min < into.Min) {
		into.Min = chunk.Min
	}
	if chunk.Max > into.Max {
		into.Max = chunk.Max
	}
}

// mergeChunks merges the sums of one chunk into an accumulator. It has to cover
// every field mapSums writes, otherwise the aggregates that go through it lose
// what the ones built in a single pass keep.
func (r *Aggregator) mergeChunks(aggregated Aggregated, chunk *Aggregated) Aggregated {

	result := aggregated
	result.ConcernedFiles = append(result.ConcernedFiles, chunk.ConcernedFiles...)
	result.NbFiles += chunk.NbFiles
	result.NbTestFiles += chunk.NbTestFiles
	result.NbClasses += chunk.NbClasses
	result.NbClassesWithCode += chunk.NbClassesWithCode
	result.NbMethods += chunk.NbMethods
	result.NbFunctions += chunk.NbFunctions

	mergeAggregateResults(&result.Loc, chunk.Loc)
	mergeAggregateResults(&result.TestLoc, chunk.TestLoc)
	mergeAggregateResults(&result.Cloc, chunk.Cloc)
	mergeAggregateResults(&result.Lloc, chunk.Lloc)

	mergeAggregateResults(&result.MethodsPerClass, chunk.MethodsPerClass)
	mergeAggregateResults(&result.LocPerClass, chunk.LocPerClass)
	mergeAggregateResults(&result.LocPerMethod, chunk.LocPerMethod)
	mergeAggregateResults(&result.LlocPerMethod, chunk.LlocPerMethod)
	mergeAggregateResults(&result.ClocPerMethod, chunk.ClocPerMethod)

	mergeAggregateResults(&result.CyclomaticComplexity, chunk.CyclomaticComplexity)
	mergeAggregateResults(&result.CyclomaticComplexityPerMethod, chunk.CyclomaticComplexityPerMethod)
	mergeAggregateResults(&result.CyclomaticComplexityPerClass, chunk.CyclomaticComplexityPerClass)

	mergeAggregateResults(&result.HalsteadDifficulty, chunk.HalsteadDifficulty)
	mergeAggregateResults(&result.HalsteadEffort, chunk.HalsteadEffort)
	mergeAggregateResults(&result.HalsteadVolume, chunk.HalsteadVolume)
	mergeAggregateResults(&result.HalsteadTime, chunk.HalsteadTime)
	mergeAggregateResults(&result.HalsteadBugs, chunk.HalsteadBugs)

	// LCOM
	mergeAggregateResults(&result.Lcom4PerClass, chunk.Lcom4PerClass)

	mergeAggregateResults(&result.MaintainabilityIndex, chunk.MaintainabilityIndex)
	mergeAggregateResults(&result.MaintainabilityIndexWithoutComments, chunk.MaintainabilityIndexWithoutComments)
	mergeAggregateResults(&result.MaintainabilityCommentWeight, chunk.MaintainabilityCommentWeight)

	mergeAggregateResults(&result.Instability, chunk.Instability)
	mergeAggregateResults(&result.EfferentCoupling, chunk.EfferentCoupling)
	mergeAggregateResults(&result.AfferentCoupling, chunk.AfferentCoupling)

	mergeAggregateResults(&result.MaintainabilityPerMethod, chunk.MaintainabilityPerMethod)
	mergeAggregateResults(&result.MaintainabilityPerMethodWithoutComments, chunk.MaintainabilityPerMethodWithoutComments)
	mergeAggregateResults(&result.MaintainabilityCommentWeightPerMethod, chunk.MaintainabilityCommentWeightPerMethod)

	result.CommitCountForPeriod += chunk.CommitCountForPeriod
	result.CommittedFilesCountForPeriod += chunk.CommittedFilesCountForPeriod

	if result.PackageRelations == nil {
		result.PackageRelations = make(map[string]map[string]int)
	}
	for from, targets := range chunk.PackageRelations {
		if result.PackageRelations[from] == nil {
			result.PackageRelations[from] = make(map[string]int)
		}
		for to, count := range targets {
			result.PackageRelations[from][to] += count
		}
	}

	result.ErroredFiles = append(result.ErroredFiles, chunk.ErroredFiles...)

	return result
}

// Reduce the sums to get the averages
func (r *Aggregator) reduceMetrics(aggregated Aggregated) Aggregated {
	// here we reduce metrics by averaging them
	result := aggregated
	if result.Loc.Counter > 0 {
		result.Loc.Avg = result.Loc.Sum / float64(result.Loc.Counter)
	}
	if result.Cloc.Counter > 0 {
		result.Cloc.Avg = result.Cloc.Sum / float64(result.Cloc.Counter)
	}
	if result.Lloc.Counter > 0 {
		result.Lloc.Avg = result.Lloc.Sum / float64(result.Lloc.Counter)
	}
	if result.MethodsPerClass.Counter > 0 {
		result.MethodsPerClass.Avg = result.MethodsPerClass.Sum / float64(result.MethodsPerClass.Counter)
	}
	if result.LocPerClass.Counter > 0 {
		result.LocPerClass.Avg = result.LocPerClass.Sum / float64(result.LocPerClass.Counter)
	}
	if result.ClocPerMethod.Counter > 0 {
		result.ClocPerMethod.Avg = result.ClocPerMethod.Sum / float64(result.ClocPerMethod.Counter)
	}
	if result.LlocPerMethod.Counter > 0 {
		result.LlocPerMethod.Avg = result.LlocPerMethod.Sum / float64(result.LlocPerMethod.Counter)
	}
	if result.LocPerMethod.Counter > 0 {
		result.LocPerMethod.Avg = result.LocPerMethod.Sum / float64(result.LocPerMethod.Counter)
	}
	if result.CyclomaticComplexityPerMethod.Counter > 0 {
		result.CyclomaticComplexityPerMethod.Avg = result.CyclomaticComplexityPerMethod.Sum / float64(result.CyclomaticComplexityPerMethod.Counter)
	}
	if result.CyclomaticComplexityPerClass.Counter > 0 {
		result.CyclomaticComplexityPerClass.Avg = result.CyclomaticComplexityPerClass.Sum / float64(result.CyclomaticComplexityPerClass.Counter)
	}
	if result.CyclomaticComplexity.Counter > 0 {
		result.CyclomaticComplexity.Avg = result.CyclomaticComplexity.Sum / float64(result.CyclomaticComplexity.Counter)
	}
	if result.HalsteadDifficulty.Counter > 0 {
		result.HalsteadDifficulty.Avg = result.HalsteadDifficulty.Sum / float64(result.HalsteadDifficulty.Counter)
	}
	if result.HalsteadEffort.Counter > 0 {
		result.HalsteadEffort.Avg = result.HalsteadEffort.Sum / float64(result.HalsteadEffort.Counter)
	}
	if result.HalsteadVolume.Counter > 0 {
		result.HalsteadVolume.Avg = result.HalsteadVolume.Sum / float64(result.HalsteadVolume.Counter)
	}
	if result.HalsteadTime.Counter > 0 {
		result.HalsteadTime.Avg = result.HalsteadTime.Sum / float64(result.HalsteadTime.Counter)
	}
	if result.HalsteadBugs.Counter > 0 {
		result.HalsteadBugs.Avg = result.HalsteadBugs.Sum / float64(result.HalsteadBugs.Counter)
	}
	if result.Lcom4PerClass.Counter > 0 {
		result.Lcom4PerClass.Avg = result.Lcom4PerClass.Sum / float64(result.Lcom4PerClass.Counter)
	}
	if result.MaintainabilityIndex.Counter > 0 {
		result.MaintainabilityIndex.Avg = result.MaintainabilityIndex.Sum / float64(result.MaintainabilityIndex.Counter)
	}
	if result.MaintainabilityIndexWithoutComments.Counter > 0 {
		result.MaintainabilityIndexWithoutComments.Avg = result.MaintainabilityIndexWithoutComments.Sum / float64(result.MaintainabilityIndexWithoutComments.Counter)
	}
	if result.MaintainabilityCommentWeight.Counter > 0 {
		result.MaintainabilityCommentWeight.Avg = result.MaintainabilityCommentWeight.Sum / float64(result.MaintainabilityCommentWeight.Counter)
	}
	if result.MaintainabilityPerMethod.Counter > 0 {
		result.MaintainabilityPerMethod.Avg = result.MaintainabilityPerMethod.Sum / float64(result.MaintainabilityPerMethod.Counter)
	}
	if result.MaintainabilityPerMethodWithoutComments.Counter > 0 {
		result.MaintainabilityPerMethodWithoutComments.Avg = result.MaintainabilityPerMethodWithoutComments.Sum / float64(result.MaintainabilityPerMethodWithoutComments.Counter)
	}
	if result.MaintainabilityCommentWeightPerMethod.Counter > 0 {
		result.MaintainabilityCommentWeightPerMethod.Avg = result.MaintainabilityCommentWeightPerMethod.Sum / float64(result.MaintainabilityCommentWeightPerMethod.Counter)
	}

	if result.EfferentCoupling.Counter > 0 {
		result.EfferentCoupling.Avg = result.EfferentCoupling.Sum / float64(result.EfferentCoupling.Counter)
	}
	if result.AfferentCoupling.Counter > 0 {
		result.AfferentCoupling.Avg = result.AfferentCoupling.Sum / float64(result.AfferentCoupling.Counter)
	}

	// afferent coupling
	// Ce / (Ce + Ca)
	if result.AfferentCoupling.Counter > 0 {
		result.Instability.Avg = result.EfferentCoupling.Sum / result.AfferentCoupling.Sum
	}

	// Count commits for the period based on `ResultOfGitAnalysis` data
	result.ResultOfGitAnalysis = r.gitSummaries
	if result.ResultOfGitAnalysis != nil {
		for _, gitAnalysis := range result.ResultOfGitAnalysis {
			result.CommitCountForPeriod += gitAnalysis.CountCommitsForLanguage
		}
	}

	// Bus factor and other metrics based on aggregated data
	for _, analyzer := range r.analyzers {
		analyzer.Calculate(&result)
	}

	return result
}

// packageRelationsDepth is the number of levels a namespace keeps in the
// package relations. They are named more coarsely than the nodes of the graph,
// which the JSON report and the MCP tools have always read them as.
const packageRelationsDepth = 2

// scopeNamespaces maps the qualified name of every class and function of the
// project onto the namespace of the file declaring it.
//
// A dependency is attached to the scope using it, and names that scope as its
// source: the namespace when the import sits at the top of the file, the class
// or the function when it sits, or is used, inside one. The packages and the
// graph are about namespaces, so a source that is a class or a function is
// brought back to the namespace it lives in. A PHP class is named below its
// namespace and would be cut down to it anyway, but a Go struct is named
// "analyzer\Aggregator" where its package is an import path, and nothing but
// this lookup relates the two.
type scopeNamespaces map[string]string

func namespacesOfScopes(files []*pb.File) scopeNamespaces {
	scopes := make(scopeNamespaces)
	for _, file := range files {
		if file == nil || file.Stmts == nil {
			continue
		}
		namespace := namespaceOfFile(file)
		if namespace == "" {
			continue
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class != nil && class.Name != nil && class.Name.Qualified != "" {
				scopes[class.Name.Qualified] = namespace
			}
		}
		for _, function := range engine.GetFunctionsInFile(file) {
			if function != nil && function.Name != nil && function.Name.Qualified != "" {
				scopes[function.Name.Qualified] = namespace
			}
		}
	}
	return scopes
}

// sourceOf returns the namespace a dependency comes from.
func (scopes scopeNamespaces) sourceOf(dependency *pb.StmtExternalDependency) string {
	if namespace, isScope := scopes[dependency.From]; isScope {
		return namespace
	}
	return dependency.From
}

// sourcesOfDependencies lists, per programming language, the namespaces the
// dependencies of the project come from, sorted so that a run cannot name its
// packages differently than the previous one. Test files are left out: the root
// of a project is what its production code shares, and a Tests namespace
// standing beside that code would hide it.
func sourcesOfDependencies(files []*pb.File, dependenciesOf func(*pb.File) []*pb.StmtExternalDependency, scopes scopeNamespaces) map[string][]string {
	sources := make(map[string]map[string]struct{})
	for _, file := range files {
		if file == nil || file.Stmts == nil || file.GetIsTest() {
			continue
		}
		language := file.GetProgrammingLanguage()
		for _, dependency := range dependenciesOf(file) {
			if dependency == nil || dependency.From == "" {
				continue
			}
			if sources[language] == nil {
				sources[language] = make(map[string]struct{})
			}
			sources[language][scopes.sourceOf(dependency)] = struct{}{}
		}
	}
	sorted := make(map[string][]string, len(sources))
	for language, owned := range sources {
		sorted[language] = slices.Sorted(maps.Keys(owned))
	}
	return sorted
}

// namespaceOfFile returns the qualified name of the namespace, package or
// module a file declares, and an empty string when it declares none.
func namespaceOfFile(file *pb.File) string {
	if file == nil || file.Stmts == nil || len(file.Stmts.StmtNamespace) == 0 {
		return ""
	}
	namespace := file.Stmts.StmtNamespace[0]
	if namespace == nil || namespace.Name == nil {
		return ""
	}
	if namespace.Name.Qualified != "" {
		return namespace.Name.Qualified
	}
	return namespace.Name.Short
}

// fileLevelDependencies lists the dependencies attached to the file itself, the
// ones the package relations are built from.
func fileLevelDependencies(file *pb.File) []*pb.StmtExternalDependency {
	return file.Stmts.StmtExternalDependencies
}

// Map the coupling to get the package relations and the afferent coupling
func (r *Aggregator) mapCoupling(aggregated *Aggregated) Aggregated {
	result := *aggregated

	// The classes of the project, by qualified name. Test files are skipped:
	// they are not production code, so they must neither contribute to the
	// coupling sums nor be reachable as a coupling source.
	//
	// The coupling is written on the classes and files themselves, which are
	// shared by every aggregate this runs on: per language, per directory, then
	// on the whole project. Each run therefore starts from zero, so that a class
	// is not counted once more per aggregate, and the last run, the one over the
	// whole project, is the one that stays.
	classesMap := make(map[string]*pb.StmtClass)
	// A dependency names its target the way the language does: PHP writes the
	// qualified name of the class, Java the package and the class apart, Go and
	// C# the package or namespace alone. The targets are indexed every one of
	// those ways, so that a class or a file is found whichever way it is named.
	classesByNamespaceAndShort := make(map[string]*pb.StmtClass)
	fileOfClass := make(map[*pb.StmtClass]*pb.File)
	filesByNamespace := make(map[string][]*pb.File)
	for _, file := range aggregated.ConcernedFiles {
		if file == nil || file.Stmts == nil || file.GetIsTest() {
			continue
		}
		if file.Stmts.Analyze != nil {
			file.Stmts.Analyze.Coupling = &pb.Coupling{}
		}
		namespace := namespaceOfFile(file)
		if namespace != "" {
			filesByNamespace[namespace] = append(filesByNamespace[namespace], file)
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class == nil || class.Name == nil || class.Name.Qualified == "" {
				continue
			}
			classesMap[class.Name.Qualified] = class
			classesByNamespaceAndShort[namespace+"|"+class.Name.Short] = class
			fileOfClass[class] = file
			if class.Stmts != nil && class.Stmts.Analyze != nil {
				class.Stmts.Analyze.Coupling = &pb.Coupling{}
			}
		}
	}
	// targetClassOf returns the class of the project a dependency points at, if
	// it points at one, and nil for a package, a namespace or a foreign class.
	targetClassOf := func(dependency *pb.StmtExternalDependency) *pb.StmtClass {
		if class := classesMap[dependency.Namespace]; class != nil {
			return class
		}
		if dependency.ClassName == "" {
			return nil
		}
		return classesByNamespaceAndShort[dependency.Namespace+"|"+dependency.ClassName]
	}
	// targetFilesOf returns the files of the project a dependency points at:
	// the one declaring the class, or every file of the package or namespace.
	targetFilesOf := func(dependency *pb.StmtExternalDependency) []*pb.File {
		if class := targetClassOf(dependency); class != nil {
			return []*pb.File{fileOfClass[class]}
		}
		return filesByNamespace[dependency.Namespace]
	}
	// dependentFiles holds, for each file, the files depending on it.
	dependentFiles := make(map[*pb.File]map[*pb.File]struct{})

	// The package relations name their packages after the root the project
	// shares, for the same reason the graph does: two levels of a project rooted
	// at Company\Project\SubProject name the project itself, and every relation
	// then reads as the project depending on itself. A project rooted higher
	// keeps the two levels it has always been named by.
	scopes := namespacesOfScopes(aggregated.ConcernedFiles)
	reducers := engine.NewNamespaceReducers(
		sourcesOfDependencies(aggregated.ConcernedFiles, fileLevelDependencies, scopes), packageRelationsDepth)

	for _, file := range aggregated.ConcernedFiles {

		if file == nil || file.Stmts == nil || file.Stmts.StmtExternalDependencies == nil {
			continue
		}

		// Test files are ignored as a source of dependencies: a class used only by
		// tests must not see its afferent coupling inflated, and test classes must
		// not be counted in the efferent/afferent coupling sums nor in the package
		// relations graph.
		if file.GetIsTest() {
			continue
		}

		// The dependencies of the file, each counted once whatever the number of
		// scopes it was attached to. A dependency is what it points at: two
		// scopes of the file using the same class make one dependency, two
		// classes used from the same scope make two.
		uniqueDependencies := make(map[string]*pb.StmtExternalDependency)
		for _, dependency := range file.Stmts.StmtExternalDependencies {
			if dependency == nil {
				continue
			}
			uniqueDependencies[dependency.Namespace+"|"+dependency.ClassName] = dependency
		}
		if file.Stmts.Analyze != nil {
			file.Stmts.Analyze.Coupling.Efferent = int32(len(uniqueDependencies))
		}
		for _, dependency := range uniqueDependencies {
			for _, target := range targetFilesOf(dependency) {
				if target == nil || target == file {
					continue
				}
				if dependentFiles[target] == nil {
					dependentFiles[target] = make(map[*pb.File]struct{})
				}
				dependentFiles[target][file] = struct{}{}
			}
		}

		// The relations are walked in a fixed order, so that a run cannot count
		// them differently than the previous one.
		for _, key := range slices.Sorted(maps.Keys(uniqueDependencies)) {
			dependency := uniqueDependencies[key]

			namespaceTo := reducers.Reduce(file.GetProgrammingLanguage(), dependency.Namespace)
			namespaceFrom := reducers.Reduce(file.GetProgrammingLanguage(), scopes.sourceOf(dependency))

			if namespaceFrom == "" || namespaceTo == "" {
				continue
			}

			// create the map if not exists
			if _, ok := result.PackageRelations[namespaceFrom]; !ok {
				result.PackageRelations[namespaceFrom] = make(map[string]int)
			}

			if _, ok := result.PackageRelations[namespaceFrom][namespaceTo]; !ok {
				result.PackageRelations[namespaceFrom][namespaceTo] = 0
			}

			// increment the counter
			result.PackageRelations[namespaceFrom][namespaceTo]++
		}

		// Zoom on each class, in order to update the afferent coupling of the class itself
		classes := engine.GetClassesInFile(file)
		for _, class := range classes {

			if class == nil || class.Name == nil || class.Name.Qualified == "" {
				continue
			}

			if class.Stmts == nil || class.Stmts.StmtExternalDependencies == nil {
				continue
			}
			uniqueClassDependencies := make(map[string]*pb.StmtExternalDependency)

			for _, dependency := range class.Stmts.StmtExternalDependencies {
				// make it unique
				if dependency == nil || dependency.ClassName == "" {
					continue
				}

				key := dependency.Namespace + "|" + dependency.ClassName
				if _, ok := uniqueClassDependencies[key]; !ok {
					uniqueClassDependencies[key] = dependency
				}
			}

			for _, dependency := range uniqueClassDependencies {
				dependencyName := dependency.ClassName

				// check if dependency is already in hashmap
				if _, ok := result.ClassesAfferentCoupling[dependencyName]; !ok {
					result.ClassesAfferentCoupling[dependencyName] = 0
				}

				result.ClassesAfferentCoupling[dependencyName]++

				fromClass := targetClassOf(dependency)
				if fromClass != nil && fromClass != class {

					if fromClass.Stmts == nil {
						fromClass.Stmts = &pb.Stmts{}
					}
					if fromClass.Stmts.Analyze == nil {
						fromClass.Stmts.Analyze = &pb.Analyze{}
					}
					if fromClass.Stmts.Analyze.Coupling == nil {
						fromClass.Stmts.Analyze.Coupling = &pb.Coupling{
							Efferent: 0,
							Afferent: 0,
						}
					}

					fromClass.Stmts.Analyze.Coupling.Afferent++
				}
			}

			if class.Stmts.Analyze.Coupling == nil {
				class.Stmts.Analyze.Coupling = &pb.Coupling{
					Efferent: 0,
					Afferent: 0,
				}
			}

			class.Stmts.Analyze.Coupling.Efferent = int32(len(uniqueClassDependencies))
			// Increment result (efferent coupling)
			result.EfferentCoupling.Sum += float64(class.Stmts.Analyze.Coupling.Efferent)
			result.EfferentCoupling.Counter++
		}
	}

	// The afferent coupling of a file is the number of files depending on it,
	// through one of its classes or through its package, which is only known
	// once every file has been walked.
	for _, file := range aggregated.ConcernedFiles {
		if file == nil || file.Stmts == nil || file.Stmts.Analyze == nil || file.Stmts.Analyze.Coupling == nil || file.GetIsTest() {
			continue
		}
		coupling := file.Stmts.Analyze.Coupling
		coupling.Afferent = int32(len(dependentFiles[file]))
		// instability, Ce / (Ce + Ca)
		if coupling.Afferent > 0 && coupling.Efferent > 0 {
			coupling.Instability = float64(coupling.Efferent) / float64(coupling.Efferent+coupling.Afferent)
		}
	}
	for _, class := range classesMap {

		if class.Stmts == nil || class.Stmts.Analyze == nil || class.Stmts.Analyze.Coupling == nil {
			continue
		}

		// instability
		if class.Stmts.Analyze.Coupling.Afferent > 0 {
			result.AfferentCoupling.Sum += float64(class.Stmts.Analyze.Coupling.Afferent)
			result.AfferentCoupling.Counter++
			if class.Stmts.Analyze.Coupling.Efferent > 0 {
				class.Stmts.Analyze.Coupling.Instability = float64(class.Stmts.Analyze.Coupling.Efferent) / float64(class.Stmts.Analyze.Coupling.Efferent+class.Stmts.Analyze.Coupling.Afferent)
			}
		}
	}

	// Afferent coupling (global, on aggregated data)
	// Ce / (Ce + Ca)
	if result.AfferentCoupling.Counter > 0 {
		result.Instability.Avg = result.EfferentCoupling.Sum / (result.AfferentCoupling.Sum + result.EfferentCoupling.Sum)
	}

	if result.EfferentCoupling.Counter > 0 {
		result.EfferentCoupling.Avg = result.EfferentCoupling.Sum / float64(result.EfferentCoupling.Counter)
	}
	if result.AfferentCoupling.Counter > 0 {
		result.AfferentCoupling.Avg = result.AfferentCoupling.Sum / float64(result.AfferentCoupling.Counter)
	}

	return result
}
