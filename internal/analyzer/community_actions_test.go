package analyzer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func actionsOfKind(cm *CommunityMetrics, kind string) []CommunityAction {
	out := []CommunityAction{}
	for _, a := range cm.Actions {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

func TestABackEdgeBecomesACutAction(t *testing.T) {
	sources := append(
		phpTightModule(`App\Billing`, map[string][]string{"A": {`App\Catalog\D`}, "B": {`App\Catalog\C`}}),
		phpTightModule(`App\Catalog`, map[string][]string{"A": {`App\Billing\D`}})...,
	)
	cm := communitiesOf(t, sources...)
	billing, catalog := communityNamed(cm, "Billing"), communityNamed(cm, "Catalog")
	if billing == nil || catalog == nil {
		t.Fatalf("expected Billing and Catalog, got %s", cm.Verdict)
	}
	cuts := actionsOfKind(cm, "cut")
	if len(cuts) != 1 {
		t.Fatalf("expected one cut, got %+v", cm.Actions)
	}
	cut := cuts[0]
	if cut.Title != "Cut Catalog → Billing" {
		t.Errorf("the light direction is the one to cut: %s", cut.Title)
	}
	if cut.Detail != "It closes a cycle: 1 reference, carried by A → D (1)." {
		t.Errorf("unexpected detail: %s", cut.Detail)
	}
	if cut.Effort != "1 reference" {
		t.Errorf("unexpected effort: %s", cut.Effort)
	}
	if cut.Gain != "Frees 2 communities from the cycle" {
		t.Errorf("cutting the only back edge frees both: %s", cut.Gain)
	}
	if !reflect.DeepEqual(cut.Communities, []string{catalog.ID, billing.ID}) || !reflect.DeepEqual(cut.Units, []string{`App\Catalog\A`}) {
		t.Errorf("the action should name the communities and the class carrying it: %+v", cut)
	}
	if cm.Actions[0].Kind != "cut" {
		t.Errorf("the cut comes first: %+v", cm.Actions)
	}
	export := ExportCommunities(cm, false)
	if len(export.Actions) != 1 || export.Actions[0].Kind != "cut" || export.Actions[0].Effort != "1 reference" || export.Actions[0].Gain != "Frees 2 communities from the cycle" {
		t.Errorf("the export should carry the actions: %+v", export.Actions)
	}
}

func TestACoChangingPairBecomesAMoveAction(t *testing.T) {
	files := historyProject(t)
	for i := 1; i <= 6; i++ {
		commitTouching(files, fmt.Sprintf("b%d", i), `App\Billing\A`, `App\Billing\B`)
	}
	for i := 1; i <= 5; i++ {
		commitTouching(files, fmt.Sprintf("x%d", i), `App\Billing\A`, `App\Catalog\D`)
	}
	commitTouching(files, "c1", `App\Catalog\C`)
	commitTouching(files, "c2", `App\Catalog\C`)
	cm := historyCommunitiesOf(t, files)
	billing, catalog := communityNamed(cm, "Billing"), communityNamed(cm, "Catalog")

	moves := actionsOfKind(cm, "move")
	if len(moves) != 1 {
		t.Fatalf("expected one move, got %+v", cm.Actions)
	}
	move := moves[0]
	if move.Title != "Move D next to A" {
		t.Errorf("the file of the less active community moves: %s", move.Title)
	}
	if move.Detail != "They changed together in 5 commits this year while sitting in Catalog and Billing." {
		t.Errorf("unexpected detail: %s", move.Detail)
	}
	if move.Effort != "1 file" || move.Gain != "The two would sit in one community" {
		t.Errorf("unexpected effort or gain: %s / %s", move.Effort, move.Gain)
	}
	if !reflect.DeepEqual(move.Communities, []string{catalog.ID, billing.ID}) || !reflect.DeepEqual(move.Units, []string{`App\Catalog\D`, `App\Billing\A`}) {
		t.Errorf("the action should name the communities and the classes: %+v", move)
	}
}

func TestAnExposedCommunityBecomesAGatherAction(t *testing.T) {
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
	gathers := actionsOfKind(cm, "gather")
	if len(gathers) != 1 {
		t.Fatalf("expected one gather, got %+v", cm.Actions)
	}
	g := gathers[0]
	if g.Title != "Gather the entries of Core" {
		t.Errorf("unexpected title: %s", g.Title)
	}
	if g.Detail != "4 of its 6 classes are used from outside; the most used, A, B and C, could front the others." {
		t.Errorf("unexpected detail: %s", g.Detail)
	}
	if g.Gain != "4 entries become the API of Core" {
		t.Errorf("unexpected gain: %s", g.Gain)
	}
	if g.Effort != "4 classes exposed" || !reflect.DeepEqual(g.Communities, []string{core.ID}) || len(g.Units) != 3 {
		t.Errorf("unexpected action: %+v", g)
	}
}

func TestAKernelKnowingItsUsersBecomesAnInvertAction(t *testing.T) {
	// The base class everybody extends references one of the modules back.
	sources := []string{
		"<?php\nnamespace App\\Shared;\n\nuse App\\Billing\\D;\n\nabstract class Model { public function __construct(D $d) {} }\n",
	}
	for _, module := range []string{`App\Billing`, `App\Catalog`, `App\Shipping`, `App\Users`} {
		sources = append(sources, phpTightModule(module, nil)...)
		sources = append(sources, fmt.Sprintf("<?php\nnamespace %s;\n\nuse App\\Shared\\Model;\n\nfinal class Entity extends Model { public function __construct(A $a) {} }\n", module))
	}
	cm := communitiesOf(t, sources...)
	if cm.Shared == nil || len(cm.Shared.Uses) == 0 {
		t.Fatalf("expected a kernel depending on a module, got %s", cm.Verdict)
	}
	inverts := actionsOfKind(cm, "invert")
	if len(inverts) != 1 {
		t.Fatalf("expected one invert, got %+v", cm.Actions)
	}
	inv := inverts[0]
	if inv.Title != "Free the shared kernel from Billing" {
		t.Errorf("unexpected title: %s", inv.Title)
	}
	if inv.Detail != "The kernel references Billing 1 time: those references should be inverted or moved out." {
		t.Errorf("unexpected detail: %s", inv.Detail)
	}
	if inv.Gain != "The kernel could change without Billing" {
		t.Errorf("unexpected gain: %s", inv.Gain)
	}
	if inv.Effort != "1 reference" || inv.Communities[0] != SharedID || !reflect.DeepEqual(inv.Units, []string{`App\Shared\Model`}) {
		t.Errorf("unexpected action: %+v", inv)
	}
}

func TestCutsAreOrderedByWhatTheyFree(t *testing.T) {
	// A ring of three (0, 1, 2) and a two-way tie between 3 and 4: the light
	// back edge inside the pair frees 2 modules, the heavier one out of the
	// ring frees 3 and comes first. A back edge that leaves the ring standing
	// (a chord) only shortens or is part of the way out.
	cm := &CommunityMetrics{
		Communities: []*Community{
			{ID: "0", ShortName: "A"}, {ID: "1", ShortName: "B"}, {ID: "2", ShortName: "C"},
			{ID: "3", ShortName: "D"}, {ID: "4", ShortName: "E"},
		},
		NodeToCommunity: map[string]string{},
		Labels:          map[string]string{},
		Edges: []CommunityEdge{
			{From: "0", To: "1", Weight: 5, InCycle: true},
			{From: "1", To: "2", Weight: 5, InCycle: true},
			{From: "2", To: "0", Weight: 3, InCycle: true, Back: true},
			{From: "3", To: "4", Weight: 4, InCycle: true},
			{From: "4", To: "3", Weight: 1, InCycle: true, Back: true},
		},
	}
	actions := actionsOf(cm)
	if len(actions) != 2 {
		t.Fatalf("expected the two cuts, got %+v", actions)
	}
	if actions[0].Title != "Cut C → A" || actions[0].Gain != "Frees 3 communities from the cycle" || actions[0].Effort != "3 references" {
		t.Errorf("the cut freeing the most modules comes first: %+v", actions[0])
	}
	if actions[1].Title != "Cut E → D" || actions[1].Gain != "Frees 2 communities from the cycle" {
		t.Errorf("then the other one: %+v", actions[1])
	}

	// two rings sharing a node: cutting a chord shortens the cycle without
	// freeing everyone
	cm.Edges = []CommunityEdge{
		{From: "0", To: "1", Weight: 5, InCycle: true},
		{From: "1", To: "0", Weight: 1, InCycle: true, Back: true},
		{From: "1", To: "2", Weight: 5, InCycle: true},
		{From: "2", To: "1", Weight: 1, InCycle: true, Back: true},
	}
	actions = actionsOf(cm)
	if len(actions) != 2 || actions[0].Gain != "Frees 1 community from the cycle" || actions[1].Gain != "Frees 1 community from the cycle" {
		t.Errorf("cutting one back edge of a figure of eight frees one module: %+v", actions)
	}
	// a shortcut inside the ring: cutting it changes nothing to who is caught
	cm.Edges = []CommunityEdge{
		{From: "0", To: "1", Weight: 5, InCycle: true},
		{From: "1", To: "2", Weight: 5, InCycle: true},
		{From: "2", To: "0", Weight: 5, InCycle: true, Back: true},
		{From: "1", To: "0", Weight: 1, InCycle: true, Back: true},
	}
	actions = actionsOf(cm)
	if len(actions) != 2 || actions[0].Title != "Cut C → A" || actions[0].Gain != "Frees 1 community from the cycle" || actions[1].Gain != "Part of the way out of the cycle" {
		t.Errorf("a shortcut alone frees nobody: %+v", actions)
	}
	// two pairs tied into one cycle of four: cutting the tie leaves two
	// cycles of two, nobody is freed but the cycle is shorter
	cm.Edges = []CommunityEdge{
		{From: "0", To: "1", Weight: 5, InCycle: true},
		{From: "1", To: "0", Weight: 5, InCycle: true},
		{From: "2", To: "3", Weight: 5, InCycle: true},
		{From: "3", To: "2", Weight: 5, InCycle: true},
		{From: "1", To: "2", Weight: 5, InCycle: true},
		{From: "3", To: "0", Weight: 1, InCycle: true, Back: true},
	}
	actions = actionsOf(cm)
	if len(actions) != 1 || actions[0].Gain != "Shortens the cycle" {
		t.Errorf("splitting a cycle in two shortens it: %+v", actions)
	}
}

func TestActionsAreBoundedToThree(t *testing.T) {
	cm := &CommunityMetrics{
		Communities: []*Community{
			{ID: "0", ShortName: "A", Units: []string{"a"}},
			{ID: "1", ShortName: "B", Units: []string{"b"}},
			{ID: "2", ShortName: "C", Units: []string{"c"}},
		},
		NodeToCommunity: map[string]string{"a": "0", "b": "1", "c": "2"},
		Labels:          map[string]string{"a": "a", "b": "b", "c": "c"},
		Edges: []CommunityEdge{
			{From: "0", To: "1", Weight: 3, InCycle: true, Back: true},
			{From: "1", To: "2", Weight: 2, InCycle: true, Back: true},
			{From: "2", To: "0", Weight: 1, InCycle: true, Back: true},
		},
		CrossReferences: []UnitReference{{From: "a", To: "b", Weight: 3}, {From: "b", To: "c", Weight: 2}, {From: "c", To: "a", Weight: 1}},
		Findings: []CommunityFinding{
			{Kind: "exposed", Communities: []string{"0"}},
		},
	}
	cm.Communities[0].Exposed = []string{"a"}
	cm.Communities[0].ExposedCount = 1
	cm.Communities[0].Size = 1
	actions := actionsOf(cm)
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %+v", actions)
	}
	kinds := []string{}
	for _, a := range actions {
		kinds = append(kinds, a.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"cut", "cut", "gather"}) {
		t.Errorf("two cuts at most, lightest first, then the gather: %v", kinds)
	}
	if actions[0].Title != "Cut C → A" || actions[1].Title != "Cut B → C" {
		t.Errorf("the lightest cuts come first: %s, %s", actions[0].Title, actions[1].Title)
	}
	if actions[0].Gain != "Frees 3 communities from the cycle" || actions[1].Gain != "Frees 3 communities from the cycle" {
		t.Errorf("each cut of a ring frees it: %s, %s", actions[0].Gain, actions[1].Gain)
	}
	if !strings.HasSuffix(actions[0].Detail, "carried by c → a (1).") {
		t.Errorf("unexpected detail: %s", actions[0].Detail)
	}
}
