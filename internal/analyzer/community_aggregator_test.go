package analyzer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	phpengine "github.com/ast-metrics/ast-metrics/internal/engine/php"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// phpClass writes a PHP class in a namespace, using the given classes: each
// use is a constructor parameter, which the engine reads as a dependency.
func phpClass(namespace, name string, uses ...string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<?php\nnamespace %s;\n\n", namespace)
	for _, u := range uses {
		fmt.Fprintf(&sb, "use %s;\n", u)
	}
	fmt.Fprintf(&sb, "\nfinal class %s {\n    public function __construct(", name)
	for i, u := range uses {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s $p%d", lastSegment(u), i)
	}
	sb.WriteString(") {}\n}\n")
	return sb.String()
}

// phpModule writes a small, tightly linked module of four classes: A uses B
// and C, B uses C and D, C uses D. Extra sources let a class of the module
// use classes outside it.
func phpTightModule(namespace string, extra map[string][]string) []string {
	deps := map[string][]string{
		"A": {namespace + `\B`, namespace + `\C`},
		"B": {namespace + `\C`, namespace + `\D`},
		"C": {namespace + `\D`},
		"D": {},
	}
	sources := []string{}
	for _, name := range []string{"A", "B", "C", "D"} {
		uses := append(append([]string{}, deps[name]...), extra[name]...)
		sources = append(sources, phpClass(namespace, name, uses...))
	}
	return sources
}

func communitiesOf(t *testing.T, sources ...string) *CommunityMetrics {
	t.Helper()
	files := make([]*pb.File, 0, len(sources))
	for _, source := range sources {
		files = append(files, parsedBy(t, &phpengine.PhpRunner{}, source))
	}
	aggregated := graphOfFiles(files...)
	if aggregated.Community == nil {
		t.Fatal("expected community metrics")
	}
	return aggregated.Community
}

func communityNamed(cm *CommunityMetrics, name string) *Community {
	for _, c := range cm.Communities {
		if c.Name == name || c.ShortName == name {
			return c
		}
	}
	return nil
}

func TestTwoModulesMakeTwoCommunitiesNamedAfterTheirNamespaces(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\D`}}),
		phpTightModule(`App\Catalog`, nil)...,
	)
	cm := communitiesOf(t, sources...)

	if cm.Granularity != GranularityClass {
		t.Errorf("PHP classes should be the units, got %q", cm.Granularity)
	}
	if cm.CommunitiesCount != 2 {
		t.Fatalf("expected 2 communities, got %d: %+v", cm.CommunitiesCount, cm.Verdict)
	}
	billing, catalog := communityNamed(cm, `App\Billing`), communityNamed(cm, `App\Catalog`)
	if billing == nil || catalog == nil {
		names := []string{}
		for _, c := range cm.Communities {
			names = append(names, c.Name)
		}
		t.Fatalf("expected the communities to be named after the namespaces, got %q", names)
	}
	if !billing.Cohesive || !catalog.Cohesive {
		t.Errorf("both communities should be cohesive")
	}
	if cm.Root != "App" {
		t.Errorf("root should be App, got %q", cm.Root)
	}
	if billing.ShortName != "Billing" {
		t.Errorf("short name should strip the root, got %q", billing.ShortName)
	}
	if len(billing.Uses) != 1 || billing.Uses[0].ID != catalog.ID || billing.Uses[0].Weight != 1 {
		t.Errorf("Billing should use Catalog once, got %+v", billing.Uses)
	}
	if len(cm.Cycles) != 0 {
		t.Errorf("no cycle expected, got %v", cm.Cycles)
	}
	if got := cm.NodeToCommunity[`App\Billing\A`]; got != billing.ID {
		t.Errorf("classes should be keyed by qualified name, got %q for App\\Billing\\A", got)
	}
	if cm.Shared != nil {
		t.Errorf("no shared kernel expected in two modules, got %v", cm.Shared.Units)
	}
	if cm.CrossShare <= 0 || cm.InternalShare >= 1 {
		t.Errorf("one dependency crosses: internal=%v cross=%v", cm.InternalShare, cm.CrossShare)
	}
}

func TestMutualDependencyIsReportedAsACycleWithTheLighterDirectionToCut(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\D`}, "B": {`App\Catalog\C`}}),
		phpTightModule(`App\Catalog`, map[string][]string{"A": {`App\Billing\D`}})...,
	)
	cm := communitiesOf(t, sources...)

	if len(cm.Cycles) != 1 || len(cm.Cycles[0]) != 2 {
		t.Fatalf("expected one cycle of two communities, got %v", cm.Cycles)
	}
	if len(cm.Findings) == 0 || cm.Findings[0].Kind != "cycle" {
		t.Fatalf("the cycle should be the first finding, got %+v", cm.Findings)
	}
	if !strings.Contains(cm.Findings[0].Detail, "Catalog → Billing, is the one to cut") {
		t.Errorf("the lighter direction should be named: %s", cm.Findings[0].Detail)
	}
	backs := 0
	for _, e := range cm.Edges {
		if e.Back {
			backs++
			if e.Weight != 1 {
				t.Errorf("the back edge should be the light one, got %+v", e)
			}
		}
	}
	if backs != 1 {
		t.Errorf("exactly one back edge expected, got %d", backs)
	}
	if cm.Verdict != "Some of these communities cannot change alone." || !strings.Contains(cm.VerdictNote, "2 of the 2 communities depend on each other in 1 cycle") {
		t.Errorf("the verdict should state the cycle: %s / %s", cm.Verdict, cm.VerdictNote)
	}
}

