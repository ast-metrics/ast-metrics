package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderCommunitiesPage generates the report of a project whose community
// metrics are given, and returns the communities page.
func renderCommunitiesPage(t *testing.T, cm *analyzer.CommunityMetrics) string {
	t.Helper()
	files := []*pb.File{{Path: "/project/src/a.go", ProgrammingLanguage: "Golang", Stmts: &pb.Stmts{}}}
	pa := analyzer.NewAggregator(files, nil).Aggregates()
	pa.Combined.Community = cm
	dir := t.TempDir()
	_, err := NewHtmlReportGenerator(dir).Generate(files, pa)
	require.NoError(t, err)
	page, err := os.ReadFile(filepath.Join(dir, "communities.html"))
	require.NoError(t, err)
	return string(page)
}

func twoCommunities() *analyzer.CommunityMetrics {
	a := &analyzer.Community{ID: "0", Name: "app/billing", ShortName: "billing", Hint: "Invoice", Size: 2, Units: []string{"Invoice", "Payment"},
		Namespaces:   []analyzer.NamespaceShare{{Namespace: "app/billing", Label: "billing", Count: 2, Share: 1}},
		Uses:         []analyzer.CommunityLink{{ID: "1", Name: "users", Weight: 3, Details: []analyzer.UnitLink{{From: "Invoice", To: "User", Weight: 3}}}},
		Externals:    []analyzer.ExternalUse{{Namespace: "fmt", Count: 2}},
		Exposed:      []string{"Invoice"},
		ExposedCount: 1, ExposedShare: 0.5, Confidence: 1}
	b := &analyzer.Community{ID: "1", Name: "app/users", ShortName: "users", Size: 1, Units: []string{"User"},
		Namespaces: []analyzer.NamespaceShare{{Namespace: "app/users", Label: "users", Count: 1, Share: 1}},
		UsedBy:     []analyzer.CommunityLink{{ID: "0", Name: "billing", Weight: 3}}, Confidence: 1}
	return &analyzer.CommunityMetrics{
		Granularity: "class", Communities: []*analyzer.Community{a, b}, CommunitiesCount: 2, UnitCount: 3,
		NodeToCommunity: map[string]string{"Invoice": "0", "Payment": "0", "User": "1"},
		Edges:           []analyzer.CommunityEdge{{From: "0", To: "1", Weight: 3}},
		InternalShare:   0.5, CrossShare: 0.5, Verdict: "Two communities.", Labels: map[string]string{},
	}
}

// TestCommunitiesPage_RowsCarryADetailPanel checks that each community row is
// followed by a hidden panel holding its namespaces, its links and its
// members, and that the row is wired to open it.
func TestCommunitiesPage_RowsCarryADetailPanel(t *testing.T) {
	page := renderCommunitiesPage(t, twoCommunities())
	assert.Contains(t, page, `id="community-0" class="comm-row" data-id="0" data-issues="" tabindex="0" aria-expanded="false" aria-controls="community-0-detail"`)
	assert.Contains(t, page, `id="community-0-detail" class="comm-detail" data-for="0" hidden`)
	assert.Contains(t, page, `data-lazy-members="0"`)
	// the summary row keeps a one-line count of the links, the panel names them
	assert.Contains(t, page, `uses <span class="font-mono text-slate-700">1</span>`)
	assert.Contains(t, page, `Invoice <span class="arrow">→</span> User`)
	assert.Contains(t, page, `Relies on <span class="ident">fmt</span>`)
	// the members and the externals no longer sit in the name cell
	assert.Less(t, strings.Index(page, `id="community-0-detail"`), strings.Index(page, `data-lazy-members="0"`))
}

