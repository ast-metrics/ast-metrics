package report

import (
	"encoding/json"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

func TestCommunityFilesJSONCarriesFilesAndCommunities(t *testing.T) {
	cm := sampleCommunities()
	cm.UnitFiles = map[string]string{
		`App\Billing\Invoice`: "/p/src/Billing/Invoice.php",
		`App\Billing\Payer`:   "/p/src/Billing/Payer.php",
		`App\Catalog\Item`:    "/p/src/Catalog/Item.php",
	}
	cm.Communities[0].Units = []string{`App\Billing\Invoice`, `App\Billing\Payer`}
	cm.Communities[0].Exposed = []string{`App\Billing\Invoice`}
	cm.Communities[0].Border = []string{`App\Billing\Payer`}
	cm.Communities[1].Units = []string{`App\Catalog\Item`}
	cm.NodeToCommunity = map[string]string{`App\Billing\Invoice`: "0", `App\Billing\Payer`: "0", `App\Catalog\Item`: "1"}
	cm.Folders = []analyzer.FolderCommunities{{
		Path: "Billing", UnitCount: 2,
		Communities: []*analyzer.Community{{ID: "0", ShortName: "Billing", Size: 2, Units: []string{`App\Billing\Invoice`, `App\Billing\Payer`}}},
		Verdict:     "All the code forms a single community.",
	}}

	var out struct {
		Dirs        []string                  `json:"dirs"`
		Files       [][2]interface{}          `json:"f"`
		Units       map[string][]int          `json:"c"`
		Communities map[string][3]interface{} `json:"n"`
		Exposed     map[string][]int          `json:"x"`
		Border      map[string][]int          `json:"m"`
		Back        []string                  `json:"b"`
		Shared      string                    `json:"s"`
		Folders     map[string]struct {
			UnitCount   int              `json:"u"`
			Communities [][5]interface{} `json:"c"`
			Verdict     string           `json:"v"`
		} `json:"d"`
	}
	if err := json.Unmarshal([]byte(communityFilesJSON(cm)), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(out.Files) != 3 || out.Dirs[int(out.Files[0][0].(float64))] != "Billing" || out.Files[0][1] != "Invoice.php" {
		t.Errorf("paths should be relative to the shared root, got %v %v", out.Dirs, out.Files)
	}
	if len(out.Units["0"]) != 2 || len(out.Units["1"]) != 1 {
		t.Errorf("units per community: %v", out.Units)
	}
	// the members list marks the entries and the mobile members by file
	if len(out.Exposed["0"]) != 1 || out.Exposed["0"][0] != 0 || len(out.Border["0"]) != 1 || out.Border["0"][0] != 1 {
		t.Errorf("exposed and border members: %v %v", out.Exposed, out.Border)
	}
	if out.Communities["0"][0] != "Billing" || out.Shared != "shared" || len(out.Back) != 1 || out.Back[0] != "1>0" {
		t.Errorf("metadata: %v %q %v", out.Communities, out.Shared, out.Back)
	}
	folder, ok := out.Folders["Billing"]
	if !ok || folder.UnitCount != 2 || len(folder.Communities) != 1 || folder.Communities[0][0] != "Billing" {
		t.Errorf("folder analysis: %+v", out.Folders)
	}
}

func TestCommunityFilesJSONIsEmptyWithoutFiles(t *testing.T) {
	if got := communityFilesJSON(&analyzer.CommunityMetrics{}); got == "" || got[0] != '{' {
		t.Errorf("expected an empty payload, got %q", got)
	}
}
