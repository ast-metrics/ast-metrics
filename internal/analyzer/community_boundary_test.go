package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

func TestExposedMembersAndForeignUsesAreReadFromTheCrossingReferences(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\B`}, "B": {`App\Catalog\B`, `App\Catalog\C`}}),
		phpTightModule(`App\Catalog`, nil)...,
	)
	cm := communitiesOf(t, sources...)
	billing, catalog := communityNamed(cm, `App\Billing`), communityNamed(cm, `App\Catalog`)
	if billing == nil || catalog == nil {
		t.Fatalf("expected Billing and Catalog, got %+v", cm.Communities)
	}
	// B is used twice from Billing, C once: heaviest first
	if !reflect.DeepEqual(catalog.Exposed, []string{`App\Catalog\B`, `App\Catalog\C`}) {
		t.Errorf("Catalog exposes B then C, got %v", catalog.Exposed)
	}
	if catalog.ExposedCount != 2 || catalog.ExposedShare != 0.5 {
		t.Errorf("2 of 4 exposed, got %d (%v)", catalog.ExposedCount, catalog.ExposedShare)
	}
	if len(billing.Exposed) != 0 || billing.ExposedCount != 0 || billing.ExposedShare != 0 {
		t.Errorf("nothing of Billing is used from outside, got %v", billing.Exposed)
	}
	if !reflect.DeepEqual(billing.ForeignUses, []string{`App\Catalog\B`, `App\Catalog\C`}) || billing.ForeignUsesCount != 2 {
		t.Errorf("Billing uses B then C, got %v (%d)", billing.ForeignUses, billing.ForeignUsesCount)
	}
	if len(catalog.ForeignUses) != 0 || catalog.ForeignUsesCount != 0 {
		t.Errorf("Catalog uses nothing foreign, got %v", catalog.ForeignUses)
	}
	for _, f := range cm.Findings {
		if f.Kind == "exposed" {
			t.Errorf("a module of 4 classes is too small for the exposed finding: %s", f.Title)
		}
	}
}

func TestTheSharedKernelCountsAsAnOutsideUserButNotAsAForeignUse(t *testing.T) {
	sources := []string{
		"<?php\nnamespace App\\Shared;\n\nuse App\\Billing\\D;\n\nabstract class Model { public function __construct(D $d) {} }\n",
	}
	for _, module := range []string{`App\Billing`, `App\Catalog`, `App\Shipping`, `App\Users`} {
		sources = append(sources, phpTightModule(module, nil)...)
		sources = append(sources, "<?php\nnamespace "+module+";\n\nuse App\\Shared\\Model;\n\nfinal class Entity extends Model { public function __construct(A $a) {} }\n")
	}
	cm := communitiesOf(t, sources...)
	if cm.Shared == nil || cm.Shared.Units[0] != `App\Shared\Model` {
		t.Fatalf("expected Model as the shared kernel, got %+v", cm.Shared)
	}
	billing := communityNamed(cm, `App\Billing`)
	if billing == nil {
		t.Fatal("expected Billing")
	}
	if !reflect.DeepEqual(billing.Exposed, []string{`App\Billing\D`}) {
		t.Errorf("D is used by the kernel: exposed, got %v", billing.Exposed)
	}
	if billing.ForeignUsesCount != 0 {
		t.Errorf("using the kernel is not a foreign use, got %v", billing.ForeignUses)
	}
	if cm.Shared.ExposedCount != 0 || cm.Shared.ForeignUsesCount != 0 || cm.Shared.Confidence != 0 || cm.Shared.Border != nil {
		t.Errorf("the kernel has no surface nor border: %+v", cm.Shared)
	}
}

func TestACommunityUsedEverywhereHasNoBoundary(t *testing.T) {
	sources := phpTightModule(`App\Core`, nil)
	sources = append(sources,
		phpClass(`App\Core`, "E", `App\Core\A`, `App\Core\B`),
		phpClass(`App\Core`, "F", `App\Core\E`, `App\Core\C`),
	)
	sources = append(sources, phpTightModule(`App\Billing`, map[string][]string{
		"A": {`App\Core\A`}, "B": {`App\Core\B`}, "C": {`App\Core\C`}, "D": {`App\Core\D`},
	})...)
	cm := communitiesOf(t, sources...)
	core := communityNamed(cm, `App\Core`)
	if core == nil || core.Size != 6 {
		t.Fatalf("expected Core with 6 classes, got %+v", cm.Communities)
	}
	if core.ExposedCount != 4 {
		t.Fatalf("4 of Core's classes are used from Billing, got %v", core.Exposed)
	}
	var found *CommunityFinding
	for i := range cm.Findings {
		if cm.Findings[i].Kind == "exposed" {
			found = &cm.Findings[i]
		}
	}
	if found == nil {
		t.Fatalf("expected an exposed finding, got %+v", cm.Findings)
	}
	if found.Title != "Core has no boundary: 4 of its 6 classes are used from outside" {
		t.Errorf("unexpected title: %s", found.Title)
	}
	if !strings.Contains(found.Detail, "A (reached from 1 community), B (reached from 1 community) and C (reached from 1 community)") {
		t.Errorf("the three most used entries should be named: %s", found.Detail)
	}
	if found.Category != "coupling" || !reflect.DeepEqual(found.Communities, []string{core.ID}) || len(found.Units) != 3 {
		t.Errorf("unexpected finding: %+v", found)
	}
}

func TestTwoWellSeparatedModulesLeaveNoBorder(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\A`}}),
		phpTightModule(`App\Catalog`, nil)...,
	)
	cm := communitiesOf(t, sources...)
	if cm.Confidence != 1 {
		t.Errorf("no resolution moves anything: confidence 1, got %v", cm.Confidence)
	}
	for _, c := range cm.Communities {
		if c.Confidence != 1 || len(c.Border) != 0 {
			t.Errorf("%s should be sure of every member, got %v %v", c.Name, c.Confidence, c.Border)
		}
	}
}