func TestABaseClassEverybodyExtendsIsTheSharedKernel(t *testing.T) {
	sources := []string{
		"<?php\nnamespace App\\Shared;\n\nabstract class Model {}\n",
	}
	for _, module := range []string{`App\Billing`, `App\Catalog`, `App\Shipping`, `App\Users`} {
		sources = append(sources, phpTightModule(module, nil)...)
		sources = append(sources, fmt.Sprintf("<?php\nnamespace %s;\n\nuse App\\Shared\\Model;\n\nfinal class Entity extends Model { public function __construct(A $a) {} }\n", module))
	}
	cm := communitiesOf(t, sources...)

	if cm.Shared == nil {
		t.Fatalf("expected a shared kernel, got communities %v", cm.Verdict)
	}
	if !reflect.DeepEqual(cm.Shared.Units, []string{`App\Shared\Model`}) {
		t.Errorf("the base class alone should be shared, got %v", cm.Shared.Units)
	}
	if cm.CommunitiesCount != 4 {
		t.Errorf("expected the 4 modules, got %d", cm.CommunitiesCount)
	}
	if len(cm.Cycles) != 0 {
		t.Errorf("a shared base class must not create cycles, got %v", cm.Cycles)
	}
	if cm.SharedShare <= 0 {
		t.Errorf("some dependencies lead to the kernel, got %v", cm.SharedShare)
	}
	found := false
	for _, f := range cm.Findings {
		if f.Kind == "shared" {
			found = true
		}
	}
	if !found {
		t.Errorf("the shared kernel should be a finding: %+v", cm.Findings)
	}
	if cm.Verdict != "This code can be split and worked on in pieces." || !strings.HasSuffix(cm.VerdictNote, ", around a shared kernel of 1 class.") || !strings.HasPrefix(cm.VerdictNote, "4 communities, no cycle, ") {
		t.Errorf("verdict should mention the kernel: %s / %s", cm.Verdict, cm.VerdictNote)
	}
}

func TestAHeavyKernelIsCalledTheCentreOfGravity(t *testing.T) {
	// Four modules leaning on a kernel of three classes, each used from two
	// classes of every module: the kernel draws more than a quarter of the
	// dependencies.
	sources := []string{
		"<?php\nnamespace App\\Shared;\n\nabstract class Model {}\n",
		"<?php\nnamespace App\\Shared;\n\nfinal class Clock {}\n",
		"<?php\nnamespace App\\Shared;\n\nfinal class Id {}\n",
	}
	for _, module := range []string{`App\Billing`, `App\Catalog`, `App\Shipping`, `App\Users`} {
		sources = append(sources, phpTightModule(module, map[string][]string{
			"A": {`App\Shared\Model`, `App\Shared\Clock`, `App\Shared\Id`},
			"B": {`App\Shared\Model`, `App\Shared\Clock`, `App\Shared\Id`},
		})...)
	}
	cm := communitiesOf(t, sources...)
	if cm.Shared == nil || cm.Shared.Size != 3 {
		t.Fatalf("expected a kernel of 3 classes, got %s", cm.Verdict)
	}
	if !kernelIsCentreOfGravity(cm) {
		t.Fatalf("a kernel drawing %.0f%% of the dependencies is the centre of gravity", cm.SharedShare*100)
	}
	shared := findingsOfKind(cm, "shared")
	if len(shared) != 1 {
		t.Fatalf("expected the shared finding, got %+v", cm.Findings)
	}
	if !strings.Contains(shared[0].Detail, "That is more than a kernel:") || !strings.Contains(shared[0].Detail, "lead to these 3 classes, which makes them the centre of gravity of the code. Anything changing there reaches 4 communities.") {
		t.Errorf("a heavy kernel is said as such: %s", shared[0].Detail)
	}
	if strings.Contains(shared[0].Detail, "This is expected of a kernel") {
		t.Errorf("a heavy kernel is not expected: %s", shared[0].Detail)
	}
	if cm.Verdict != "Anything changing in the shared kernel reaches most of the code." || !strings.HasSuffix(cm.VerdictNote, "% of the dependencies lead to its 3 classes, used by 4 of the 4 communities.") {
		t.Errorf("without a cycle, the verdict names the heavy kernel: %s / %s", cm.Verdict, cm.VerdictNote)
	}
}

