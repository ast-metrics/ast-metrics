package report

import (
	"crypto/md5"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"

	"github.com/flosch/pongo2/v5"
	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/classifier"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/ui"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	//go:embed templates/*
	htmlContent embed.FS
)

// cachedLangData holds pre-computed JSON strings for a given scope (the whole
// project, one language, or one analyzed directory).
type cachedLangData struct {
	filesJSON           string
	risksJSON           string
	risksByPath         map[string][]riskItemForTpl
	nodeToCommunityJSON string
	testQualityJSON     string
	fileDepsJSON        string
	folderDepsJSON      string
	depFileCount        int
	dictionaryJSON      string
}

type HtmlReportGenerator struct {
	// The path where the report will be generated
	ReportPath string
	// langCache holds pre-computed JSON per scope data key (built once in Generate)
	langCache map[string]*cachedLangData
}

func NewHtmlReportGenerator(reportPath string) Reporter {
	return &HtmlReportGenerator{
		ReportPath: reportPath,
	}
}

func (v *HtmlReportGenerator) Generate(files []*pb.File, projectAggregated analyzer.ProjectAggregated) ([]GeneratedReport, error) {

	// Ensure report is required
	if v.ReportPath == "" {
		return nil, nil
	}

	// Ensure destination folder exists
	err := v.EnsureFolder(v.ReportPath)
	if err != nil {
		return nil, err
	}

	// copy the templates from embed, to temporary folder
	baseTemplateDir := fmt.Sprintf("%s/templates", os.TempDir())
	err = os.MkdirAll(baseTemplateDir, os.ModePerm)
	if err != nil {
		return nil, err
	}
	// ensure partials subfolder exists under base
	partialsDir := fmt.Sprintf("%s/partials", baseTemplateDir)
	if err := os.MkdirAll(partialsDir, os.ModePerm); err != nil {
		return nil, err
	}

	for _, file := range []string{
		"index.html",
		"layout.html",
		"risks.html",
		"compare.html",
		"explorer.html",
		"classes.html",
		"metrics.html",
		"linters.html",
		"classification.html",
		"componentChartRadiusBar.html",
		"componentTableRisks.html",
		"componentTableCompareBranch.html",
		"componentChartRadiusBarMaintainability.html",
		"componentChartRadiusBarLoc.html",
		"componentChartRadiusBarComplexity.html",
		"componentChartRadiusBarInstability.html",
		"componentChartRadiusBarEfferent.html",
		"componentChartRadiusBarAfferent.html",
		"componentDependencyDiagram.html",
		"componentBubbleChart.html",
		"componentComparaisonBadge.html",
		"componentComparaisonOperator.html",
		"communities.html",
		"dependencies.html",
		"busfactor.html",
		"testquality.html",
		"partials/suggestions.html",
		"partials/file_explorer_sidebar.html",
		"partials/language_tabs.html",
	} {
		// read the file
		bytes, err := htmlContent.ReadFile(fmt.Sprintf("templates/html/%s", file))
		if err != nil {
			return nil, err
		}

		// write the file to temporary folder (/tmp) preserving subpaths under baseTemplateDir
		outPath := fmt.Sprintf("%s/%s", baseTemplateDir, file)
		// ensure parent directory exists (e.g., for partials)
		if dir := outPath[:len(outPath)-len(file)]; dir != "" {
			if err := os.MkdirAll(strings.TrimRight(dir, "/"), os.ModePerm); err != nil {
				return nil, err
			}
		}
		err = os.WriteFile(outPath, bytes, 0644)
		if err != nil {
			return nil, err
		}
	}

	// Define loader rooted at the base template directory
	loader := pongo2.MustNewLocalFileSystemLoader(baseTemplateDir)
	pongo2.DefaultSet = pongo2.NewSet(baseTemplateDir, loader)

	// Custom filters
	v.RegisterFilters()

	// Build the list of available scopes: the whole project, then one per
	// programming language, then one per analyzed directory (CLI argument).
	scopeDefs := buildScopes(files, projectAggregated)

	// Pre-compute JSON data once per scope to avoid redundant work across pages
	v.langCache = make(map[string]*cachedLangData)
	for _, scope := range scopeDefs {
		cd := &cachedLangData{}
		dict := NewStringDictionary()

		cd.filesJSON = buildFilesJSONPruned(files, scope.Keep)

		// Build risks
		cd.risksByPath = map[string][]riskItemForTpl{}
		ra := analyzer.NewRiskAnalyzer()
		for _, f := range files {
			if !scope.keeps(f) {
				continue
			}
			items := ra.DetectFileRisks(f)
			if len(items) > 0 {
				converted := make([]riskItemForTpl, 0, len(items))
				for _, it := range items {
					converted = append(converted, riskItemForTpl{ID: it.ID, Title: it.Title, Severity: it.Severity, Details: it.Details})
				}
				cd.risksByPath[f.Path] = converted
			}
		}
		cd.risksJSON = buildRisksJSON(cd.risksByPath, dict)

		// Community
		currentView := scope.View
		cd.nodeToCommunityJSON = "{}"
		if currentView.Community != nil && len(currentView.Community.NodeToCommunity) > 0 {
			cd.nodeToCommunityJSON = buildNodeToCommunityJSON(currentView.Community.NodeToCommunity)
		}

		cd.testQualityJSON = "{}"
		if currentView.TestQuality != nil {
			cd.testQualityJSON = analyzer.BuildTestQualityJSON(currentView.TestQuality)
		}

		cd.fileDepsJSON = buildFileDepsJSON(files, scope.Keep, dict)

		// Count files for this scope
		fileCount := 0
		for _, f := range files {
			if !scope.keeps(f) {
				continue
			}
			fileCount++
		}
		cd.depFileCount = fileCount

		// Build folder-level deps for dependency graph folder view
		cd.folderDepsJSON = buildFolderDepsJSON(files, scope.Keep, dict)

		cd.dictionaryJSON = dict.ToJSON()

		v.langCache[scope.DataKey] = cd
	}

	// Write shared data JS files (one per scope) to avoid duplicating JSON in every HTML page
	dataDir := fmt.Sprintf("%s/data", v.ReportPath)
	err = v.EnsureFolder(dataDir)
	if err != nil {
		return nil, err
	}
	for dataKey, cd := range v.langCache {
		var jsBuilder strings.Builder
		jsBuilder.WriteString("window.__AST_DATA__={")
		jsBuilder.WriteString("files:")
		jsBuilder.WriteString(cd.filesJSON)
		jsBuilder.WriteString(",risks:")
		jsBuilder.WriteString(cd.risksJSON)
		jsBuilder.WriteString(",dictionary:")
		jsBuilder.WriteString(cd.dictionaryJSON)
		jsBuilder.WriteString(",fileDeps:")
		if cd.fileDepsJSON == "" || cd.fileDepsJSON == "{}" {
			jsBuilder.WriteString("{}")
		} else {
			jsBuilder.WriteString(cd.fileDepsJSON)
		}
		jsBuilder.WriteString(",folderDeps:")
		if cd.folderDepsJSON == "" {
			jsBuilder.WriteString("null")
		} else {
			jsBuilder.WriteString(cd.folderDepsJSON)
		}
		jsBuilder.WriteString(",depFileCount:")
		jsBuilder.WriteString(fmt.Sprintf("%d", cd.depFileCount))
		jsBuilder.WriteString(",nodeToCommunity:")
		jsBuilder.WriteString(cd.nodeToCommunityJSON)
		jsBuilder.WriteString(",testQuality:")
		jsBuilder.WriteString(cd.testQualityJSON)
		jsBuilder.WriteString("};")
		dataFile := fmt.Sprintf("%s/data_%s.js", dataDir, dataKey)
		if err := os.WriteFile(dataFile, []byte(jsBuilder.String()), 0644); err != nil {
			return nil, err
		}
	}

	// Write shared linter data file (one copy for all languages, dictionary-encoded)
	linterJS := buildLinterDataJS(projectAggregated.Evaluation)
	if err := os.WriteFile(fmt.Sprintf("%s/linters.js", dataDir), []byte(linterJS), 0644); err != nil {
		return nil, err
	}

	// One page per template, for every scope (all / language / directory)
	for _, template := range []string{
		"index.html",
		"risks.html",
		"explorer.html",
		"classes.html",
		"metrics.html",
		"compare.html",
		"communities.html",
		"dependencies.html",
		"linters.html",
		"busfactor.html",
		"testquality.html",
		"classification.html",
	} {
		for _, scope := range scopeDefs {
			// errors are logged by GenerateScopePage: a single broken page must
			// not discard the whole report
			v.GenerateScopePage(template, scope, scopeDefs, files, projectAggregated)
		}
	}

	// copy images
	err = v.EnsureFolder(fmt.Sprintf("%s/images", v.ReportPath))
	if err != nil {
		return nil, err
	}

	// copy each image
	for _, file := range []string{
		"help-community.png",
		"logo-ast-metrics-small.png",
		"icon-ai.webp",
		"icon-classifier.webp",
		"icon-fingerprint.webp",
	} {
		// read the file
		htmlContent, err := htmlContent.ReadFile(fmt.Sprintf("templates/html/images/%s", file))
		if err != nil {
			return nil, err
		}

		// write the file to temporary folder
		err = os.WriteFile(fmt.Sprintf("%s/images/%s", v.ReportPath, file), htmlContent, 0644)
		if err != nil {
			return nil, err
		}
	}

	// cleanup temporary folder
	err = os.RemoveAll(baseTemplateDir)
	if err != nil {
		return nil, err
	}

	reports := []GeneratedReport{
		{
			Path:        v.ReportPath,
			Type:        "html",
			Description: "The HTML reports allow you to visualize the metrics of your project in a web browser.",
			Icon:        "📊",
		},
	}

	return reports, nil
}

