package cribelo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ast-metrics/ast-metrics/internal/review"
)

// A function that was repaired several times this year is one a reader
// slows down on: the fixes say its contract is easy to get wrong. Git
// follows the lines of a function through history (git log -L), which
// costs a fraction of a second per function on a large repository, so only
// the functions of the files to read are asked about, the most complex
// first, and not more than a dozen.
const (
	// historyWindow is the span the fixes of a function are counted on.
	historyWindow int64 = 365 * 24 * 3600
	// historyMaxFunctions bounds the git calls of one plan.
	historyMaxFunctions = 12
	// historyMinFixes is the number of fixes from which the fact is stated.
	historyMinFixes = 2
)

var revertPattern = regexp.MustCompile(`(?i)(?:^|[^\pL])revert(?:s|ed)?(?:[^\pL]|$)`)

// functionHistory is what git says about the lines of one function.
type functionHistory struct {
	commits, fixes, reverts int
}

// historyFacts states, for the touched functions of the files to read, how
// many fixes their lines took in the past year. The ranges come from the
// base version of each file, where the lines are the ones git knows.
func historyFacts(repo *Repo, baseSha string, base *Parsed, files []FileReport, changes map[string]Change, now int64) []Fact {
	if repo == nil || base == nil || baseSha == "" {
		return nil
	}
	type ask struct {
		path, oldPath, name string
		start, end, weight  int
	}
	var asks []ask
	for _, f := range files {
		if !f.Read || f.Test {
			continue
		}
		baseFile := base.Files[f.Path]
		if baseFile == nil {
			continue
		}
		baseFns := review.CollectFunctions(baseFile)
		oldPath := f.Path
		if c, ok := changes[f.Path]; ok && c.OldPath != "" {
			oldPath = c.OldPath
		}
		for _, fn := range f.Functions {
			before := baseFns[fn.Name]
			if fn.New || before == nil || before.Location == nil {
				continue
			}
			asks = append(asks, ask{f.Path, oldPath, fn.Name, int(before.Location.StartLine), int(before.Location.EndLine), fn.After})
		}
	}
	sort.SliceStable(asks, func(i, j int) bool { return asks[i].weight > asks[j].weight })
	if len(asks) > historyMaxFunctions {
		asks = asks[:historyMaxFunctions]
	}

	results := make([]functionHistory, len(asks))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, a := range asks {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int, a ask) {
			defer wg.Done()
			defer func() { <-slots }()
			results[i] = repo.linesHistory(baseSha, a.oldPath, a.start, a.end, now-historyWindow)
		}(i, a)
	}
	wg.Wait()

	// One fact per file: its functions with a fix record, the most fixed first.
	byPath := map[string][]fixedFunction{}
	var order []string
	for i, a := range asks {
		h := results[i]
		if h.fixes < historyMinFixes {
			continue
		}
		if _, seen := byPath[a.path]; !seen {
			order = append(order, a.path)
		}
		byPath[a.path] = append(byPath[a.path], fixedFunction{name: shortName(a.name), fixes: h.fixes, reverts: h.reverts})
	}
	var facts []Fact
	for _, p := range order {
		facts = append(facts, newFact(FactHistory, p, nil, historyText(byPath[p])))
	}
	return facts
}

type fixedFunction struct {
	name           string
	fixes, reverts int
}

// historyText words the fix record of the functions of one file: the most
// fixed first with its reverts, the next one in short, the rest counted.
func historyText(fns []fixedFunction) string {
	sort.SliceStable(fns, func(i, j int) bool { return fns[i].fixes > fns[j].fixes })
	first := fns[0]
	text := fmt.Sprintf("%s was fixed %d times in the past year", code(first.name+"()"), first.fixes)
	switch first.reverts {
	case 0:
	case 1:
		text += ", once by a revert"
	default:
		text += fmt.Sprintf(", %d times by a revert", first.reverts)
	}
	if len(fns) > 1 {
		text += fmt.Sprintf(", %s %d times", code(fns[1].name+"()"), fns[1].fixes)
	}
	if len(fns) > 2 {
		text += fmt.Sprintf(", +%d more %s", len(fns)-2, plural(len(fns)-2, "function"))
	}
	return text
}

// linesHistory counts the commits that touched a line range of a file since
// a date, and how many of them were fixes or reverts.
func (r *Repo) linesHistory(ref, path string, start, end int, since int64) functionHistory {
	var h functionHistory
	if start < 1 || end < start {
		return h
	}
	out, err := r.git("log", "--no-patch", "--format=%s", fmt.Sprintf("--since=%d", since),
		fmt.Sprintf("-L%d,%d:%s", start, end, path), ref)
	if err != nil {
		return h
	}
	for _, subject := range strings.Split(out, "\n") {
		if strings.TrimSpace(subject) == "" {
			continue
		}
		h.commits++
		if IsFixMessage(subject) {
			h.fixes++
		}
		if revertPattern.MatchString(subject) {
			h.reverts++
		}
	}
	return h
}
