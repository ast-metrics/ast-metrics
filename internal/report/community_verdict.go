package report

import (
	"html"
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
	return out
}