func TestASmallKernelIsExpected(t *testing.T) {
	sources := []string{
		"<?php\nnamespace App\\Shared;\n\nabstract class Model {}\n",
	}
	for _, module := range []string{`App\Billing`, `App\Catalog`, `App\Shipping`, `App\Users`} {
		sources = append(sources, phpTightModule(module, nil)...)
		sources = append(sources, fmt.Sprintf("<?php\nnamespace %s;\n\nuse App\\Shared\\Model;\n\nfinal class Entity extends Model { public function __construct(A $a) {} }\n", module))
	}
	cm := communitiesOf(t, sources...)
	shared := findingsOfKind(cm, "shared")
	if len(shared) != 1 || !strings.Contains(shared[0].Detail, "This is expected of a kernel") {
		t.Errorf("a small kernel is expected: %+v", shared)
	}
	if strings.Contains(cm.Verdict, "shared kernel reaches") {
		t.Errorf("a small kernel is no centre of gravity: %s", cm.Verdict)
	}
}

func TestAFeatureCuttingAcrossLayersIsNamedAfterItsWord(t *testing.T) {
	// A layered application: the classes about invoices sit in three
	// namespaces and depend on each other.
	sources := []string{
		phpClass(`App\Controller`, "InvoiceController", `App\Service\InvoiceService`, `App\Form\InvoiceType`),
		phpClass(`App\Service`, "InvoiceService", `App\Repository\InvoiceRepository`, `App\Entity\Invoice`),
		phpClass(`App\Repository`, "InvoiceRepository", `App\Entity\Invoice`),
		phpClass(`App\Form`, "InvoiceType", `App\Entity\Invoice`),
		phpClass(`App\Entity`, "Invoice"),
		phpClass(`App\Controller`, "UserController", `App\Service\UserService`),
		phpClass(`App\Service`, "UserService", `App\Repository\UserRepository`, `App\Entity\User`),
		phpClass(`App\Repository`, "UserRepository", `App\Entity\User`),
		phpClass(`App\Entity`, "User"),
	}
	cm := communitiesOf(t, sources...)

	if cm.CommunitiesCount != 2 {
		t.Fatalf("expected the two features, got %d (%s)", cm.CommunitiesCount, cm.Verdict)
	}
	if communityNamed(cm, "Invoice") == nil || communityNamed(cm, "User") == nil {
		names := []string{}
		for _, c := range cm.Communities {
			names = append(names, c.Name)
		}
		t.Errorf("expected Invoice and User, got %q", names)
	}
	if cm.CohesiveCount != 0 {
		t.Errorf("features across layers are not cohesive, got %d", cm.CohesiveCount)
	}
}

func TestPairsLinkedToNothingElseStandApart(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, nil),
		phpClass(`App\Tools`, "Lonely", `App\Tools\Buddy`),
		phpClass(`App\Tools`, "Buddy"),
	)
	cm := communitiesOf(t, sources...)

	if cm.CommunitiesCount != 1 {
		t.Errorf("a pair is not a community, got %d", cm.CommunitiesCount)
	}
	if cm.IsolatedUnits < 2 {
		t.Errorf("the pair should be counted apart, got %d", cm.IsolatedUnits)
	}
}

