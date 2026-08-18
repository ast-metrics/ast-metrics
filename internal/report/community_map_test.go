package report

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

func sampleCommunities() *analyzer.CommunityMetrics {
	billing := &analyzer.Community{ID: "0", Name: `App\Billing`, ShortName: "Billing", Size: 12, Cohesive: true,
		Namespaces: []analyzer.NamespaceShare{{Namespace: `App\Billing`, Label: "Billing", Count: 12, Share: 1}}}
	catalog := &analyzer.Community{ID: "1", Name: `App\Catalog`, ShortName: "Catalog", Size: 8, Cohesive: true}
	users := &analyzer.Community{ID: "2", Name: `App\Users`, ShortName: "Users", Size: 5, Cohesive: true}
	shared := &analyzer.Community{ID: analyzer.SharedID, Shared: true, Name: `Shared kernel (App\Shared)`, ShortName: "Shared kernel (Shared)", Size: 3, Hint: "Model, Uuid",
		UsedBy: []analyzer.CommunityLink{{ID: "0"}, {ID: "1"}, {ID: "2"}}}
	return &analyzer.CommunityMetrics{
		Granularity:      analyzer.GranularityClass,
		Communities:      []*analyzer.Community{billing, catalog, users, shared},
		CommunitiesCount: 3,
		Shared:           shared,
		SharedShare:      0.2,
		Edges: []analyzer.CommunityEdge{
			{From: "0", To: "1", Weight: 6, InCycle: true},
			{From: "1", To: "0", Weight: 1, InCycle: true, Back: true},
			{From: "0", To: "2", Weight: 3},
			{From: "0", To: analyzer.SharedID, Weight: 9, Shared: true},
		},
		Cycles: [][]string{{"0", "1"}},
	}
}

func TestCommunityMapDrawsBoxesEdgesAndTheKernel(t *testing.T) {
	svg := communityMapSVG(sampleCommunities())

	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("expected an inline svg, got %.60q", svg)
	}
	for _, expected := range []string{
		`data-id="0"`, `data-id="1"`, `data-id="2"`,
		">Billing<", ">Catalog<", ">Users<",
		`data-from="0" data-to="1"`,
		`class="cm-edge cm-edge--cycle" data-from="1" data-to="0"`,
		"Shared kernel (Shared)",
		"12 classes",
		// what the "after the cuts" view says: one back edge of one
		// reference, and two layers once it is gone
		`data-cuts="1" data-cut-refs="1" data-layers="2"`,
	} {
		if !strings.Contains(svg, expected) {
			t.Errorf("expected the map to contain %q", expected)
		}
	}
	// the dependency on the kernel is not an arrow
	if strings.Contains(svg, `data-to="shared"`) {
		t.Errorf("edges to the shared kernel are not drawn")
	}
	if strings.Count(svg, `stroke-dasharray="6 4"`) != 1 {
		t.Errorf("exactly the back edge should be dashed")
	}
}

func TestCommunityMapPutsWhatNobodyUsesOnTop(t *testing.T) {
	svg := communityMapSVG(sampleCommunities())
	// Billing depends on Catalog and Users: it comes first, above them.
	billing := strings.Index(svg, `data-id="0"`)
	catalog := strings.Index(svg, `data-id="1"`)
	yOf := func(from int) float64 {
		rest := svg[from:]
		i := strings.Index(rest, ` y="`)
		y, _ := strconv.ParseFloat(rest[i+4:i+4+strings.Index(rest[i+4:], `"`)], 64)
		return y
	}
	if yOf(billing) >= yOf(catalog) {
		t.Errorf("Billing (y=%v) should sit above Catalog (y=%v)", yOf(billing), yOf(catalog))
	}
}

func TestCommunityMapIsEmptyWithoutCommunities(t *testing.T) {
	if svg := communityMapSVG(&analyzer.CommunityMetrics{}); svg != "" {
		t.Errorf("expected nothing, got %q", svg)
	}
	if svg := communityMapSVG(nil); svg != "" {
		t.Errorf("expected nothing for nil")
	}
}

func TestFitLabelKeepsBothEnds(t *testing.T) {
	if got := fitLabel("Backoffice\\Courses + Shared\\Infrastructure", 20); got != "Backoffice\\C…ructure" {
		t.Errorf("got %q", got)
	}
	if got := fitLabel("short", 20); got != "short" {
		t.Errorf("got %q", got)
	}
}

func TestCommunityBlocksMapDrawsTheBlocksWithTheirMembers(t *testing.T) {
	cm := sampleCommunities()
	if svg := communityBlocksSVG(cm); svg != "" {
		t.Fatalf("no blocks, no zoomed-out map, got %.60q", svg)
	}
	cm.Blocks = []analyzer.CommunityBlock{
		{ID: "b0", Name: "Billing + 1", Communities: []string{"0", "1"}, Size: 20},
		{ID: "b1", Name: "Users", Communities: []string{"2"}, Size: 5},
	}
	cm.BlockEdges = []analyzer.CommunityEdge{
		{From: "b0", To: "b1", Weight: 3},
		{From: "b0", To: analyzer.SharedID, Weight: 9, Shared: true},
	}
	svg := communityBlocksSVG(cm)
	for _, expected := range []string{
		`class="cm-box cm-block" role="button" tabindex="0" data-id="b0" data-communities="0 1"`,
		`data-id="b1" data-communities="2"`,
		">Billing + 1<", ">Users<",
		"20 classes in 2 communities",
		`class="cm-member"`, ">Billing<", ">Catalog<",
		`data-from="b0" data-to="b1"`,
		"Shared kernel (Shared)",
		`data-cuts="0" data-cut-refs="0" data-layers="2"`,
		"community-map--blocks",
	} {
		if !strings.Contains(svg, expected) {
			t.Errorf("expected the zoomed-out map to contain %q", expected)
		}
	}
	// a block of one community lists no member: its title is that community
	if strings.Count(svg, `class="cm-member"`) != 2 {
		t.Errorf("only the members of the block of two should be listed")
	}
	if strings.Contains(svg, `href="#community-b0"`) {
		t.Errorf("a block leads to no community row")
	}
}
