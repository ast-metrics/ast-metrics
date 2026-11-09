package cribelo

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// A coverage report, when the repository has one, tells which of the
// changed lines no test runs. The report is read as it is found: an lcov
// file, a Cobertura or Clover XML, or a Go coverage profile. Nothing is
// run; the report is whatever the last test run left.

// coverageCandidates are the places a report is looked for, in order, when
// none is given.
var coverageCandidates = []string{
	"coverage.out", "cover.out", "coverage.txt",
	"lcov.info", "coverage/lcov.info", "coverage.lcov", "coverage/coverage.lcov",
	"coverage.xml", "coverage/coverage.xml", "cobertura.xml", "coverage/cobertura.xml", "coverage/cobertura-coverage.xml",
	"clover.xml", "coverage/clover.xml", "build/logs/clover.xml",
}

// fileCoverage holds, for one file, the lines a test ran and the lines the
// report instruments, which tells a line nobody runs from a blank or a
// brace.
type fileCoverage struct {
	covered, instrumented map[int]bool
}

func newFileCoverage() *fileCoverage {
	return &fileCoverage{covered: map[int]bool{}, instrumented: map[int]bool{}}
}

func (f *fileCoverage) mark(line, hits int) {
	f.instrumented[line] = true
	if hits > 0 {
		f.covered[line] = true
	}
}

// Coverage is a report mapped onto repository paths.
type Coverage struct {
	// Source is the report file, relative to the root.
	Source string
	files  map[string]*fileCoverage
}

// findCoverage returns the first report present under root, or "".
func findCoverage(root string) string {
	for _, candidate := range coverageCandidates {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate))); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// loadCoverage reads a report. The format is told from the content, not
// the name. Paths in the report are made relative to the root: absolute
// paths by stripping it, Go import paths by stripping the module path of
// go.mod, the rest kept as they are.
func loadCoverage(root, rel string) (*Coverage, error) {
	// The report is named from the repository root, or by an absolute path
	// when the tests wrote it elsewhere.
	path := filepath.FromSlash(rel)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cov := &Coverage{Source: filepath.Base(rel), files: map[string]*fileCoverage{}}
	text := string(data)
	switch {
	case strings.HasPrefix(strings.TrimSpace(text), "mode:"):
		cov.parseGoProfile(root, text)
	case strings.Contains(text, "<coverage") && strings.Contains(text, "<class"):
		if err := cov.parseCobertura(root, data); err != nil {
			return nil, err
		}
	case strings.Contains(text, "<coverage") && strings.Contains(text, "<file"):
		if err := cov.parseClover(root, data); err != nil {
			return nil, err
		}
	case strings.Contains(text, "\nDA:") || strings.HasPrefix(text, "SF:") || strings.HasPrefix(text, "TN:"):
		cov.parseLcov(root, text)
	default:
		return nil, fmt.Errorf("%s: coverage format not recognized (lcov, Cobertura, Clover or Go profile)", rel)
	}
	return cov, nil
}

func (c *Coverage) file(root, reported string) *fileCoverage {
	rel := filepath.ToSlash(reported)
	if filepath.IsAbs(reported) {
		if r, err := filepath.Rel(root, reported); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	rel = strings.TrimPrefix(rel, "./")
	f := c.files[rel]
	if f == nil {
		f = newFileCoverage()
		c.files[rel] = f
	}
	return f
}

func (c *Coverage) parseLcov(root, text string) {
	var current *fileCoverage
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = c.file(root, strings.TrimSpace(line[3:]))
		case strings.HasPrefix(line, "DA:") && current != nil:
			fields := strings.Split(line[3:], ",")
			if len(fields) < 2 {
				continue
			}
			n, err1 := strconv.Atoi(fields[0])
			hits, err2 := strconv.Atoi(fields[1])
			if err1 == nil && err2 == nil {
				current.mark(n, hits)
			}
		case line == "end_of_record":
			current = nil
		}
	}
}

// parseGoProfile reads the profile `go test -coverprofile` writes: one
// block per line, "import/path/file.go:startLine.col,endLine.col statements count".
func (c *Coverage) parseGoProfile(root, text string) {
	module := goModulePath(root)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "mode:") || strings.TrimSpace(line) == "" {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		name := line[:colon]
		var startLine, startCol, endLine, endCol, statements, count int
		if _, err := fmt.Sscanf(line[colon+1:], "%d.%d,%d.%d %d %d", &startLine, &startCol, &endLine, &endCol, &statements, &count); err != nil {
			continue
		}
		rel := name
		if module != "" && strings.HasPrefix(name, module+"/") {
			rel = strings.TrimPrefix(name, module+"/")
		}
		f := c.file(root, rel)
		for n := startLine; n <= endLine; n++ {
			f.mark(n, count)
		}
	}
}