func TestFilesUnderATestDirectoryTakeNoPart(t *testing.T) {
	files := []*pb.File{}
	for _, source := range phpTightModule(`App\Billing`, nil) {
		files = append(files, parsedBy(t, &phpengine.PhpRunner{}, source))
	}
	fixture := parsedBy(t, &phpengine.PhpRunner{}, phpClass(`App\Billing`, "Dummy", `App\Billing\A`))
	fixture.Path = "/project/tests/Fixtures/Dummy.php"
	files = append(files, fixture)

	cm := graphOfFiles(files...).Community
	if _, placed := cm.NodeToCommunity[`App\Billing\Dummy`]; placed {
		t.Errorf("a fixture must not be a unit")
	}
	if cm.CommunitiesCount != 1 || cm.Communities[0].Size != 4 {
		t.Errorf("expected the 4 classes of the module, got %+v", cm.Communities)
	}
}

func TestSplitCamel(t *testing.T) {
	cases := map[string][]string{
		"TagArrayToStringTransformer": {"Tag", "Array", "To", "String", "Transformer"},
		"HTTPClient":                  {"HTTP", "Client"},
		"user_repository":             {"User", "Repository"},
		"Invoice":                     {"Invoice"},
	}
	for name, expected := range cases {
		if got := splitCamel(name); !reflect.DeepEqual(got, expected) {
			t.Errorf("splitCamel(%q) = %v, expected %v", name, got, expected)
		}
	}
}

func TestStripRootHandlesAOneLevelRoot(t *testing.T) {
	if got := stripRoot(`App\Billing + App\Catalog`, "App"); got != "Billing + Catalog" {
		t.Errorf("got %q", got)
	}
	if got := stripRoot("internal/engine/php", "internal"); got != "engine/php" {
		t.Errorf("got %q", got)
	}
	if got := stripRoot("Other", "App"); got != "Other" {
		t.Errorf("got %q", got)
	}
}