type riskItemForTpl struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Severity float64 `json:"severity"`
	Details  string  `json:"details"`
}

// fileFilter selects the files belonging to a scope. A nil filter keeps every file.
type fileFilter func(*pb.File) bool

// keepFile applies a filter, treating a nil filter as "keep everything".
func keepFile(keep fileFilter, f *pb.File) bool {
	return keep == nil || keep(f)
}

// buildFilesJSONPruned builds a pruned JSON array of files with pathHash injected.
func buildFilesJSONPruned(files []*pb.File, keep fileFilter) string {
	mo := protojson.MarshalOptions{EmitUnpopulated: false, UseEnumNumbers: false, Indent: ""}
	var b strings.Builder
	b.WriteString("[")
	first := true
	for _, f := range files {
		if !keepFile(keep, f) {
			continue
		}
		cf := proto.Clone(f).(*pb.File)
		pruneFile(cf)

		data, err := mo.Marshal(cf)
		if err != nil {
			data = []byte("{}")
		}

		// Round-trip: unmarshal into map, add pathHash, re-marshal
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			m = map[string]any{}
		}
		m["pathHash"] = hashPathForExplorer(cf.GetPath())
		reData, err := json.Marshal(m)
		if err != nil {
			reData = []byte("{}")
		}

		if !first {
			b.WriteString(",")
		}
		b.Write(reData)
		first = false
	}
	b.WriteString("]")
	return b.String()
}

func hashPathForExplorer(path string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	return fmt.Sprintf("%016x", h.Sum64())
}

func pruneFile(f *pb.File) {
	if f.Stmts == nil {
		return
	}
	s := f.Stmts

	classes := engine.GetClassesInFile(f)
	for _, c := range classes {
		pruneClass(c)
	}
	s.StmtClass = classes

	outsideFunctions := engine.GetFunctionsOutsideClassesInFile(f)
	for _, fn := range outsideFunctions {
		pruneFunction(fn)
	}
	s.StmtFunction = outsideFunctions

	s.StmtInterface = nil
	s.StmtTrait = nil
	s.StmtUse = nil
	s.StmtNamespace = nil
	s.StmtDecisionIf = nil
	s.StmtDecisionElseIf = nil
	s.StmtDecisionElse = nil
	s.StmtDecisionCase = nil
	s.StmtLoop = nil
	s.StmtDecisionSwitch = nil
	s.StmtExternalDependencies = nil
}

func pruneClass(c *pb.StmtClass) {
	c.Location = nil
	c.Comments = nil
	c.Operators = nil
	c.Operands = nil
	c.Extends = nil
	c.Implements = nil
	c.Uses = nil
	c.LinesOfCode = nil
	if c.Stmts != nil {
		for _, m := range c.Stmts.StmtFunction {
			pruneFunction(m)
		}
		c.Stmts.StmtClass = nil
		c.Stmts.StmtInterface = nil
		c.Stmts.StmtTrait = nil
		c.Stmts.StmtUse = nil
		c.Stmts.StmtNamespace = nil
		c.Stmts.StmtDecisionIf = nil
		c.Stmts.StmtDecisionElseIf = nil
		c.Stmts.StmtDecisionElse = nil
		c.Stmts.StmtDecisionCase = nil
		c.Stmts.StmtLoop = nil
		c.Stmts.StmtDecisionSwitch = nil
		c.Stmts.StmtExternalDependencies = nil
	}
}

func pruneFunction(m *pb.StmtFunction) {
	m.Location = nil
	m.Comments = nil
	m.Operators = nil
	m.Operands = nil
	m.MethodCalls = nil
	m.Parameters = nil
	m.Externals = nil
	m.LinesOfCode = nil
	if m.Stmts != nil {
		if m.Stmts.Analyze != nil {
			// keep Complexity only
			m.Stmts.Analyze.Volume = nil
			m.Stmts.Analyze.Maintainability = nil
			m.Stmts.Analyze.Risk = nil
			m.Stmts.Analyze.Coupling = nil
			m.Stmts.Analyze.ClassCohesion = nil
		}
		m.Stmts.StmtClass = nil
		m.Stmts.StmtFunction = nil
		m.Stmts.StmtInterface = nil
		m.Stmts.StmtTrait = nil
		m.Stmts.StmtUse = nil
		m.Stmts.StmtNamespace = nil
		m.Stmts.StmtDecisionIf = nil
		m.Stmts.StmtDecisionElseIf = nil
		m.Stmts.StmtDecisionElse = nil
		m.Stmts.StmtDecisionCase = nil
		m.Stmts.StmtLoop = nil
		m.Stmts.StmtDecisionSwitch = nil
		m.Stmts.StmtExternalDependencies = nil
	}
}

