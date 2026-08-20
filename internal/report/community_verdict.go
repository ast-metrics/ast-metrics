package report

import (
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

// verdictLinks turns the names quoted by the verdict into links: a community
// name opens its row, a class of the shared kernel opens the kernel's row with
// that member marked. The text is escaped first; the longest names are
// replaced first so that "Github" never eats "GithubEvents"; a name already
// inside a link is not touched again.
func verdictLinks(text string, cm *analyzer.CommunityMetrics) string {
	escaped := html.EscapeString(text)
	if cm == nil || text == "" {
		return escaped
	}
	type target struct {
		name, href, communities, member string
	}
	targets := []target{}
	for _, c := range cm.Communities {
		if c.ShortName != "" {
			targets = append(targets, target{name: html.EscapeString(c.ShortName), href: "#community-" + c.ID, communities: c.ID})
		}
	}
	if cm.Shared != nil {
		for _, hub := range cm.Shared.Hubs {
			if label := cm.Labels[hub]; label != "" {
				targets = append(targets, target{name: html.EscapeString(label), href: "#community-" + analyzer.SharedID, communities: analyzer.SharedID, member: label})
			}
		}
	}
	sort.SliceStable(targets, func(i, j int) bool { return len(targets[i].name) > len(targets[j].name) })

	// Replace through placeholders, so a shorter name is never found again
	// inside the link written for a longer one.
	out := escaped
	links := []string{}
	for _, t := range targets {
		if !strings.Contains(out, t.name) {
			continue
		}
		link := `<a class="verdict-link" href="` + t.href + `" data-communities="` + t.communities + `"`
		if t.member != "" {
			link += ` data-member="` + html.EscapeString(t.member) + `"`
		}
		link += `>` + t.name + `</a>`
		links = append(links, link)
		out = strings.ReplaceAll(out, t.name, "\x00"+string(rune('A'+len(links)-1))+"\x00")
	}
	for i, link := range links {
		out = strings.ReplaceAll(out, "\x00"+string(rune('A'+i))+"\x00", link)
	}
	// "and N more" after the members of a cycle opens the table filtered on
	// the communities caught in one.
	if strings.Contains(escaped, "cycle") {
		out = andMore.ReplaceAllString(out, `<a class="verdict-link" href="#communities" data-filter="cycle">$1</a>`)
	}
	return out
}

// andMore matches the "and N more" closing a list of names.
var andMore = regexp.MustCompile(`\b(and \d+ more)\b`)

// issueOrder is the order the table's filters and pills follow, the same
// as the findings: what is caught in a cycle first, then what has no
// boundary, what is scattered, then what the commits say.
var issueOrder = []string{"cycle", "exposed", "spread", "history-crossed", "history-loose"}

// issueLabels are the words of the finding pills, reused on the rows.
var issueLabels = map[string]string{
	"cycle":           "cycle",
	"exposed":         "no boundary",
	"spread":          "spread",
	"history-crossed": "changes together",
	"history-loose":   "never as a whole",
}

// issueTips say in one line what each issue means, for the tooltips.
var issueTips = map[string]string{
	"cycle":           "In a cycle: this community and others reach each other through their dependencies, none can change alone.",
	"exposed":         "No boundary: reached from everywhere, no member acts as its entry.",
	"spread":          "Spread: filed in several folders, one module scattered across the tree.",
	"history-crossed": "Changes together: the same commits keep touching this community and another.",
	"history-loose":   "Never as a whole: its members change with other code more than with each other.",
}

// issueLabel gives the words of the pill for an issue kind.
func issueLabel(kind string) string {
	if label, ok := issueLabels[kind]; ok {
		return label
	}
	return kind
}

// issueTip gives the tooltip of an issue kind.
func issueTip(kind string) string {
	if tip, ok := issueTips[kind]; ok {
		return tip
	}
	return kind
}

// issueCount is one quick filter of the table: an issue kind, its label,
// how many communities carry it.
type issueCount struct {
	Kind, Label string
	Count       int
}

// communityIssueCounts counts, per issue kind and in the order of the
// pills, the ranked communities carrying it; the shared kernel is not
// counted, it sits apart from the ranking. A kind nobody carries is left
// out: the filters only offer what is there.
func communityIssueCounts(cm *analyzer.CommunityMetrics) []issueCount {
	if cm == nil {
		return nil
	}
	counts := map[string]int{}
	for _, c := range cm.Communities {
		if c.Shared {
			continue
		}
		for _, kind := range c.Issues {
			counts[kind]++
		}
	}
	out := []issueCount{}
	for _, kind := range issueOrder {
		if counts[kind] > 0 {
			out = append(out, issueCount{Kind: kind, Label: issueLabel(kind), Count: counts[kind]})
		}
	}
	return out
}

// hasFinding tells whether the findings hold one of the kind.
func hasFinding(cm *analyzer.CommunityMetrics, kind string) bool {
	if cm == nil {
		return false
	}
	for _, f := range cm.Findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}