func TestAClassPulledByTwoModulesSitsOnTheBorder(t *testing.T) {
	// Every class of Billing uses its namesake in Catalog: Catalog's D, the
	// least tied to its own module, follows Billing at a coarser resolution.
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{
			"A": {`App\Catalog\A`}, "B": {`App\Catalog\B`}, "C": {`App\Catalog\C`}, "D": {`App\Catalog\D`},
		}),
		phpTightModule(`App\Catalog`, nil)...,
	)
	sources = append(sources, phpClass(`App\Billing`, "E", `App\Billing\A`, `App\Billing\B`))
	cm := communitiesOf(t, sources...)
	if cm.CommunitiesCount != 2 {
		t.Fatalf("expected 2 communities, got %d", cm.CommunitiesCount)
	}
	billing, catalog := communityNamed(cm, `App\Billing`), communityNamed(cm, `App\Catalog`)
	if !reflect.DeepEqual(catalog.Border, []string{`App\Catalog\D`}) || catalog.Confidence != 0.75 {
		t.Errorf("Catalog's D should be on the border, got %v (%v)", catalog.Border, catalog.Confidence)
	}
	if len(billing.Border) != 0 || billing.Confidence != 1 {
		t.Errorf("Billing is sure of its members, got %v (%v)", billing.Border, billing.Confidence)
	}
	// 8 of the 9 placed units stay put
	if want := 8.0 / 9.0; cm.Confidence < want-1e-9 || cm.Confidence > want+1e-9 {
		t.Errorf("confidence should be unit-weighted, got %v", cm.Confidence)
	}
}

func TestExportCarriesTheSurfaceAndTheBorder(t *testing.T) {
	cm := &CommunityMetrics{
		Confidence:   0.9,
		LargestCycle: 3,
		Communities: []*Community{{
			ID: "0", Name: "Billing", ShortName: "Billing", Size: 4, Units: []string{"A", "B", "C", "D"},
			Exposed: []string{"A", "B"}, ExposedCount: 2, ExposedShare: 0.5,
			ForeignUses: []string{"X"}, ForeignUsesCount: 1,
			Border: []string{"D"}, Confidence: 0.75,
		}},
	}
	brief := ExportCommunities(cm, false)
	if brief.Confidence != 0.9 || brief.LargestCycle != 3 {
		t.Errorf("confidence and largest cycle should be exported, got %+v", brief)
	}
	c := brief.Communities[0]
	if c.ExposedCount != 2 || c.ExposedShare != 0.5 || c.ForeignUsesCount != 1 || c.Confidence != 0.75 {
		t.Errorf("counts should be exported without members, got %+v", c)
	}
	if c.Exposed != nil || c.Border != nil {
		t.Errorf("lists come with the members only, got %+v", c)
	}
	full := ExportCommunities(cm, true).Communities[0]
	if !reflect.DeepEqual(full.Exposed, []string{"A", "B"}) || !reflect.DeepEqual(full.Border, []string{"D"}) {
		t.Errorf("lists should be exported with the members, got %+v", full)
	}
}