func TestNameOfCommunityPrefersTheNamespaceThenTheWordThenTheHub(t *testing.T) {
	cases := []struct {
		name   string
		shares []NamespaceShare
		units  []string
		hubs   []string
		want   string
	}{
		{
			name:   "one namespace holding half of it",
			shares: []NamespaceShare{{Namespace: `App\Billing`, Share: 0.5}, {Namespace: `App\Catalog`, Share: 0.3}, {Namespace: `App\Users`, Share: 0.2}},
			units:  []string{"Invoice", "InvoiceLine", "Product", "User"},
			hubs:   []string{"Invoice"},
			want:   `App\Billing`,
		},
		{
			name:   "a word the classes share when the namespaces are layers",
			shares: []NamespaceShare{{Namespace: `App\Controller`, Share: 0.4}, {Namespace: `App\Entity`, Share: 0.4}, {Namespace: `App\Form`, Share: 0.2}},
			units:  []string{"InvoiceController", "Invoice", "InvoiceType", "InvoiceLine", "Address"},
			hubs:   []string{"Invoice", "InvoiceController"},
			want:   "Invoice",
		},
		{
			name:   "the hub when the classes share no word",
			shares: []NamespaceShare{{Namespace: `App\Component\Gamification`, Share: 0.4}, {Namespace: `App\Component\Admin`, Share: 0.35}, {Namespace: `App\Component\Log`, Share: 0.25}},
			units:  []string{"GithubOrganization", "Badge", "AdminPanel", "LogEventVisit", "Reward"},
			hubs:   []string{"GithubOrganization", "LogEventVisit"},
			want:   "GithubOrganization",
		},
		{
			name:   "two namespaces at most when there is no hub",
			shares: []NamespaceShare{{Namespace: `App\Gamification`, Share: 0.4}, {Namespace: `App\Admin`, Share: 0.35}, {Namespace: `App\Log`, Share: 0.25}},
			units:  []string{"GithubOrganization", "Badge", "AdminPanel", "LogEventVisit", "Reward"},
			want:   `App\Gamification + App\Admin`,
		},
		{
			name:   "a layer word takes the namespace holding most of the classes beside it",
			shares: []NamespaceShare{{Namespace: `App\Component\Scm`, Share: 0.4}, {Namespace: `App\Component\Github`, Share: 0.35}, {Namespace: `App\Message`, Share: 0.25}},
			units:  []string{"GitRepoRepository", "OrganizationRepository", "PullRequestRepository", "CommitRepository", "SyncMessage"},
			hubs:   []string{"GitRepoRepository"},
			want:   `Repository · App\Component\Scm`,
		},
		{
			name:   "code outside any namespace is named by its word",
			shares: []NamespaceShare{{Namespace: "", Share: 1}},
			units:  []string{"UserRepository", "UserService", "Mailer"},
			hubs:   []string{"UserService"},
			want:   "User",
		},
		{
			name:   "code outside any namespace with no word is named by its hub",
			shares: []NamespaceShare{{Namespace: "", Share: 1}},
			units:  []string{"Repository", "Service", "Mailer"},
			hubs:   []string{"Service"},
			want:   "Service",
		},
	}
	for _, c := range cases {
		if got := nameOfCommunity(c.shares, c.units, c.hubs); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDisambiguateNamesJoinsTheSecondHubToACommunityNamedAfterItsHub(t *testing.T) {
	communities := []*Community{
		{ID: "0", Name: "GithubOrganization", Hint: "GithubOrganization, Badge, Reward"},
		{ID: "1", Name: "GithubOrganization", Hint: "GithubOrganization, LogEventVisit"},
		{ID: "2", Name: `App\Billing`, Hint: "Invoice, InvoiceLine"},
		{ID: "3", Name: `App\Billing`, Hint: "Payment, Refund"},
		{ID: "4", Name: `App\Billing`, Hint: ""},
		{ID: "5", Name: "Shared", Shared: true, Hint: "Model"},
		{ID: "6", Name: "Shared", Hint: "Model"},
		{ID: "7", Name: `Repository · App\Scm`, Hint: "GitRepoRepository, CommitRepository"},
		{ID: "8", Name: `Repository · App\Scm`, Hint: "OrganizationRepository"},
	}
	disambiguateNames(communities)
	want := []string{"GithubOrganization", "GithubOrganization · LogEventVisit", `App\Billing`, `App\Billing (Payment)`, `App\Billing #4`, "Shared", "Shared", `Repository · App\Scm`, `Repository · App\Scm (OrganizationRepository)`}
	for i, c := range communities {
		if c.Name != want[i] {
			t.Errorf("community %s: got %q, want %q", c.ID, c.Name, want[i])
		}
	}
}

func TestTopLevelCodeAmongClassesIsNamedByItsPackage(t *testing.T) {
	// A project of classes where three packages of top-level code depend on
	// each other, the way buildUnitGraph files a Go package among PHP
	// classes: the unit is the package path, filed under its parent.
	packages := []string{
		"github.com/acme/tool/internal/engine/dependency",
		"github.com/acme/tool/internal/analyzer/graph",
		"github.com/acme/tool/internal/report/html",
	}
	g := &unitGraph{
		Namespace:   map[string]string{},
		IsClass:     map[string]bool{},
		IsInterface: map[string]bool{},
		IsFile:      map[string]bool{},
		Out:         map[string]map[string]int{},
		In:          map[string]map[string]int{},
		Externals:   map[string]map[string]int{},
		Granularity: GranularityClass,
		Language:    map[string]string{"Golang": GranularityNamespace},
	}
	link := func(a, b string, w int) {
		if g.Out[a] == nil {
			g.Out[a] = map[string]int{}
		}
		if g.In[b] == nil {
			g.In[b] = map[string]int{}
		}
		g.Out[a][b] = w
		g.In[b][a] = w
	}
	for _, p := range packages {
		g.Namespace[p] = parentNamespace(p)
	}
	link(packages[0], packages[1], 3)
	link(packages[1], packages[2], 2)
	link(packages[0], packages[2], 1)
	g.Units = append([]string{}, packages...)
	g.Total = 3

	cm := computeCommunities(g, nil)
	if cm.CommunitiesCount != 1 {
		t.Fatalf("expected one community, got %d", cm.CommunitiesCount)
	}
	if got := cm.Labels[packages[0]]; got != "engine/dependency (top-level code)" {
		t.Errorf("label should strip the root and mark the code, got %q", got)
	}
	name := cm.Communities[0].Name
	if strings.Contains(name, "Code)") || strings.Contains(name, "Level") {
		t.Errorf("the mark of top-level code must not be split into a name: %q", name)
	}
	if !strings.HasSuffix(name, " (top-level code)") {
		t.Errorf("the community should be named after the package at its heart, got %q", name)
	}
	if token := dominantToken(labelsOf(packages, cm.Labels)); token != "" {
		t.Errorf("packages sharing no word must yield no token, got %q", token)
	}
}

func TestVerdictNamesABlobWhenOneCycleHoldsMostCommunities(t *testing.T) {
	blob, cycles := "A change in the middle of this code reaches everywhere.", "Some of these communities cannot change alone."
	cases := []struct {
		count    int
		cycles   [][]string
		verdict  string
		wantNote string
	}{
		{5, [][]string{{"0", "1", "2", "3"}}, blob, "4 of the 5 communities depend on each other in 1 cycle."},
		{10, [][]string{{"0", "1", "2", "3"}}, blob, "4 of the 10 communities depend on each other in 1 cycle."},
		{12, [][]string{{"0", "1", "2", "3"}}, cycles, "4 of the 12 communities depend on each other in 1 cycle."},
		{5, [][]string{{"0", "1"}, {"2", "3", "4"}}, cycles, "5 of the 5 communities depend on each other in 2 cycles."},
		{4, [][]string{{"0", "1", "2"}}, cycles, "3 of the 4 communities depend on each other in 1 cycle."},
	}
	for _, c := range cases {
		cm := &CommunityMetrics{Granularity: GranularityClass, CommunitiesCount: c.count, Cycles: c.cycles}
		if verdict, note := verdictOf(cm); verdict != c.verdict || note != c.wantNote {
			t.Errorf("%d communities, cycles %v: got %q / %q, want %q / %q", c.count, c.cycles, verdict, note, c.verdict, c.wantNote)
		}
	}
	// with a kernel, the note adds the share of the dependencies leading to it
	cm := &CommunityMetrics{Granularity: GranularityClass, CommunitiesCount: 5, Cycles: [][]string{{"0", "1"}}, Shared: &Community{ID: SharedID, Shared: true, Size: 3}, SharedShare: 0.31}
	if _, note := verdictOf(cm); note != "2 of the 5 communities depend on each other in 1 cycle, and 31% of the dependencies lead to 3 shared classes." {
		t.Errorf("unexpected note: %s", note)
	}
	// in namespace granularity the kernel holds packages
	cm.Granularity = GranularityNamespace
	if _, note := verdictOf(cm); !strings.HasSuffix(note, "3 shared packages.") {
		t.Errorf("unexpected note: %s", note)
	}
}

func TestVerdictReadsTheHistoryThenTheLayers(t *testing.T) {
	// a history working across the boundaries: over 20 commits, under 40% agree
	cm := &CommunityMetrics{Granularity: GranularityClass, CommunitiesCount: 4, CohesiveCount: 4, HistoryAvailable: true, HistoryCommits: 25, HistoryAgreement: 0.3, CrossShare: 0.1}
	if verdict, note := verdictOf(cm); verdict != "People work across these boundaries, not inside them." || note != "Only 30% of the commits stay in a single community; the code forms 4 of them." {
		t.Errorf("unexpected verdict: %s / %s", verdict, note)
	}
	// too few commits to conclude: the layers, then the clean case
	cm.HistoryCommits = 10
	cm.CohesiveCount = 1
	if verdict, note := verdictOf(cm); verdict != "The folders describe layers; the code lives as features." || note != "3 of the 4 communities cut across the namespaces: what changes together sits in several places." {
		t.Errorf("unexpected verdict: %s / %s", verdict, note)
	}
	cm.CohesiveCount = 3
	if verdict, note := verdictOf(cm); verdict != "This code can be split and worked on in pieces." || note != "4 communities, no cycle, 10% of the dependencies cross between them." {
		t.Errorf("unexpected verdict: %s / %s", verdict, note)
	}
	// a cycle beats the history
	cm.HistoryCommits, cm.Cycles = 25, [][]string{{"0", "1"}}
	if verdict, _ := verdictOf(cm); verdict != "Some of these communities cannot change alone." {
		t.Errorf("unexpected verdict: %s", verdict)
	}
}

func TestLargestCycleIsMeasured(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\D`}, "B": {`App\Catalog\C`}}),
		phpTightModule(`App\Catalog`, map[string][]string{"A": {`App\Billing\D`}})...,
	)
	cm := communitiesOf(t, sources...)
	if cm.LargestCycle != 2 {
		t.Errorf("expected a cycle of 2, got %d", cm.LargestCycle)
	}
}
