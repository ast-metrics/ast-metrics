package analyzer

import (
	"fmt"
	"strings"
	"testing"

	phpengine "github.com/ast-metrics/ast-metrics/internal/engine/php"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// historyProject parses two tight modules, Billing and Catalog, and a wide
// star-shaped module of 24 classes, and returns the files keyed by the class
// they declare, so that a test can hand each of them commits.
func historyProject(t *testing.T) map[string]*pb.File {
	t.Helper()
	sources := map[string]string{}
	for _, module := range []string{`App\Billing`, `App\Catalog`} {
		for i, source := range phpTightModule(module, nil) {
			sources[module+`\`+[]string{"A", "B", "C", "D"}[i]] = source
		}
	}
	sources[`App\Wide\Core`] = phpClass(`App\Wide`, "Core")
	for i := 1; i <= 23; i++ {
		name := fmt.Sprintf("Leaf%d", i)
		sources[`App\Wide\`+name] = phpClass(`App\Wide`, name, `App\Wide\Core`)
	}
	files := map[string]*pb.File{}
	for name, source := range sources {
		files[name] = parsedBy(t, &phpengine.PhpRunner{}, source)
	}
	return files
}

// commitTouching gives the same commit hash to every file named.
func commitTouching(files map[string]*pb.File, hash string, names ...string) {
	for _, name := range names {
		file := files[name]
		if file.Commits == nil {
			file.Commits = &pb.Commits{CountCommiters: 1}
		}
		file.Commits.Count++
		file.Commits.Commits = append(file.Commits.Commits, &pb.Commit{Hash: hash, Author: "dev"})
	}
}

func historyCommunitiesOf(t *testing.T, files map[string]*pb.File) *CommunityMetrics {
	t.Helper()
	list := make([]*pb.File, 0, len(files))
	for _, file := range files {
		list = append(list, file)
	}
	aggregated := graphOfFiles(list...)
	if aggregated.Community == nil {
		t.Fatal("expected community metrics")
	}
	cm := aggregated.Community
	if cm.CommunitiesCount != 3 {
		t.Fatalf("expected the 3 modules, got %d: %s", cm.CommunitiesCount, cm.Verdict)
	}
	return cm
}

func findingsOfKind(cm *CommunityMetrics, kind string) []CommunityFinding {
	out := []CommunityFinding{}
	for _, f := range cm.Findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestHistoryCrossesTheCommunitiesWithTheCommits(t *testing.T) {
	files := historyProject(t)
	// Six commits inside Billing, five crossing from Billing to Catalog, two
	// touching a single file of Catalog, and one bulk change over every file.
	for i := 1; i <= 6; i++ {
		commitTouching(files, fmt.Sprintf("b%d", i), `App\Billing\A`, `App\Billing\B`)
	}
	for i := 1; i <= 5; i++ {
		commitTouching(files, fmt.Sprintf("x%d", i), `App\Billing\A`, `App\Catalog\D`)
	}
	commitTouching(files, "c1", `App\Catalog\C`)
	commitTouching(files, "c2", `App\Catalog\C`)
	all := make([]string, 0, len(files))
	for name := range files {
		all = append(all, name)
	}
	commitTouching(files, "big", all...)
	cm := historyCommunitiesOf(t, files)

	if !cm.HistoryAvailable {
		t.Fatal("history should be available")
	}
	if cm.HistoryCommits != 13 {
		t.Errorf("13 commits once the bulk change is ignored, got %d", cm.HistoryCommits)
	}
	if got, want := cm.HistoryAgreement, 6.0/11.0; got < want-0.001 || got > want+0.001 {
		t.Errorf("6 of the 11 multi-file commits stay inside a community, got agreement %v", got)
	}
	billing, catalog, wide := communityNamed(cm, "Billing"), communityNamed(cm, "Catalog"), communityNamed(cm, "Wide")
	if billing == nil || catalog == nil || wide == nil {
		t.Fatalf("expected Billing, Catalog and Wide, got %s", cm.Verdict)
	}
	if billing.HistoryCommits != 11 || catalog.HistoryCommits != 7 || wide.HistoryCommits != 0 {
		t.Errorf("commits per community: Billing %d, Catalog %d, Wide %d", billing.HistoryCommits, catalog.HistoryCommits, wide.HistoryCommits)
	}
	if got, want := billing.HistoryCohesion, 6.0/11.0; got < want-0.001 || got > want+0.001 {
		t.Errorf("Billing cohesion should be 6/11, got %v", got)
	}
	if catalog.HistoryCohesion != 0 {
		t.Errorf("no commit stays inside Catalog, got cohesion %v", catalog.HistoryCohesion)
	}
	if len(catalog.ChangesWith) != 1 || catalog.ChangesWith[0].ID != billing.ID || catalog.ChangesWith[0].Commits != 5 {
		t.Fatalf("Catalog should change with Billing 5 times, got %+v", catalog.ChangesWith)
	}
	if got, want := catalog.ChangesWith[0].Share, 5.0/7.0; got < want-0.001 || got > want+0.001 {
		t.Errorf("share should be 5/7, got %v", got)
	}
	if len(billing.ChangesWith) != 1 || billing.ChangesWith[0].ID != catalog.ID || billing.ChangesWith[0].Commits != 5 {
		t.Errorf("Billing should change with Catalog 5 times, got %+v", billing.ChangesWith)
	}
	crossed := findingsOfKind(cm, "history-crossed")
	if len(crossed) != 1 {
		t.Fatalf("expected one history-crossed finding, got %+v", cm.Findings)
	}
	if crossed[0].Title != "Catalog and Billing change together" {
		t.Errorf("the less active community comes first: %s", crossed[0].Title)
	}
	if !strings.Contains(crossed[0].Detail, "5 of the 7 commits touching Catalog also touch Billing") || !strings.Contains(crossed[0].Detail, "(5 times)") {
		t.Errorf("the detail should give the figures and the file pair: %s", crossed[0].Detail)
	}
	if crossed[0].Category != "cohesion" || len(crossed[0].Communities) != 2 || crossed[0].Communities[0] != catalog.ID {
		t.Errorf("finding should be filed under cohesion and name both communities: %+v", crossed[0])
	}
	if cm.Findings[0].Kind != "history-crossed" {
		t.Errorf("without cycles the history should open the findings, got %s", cm.Findings[0].Kind)
	}
	if len(findingsOfKind(cm, "history-loose")) != 0 {
		t.Errorf("no community is loose here: %+v", findingsOfKind(cm, "history-loose"))
	}
	export := ExportCommunities(cm, false)
	if !export.HistoryAvailable || export.HistoryCommits != 13 {
		t.Errorf("export should carry the history: %+v", export)
	}
}

func TestHistoryFindingsStayQuietUnderTheThresholds(t *testing.T) {
	files := historyProject(t)
	for i := 1; i <= 4; i++ {
		commitTouching(files, fmt.Sprintf("x%d", i), `App\Billing\A`, `App\Catalog\D`)
	}
	cm := historyCommunitiesOf(t, files)

	billing := communityNamed(cm, "Billing")
	if len(billing.ChangesWith) != 1 || billing.ChangesWith[0].Commits != 4 {
		t.Errorf("the co-change is still listed: %+v", billing.ChangesWith)
	}
	if billing.HistoryCohesion != 0 {
		t.Errorf("cohesion means nothing under 5 multi-file commits, got %v", billing.HistoryCohesion)
	}
	if n := len(findingsOfKind(cm, "history-crossed")); n != 0 {
		t.Errorf("4 shared commits are under the threshold, got %d findings", n)
	}
}

func TestNoHistoryLeavesTheMetricsEmpty(t *testing.T) {
	cm := historyCommunitiesOf(t, historyProject(t))
	if cm.HistoryAvailable || cm.HistoryCommits != 0 || cm.HistoryAgreement != 0 {
		t.Errorf("no git data, no history: %+v", cm)
	}
	if n := len(findingsOfKind(cm, "history-crossed")) + len(findingsOfKind(cm, "history-loose")); n != 0 {
		t.Errorf("no history finding without history, got %d", n)
	}
}

func TestACommunityWhoseCommitsAlwaysLeaveItIsLoose(t *testing.T) {
	files := historyProject(t)
	// Twenty commits each touch a leaf of Wide and a class of Billing: Wide
	// is worked on class by class, never as a whole.
	for i := 1; i <= 20; i++ {
		commitTouching(files, fmt.Sprintf("w%d", i), fmt.Sprintf(`App\Wide\Leaf%d`, i), `App\Billing\A`)
	}
	cm := historyCommunitiesOf(t, files)

	wide := communityNamed(cm, "Wide")
	if wide.HistoryCommits != 20 || wide.HistoryCohesion != 0 {
		t.Fatalf("Wide: %d commits, cohesion %v", wide.HistoryCommits, wide.HistoryCohesion)
	}
	loose := findingsOfKind(cm, "history-loose")
	if len(loose) != 1 || loose[0].Title != "Wide never changes as a whole" {
		t.Fatalf("expected Wide to be reported as loose, got %+v", loose)
	}
	if !strings.Contains(loose[0].Detail, "Its 24 classes were touched by 20 commits this year, but only 0% of the commits") {
		t.Errorf("detail: %s", loose[0].Detail)
	}
	if len(findingsOfKind(cm, "history-crossed")) != 1 {
		t.Errorf("Wide and Billing also change together: %+v", cm.Findings)
	}
}
