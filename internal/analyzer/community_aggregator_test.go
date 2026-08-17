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
	if !strings.Contains(cm.VerdictNote, "cycle") {
		t.Errorf("the verdict should mention the cycle: %s", cm.VerdictNote)
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
	if !strings.Contains(cm.Verdict, "shared kernel of 1 class") {
		t.Errorf("verdict should mention the kernel: %s", cm.Verdict)
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
