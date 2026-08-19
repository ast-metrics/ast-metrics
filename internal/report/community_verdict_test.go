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