func buildNodeToCommunityJSON(n2c map[string]string) string {
	if len(n2c) == 0 {
		return "{}"
	}
	data, err := json.Marshal(n2c)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func buildRisksJSON(risksByPath map[string][]riskItemForTpl, dict *StringDictionary) string {
	hashed := make(map[string][]riskItemForTpl, len(risksByPath))
	for p, items := range risksByPath {
		hashed[dict.Add(p)] = items
	}
	data, err := json.Marshal(hashed)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// linterDataJS builds the content of data/linters.js: a dictionary-encoded
// representation of linter errors and successes.
// Format: window.__AST_LINTERS__={d:{hash:string,...},e:[[ruleHash,sevHash,fileHash,msg],...],s:[[ruleHash,sevHash,fileHash,msg],...]}
func buildLinterDataJS(eval *requirement.EvaluationResult) string {
	dict := NewStringDictionary()
	encodeOutcomes := func(outcomes []requirement.RuleOutcome) string {
		if len(outcomes) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[")
		for i, o := range outcomes {
			if i > 0 {
				b.WriteString(",")
			}
			ruleHash := dict.Add(o.Rule)
			sevHash := dict.Add(string(o.Severity))
			fileHash := dict.Add(o.File)
			msgBytes, _ := json.Marshal(o.Message)
			fmt.Fprintf(&b, "[%q,%q,%q,%s]", ruleHash, sevHash, fileHash, msgBytes)
		}
		b.WriteString("]")
		return b.String()
	}

	var errJSON, succJSON string
	if eval == nil {
		errJSON = "[]"
		succJSON = "[]"
	} else {
		errJSON = encodeOutcomes(eval.Errors)
		succJSON = encodeOutcomes(eval.Successes)
	}

	var js strings.Builder
	js.WriteString("window.__AST_LINTERS__={d:")
	js.WriteString(dict.ToJSON())
	js.WriteString(",e:")
	js.WriteString(errJSON)
	js.WriteString(",s:")
	js.WriteString(succJSON)
	js.WriteString("};")
	return js.String()
}

// tsResolveExtensions lists the file extensions that ES module resolution may
// omit from a relative import specifier (e.g. `import x from './Foo'`),
// longest suffix first so `.d.ts` is stripped before the shorter `.ts`.
var tsResolveExtensions = []string{".d.ts", ".tsx", ".ts", ".jsx", ".js", ".mjs", ".cjs", ".vue"}

// stripResolveExtension removes a known JS/TS/Vue extension from p, if present.
func stripResolveExtension(p string) string {
	for _, ext := range tsResolveExtensions {
		if strings.HasSuffix(p, ext) {
			return strings.TrimSuffix(p, ext)
		}
	}
	return p
}

// resolveRelativeImport resolves a relative import specifier (starting with
// "." or "..") from fromPath to the path of an analyzed file, the way Node/TS
// module resolution would: the specifier is joined to the importing file's
// directory, its extension (if any) is stripped, and the result is looked up
// against every known file with its own extension stripped. Directory imports
// resolve through the "index" entry registered for that directory.
func resolveRelativeImport(fromPath, spec string, noExtToFile map[string]string) string {
	if !strings.HasPrefix(spec, ".") {
		return ""
	}
	joined := filepath.Clean(filepath.Join(filepath.Dir(fromPath), spec))
	if fp, ok := noExtToFile[stripResolveExtension(joined)]; ok {
		return fp
	}
	return ""
}

// buildFileDepsJSON builds a JSON map of file dependency relationships keyed by path hash.
func buildFileDepsJSON(files []*pb.File, keep fileFilter, dict *StringDictionary) string {
	// Step 1: Build class qualified name -> file path lookup, and an
	// extension-less path -> file path lookup used to resolve TS/JS relative
	// imports (e.g. "./GeButton"), which name a file rather than a class.
	classToFile := map[string]string{}
	noExtToFile := map[string]string{}
	for _, f := range files {
		if !keepFile(keep, f) {
			continue
		}
		if f.Stmts == nil {
			continue
		}
		classes := engine.GetClassesInFile(f)
		for _, c := range classes {
			if c.Name == nil {
				continue
			}
			if q := c.Name.GetQualified(); q != "" {
				classToFile[q] = f.Path
			}
			if s := c.Name.GetShort(); s != "" {
				if _, exists := classToFile[s]; !exists {
					classToFile[s] = f.Path
				}
			}
		}

		noExtToFile[stripResolveExtension(f.Path)] = f.Path
		if stripResolveExtension(filepath.Base(f.Path)) == "index" {
			dir := filepath.Dir(f.Path)
			if _, exists := noExtToFile[dir]; !exists {
				noExtToFile[dir] = f.Path
			}
		}
	}

	// Step 2: Build efferent map from StmtExternalDependencies
	type depInfo struct {
		path  string
		short string
	}
	efferent := map[string]map[string]depInfo{}

	for _, f := range files {
		if !keepFile(keep, f) {
			continue
		}
		if f.Stmts == nil {
			continue
		}

		deps := f.Stmts.GetStmtExternalDependencies()
		for _, ns := range f.Stmts.GetStmtNamespace() {
			if ns != nil && ns.Stmts != nil {
				deps = append(deps, ns.Stmts.GetStmtExternalDependencies()...)
			}
		}

		for _, dep := range deps {
			if dep == nil {
				continue
			}
			targetFile := ""
			if ns := dep.GetNamespace(); ns != "" {
				if strings.HasPrefix(ns, ".") {
					targetFile = resolveRelativeImport(f.Path, ns, noExtToFile)
				}
				if targetFile == "" {
					if fp, ok := classToFile[ns]; ok {
						targetFile = fp
					}
				}
			}
			if targetFile == "" {
				if cn := dep.GetClassName(); cn != "" {
					if fp, ok := classToFile[cn]; ok {
						targetFile = fp
					}
				}
			}
			if targetFile == "" || targetFile == f.Path {
				continue
			}
			if efferent[f.Path] == nil {
				efferent[f.Path] = map[string]depInfo{}
			}
			short := targetFile
			if idx := strings.LastIndex(targetFile, "/"); idx >= 0 {
				short = targetFile[idx+1:]
			}
			efferent[f.Path][targetFile] = depInfo{path: targetFile, short: short}
		}
	}

	// Step 3: Invert to get afferent
	afferent := map[string]map[string]depInfo{}
	for srcFile, targets := range efferent {
		srcShort := srcFile
		if idx := strings.LastIndex(srcFile, "/"); idx >= 0 {
			srcShort = srcFile[idx+1:]
		}
		for tgtPath := range targets {
			if afferent[tgtPath] == nil {
				afferent[tgtPath] = map[string]depInfo{}
			}
			afferent[tgtPath][srcFile] = depInfo{path: srcFile, short: srcShort}
		}
	}

	// Step 4: Collect all files that have any dependency
	allFiles := map[string]struct{}{}
	for k := range efferent {
		allFiles[k] = struct{}{}
	}
	for k := range afferent {
		allFiles[k] = struct{}{}
	}

	if len(allFiles) == 0 {
		return "{}"
	}

	// Step 5: Build struct map keyed by hash
	result := make(map[string]fileDepsEntry, len(allFiles))
	for fp := range allFiles {
		entry := fileDepsEntry{
			Efferent: make([]depRef, 0),
			Afferent: make([]depRef, 0),
		}
		if eff, ok := efferent[fp]; ok {
			for _, d := range eff {
				entry.Efferent = append(entry.Efferent, depRef{
					Path:  dict.Add(d.path),
					Short: d.short,
				})
			}
		}
		if aff, ok := afferent[fp]; ok {
			for _, d := range aff {
				entry.Afferent = append(entry.Afferent, depRef{
					Path:  dict.Add(d.path),
					Short: d.short,
				})
			}
		}
		result[dict.Add(fp)] = entry
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// buildFolderDepsJSON aggregates file-level dependencies to folder-level.
// Keys are hashed via the dictionary.
func buildFolderDepsJSON(files []*pb.File, keep fileFilter, dict *StringDictionary) string {
	classToFile := map[string]string{}
	noExtToFile := map[string]string{}
	for _, f := range files {
		if !keepFile(keep, f) {
			continue
		}
		if f.Stmts == nil {
			continue
		}
		classes := engine.GetClassesInFile(f)
		for _, c := range classes {
			if c.Name == nil {
				continue
			}
			if q := c.Name.GetQualified(); q != "" {
				classToFile[q] = f.Path
			}
			if s := c.Name.GetShort(); s != "" {
				if _, exists := classToFile[s]; !exists {
					classToFile[s] = f.Path
				}
			}
		}

		noExtToFile[stripResolveExtension(f.Path)] = f.Path
		if stripResolveExtension(filepath.Base(f.Path)) == "index" {
			dir := filepath.Dir(f.Path)
			if _, exists := noExtToFile[dir]; !exists {
				noExtToFile[dir] = f.Path
			}
		}
	}

	type edge struct {
		src string
		dst string
	}
	var edges []edge
	filesByFolder := map[string]map[string]struct{}{}

	for _, f := range files {
		if !keepFile(keep, f) {
			continue
		}
		if f.Stmts == nil {
			continue
		}

		srcDir := path.Dir(f.Path)
		if filesByFolder[srcDir] == nil {
			filesByFolder[srcDir] = map[string]struct{}{}
		}
		filesByFolder[srcDir][f.Path] = struct{}{}

		deps := f.Stmts.GetStmtExternalDependencies()
		for _, ns := range f.Stmts.GetStmtNamespace() {
			if ns != nil && ns.Stmts != nil {
				deps = append(deps, ns.Stmts.GetStmtExternalDependencies()...)
			}
		}

		for _, dep := range deps {
			if dep == nil {
				continue
			}
			targetFile := ""
			if ns := dep.GetNamespace(); ns != "" {
				if strings.HasPrefix(ns, ".") {
					targetFile = resolveRelativeImport(f.Path, ns, noExtToFile)
				}
				if targetFile == "" {
					if fp, ok := classToFile[ns]; ok {
						targetFile = fp
					}
				}
			}
			if targetFile == "" {
				if cn := dep.GetClassName(); cn != "" {
					if fp, ok := classToFile[cn]; ok {
						targetFile = fp
					}
				}
			}
			if targetFile == "" || targetFile == f.Path {
				continue
			}
			edges = append(edges, edge{src: f.Path, dst: targetFile})
		}
	}

	// Aggregate to folder level
	type folderEdgeCount struct {
		count int
	}
	folderEfferent := map[string]map[string]*folderEdgeCount{}
	folderAfferent := map[string]map[string]*folderEdgeCount{}
	folderFileCount := map[string]int{}

	for dir, fset := range filesByFolder {
		folderFileCount[dir] = len(fset)
	}

	for _, e := range edges {
		srcDir := path.Dir(e.src)
		dstDir := path.Dir(e.dst)
		if srcDir == dstDir {
			continue
		}
		if folderEfferent[srcDir] == nil {
			folderEfferent[srcDir] = map[string]*folderEdgeCount{}
		}
		if folderEfferent[srcDir][dstDir] == nil {
			folderEfferent[srcDir][dstDir] = &folderEdgeCount{}
		}
		folderEfferent[srcDir][dstDir].count++

		if folderAfferent[dstDir] == nil {
			folderAfferent[dstDir] = map[string]*folderEdgeCount{}
		}
		if folderAfferent[dstDir][srcDir] == nil {
			folderAfferent[dstDir][srcDir] = &folderEdgeCount{}
		}
		folderAfferent[dstDir][srcDir].count++
	}

	allFolders := map[string]struct{}{}
	for k := range folderEfferent {
		allFolders[k] = struct{}{}
	}
	for k := range folderAfferent {
		allFolders[k] = struct{}{}
	}

	if len(allFolders) == 0 {
		return ""
	}

	// Build payload using structs
	payload := folderDepsPayload{
		Folders:       make(map[string]folderDepsEntry, len(allFolders)),
		FilesByFolder: make(map[string][]string),
	}

	for dir := range allFolders {
		entry := folderDepsEntry{
			Efferent: make([]folderDepRef, 0),
			Afferent: make([]folderDepRef, 0),
		}
		if eff, ok := folderEfferent[dir]; ok {
			for target, fe := range eff {
				entry.Efferent = append(entry.Efferent, folderDepRef{
					Path:  dict.Add(target),
					Count: fe.count,
				})
			}
		}
		if aff, ok := folderAfferent[dir]; ok {
			for source, fe := range aff {
				entry.Afferent = append(entry.Afferent, folderDepRef{
					Path:  dict.Add(source),
					Count: fe.count,
				})
			}
		}
		fc := folderFileCount[dir]
		if fc == 0 {
			fc = 1
		}
		entry.FileCount = fc
		payload.Folders[dict.Add(dir)] = entry

		// filesByFolder
		if fset, ok := filesByFolder[dir]; ok && len(fset) > 0 {
			flist := make([]string, 0, len(fset))
			for fp := range fset {
				flist = append(flist, dict.Add(fp))
			}
			payload.FilesByFolder[dict.Add(dir)] = flist
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

// scopeKind values used both in Go and in the templates (context "scopeKind").
const (
	scopeKindAll       = "all"
	scopeKindLanguage  = "language"
	scopeKindDirectory = "directory"
)

// scopeDef describes one navigable view of the report: the whole project, a
// single programming language, or a single analyzed directory.
type scopeDef struct {
	// Kind is one of scopeKindAll, scopeKindLanguage, scopeKindDirectory.
	Kind string
	// Label is what the user reads ("All languages", "Golang", "internal/analyzer").
	Label string
	// Suffix is appended to a page base name ("", "_Golang", "_dir_internal-analyzer").
	Suffix string
	// DataKey identifies the data file: data/data_<DataKey>.js
	DataKey string
	// FileCount is the number of files inside the scope.
	FileCount int
	// View is the aggregate matching the scope.
	View analyzer.Aggregated
	// Keep filters the files belonging to the scope. A nil filter keeps everything.
	Keep fileFilter
	// LanguageName is the language of a language scope, "All" otherwise. It feeds
	// the legacy "currentLanguage" template variable.
	LanguageName string
}

// keeps tells whether a file belongs to the scope.
func (s scopeDef) keeps(f *pb.File) bool {
	if s.Keep == nil {
		return true
	}
	return s.Keep(f)
}

// scopeForTemplate is the template-facing projection of a scopeDef.
type scopeForTemplate struct {
	Kind      string
	Label     string
	Suffix    string
	FileCount int
	IsActive  bool
}

// buildScopes returns the ordered list of scopes offered by the report: the
// whole project first, then each programming language, then each analyzed
// directory. Languages and directories are sorted by label so that the
// generated pages are stable across runs.
func buildScopes(files []*pb.File, projectAggregated analyzer.ProjectAggregated) []scopeDef {
	scopes := make([]scopeDef, 0, 1+len(projectAggregated.ByProgrammingLanguage)+len(projectAggregated.ByDirectory))

	scopes = append(scopes, scopeDef{
		Kind:         scopeKindAll,
		Label:        "All languages",
		Suffix:       "",
		DataKey:      "All",
		FileCount:    len(files),
		View:         projectAggregated.Combined,
		Keep:         nil,
		LanguageName: "All",
	})

	languages := make([]string, 0, len(projectAggregated.ByProgrammingLanguage))
	for language := range projectAggregated.ByProgrammingLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		lang := language
		scopes = append(scopes, scopeDef{
			Kind:         scopeKindLanguage,
			Label:        lang,
			Suffix:       fmt.Sprintf("_%s", languageURLSlug(lang)),
			DataKey:      languageURLSlug(lang),
			FileCount:    countFiles(files, func(f *pb.File) bool { return f.GetProgrammingLanguage() == lang }),
			View:         projectAggregated.ByProgrammingLanguage[lang],
			Keep:         func(f *pb.File) bool { return f.GetProgrammingLanguage() == lang },
			LanguageName: lang,
		})
	}

	directories := make([]string, 0, len(projectAggregated.ByDirectory))
	for dir := range projectAggregated.ByDirectory {
		directories = append(directories, dir)
	}
	sort.Slice(directories, func(i, j int) bool {
		return directoryScopeLabel(directories[i]) < directoryScopeLabel(directories[j])
	})
	usedSlugs := map[string]bool{}
	for _, language := range languages {
		usedSlugs[languageURLSlug(language)] = true
	}
	for _, directory := range directories {
		dir := directory
		slug := uniqueSlug(directoryURLSlug(dir), usedSlugs)
		view := projectAggregated.ByDirectory[dir]
		// the aggregator already decided which files belong to this analyzed path
		// (the most specific one wins when paths are nested): reuse its verdict so
		// that the JSON data and the aggregates always describe the same files
		inScope := make(map[string]bool, len(view.ConcernedFiles))
		for _, f := range view.ConcernedFiles {
			inScope[f.GetPath()] = true
		}
		scopes = append(scopes, scopeDef{
			Kind:      scopeKindDirectory,
			Label:     directoryScopeLabel(dir),
			Suffix:    fmt.Sprintf("_dir_%s", slug),
			DataKey:   fmt.Sprintf("dir_%s", slug),
			FileCount: len(view.ConcernedFiles),
			View:      view,
			Keep:      func(f *pb.File) bool { return inScope[f.GetPath()] },
			// a directory scope mixes languages: it behaves like the global view
			// for every language-specific condition of the templates
			LanguageName: "All",
		})
	}

	return scopes
}

// countFiles counts the files matching a filter.
func countFiles(files []*pb.File, keep fileFilter) int {
	count := 0
	for _, f := range files {
		if keep != nil && !keep(f) {
			continue
		}
		count++
	}
	return count
}

func (v *HtmlReportGenerator) GenerateScopePage(template string, scope scopeDef, scopes []scopeDef, files []*pb.File, projectAggregated analyzer.ProjectAggregated) error {

	// Compile the index.html template
	tpl, err := pongo2.DefaultSet.FromFile(template)
	if err != nil {
		log.Error(err)
		return err
	}
	// Render it, passing projectAggregated and files as context
	datetime := time.Now().Format("2006-01-02 15:04")

	// Use pre-computed cached data for this scope
	cd := v.langCache[scope.DataKey]
	if cd == nil {
		cd = &cachedLangData{}
	}
	dataScriptPath := fmt.Sprintf("data/data_%s.js", scope.DataKey)
	linterScriptPath := "data/linters.js"

	// The scope switcher needs every scope, with the current one flagged
	scopesForTemplate := make([]scopeForTemplate, 0, len(scopes))
	hasDirectoryScopes := false
	for _, s := range scopes {
		if s.Kind == scopeKindDirectory {
			hasDirectoryScopes = true
		}
		scopesForTemplate = append(scopesForTemplate, scopeForTemplate{
			Kind:      s.Kind,
			Label:     s.Label,
			Suffix:    s.Suffix,
			FileCount: s.FileCount,
			IsActive:  s.Suffix == scope.Suffix,
		})
	}

	out, err := tpl.Execute(pongo2.Context{
		"datetime":               datetime,
		"page":                   template,
		"currentLanguage":        scope.LanguageName,
		"currentView":            scope.View,
		"projectAggregated":      projectAggregated,
		"files":                  files,
		"risksByPath":            cd.risksByPath,
		"dataScriptPath":         dataScriptPath,
		"linterScriptPath":       linterScriptPath,
		"classificationFamilies": classifier.ClassificationFamilies,
		"scopeSuffix":            scope.Suffix,
		"scopeLabel":             scope.Label,
		"scopeKind":              scope.Kind,
		"scopes":                 scopesForTemplate,
		"hasDirectoryScopes":     hasDirectoryScopes,
	})
	if err != nil {
		log.Error(err)
		return err
	}

	// Write the result to the file
	// prefix is template without the .html part
	pagePrefix := template[:len(template)-5]
	file, err := os.Create(fmt.Sprintf("%s/%s%s.html", v.ReportPath, pagePrefix, scope.Suffix))
	if err != nil {
		log.Error(err)
		return err
	}
	defer file.Close()
	file.WriteString(out)

	return nil
}

func (v *HtmlReportGenerator) EnsureFolder(path string) error {
	// check if the folder exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// create it
		err := os.MkdirAll(path, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}

// languageURLSlug returns a language name safe for use in file names and URLs.
// "C#" would otherwise produce links like "index_C#.html" where the browser
// treats "#" as a fragment delimiter. Existing language names are unchanged.
func languageURLSlug(language string) string {
	s := strings.ReplaceAll(language, "#", "Sharp")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// directoryURLSlug turns an analyzed path into a token safe for file names and
// URLs: "./internal/analyzer" becomes "internal-analyzer". Every character that
// is neither a letter, a digit nor an underscore becomes a dash; dashes are
// then compacted and trimmed.
func directoryURLSlug(directory string) string {
	label := directoryScopeLabel(directory)

	var b strings.Builder
	lastWasDash := false
	for _, r := range label {
		isSafe := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isSafe {
			b.WriteRune(r)
			lastWasDash = false
			continue
		}
		if !lastWasDash {
			b.WriteRune('-')
			lastWasDash = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "root"
	}
	return slug
}

// uniqueSlug guarantees that a slug is not already taken, appending a counter
// when needed. It records the returned slug as taken.
func uniqueSlug(slug string, used map[string]bool) string {
	candidate := slug
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s-%d", slug, i)
	}
	used[candidate] = true
	return candidate
}

// directoryScopeLabel shortens an analyzed path for display: relative to the
// current working directory when it lives under it, unchanged otherwise.
func directoryScopeLabel(directory string) string {
	cleaned := filepath.Clean(directory)
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, cleaned); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return filepath.Base(cleaned)
			}
			cleaned = rel
		}
	}
	return filepath.ToSlash(cleaned)
}

func (v *HtmlReportGenerator) RegisterFilters() {

	// langSlug converts a language name to a URL-safe slug (e.g. "C#" -> "CSharp").
	// Usage: href="index_{{ languageName|langSlug }}.html"
	pongo2.RegisterFilter("langSlug", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		return pongo2.AsValue(languageURLSlug(in.String())), nil
	})

	// jsonEncode marshals any value to a JSON string for embedding in <script>.
	// Usage: {{ value|jsonEncode|safe }}
	pongo2.RegisterFilter("jsonEncode", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		b, e := json.Marshal(in.Interface())
		if e != nil {
			return pongo2.AsValue("null"), nil
		}
		return pongo2.AsValue(string(b)), nil
	})

	// countCategory counts suggestions matching a given category string.
	// Usage: {{ suggestions|countCategory:"coupling" }}
	pongo2.RegisterFilter("countCategory", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		cat := param.String()
		suggestions, ok := in.Interface().([]analyzer.Suggestion)
		if !ok {
			return pongo2.AsValue(0), nil
		}
		known := map[string]bool{"coupling": true, "purity": true, "boundary": true}
		count := 0
		for _, s := range suggestions {
			if cat == "other" {
				if !known[s.Category] {
					count++
				}
			} else if s.Category == cat {
				count++
			}
		}
		return pongo2.AsValue(count), nil
	})

	pongo2.RegisterFilter("sortMaintainabilityIndex", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the list to sort
		// create new empty list
		list := make([]*pb.StmtClass, 0)

		// append to the list when file contians at lease one class
		for _, file := range in.Interface().([]*pb.File) {
			// test files are not production code: never suggest them for refactoring
			if file == nil || file.GetIsTest() || file.Stmts == nil {
				continue
			}
			if len(file.Stmts.StmtClass) == 0 {
				continue
			}

			classes := engine.GetClassesInFile(file)

			for _, class := range classes {
				if class.Stmts.Analyze.Maintainability == nil {
					continue
				}

				if *class.Stmts.Analyze.Maintainability.MaintainabilityIndex < 1 {
					continue
				}

				if *class.Stmts.Analyze.Maintainability.MaintainabilityIndex > 65 {
					continue
				}

				list = append(list, class)
			}
		}

		// sort the list, manually
		sort.Slice(list, func(i, j int) bool {
			if list[i].Stmts.Analyze.Maintainability == nil {
				return false
			}
			if list[j].Stmts.Analyze.Maintainability == nil {
				return true
			}

			// get first class in file
			class1 := list[i]
			class2 := list[j]

			return *class1.Stmts.Analyze.Maintainability.MaintainabilityIndex < *class2.Stmts.Analyze.Maintainability.MaintainabilityIndex
		})

		// keep only the first 10
		if len(list) > 10 {
			list = list[:10]
		}

		return pongo2.AsValue(list), nil
	})

	pongo2.RegisterFilter("jsonForChartDependency", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// create json for chart dependency, like:
		// [ { "source": "A", "target": "B", "value": 1 }, { "source": "A", "target": "C", "value": 1 } ]

		// receive map[string]map[string]int in input
		relations := in.Interface().(map[string]map[string]int)
		json := "["
		for source, targets := range relations {
			for target, value := range targets {
				json += fmt.Sprintf(
					"{ \"source\": \"%s\", \"target\": \"%s\", \"value\": %d },",
					strings.ReplaceAll(source, "\\", "\\\\"),
					strings.ReplaceAll(target, "\\", "\\\\"),
					value,
				)
			}
		}
		json = json[:len(json)-1] + "]"

		if json == "]" {
			// occurs when no relations are found
			json = "[]"
		}

		return pongo2.AsSafeValue(json), nil
	})

	pongo2.RegisterFilter("sortRisk", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {

		rowsToKeep := 10
		if param.Integer() > 0 {
			rowsToKeep = param.Integer()
		}

		// Sort by risk of file
		files := in.Interface().([]*pb.File)
		sort.Slice(files, func(i, j int) bool {
			if files[i].Stmts == nil && files[j].Stmts == nil || files[i].Stmts == nil || files[j].Stmts == nil || files[i].Stmts.Analyze == nil || files[j].Stmts.Analyze == nil {
				return false
			}

			if files[i].Stmts.Analyze.Risk == nil && files[j].Stmts.Analyze.Risk == nil {
				return false
			}

			if files[i].Stmts.Analyze.Risk == nil {
				return false
			}

			if files[j].Stmts.Analyze.Risk == nil {
				return true
			}

			return files[i].Stmts.Analyze.Risk.Score > files[j].Stmts.Analyze.Risk.Score
		})

		// keep only the first n
		if len(files) > rowsToKeep {
			files = files[:rowsToKeep]
		}

		return pongo2.AsValue(files), nil
	})

	// selectTopRiskEntries flattens files into class/file rows and caps the total number of rows
	pongo2.RegisterFilter("selectTopRiskEntries", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		rowsToKeep := 10
		if param != nil && param.Integer() > 0 {
			rowsToKeep = param.Integer()
		}

		// defensive: empty input
		if in == nil || in.IsNil() {
			return pongo2.AsValue([]interface{}{}), nil
		}

		// Sort by risk of file first (reuse logic)
		files := in.Interface().([]*pb.File)
		sort.Slice(files, func(i, j int) bool {
			if files[i] == nil || files[j] == nil || files[i].Stmts == nil || files[j].Stmts == nil || files[i].Stmts.Analyze == nil || files[j].Stmts.Analyze == nil {
				return false
			}
			if files[i].Stmts.Analyze.Risk == nil && files[j].Stmts.Analyze.Risk == nil {
				return false
			}
			if files[i].Stmts.Analyze.Risk == nil {
				return false
			}
			if files[j].Stmts.Analyze.Risk == nil {
				return true
			}
			return files[i].Stmts.Analyze.Risk.Score > files[j].Stmts.Analyze.Risk.Score
		})

		type RiskEntry struct {
			File  *pb.File
			Class *pb.StmtClass
			Name  string
		}

		entries := make([]*RiskEntry, 0, rowsToKeep)

		add := func(file *pb.File, class *pb.StmtClass, name string) bool {
			entries = append(entries, &RiskEntry{File: file, Class: class, Name: name})
			return len(entries) >= rowsToKeep
		}

		for _, file := range files {
			if file == nil || file.Stmts == nil {
				continue
			}
			// test files are not production code: they must not show up as
			// high-risk components / refactoring candidates
			if file.GetIsTest() {
				continue
			}
			// if no classes, treat file as a single row
			if len(file.Stmts.StmtClass) == 0 {
				name := file.Path
				if name == "" {
					name = "(unknown)"
				}
				// Create a dummy class holder so template fields (class.Stmts...) remain available
				dummy := &pb.StmtClass{Stmts: file.Stmts}
				if add(file, dummy, name) {
					break
				}
				continue
			}
			// else, iterate classes
			for _, class := range file.Stmts.StmtClass {
				if class == nil {
					continue
				}
				name := ""
				if class.Name != nil {
					name = class.Name.Qualified
				}
				if name == "" {
					name = file.Path
				}
				if add(file, class, name) {
					break
				}
			}
			if len(entries) >= rowsToKeep {
				break
			}
		}

		return pongo2.AsValue(entries), nil
	})

	// filter to format number. Ex: 1234 -> 1 K
	if !pongo2.FilterExists("stringifyNumber") {
		pongo2.RegisterFilter("stringifyNumber", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
			// get the number to format
			number := in.Integer()

			// format it
			if number > 1000000 {
				return pongo2.AsValue(fmt.Sprintf("%.1f M", float64(number)/1000000)), nil
			} else if number > 1000 {
				return pongo2.AsValue(fmt.Sprintf("%.1f K", float64(number)/1000)), nil
			}

			return pongo2.AsValue(number), nil
		})
	}

	// filter that Return new Cli.NewComponentBarchartCyclomaticByMethodRepartition(aggregated, files)
	pongo2.RegisterFilter("barchartCyclomaticByMethodRepartition", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentBarchartCyclomaticByMethodRepartition{
			Aggregated: aggregated,
			Files:      files,
		}
		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter barchartCyclomaticByMethodRepartition
	pongo2.RegisterFilter("barchartCyclomaticByMethodRepartition", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentBarchartCyclomaticByMethodRepartition{
			Aggregated: aggregated,
			Files:      files,
		}
		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter barchartMaintainabilityIndexRepartition
	pongo2.RegisterFilter("barchartMaintainabilityIndexRepartition", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentBarchartMaintainabilityIndexRepartition{
			Aggregated: aggregated,
			Files:      files,
		}

		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter barchartLocPerMethodRepartition
	pongo2.RegisterFilter("barchartLocPerMethodRepartition", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentBarchartLocByMethodRepartition{
			Aggregated: aggregated,
			Files:      files,
		}
		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter barchartLcomRepartition
	pongo2.RegisterFilter("barchartLcomRepartition", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentBarchartLcomRepartition{
			Aggregated: aggregated,
			Files:      files,
		}
		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter lineChartGitActivity
	pongo2.RegisterFilter("lineChartGitActivity", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the aggregated and files
		aggregated := in.Interface().(analyzer.Aggregated)
		files := aggregated.ConcernedFiles

		// create the component
		comp := ui.ComponentLineChartGitActivity{
			Aggregated: aggregated,
			Files:      files,
		}
		return pongo2.AsSafeValue(comp.AsHtml()), nil
	})

	// filter groupByLabel
	pongo2.RegisterFilter("groupByLabel", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		predictions, ok := in.Interface().([]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue(map[string][]classifier.ClassPrediction{}), nil
		}

		grouped := make(map[string][]classifier.ClassPrediction)
		for _, p := range predictions {
			if len(p.Predictions) > 0 {
				label := p.Predictions[0].Label
				grouped[label] = append(grouped[label], p)
			} else {
				grouped["Unknown"] = append(grouped["Unknown"], p)
			}
		}
		return pongo2.AsValue(grouped), nil
	})

	// filter getLabelDescription: returns the description for a classification label
	pongo2.RegisterFilter("getLabelDescription", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		label := in.String()
		description := classifier.GetDescription(label)
		return pongo2.AsValue(description), nil
	})

	// filter filterTestFiles: filters out predictions for test files
	pongo2.RegisterFilter("filterTestFiles", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		predictions, ok := in.Interface().([]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue([]classifier.ClassPrediction{}), nil
		}

		files, ok := param.Interface().([]*pb.File)
		if !ok {
			// If files param is not provided, return all predictions
			return pongo2.AsValue(predictions), nil
		}

		// Create a map of test file paths for quick lookup
		testFilePaths := make(map[string]bool)
		for _, f := range files {
			if f.IsTest {
				testFilePaths[f.Path] = true
				if f.ShortPath != "" && f.ShortPath != f.Path {
					testFilePaths[f.ShortPath] = true
				}
			}
		}

		// Filter out predictions for test files
		filtered := make([]classifier.ClassPrediction, 0, len(predictions))
		for _, pred := range predictions {
			if !testFilePaths[pred.File] {
				filtered = append(filtered, pred)
			}
		}

		return pongo2.AsValue(filtered), nil
	})

	// filter groupByFamilyAndLabel: groups predictions by family first, then by label
	pongo2.RegisterFilter("groupByFamilyAndLabel", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		predictions, ok := in.Interface().([]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue(classifier.FamilyGroupedPredictions{}), nil
		}
		grouped := classifier.GroupByFamilyAndLabel(predictions)
		return pongo2.AsValue(grouped), nil
	})

	// filter capitalizeFirst: capitalizes the first letter of a string
	pongo2.RegisterFilter("capitalizeFirst", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		s := in.String()
		if len(s) == 0 {
			return pongo2.AsValue(""), nil
		}
		return pongo2.AsValue(strings.ToUpper(s[:1]) + s[1:]), nil
	})

	// filter getMapValue: gets a value from a map using a key
	pongo2.RegisterFilter("getMapValue", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		key := param.String()
		// Try different map types
		switch m := in.Interface().(type) {
		case map[string]interface{}:
			if val, exists := m[key]; exists {
				return pongo2.AsValue(val), nil
			}
		case classifier.FamilyGroupedPredictions:
			if val, exists := m[key]; exists {
				return pongo2.AsValue(val), nil
			}
		}
		return pongo2.AsValue(nil), nil
	})

	// filter countFamilyItems: counts total items in a family data map
	pongo2.RegisterFilter("countFamilyItems", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		familyData, ok := in.Interface().(map[string][]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue(0), nil
		}
		count := 0
		for _, items := range familyData {
			count += len(items)
		}
		return pongo2.AsValue(count), nil
	})

	// filter getArchitectureDiagramData: returns JSON data for architecture diagram
	pongo2.RegisterFilter("getArchitectureDiagramData", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		grouped, ok := in.Interface().(classifier.FamilyGroupedPredictions)
		if !ok {
			return pongo2.AsValue("{}"), nil
		}

		// Build diagram data structure
		type CategoryData struct {
			Label       string `json:"label"`
			ShortName   string `json:"shortName"`
			Count       int    `json:"count"`
			Family      string `json:"family"`
			Description string `json:"description"`
		}

		type LayerData struct {
			Family      string         `json:"family"`
			Description string         `json:"description"`
			Color       string         `json:"color"`
			Categories  []CategoryData `json:"categories"`
		}

		layers := make([]LayerData, 0)
		families := classifier.ClassificationFamilies

		for _, family := range families {
			familyData, exists := grouped[family.Key]
			if !exists {
				continue
			}
			if len(familyData) == 0 {
				continue
			}

			categories := make([]CategoryData, 0)
			for label, items := range familyData {
				parts := strings.Split(label, ":")
				shortName := label
				// If we have at least 2 parts, use the last 2 (subcategory + name)
				// Example: "component:messaging:handler" -> "messaging handler"
				if len(parts) >= 2 {
					shortName = parts[len(parts)-2] + " " + parts[len(parts)-1]
				} else if len(parts) == 1 {
					shortName = parts[0]
				}
				description := classifier.GetDescription(label)
				categories = append(categories, CategoryData{
					Label:       label,
					ShortName:   shortName,
					Count:       len(items),
					Family:      family.Key,
					Description: description,
				})
			}

			if len(categories) > 0 {
				layers = append(layers, LayerData{
					Family:      family.Key,
					Description: family.Description,
					Color:       family.Color,
					Categories:  categories,
				})
			}
		}

		jsonData, jsonErr := json.Marshal(layers)
		if jsonErr != nil {
			return pongo2.AsValue("{}"), nil
		}

		return pongo2.AsValue(string(jsonData)), nil
	})

	// filter getCategoryDependenciesWithFiles: extracts dependencies between categories using files
	pongo2.RegisterFilter("getCategoryDependenciesWithFiles", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// in should be projectAggregated.Predictions
		// param should be files array
		predictions, ok := in.Interface().([]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue("[]"), nil
		}

		files, ok := param.Interface().([]*pb.File)
		if !ok {
			return pongo2.AsValue("[]"), nil
		}

		// Create map: class qualified name -> prediction label
		classToLabel := make(map[string]string)
		for _, pred := range predictions {
			if len(pred.Predictions) > 0 {
				classToLabel[pred.Class] = pred.Predictions[0].Label
			}
		}

		// Create map: file path -> file object
		fileMap := make(map[string]*pb.File)
		for _, f := range files {
			key := f.ShortPath
			if key == "" {
				key = f.Path
			}
			fileMap[key] = f
		}

		// Build dependency links between categories
		type DependencyLink struct {
			FromCategory string `json:"fromCategory"`
			ToCategory   string `json:"toCategory"`
			Count        int    `json:"count"`
		}

		linksMap := make(map[string]int) // "fromLabel->toLabel" -> count

		// For each prediction, find its file and class, then extract dependencies
		for _, pred := range predictions {
			if len(pred.Predictions) == 0 {
				continue
			}
			fromLabel := pred.Predictions[0].Label

			// Find the file
			file, exists := fileMap[pred.File]
			if !exists {
				continue
			}

			// Find the class in the file
			classes := engine.GetClassesInFile(file)
			var targetClass *pb.StmtClass
			for _, class := range classes {
				className := ""
				if class.Name != nil {
					className = class.Name.Qualified
					if className == "" {
						className = class.Name.Short
					}
				}
				if className == pred.Class {
					targetClass = class
					break
				}
			}

			if targetClass == nil {
				continue
			}

			// Get dependencies for this specific class
			className := pred.Class
			var classDeps []*pb.StmtExternalDependency

			// Get explicit dependencies from class stmts
			if targetClass.Stmts != nil {
				for _, dep := range targetClass.Stmts.StmtExternalDependencies {
					if dep != nil {
						depCopy := *dep
						depCopy.From = className
						classDeps = append(classDeps, &depCopy)
					}
				}
			}

			// Get dependencies from extends/implements/uses
			for _, ext := range targetClass.Extends {
				if ext != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: ext.Qualified,
						ClassName: ext.Short,
						From:      className,
					})
				}
			}
			for _, impl := range targetClass.Implements {
				if impl != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: impl.Qualified,
						ClassName: impl.Short,
						From:      className,
					})
				}
			}
			for _, use := range targetClass.Uses {
				if use != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: use.Qualified,
						ClassName: use.Short,
						From:      className,
					})
				}
			}

			// Get dependencies from methods
			if targetClass.Stmts != nil {
				for _, method := range targetClass.Stmts.StmtFunction {
					for _, ext := range method.Externals {
						if ext != nil {
							ns := ext.Qualified
							if ns == "" {
								ns = ext.Short
							}
							classDeps = append(classDeps, &pb.StmtExternalDependency{
								Namespace: ns,
								ClassName: ext.Short,
								From:      className,
							})
						}
					}
					// Also get explicit dependencies from method stmts
					if method.Stmts != nil {
						for _, dep := range method.Stmts.StmtExternalDependencies {
							if dep != nil {
								depCopy := *dep
								depCopy.From = className
								classDeps = append(classDeps, &depCopy)
							}
						}
					}
				}
			}

			// Process each dependency
			for _, dep := range classDeps {
				if dep == nil {
					continue
				}

				// Find the target class in predictions
				targetClassName := dep.ClassName
				if dep.Namespace != "" {
					// Try to construct qualified name
					if !strings.Contains(targetClassName, "::") && !strings.Contains(targetClassName, ".") {
						targetClassName = dep.Namespace + "::" + dep.ClassName
					}
				}

				// Try different variations of the class name
				toLabel := ""
				if label, ok := classToLabel[targetClassName]; ok {
					toLabel = label
				} else if label, ok := classToLabel[dep.ClassName]; ok {
					toLabel = label
				} else if dep.Namespace != "" {
					// Try namespace::className
					fullName := dep.Namespace + "::" + dep.ClassName
					if label, ok := classToLabel[fullName]; ok {
						toLabel = label
					}
				}

				if toLabel != "" && toLabel != fromLabel {
					key := fromLabel + "->" + toLabel
					linksMap[key]++
				}
			}
		}

		// Convert to list
		linksList := make([]DependencyLink, 0, len(linksMap))
		for key, count := range linksMap {
			parts := strings.Split(key, "->")
			if len(parts) == 2 {
				linksList = append(linksList, DependencyLink{
					FromCategory: parts[0],
					ToCategory:   parts[1],
					Count:        count,
				})
			}
		}

		jsonData, jsonErr := json.Marshal(linksList)
		if jsonErr != nil {
			return pongo2.AsValue("[]"), nil
		}

		return pongo2.AsValue(string(jsonData)), nil
	})

	// filter getCategoryDependenciesWithFiles: extracts dependencies between categories using files
	pongo2.RegisterFilter("getCategoryDependenciesWithFiles", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// in should be projectAggregated.Predictions
		// param should be files array
		predictions, ok := in.Interface().([]classifier.ClassPrediction)
		if !ok {
			return pongo2.AsValue("[]"), nil
		}

		files, ok := param.Interface().([]*pb.File)
		if !ok {
			return pongo2.AsValue("[]"), nil
		}

		// Create map: class qualified name -> prediction label
		classToLabel := make(map[string]string)
		for _, pred := range predictions {
			if len(pred.Predictions) > 0 {
				classToLabel[pred.Class] = pred.Predictions[0].Label
			}
		}

		// Create map: file path -> file object
		fileMap := make(map[string]*pb.File)
		for _, f := range files {
			key := f.ShortPath
			if key == "" {
				key = f.Path
			}
			fileMap[key] = f
		}

		// Build dependency links between categories
		type DependencyLink struct {
			FromCategory string `json:"fromCategory"`
			ToCategory   string `json:"toCategory"`
			Count        int    `json:"count"`
		}

		linksMap := make(map[string]int) // "fromLabel->toLabel" -> count

		// For each prediction, find its file and class, then extract dependencies
		for _, pred := range predictions {
			if len(pred.Predictions) == 0 {
				continue
			}
			fromLabel := pred.Predictions[0].Label

			// Find the file
			file, exists := fileMap[pred.File]
			if !exists {
				continue
			}

			// Find the class in the file
			classes := engine.GetClassesInFile(file)
			var targetClass *pb.StmtClass
			for _, class := range classes {
				className := ""
				if class.Name != nil {
					className = class.Name.Qualified
					if className == "" {
						className = class.Name.Short
					}
				}
				if className == pred.Class {
					targetClass = class
					break
				}
			}

			if targetClass == nil {
				continue
			}

			// Get dependencies for this specific class
			className := pred.Class
			var classDeps []*pb.StmtExternalDependency

			// Get explicit dependencies from class stmts
			if targetClass.Stmts != nil {
				for _, dep := range targetClass.Stmts.StmtExternalDependencies {
					if dep != nil {
						depCopy := *dep
						depCopy.From = className
						classDeps = append(classDeps, &depCopy)
					}
				}
			}

			// Get dependencies from extends/implements/uses
			for _, ext := range targetClass.Extends {
				if ext != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: ext.Qualified,
						ClassName: ext.Short,
						From:      className,
					})
				}
			}
			for _, impl := range targetClass.Implements {
				if impl != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: impl.Qualified,
						ClassName: impl.Short,
						From:      className,
					})
				}
			}
			for _, use := range targetClass.Uses {
				if use != nil {
					classDeps = append(classDeps, &pb.StmtExternalDependency{
						Namespace: use.Qualified,
						ClassName: use.Short,
						From:      className,
					})
				}
			}

			// Get dependencies from methods
			if targetClass.Stmts != nil {
				for _, method := range targetClass.Stmts.StmtFunction {
					for _, ext := range method.Externals {
						if ext != nil {
							ns := ext.Qualified
							if ns == "" {
								ns = ext.Short
							}
							classDeps = append(classDeps, &pb.StmtExternalDependency{
								Namespace: ns,
								ClassName: ext.Short,
								From:      className,
							})
						}
					}
					// Also get explicit dependencies from method stmts
					if method.Stmts != nil {
						for _, dep := range method.Stmts.StmtExternalDependencies {
							if dep != nil {
								depCopy := *dep
								depCopy.From = className
								classDeps = append(classDeps, &depCopy)
							}
						}
					}
				}
			}

			// Process each dependency
			for _, dep := range classDeps {
				if dep == nil {
					continue
				}

				// Find the target class in predictions
				targetClassName := dep.ClassName
				if dep.Namespace != "" {
					// Try to construct qualified name
					if !strings.Contains(targetClassName, "::") && !strings.Contains(targetClassName, ".") {
						targetClassName = dep.Namespace + "::" + dep.ClassName
					}
				}

				// Try different variations of the class name
				toLabel := ""
				if label, ok := classToLabel[targetClassName]; ok {
					toLabel = label
				} else if label, ok := classToLabel[dep.ClassName]; ok {
					toLabel = label
				} else if dep.Namespace != "" {
					// Try namespace::className
					fullName := dep.Namespace + "::" + dep.ClassName
					if label, ok := classToLabel[fullName]; ok {
						toLabel = label
					}
				}

				if toLabel != "" && toLabel != fromLabel {
					key := fromLabel + "->" + toLabel
					linksMap[key]++
				}
			}
		}

		// Convert to list
		linksList := make([]DependencyLink, 0, len(linksMap))
		for key, count := range linksMap {
			parts := strings.Split(key, "->")
			if len(parts) == 2 {
				linksList = append(linksList, DependencyLink{
					FromCategory: parts[0],
					ToCategory:   parts[1],
					Count:        count,
				})
			}
		}

		jsonData, jsonErr := json.Marshal(linksList)
		if jsonErr != nil {
			return pongo2.AsValue("[]"), nil
		}

		return pongo2.AsValue(string(jsonData)), nil
	})

	// filter convertOneFileToCollection
	pongo2.RegisterFilter("convertOneFileToCollection", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		file := in.Interface().(*pb.File)
		return pongo2.AsValue([]*pb.File{file}), nil
	})

	// filter getClassesInFile: returns classes via GetClassesInFile (namespace-aware).
	// After protobuf serialization/deserialization, file.Stmts.StmtClass and
	// namespace.Stmts.StmtClass are different objects. Coupling is computed on
	// GetClassesInFile results, so templates must use this filter.
	pongo2.RegisterFilter("getClassesInFile", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		file := in.Interface().(*pb.File)
		return pongo2.AsValue(engine.GetClassesInFile(file)), nil
	})

	// filter : has class or uis procedural script
	pongo2.RegisterFilter("fileHasClasses", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		file := in.Interface().(*pb.File)
		return pongo2.AsValue(len(engine.GetClassesInFile(file)) > 0), nil
	})

	// filter : has class or uis procedural script
	pongo2.RegisterFilter("toCollectionOfParsableComponents", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		file := in.Interface().(*pb.File)

		if len(engine.GetClassesInFile(file)) > 0 {
			return pongo2.AsValue(engine.GetClassesInFile(file)), nil
		}

		collection := make([]*pb.StmtFunction, 0)
		collection = append(collection, file.Stmts.StmtFunction...)

		return pongo2.AsValue(collection), nil
	})

	// filter contributorInitials: extracts initials from a name (e.g., "John Doe" -> "JD")
	pongo2.RegisterFilter("contributorInitials", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		name := in.String()
		if name == "" {
			return pongo2.AsValue("?"), nil
		}

		// Split by common separators and get first letter of each word
		parts := strings.Fields(name)
		initials := strings.Builder{}
		for _, part := range parts {
			if len(part) > 0 {
				// Get first letter (handling unicode)
				for _, r := range part {
					if unicode.IsLetter(r) {
						initials.WriteRune(unicode.ToUpper(r))
						break
					}
				}
			}
		}

		result := initials.String()
		if result == "" {
			// Fallback: use first character
			for _, r := range name {
				if unicode.IsPrint(r) {
					result = strings.ToUpper(string(r))
					break
				}
			}
			if result == "" {
				result = "?"
			}
		}

		// Limit to 2-3 characters max
		if len([]rune(result)) > 3 {
			result = string([]rune(result)[:3])
		}

		return pongo2.AsValue(result), nil
	})

	// gravatarUrl builds the avatar URL of an email address. Returns an empty
	// string when there is no address, so the template falls back to initials.
	// Usage: {{ committer.Email|gravatarUrl:80 }}
	pongo2.RegisterFilter("gravatarUrl", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		email := strings.ToLower(strings.TrimSpace(in.String()))
		if email == "" || !strings.Contains(email, "@") {
			return pongo2.AsValue(""), nil
		}
		size := 80
		if param != nil && param.Integer() > 0 {
			size = param.Integer()
		}
		sum := md5.Sum([]byte(email))
		// d=404 so a missing avatar fails instead of returning a placeholder:
		// the template then keeps its initials.
		return pongo2.AsValue(fmt.Sprintf("https://www.gravatar.com/avatar/%x?s=%d&d=404", sum, size)), nil
	})

	// filter contributorColor: generates a consistent color based on name hash
	pongo2.RegisterFilter("contributorColor", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		name := in.String()
		if name == "" {
			return pongo2.AsValue("#9ca3af"), nil // gray fallback
		}

		// Generate hash from name
		h := fnv.New32a()
		h.Write([]byte(name))
		hash := h.Sum32()

		// Identity palette: cool hues only. Green, amber and red carry severity
		// everywhere else in the report, so a contributor must never wear them.
		colors := []string{
			"#3b82f6", // blue
			"#8b5cf6", // purple
			"#ec4899", // pink
			"#06b6d4", // cyan
			"#6366f1", // indigo
			"#14b8a6", // teal
			"#a855f7", // violet
			"#0ea5e9", // sky
			"#d946ef", // fuchsia
			"#64748b", // slate
			"#7c3aed", // deep violet
			"#0891b2", // deep cyan
		}

		colorIndex := int(hash) % len(colors)
		return pongo2.AsValue(colors[colorIndex]), nil
	})

	// filter getRoleCategory: extracts category from a role label
	pongo2.RegisterFilter("getRoleCategory", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		label := in.String()
		parts := strings.Split(label, ":")
		if len(parts) >= 2 {
			return pongo2.AsValue(parts[1]), nil
		}
		return pongo2.AsValue("unknown"), nil
	})

	// filter getRoleShortName: extracts short name from a role label
	pongo2.RegisterFilter("getRoleShortName", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		label := in.String()
		parts := strings.Split(label, ":")
		if len(parts) >= 3 {
			return pongo2.AsValue(parts[2]), nil
		}
		if len(parts) >= 2 {
			return pongo2.AsValue(parts[1]), nil
		}
		return pongo2.AsValue(label), nil
	})

	// filter getUniqueRoles: extracts unique roles from role flows
	pongo2.RegisterFilter("getUniqueRoles", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		flows, ok := in.Interface().([]analyzer.RoleFlow)
		if !ok {
			return pongo2.AsValue([]string{}), nil
		}
		roleSet := make(map[string]bool)
		for _, flow := range flows {
			roleSet[flow.FromRole] = true
			roleSet[flow.ToRole] = true
		}
		roles := make([]string, 0, len(roleSet))
		for role := range roleSet {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		return pongo2.AsValue(roles), nil
	})

	// filter escapejs: escapes a string for safe use in JavaScript
	pongo2.RegisterFilter("escapejs", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		str := in.String()
		// Escape backslashes first (important!)
		str = strings.ReplaceAll(str, "\\", "\\\\")
		// Escape quotes
		str = strings.ReplaceAll(str, "\"", "\\\"")
		str = strings.ReplaceAll(str, "'", "\\'")
		// Escape newlines
		str = strings.ReplaceAll(str, "\n", "\\n")
		str = strings.ReplaceAll(str, "\r", "\\r")
		// Escape tabs
		str = strings.ReplaceAll(str, "\t", "\\t")
		return pongo2.AsValue(str), nil
	})
}
