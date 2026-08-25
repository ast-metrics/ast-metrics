package report

import (
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

func TestVerdictLinksNameTheRowsWithoutEatingShorterNames(t *testing.T) {
	cm := &analyzer.CommunityMetrics{
		Communities: []*analyzer.Community{
			{ID: "0", ShortName: "Github"},
			{ID: "1", ShortName: "GithubEvents"},
			{ID: analyzer.SharedID, Shared: true, ShortName: "Shared kernel (User)", Hubs: []string{`App\User`}},
		},
		Labels: map[string]string{`App\User`: "User"},
	}
	cm.Shared = cm.Communities[2]
	out := verdictLinks("2 communities: GithubEvents and Github. 40% lead to 3 shared classes (User…) <b>", cm)
	if !strings.Contains(out, `<a class="verdict-link" href="#community-1" data-communities="1">GithubEvents</a>`) {
		t.Errorf("the longer name should be linked whole: %s", out)
	}
	if !strings.Contains(out, `href="#community-0" data-communities="0">Github</a>.`) {
		t.Errorf("the shorter name should be linked on its own: %s", out)
	}
	if !strings.Contains(out, `href="#community-shared" data-communities="shared" data-member="User">User</a>`) {
		t.Errorf("a kernel class should open the kernel row on that member: %s", out)
	}
	if !strings.Contains(out, "&lt;b&gt;") {
		t.Errorf("the text must be escaped: %s", out)
	}
	if verdictLinks("plain", nil) != "plain" {
		t.Errorf("without metrics the text is returned escaped, untouched")
	}
}

func TestVerdictLinksAndMoreOpensTheCycleFilter(t *testing.T) {
	cm := &analyzer.CommunityMetrics{Communities: []*analyzer.Community{{ID: "0", ShortName: "Github"}}}
	out := verdictLinks("9 of the 23 communities depend on each other in 1 cycle: Github, Users, Billing and 6 more.", cm)
	if !strings.Contains(out, `<a class="verdict-link" href="#communities" data-filter="cycle">and 6 more</a>.`) {
		t.Errorf("the rest of the cycle should open the table filtered on the cycle: %s", out)
	}
	// without a cycle in the sentence, "and N more" is left alone
	if out := verdictLinks("Filed under a, b and 2 more.", cm); strings.Contains(out, "data-filter") {
		t.Errorf("no filter without a cycle: %s", out)
	}
}

func TestCommunityIssueCountsFollowThePillOrderAndSkipTheKernel(t *testing.T) {
	cm := &analyzer.CommunityMetrics{Communities: []*analyzer.Community{
		{ID: "0", Issues: []string{"history-crossed", "cycle"}},
		{ID: "1", Issues: []string{"cycle", "exposed"}},
		{ID: "2"},
		{ID: analyzer.SharedID, Shared: true, Issues: []string{"cycle"}},
	}}
	got := communityIssueCounts(cm)
	want := []issueCount{{Kind: "cycle", Label: "cycle", Count: 2}, {Kind: "exposed", Label: "no boundary", Count: 1}, {Kind: "history-crossed", Label: "changes together", Count: 1}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filter %d: got %v, want %v", i, got[i], want[i])
		}
	}
	if communityIssueCounts(nil) != nil {
		t.Errorf("no metrics, no filters")
	}
	if issueLabel("history-loose") != "never as a whole" || issueLabel("odd") != "odd" {
		t.Errorf("labels: %q %q", issueLabel("history-loose"), issueLabel("odd"))
	}
}