// goModulePath reads the module line of go.mod at root.
func goModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

type coberturaReport struct {
	Sources []string `xml:"sources>source"`
	Classes []struct {
		Filename string `xml:"filename,attr"`
		Lines    []struct {
			Number int `xml:"number,attr"`
			Hits   int `xml:"hits,attr"`
		} `xml:"lines>line"`
	} `xml:"packages>package>classes>class"`
}

// parseCobertura reads the format of coverage.py, Jacoco's exports,
// Istanbul's cobertura reporter and dotnet-coverage. Filenames are relative
// to one of the sources; the one that gives an existing file wins.
func (c *Coverage) parseCobertura(root string, data []byte) error {
	var report coberturaReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return err
	}
	for _, class := range report.Classes {
		rel := class.Filename
		if !filepath.IsAbs(rel) {
			for _, source := range report.Sources {
				joined := filepath.Join(strings.TrimSpace(source), filepath.FromSlash(rel))
				if _, err := os.Stat(joined); err == nil {
					rel = joined
					break
				}
			}
		}
		f := c.file(root, rel)
		for _, line := range class.Lines {
			f.mark(line.Number, line.Hits)
		}
	}
	return nil
}

type cloverReport struct {
	Files []struct {
		Name  string `xml:"name,attr"`
		Path  string `xml:"path,attr"`
		Lines []struct {
			Num   int    `xml:"num,attr"`
			Count int    `xml:"count,attr"`
			Type  string `xml:"type,attr"`
		} `xml:"line"`
	} `xml:"project>package>file"`
	TopFiles []struct {
		Name  string `xml:"name,attr"`
		Path  string `xml:"path,attr"`
		Lines []struct {
			Num   int    `xml:"num,attr"`
			Count int    `xml:"count,attr"`
			Type  string `xml:"type,attr"`
		} `xml:"line"`
	} `xml:"project>file"`
}

// parseClover reads the format of PHPUnit and of Istanbul's clover
// reporter. Methods are left out: a method line is covered when its
// statements are.
func (c *Coverage) parseClover(root string, data []byte) error {
	var report cloverReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return err
	}
	for _, file := range append(report.Files, report.TopFiles...) {
		name := file.Path
		if name == "" {
			name = file.Name
		}
		f := c.file(root, name)
		for _, line := range file.Lines {
			if line.Type == "method" {
				continue
			}
			f.mark(line.Num, line.Count)
		}
	}
	return nil
}

// uncovered counts, among the lines of a file's hunks, those the report
// instruments and no test ran, and those it instruments at all.
func (c *Coverage) uncovered(rel string, hunks []Hunk) (uncovered, instrumented int) {
	f := c.files[rel]
	if f == nil {
		f = c.files[path.Base(rel)]
	}
	if f == nil {
		return 0, 0
	}
	for _, h := range hunks {
		for n := h.Start; n <= h.End; n++ {
			if !f.instrumented[n] {
				continue
			}
			instrumented++
			if !f.covered[n] {
				uncovered++
			}
		}
	}
	return uncovered, instrumented
}

// coverageFacts states, for the files of the reading list the report
// knows, how many of their changed lines no test runs. A handful of
// uncovered lines in a well covered change is not worth a line of the plan:
// the fact is stated from ten uncovered lines, or half of the instrumented
// ones.
func coverageFacts(cov *Coverage, files []FileReport, changes map[string]Change) []Fact {
	if cov == nil {
		return nil
	}
	var facts []Fact
	for _, f := range files {
		if !f.Read || f.Test {
			continue
		}
		c, ok := changes[f.Path]
		if !ok {
			continue
		}
		uncovered, instrumented := cov.uncovered(f.Path, c.Hunks)
		if uncovered == 0 || (uncovered < 10 && uncovered*2 < instrumented) {
			continue
		}
		facts = append(facts, newFact(FactCoverage, f.Path, nil,
			fmt.Sprintf("%d of %d changed %s not run by any test, according to %s", uncovered, instrumented, plural(instrumented, "line"), code(cov.Source))))
	}
	return facts
}
