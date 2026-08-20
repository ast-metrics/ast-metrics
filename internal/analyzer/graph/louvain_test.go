package graph

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func clique(prefix string, size int, weight float64) []WeightedEdge {
	edges := []WeightedEdge{}
	for i := 0; i < size; i++ {
		for j := i + 1; j < size; j++ {
			edges = append(edges, WeightedEdge{A: fmt.Sprintf("%s%d", prefix, i), B: fmt.Sprintf("%s%d", prefix, j), Weight: weight})
		}
	}
	return edges
}

func TestLouvainSeparatesTwoCliquesJoinedByOneEdge(t *testing.T) {
	edges := append(clique("a", 5, 1), clique("b", 5, 1)...)
	edges = append(edges, WeightedEdge{A: "a0", B: "b0", Weight: 1})

	p := Louvain(nil, edges, 1)

	if p.Count != 2 {
		t.Fatalf("expected 2 communities, got %d: %v", p.Count, p.Community)
	}
	for i := 1; i < 5; i++ {
		if p.Community[fmt.Sprintf("a%d", i)] != p.Community["a0"] {
			t.Errorf("a%d should sit with a0", i)
		}
		if p.Community[fmt.Sprintf("b%d", i)] != p.Community["b0"] {
			t.Errorf("b%d should sit with b0", i)
		}
	}
	if p.Community["a0"] == p.Community["b0"] {
		t.Errorf("the two cliques should be apart")
	}
	if p.Modularity < 0.3 {
		t.Errorf("modularity of two cliques should be high, got %v", p.Modularity)
	}
}

func TestLouvainNumbersCommunitiesBySizeThenSmallestMember(t *testing.T) {
	edges := append(clique("z", 3, 1), clique("m", 6, 1)...)
	edges = append(edges, clique("a", 3, 1)...)

	p := Louvain(nil, edges, 1)

	if p.Count != 3 {
		t.Fatalf("expected 3 communities, got %d", p.Count)
	}
	if p.Community["m0"] != 0 {
		t.Errorf("largest community should be 0, got %d", p.Community["m0"])
	}
	if p.Community["a0"] != 1 || p.Community["z0"] != 2 {
		t.Errorf("equal sizes should be ordered by smallest member: a=%d z=%d", p.Community["a0"], p.Community["z0"])
	}
}

func TestLouvainIsDeterministicWhateverTheEdgeOrder(t *testing.T) {
	edges := []WeightedEdge{}
	for c := 0; c < 6; c++ {
		edges = append(edges, clique(fmt.Sprintf("c%d_", c), 7, 1)...)
	}
	// a ring between the cliques plus a few random bridges
	for c := 0; c < 6; c++ {
		edges = append(edges, WeightedEdge{A: fmt.Sprintf("c%d_0", c), B: fmt.Sprintf("c%d_1", (c+1)%6), Weight: 1})
	}
	rng := rand.New(rand.NewSource(7))
	for k := 0; k < 12; k++ {
		edges = append(edges, WeightedEdge{
			A:      fmt.Sprintf("c%d_%d", rng.Intn(6), rng.Intn(7)),
			B:      fmt.Sprintf("c%d_%d", rng.Intn(6), rng.Intn(7)),
			Weight: float64(1 + rng.Intn(3)),
		})
	}
	reference := Louvain(nil, edges, 1)
	for run := 0; run < 5; run++ {
		shuffled := append([]WeightedEdge{}, edges...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := Louvain(nil, shuffled, 1)
		if !reflect.DeepEqual(got.Community, reference.Community) || got.Modularity != reference.Modularity {
			t.Fatalf("run %d differs from the reference", run)
		}
	}
	if reference.Count != 6 {
		t.Errorf("expected the 6 cliques, got %d communities", reference.Count)
	}
}

func TestLouvainWeightsDecideWhereABridgeGoes(t *testing.T) {
	// x is linked to both cliques, three times more strongly to b.
	edges := append(clique("a", 4, 1), clique("b", 4, 1)...)
	edges = append(edges, WeightedEdge{A: "x", B: "a0", Weight: 1}, WeightedEdge{A: "x", B: "b0", Weight: 3})

	p := Louvain(nil, edges, 1)

	if p.Community["x"] != p.Community["b0"] {
		t.Errorf("x should join the clique it is most connected to")
	}
}

func TestLouvainLeavesLonelyNodesAlone(t *testing.T) {
	p := Louvain([]string{"solo", "duo"}, clique("k", 3, 1), 1)
	if p.Count != 3 {
		t.Fatalf("expected the clique and two singletons, got %d", p.Count)
	}
	if p.Community["solo"] == p.Community["duo"] {
		t.Errorf("unconnected nodes must not be grouped")
	}
	if p.Community["k0"] != 0 {
		t.Errorf("the clique should be community 0")
	}
}

func TestLouvainOnEmptyGraph(t *testing.T) {
	p := Louvain(nil, nil, 1)
	if p.Count != 0 || len(p.Community) != 0 {
		t.Errorf("expected no community, got %+v", p)
	}
}

func TestLouvainHigherResolutionSplitsMore(t *testing.T) {
	// Two cliques joined tightly: at low resolution they merge, at high they split.
	edges := append(clique("a", 4, 1), clique("b", 4, 1)...)
	edges = append(edges, WeightedEdge{A: "a0", B: "b0", Weight: 1}, WeightedEdge{A: "a1", B: "b1", Weight: 1}, WeightedEdge{A: "a2", B: "b2", Weight: 1})

	low := Louvain(nil, edges, 0.2)
	high := Louvain(nil, edges, 1.5)
	if low.Count > high.Count {
		t.Errorf("resolution 0.2 gave %d communities, 1.5 gave %d", low.Count, high.Count)
	}
}