// TestCommunitiesPage_WhereToStart checks the block listing the actions, and
// its calm line when there is nothing urgent.
func TestCommunitiesPage_WhereToStart(t *testing.T) {
	cm := twoCommunities()
	page := renderCommunitiesPage(t, cm)
	assert.Contains(t, page, "Nothing urgent: the communities are separated cleanly.")
	assert.NotContains(t, page, `class="action-row"`)

	cm.Actions = []analyzer.CommunityAction{
		{Kind: "cut", Title: "Cut billing → users", Detail: "It closes a cycle.", Effort: "3 references", Gain: "Frees 2 modules from the cycle", Communities: []string{"0", "1"}, Units: []string{"Invoice"}},
		{Kind: "move", Title: "Move Invoice next to User", Detail: "They change together.", Effort: "1 class", Communities: []string{"0"}},
	}
	page = renderCommunitiesPage(t, cm)
	assert.NotContains(t, page, "Nothing urgent")
	assert.Contains(t, page, `<a class="action-row" href="#community-0" data-communities="0 1">`)
	assert.Contains(t, page, `action-dot action-dot--cut`)
	assert.Contains(t, page, `action-dot action-dot--move`)
	assert.Contains(t, page, `<span class="action-title">Cut billing → users</span>`)
	assert.Contains(t, page, `<span class="action-effort">3 references</span>`)
	assert.Contains(t, page, `<span class="action-gain">Frees 2 modules from the cycle</span>`)
	assert.Less(t, strings.Index(page, `<span class="action-title">Cut billing`), strings.Index(page, `<span class="action-gain">`))
	assert.Less(t, strings.Index(page, `<span class="action-gain">`), strings.Index(page, `<span class="action-detail">It closes`))
	assert.Equal(t, 1, strings.Count(page, `class="action-gain"`))
	assert.Equal(t, 2, strings.Count(page, `class="action-row"`))
}

// TestCommunitiesPage_ThreeSections checks the layout: the actions sit in a
// strip inside the map card, and the findings, the table and its history
// view share one card driven by tabs, the History tab only with commits.
func TestCommunitiesPage_ThreeSections(t *testing.T) {
	cm := twoCommunities()
	page := renderCommunitiesPage(t, cm)
	assert.Contains(t, page, `<div class="start-strip" id="actions-card">`)
	assert.Less(t, strings.Index(page, `id="community-map-wrap"`), strings.Index(page, `id="actions-card"`))
	assert.Less(t, strings.Index(page, `id="actions-card"`), strings.Index(page, `id="reading-card"`))
	assert.Contains(t, page, `data-panel="findings" aria-selected="true" aria-controls="panel-findings"`)
	assert.Contains(t, page, `data-panel="communities" aria-selected="false" aria-controls="panel-communities"`)
	assert.Contains(t, page, `Communities<span class="tab-count">2</span>`)
	assert.Contains(t, page, `id="panel-communities" role="tabpanel" hidden`)
	assert.NotContains(t, page, `data-panel="history"`)
	assert.NotContains(t, page, `data-table-view`)

	cm.HistoryAvailable, cm.HistoryCommits = true, 12
	page = renderCommunitiesPage(t, cm)
	assert.Contains(t, page, `data-panel="history" aria-selected="false" aria-controls="panel-communities"`)
	assert.Contains(t, page, `id="table-sub-history" hidden`)
}

// TestCommunitiesPage_LayeredHelp checks the layered pedagogy: a link to the
// documentation in the corner of the hero, no kicker above the verdict, one "How to read this" disclosure
// under the map's sub-heading and one at the top of the tabs card, both
// closed in the markup (the script opens them on the first visit), and a
// tooltip on every finding pill.
func TestCommunitiesPage_LayeredHelp(t *testing.T) {
	cm := twoCommunities()
	cm.Findings = []analyzer.CommunityFinding{{Kind: "cycle", Title: "A cycle", Detail: "Two communities."}}
	page := renderCommunitiesPage(t, cm)
	assert.Contains(t, page, `<a class="lead-more hero-corner-link" href="https://ast-metrics.dev/metrics/community-detection/" target="_blank" rel="noopener">How this is computed &rarr;</a>`)
	assert.Less(t, strings.Index(page, `hero-corner-link`), strings.Index(page, `<h1 class="verdict-title">`))
	assert.NotContains(t, page, `Communities read from the dependency graph`)
	assert.Equal(t, 2, strings.Count(page, `<details class="howto`))
	assert.NotContains(t, page, `<details class="howto" open`)
	assert.Contains(t, page, `am-communities-howto`)
	assert.Less(t, strings.Index(page, `Arrows point to what a community depends on`), strings.Index(page, `<details class="howto" data-howto>`))
	assert.Less(t, strings.Index(page, `<details class="howto" data-howto>`), strings.Index(page, `id="community-map-wrap"`))
	assert.Less(t, strings.Index(page, `aria-label="Reading the communities"`), strings.Index(page, `<details class="howto mb-4" data-howto>`))
	assert.Less(t, strings.Index(page, `<details class="howto mb-4" data-howto>`), strings.Index(page, `id="panel-findings"`))
	assert.Contains(t, page, `class="finding-kind finding-kind--cycle" tabindex="0" data-tip="Communities that reach each other`)
}

