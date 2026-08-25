package analyzer

import (
	"fmt"
	"reflect"
	"testing"
)

// blocksFixture builds a community graph of n communities named c0..c(n-1),
// sized by their position, and the directed edges given as "from>to:weight".
func blocksFixture(n int, edges ...string) *CommunityMetrics {
	cm := &CommunityMetrics{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%d", i)
		cm.Communities = append(cm.Communities, &Community{ID: id, ShortName: "C" + id, Size: 20 - i})
	}
	cm.CommunitiesCount = n
	for _, spec := range edges {
		var from, to string
		var w int
		fmt.Sscanf(spec, "%1s>%1s:%d", &from, &to, &w)
		cm.Edges = append(cm.Edges, CommunityEdge{From: from, To: to, Weight: w})
	}
	return cm
}

// Two macro-groups: 0,1,2 tightly linked, 3,4,5 tightly linked, one light
// edge from 2 to 3 and one back from 5 to 0.
func twoMacroGroups() *CommunityMetrics {
	return blocksFixture(6,
		"0>1:9", "1>2:8", "0>2:7", "2>1:3",
		"3>4:9", "4>5:8", "3>5:7", "5>4:3",
		"2>3:1", "5>0:1",
	)
}

func TestTwoMacroGroupsOfCommunitiesMakeTwoBlocks(t *testing.T) {
	cm := twoMacroGroups()
	blocks, edges := blocksOf(cm)
	if len(blocks) != 2 {
		t.Fatalf("expected two blocks, got %+v", blocks)
	}
	if !reflect.DeepEqual(blocks[0].Communities, []string{"0", "1", "2"}) || !reflect.DeepEqual(blocks[1].Communities, []string{"3", "4", "5"}) {
		t.Errorf("unexpected membership: %+v", blocks)
	}
	if blocks[0].ID != "b0" || blocks[1].ID != "b1" {
		t.Errorf("ids: %+v", blocks)
	}
	if blocks[0].Name != "C0 + 2" || blocks[0].Size != 20+19+18 {
		t.Errorf("the block takes the name of its largest community: %+v", blocks[0])
	}
	if len(edges) != 2 {
		t.Fatalf("expected two block edges, got %+v", edges)
	}
	// both directions form a cycle between the blocks: the lighter one, or
	// on a tie the one against the sequence, is the back edge
	if !edges[0].InCycle || !edges[1].InCycle {
		t.Errorf("the block edges should be in a cycle: %+v", edges)
	}
	backs := 0
	for _, e := range edges {
		if e.Back {
			backs++
		}
	}
	if backs != 1 {
		t.Errorf("exactly one back edge expected: %+v", edges)
	}
	export := ExportCommunities(cm, false)
	if len(export.Blocks) != 0 {
		t.Errorf("the export carries what cm holds, nothing was assigned yet: %+v", export.Blocks)
	}
	cm.Blocks, cm.BlockEdges = blocks, edges
	export = ExportCommunities(cm, false)
	if len(export.Blocks) != 2 || export.Blocks[0].Name != "C0 + 2" || len(export.BlockEdges) != 2 {
		t.Errorf("unexpected export: %+v %+v", export.Blocks, export.BlockEdges)
	}
}

func TestTwoCommunitiesHaveNoBlocks(t *testing.T) {
	cm := blocksFixture(2, "0>1:5", "1>0:1")
	if blocks, edges := blocksOf(cm); blocks != nil || edges != nil {
		t.Errorf("nothing to zoom out to with two communities: %+v", blocks)
	}
}

func TestOneTightGroupHasNoBlocks(t *testing.T) {
	// everything linked to everything at the same weight folds into one
	// block, which is no zoom-out
	cm := blocksFixture(4, "0>1:5", "1>2:5", "2>3:5", "3>0:5", "0>2:5", "1>3:5")
	if blocks, _ := blocksOf(cm); blocks != nil {
		t.Errorf("one block of everything is nothing to zoom out to: %+v", blocks)
	}
}

func TestALoneCommunityJoinsTheBlockItExchangesTheMostWith(t *testing.T) {
	// 6 is loosely linked to the second group; 7 is linked to nothing
	cm := twoMacroGroups()
	cm.Communities = append(cm.Communities, &Community{ID: "6", ShortName: "C6", Size: 2}, &Community{ID: "7", ShortName: "C7", Size: 2})
	cm.CommunitiesCount = 8
	cm.Edges = append(cm.Edges, CommunityEdge{From: "6", To: "4", Weight: 1})
	blocks, _ := blocksOf(cm)
	if len(blocks) != 3 {
		t.Fatalf("expected two blocks and one standalone, got %+v", blocks)
	}
	if !reflect.DeepEqual(blocks[1].Communities, []string{"3", "4", "5", "6"}) {
		t.Errorf("6 should join the second group: %+v", blocks)
	}
	if !reflect.DeepEqual(blocks[2].Communities, []string{"7"}) || blocks[2].Name != "C7" {
		t.Errorf("7, linked to nothing, stays alone: %+v", blocks)
	}
}

func TestBlocksAreDeterministic(t *testing.T) {
	first, firstEdges := blocksOf(twoMacroGroups())
	for i := 0; i < 20; i++ {
		again, againEdges := blocksOf(twoMacroGroups())
		if !reflect.DeepEqual(first, again) || !reflect.DeepEqual(firstEdges, againEdges) {
			t.Fatalf("run %d differs: %+v vs %+v", i, first, again)
		}
	}
}