// TestCommunitiesPage_RowsFlagTheirIssues checks that a row carries the
// kinds of its issues, as pills after the name and as data for the quick
// filters, that the filters only offer the kinds present, and that a calm
// row gets nothing.
func TestCommunitiesPage_RowsFlagTheirIssues(t *testing.T) {
	cm := twoCommunities()
	page := renderCommunitiesPage(t, cm)
	assert.NotContains(t, page, `id="issue-filters"`)
	assert.NotContains(t, page, `class="issue-pill `)

	cm.Communities[0].Issues = []string{"cycle", "history-crossed", "spread"}
	cm.Communities[1].Issues = []string{"cycle"}
	page = renderCommunitiesPage(t, cm)
	assert.Contains(t, page, `data-id="0" data-issues="cycle history-crossed spread"`)
	assert.Contains(t, page, `data-id="1" data-issues="cycle"`)
	assert.Contains(t, page, `<span class="issue-pill finding-kind--cycle" tabindex="0" data-tip="In a cycle:`)
	assert.Contains(t, page, `>changes together</span>`)
	// two pills at most, the rest counted with the names in the tooltip
	assert.NotContains(t, page, `>spread</span>`)
	assert.Contains(t, page, `<span class="issue-pill issue-more" tabindex="0" data-tip="spread">+1</span>`)
	assert.Contains(t, page, `id="issue-filters"`)
	assert.Contains(t, page, `<button type="button" class="filter-chip is-active" data-filter="" aria-pressed="true">All <span class="n">2</span></button>`)
	assert.Contains(t, page, `data-filter="cycle" aria-pressed="false"`)
	assert.Contains(t, page, `>In a cycle <span class="n">2</span></button>`)
	assert.Contains(t, page, `>Changes together <span class="n">1</span></button>`)
	assert.Contains(t, page, `>Spread <span class="n">1</span></button>`)
	assert.NotContains(t, page, `data-filter="exposed"`)
	assert.Less(t, strings.Index(page, `id="issue-filters"`), strings.Index(page, `<table class="comm-table`))
}

// TestCommunitiesPage_KernelSitsApart checks that the shared kernel is not
// a ranked row but its own block above the filters, keeping the ids the map
// and the verdict rely on, with its facts and its detail panel.
func TestCommunitiesPage_KernelSitsApart(t *testing.T) {
	cm := twoCommunities()
	page := renderCommunitiesPage(t, cm)
	assert.NotContains(t, page, `id="kernel-block"`)

	kernel := &analyzer.Community{ID: analyzer.SharedID, Shared: true, ShortName: "Shared kernel (Money)", Size: 2, Units: []string{"Money", "Clock"}, Hubs: []string{"Money"},
		Namespaces: []analyzer.NamespaceShare{{Namespace: "app/shared", Label: "shared", Count: 2, Share: 1}},
		UsedBy:     []analyzer.CommunityLink{{ID: "0", Name: "billing", Weight: 4}, {ID: "1", Name: "users", Weight: 1}},
		Uses:       []analyzer.CommunityLink{{ID: "0", Name: "billing", Weight: 1}}}
	cm.Communities = append(cm.Communities, kernel)
	cm.Shared = kernel
	cm.SharedShare = 0.3
	cm.Labels = map[string]string{"Money": "Money", "Clock": "Clock"}
	cm.Findings = []analyzer.CommunityFinding{{Kind: "shared-leak", Title: "The kernel leaks", Communities: []string{analyzer.SharedID}}}
	page = renderCommunitiesPage(t, cm)
	assert.Contains(t, page, `<div class="kernel-block" id="kernel-block">`)
	assert.Contains(t, page, `id="community-shared" class="comm-row kernel-row" data-id="shared" tabindex="0" aria-expanded="false" aria-controls="community-shared-detail"`)
	assert.Contains(t, page, `id="community-shared-detail" class="comm-detail" data-for="shared" hidden`)
	assert.Contains(t, page, `data-lazy-members="shared"`)
	assert.Contains(t, page, `>kernel leak</span>`)
	assert.Contains(t, page, `used by 2</span> <span class="cell-sub">of 2 communities`)
	assert.Contains(t, page, `30%</span> <span class="cell-sub">of all dependencies lead here`)
	assert.Contains(t, page, `>Money</span>`)
	// out of the ranking: no table row for it, and the block comes first
	assert.NotContains(t, page, `<tr id="community-shared"`)
	assert.Equal(t, 1, strings.Count(page, `id="community-shared"`))
	assert.Less(t, strings.Index(page, `id="kernel-block"`), strings.Index(page, `id="table-sub-structure"`))
	assert.Less(t, strings.Index(page, `id="kernel-block"`), strings.Index(page, `<table class="comm-table`))
}
