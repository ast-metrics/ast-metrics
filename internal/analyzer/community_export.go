package analyzer

import "strings"

// CommunitiesExport is the community analysis laid out for a reader that did
// not see the report: the JSON report and the MCP tools both serve it.
type CommunitiesExport struct {
	Verdict          string                   `json:"verdict"`
	Granularity      string                   `json:"granularity"`
	Root             string                   `json:"root,omitempty"`
	CommunitiesCount int                      `json:"communitiesCount"`
	LargestSize      int                      `json:"largestSize"`
	UnitsGrouped     int                      `json:"unitsGrouped"`
	UnitsIsolated    int                      `json:"unitsIsolated"`
	InternalShare    float64                  `json:"internalShare"`
	SharedShare      float64                  `json:"sharedShare"`
	CrossShare       float64                  `json:"crossShare"`
	Modularity       float64                  `json:"modularity"`
	Confidence       float64                  `json:"confidence"`
	LargestCycle     int                      `json:"largestCycle"`
	CohesiveCount    int                      `json:"cohesiveCount"`
	HistoryAvailable bool                     `json:"historyAvailable"`
	HistoryCommits   int                      `json:"historyCommits"`
	HistoryAgreement float64                  `json:"historyAgreement"`
	Communities      []CommunityExport        `json:"communities"`
	Edges            []CommunityEdgeExport    `json:"edges"`
	Cycles           [][]string               `json:"cycles"`
	Findings         []CommunityFindingExport `json:"findings"`
	Actions          []CommunityActionExport  `json:"actions"`
	// Blocks group the communities at a coarser grain, the zoomed-out map;
	// BlockEdges are the dependencies between them. Absent when there is
	// nothing to zoom out to.
	Blocks     []CommunityBlockExport `json:"blocks,omitempty"`
	BlockEdges []BlockEdgeExport      `json:"blockEdges,omitempty"`
}

// CommunityBlockExport is one block of communities.
type CommunityBlockExport struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Communities []string `json:"communities"`
	Size        int      `json:"size"`
}

// BlockEdgeExport is a dependency between two blocks, or between a block and
// the shared kernel.
type BlockEdgeExport struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Weight  int    `json:"weight"`
	InCycle bool   `json:"inCycle"`
	Back    bool   `json:"back"`
}

// CommunityExport is one community, the shared kernel included.
type CommunityExport struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	ShortName  string                 `json:"shortName"`
	Shared     bool                   `json:"shared,omitempty"`
	Size       int                    `json:"size"`
	Cohesive   bool                   `json:"cohesive"`
	Namespaces []NamespaceShareExport `json:"namespaces"`
	Hubs       []string               `json:"hubs,omitempty"`
	Members    []string               `json:"members,omitempty"`
	Uses       []CommunityLinkExport  `json:"uses,omitempty"`
	UsedBy     []CommunityLinkExport  `json:"usedBy,omitempty"`
	Externals  []ExternalUseExport    `json:"externals,omitempty"`
	Internal   int                    `json:"internalReferences"`
	Outbound   int                    `json:"outboundReferences"`
	Inbound    int                    `json:"inboundReferences"`
	// ExposedCount is the number of members used from outside the community,
	// ExposedShare that number over the size; Exposed lists them, most used
	// first, when the members are listed. ForeignUsesCount is the number of
	// units of other communities this one uses, the shared kernel left out.
	ExposedCount     int      `json:"exposedCount"`
	ExposedShare     float64  `json:"exposedShare"`
	Exposed          []string `json:"exposed,omitempty"`
	ForeignUsesCount int      `json:"foreignUsesCount"`
	// Border lists the members another resolution of the detection places
	// elsewhere, when the members are listed; Confidence is the share of the
	// members every resolution keeps here.
	Border        []string          `json:"border,omitempty"`
	Confidence    float64           `json:"confidence"`
	BusFactor     int               `json:"busFactor,omitempty"`
	TopCommitters []CommitterExport `json:"topCommitters,omitempty"`
	// History: the commits of the year touching the community, the share of
	// its multi-file commits staying inside it, and the communities the same
	// commits reach.
	HistoryCommits  int              `json:"historyCommits"`
	HistoryCohesion float64          `json:"historyCohesion"`
	ChangesWith     []CoChangeExport `json:"changesWith,omitempty"`
}

type CoChangeExport struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Commits int     `json:"commits"`
	Share   float64 `json:"share"`
}

type NamespaceShareExport struct {
	Namespace string  `json:"namespace"`
	Count     int     `json:"count"`
	Share     float64 `json:"share"`
}

type CommunityLinkExport struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Weight  int              `json:"references"`
	Details []UnitLinkExport `json:"details,omitempty"`
}

type UnitLinkExport struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Weight int    `json:"references"`
}

type ExternalUseExport struct {
	Namespace string `json:"namespace"`
	Count     int    `json:"references"`
}

type CommitterExport struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

type CommunityEdgeExport struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Weight  int    `json:"references"`
	InCycle bool   `json:"inCycle"`
	Back    bool   `json:"back"`
	Shared  bool   `json:"shared"`
}

type CommunityFindingExport struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Communities []string `json:"communities,omitempty"`
	Units       []string `json:"units,omitempty"`
}

// CommunityActionExport is one thing to do first, derived from the findings.
type CommunityActionExport struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Effort      string   `json:"effort"`
	Gain        string   `json:"gain,omitempty"`
	Communities []string `json:"communities,omitempty"`
	Units       []string `json:"units,omitempty"`
}

// ExportCommunities lays the community metrics out for the JSON report and
// the MCP tools. Members are listed when withMembers is set: they are the
// bulk of the payload.
func ExportCommunities(cm *CommunityMetrics, withMembers bool) *CommunitiesExport {
	if cm == nil {
		return nil
	}
	export := &CommunitiesExport{
		Verdict:          strings.TrimSpace(cm.Verdict + " " + cm.VerdictNote + " " + cm.VerdictAside),
		Granularity:      cm.Granularity,
		Root:             cm.Root,
		CommunitiesCount: cm.CommunitiesCount,
		LargestSize:      cm.MaxSize,
		UnitsGrouped:     cm.UnitCount,
		UnitsIsolated:    cm.IsolatedUnits,
		InternalShare:    cm.InternalShare,
		SharedShare:      cm.SharedShare,
		CrossShare:       cm.CrossShare,
		Modularity:       cm.Modularity,
		Confidence:       cm.Confidence,
		LargestCycle:     cm.LargestCycle,
		CohesiveCount:    cm.CohesiveCount,
		HistoryAvailable: cm.HistoryAvailable,
		HistoryCommits:   cm.HistoryCommits,
		HistoryAgreement: cm.HistoryAgreement,
		Communities:      make([]CommunityExport, 0, len(cm.Communities)),
		Edges:            make([]CommunityEdgeExport, 0, len(cm.Edges)),
		Cycles:           cm.Cycles,
		Findings:         make([]CommunityFindingExport, 0, len(cm.Findings)),
		Actions:          make([]CommunityActionExport, 0, len(cm.Actions)),
	}
	if export.Cycles == nil {
		export.Cycles = [][]string{}
	}
	links := func(in []CommunityLink) []CommunityLinkExport {
		out := make([]CommunityLinkExport, 0, len(in))
		for _, l := range in {
			details := make([]UnitLinkExport, 0, len(l.Details))
			for _, d := range l.Details {
				details = append(details, UnitLinkExport{From: d.From, To: d.To, Weight: d.Weight})
			}
			out = append(out, CommunityLinkExport{ID: l.ID, Name: l.Name, Weight: l.Weight, Details: details})
		}
		return out
	}
	for _, c := range cm.Communities {
		item := CommunityExport{
			ID: c.ID, Name: c.Name, ShortName: c.ShortName, Shared: c.Shared, Size: c.Size, Cohesive: c.Cohesive,
			Hubs: c.Hubs, Internal: c.InternalWeight, Outbound: c.OutWeight, Inbound: c.InWeight, BusFactor: c.BusFactor,
			Uses: links(c.Uses), UsedBy: links(c.UsedBy),
			ExposedCount: c.ExposedCount, ExposedShare: c.ExposedShare, ForeignUsesCount: c.ForeignUsesCount, Confidence: c.Confidence,
			HistoryCommits: c.HistoryCommits, HistoryCohesion: c.HistoryCohesion,
		}
		for _, cc := range c.ChangesWith {
			item.ChangesWith = append(item.ChangesWith, CoChangeExport{ID: cc.ID, Name: cc.Name, Commits: cc.Commits, Share: cc.Share})
		}
		for _, s := range c.Namespaces {
			item.Namespaces = append(item.Namespaces, NamespaceShareExport{Namespace: s.Namespace, Count: s.Count, Share: s.Share})
		}
		for _, e := range c.Externals {
			item.Externals = append(item.Externals, ExternalUseExport{Namespace: e.Namespace, Count: e.Count})
		}
		for _, t := range c.TopCommitters {
			item.TopCommitters = append(item.TopCommitters, CommitterExport{Name: t.Name, Commits: t.Commits})
		}
		if withMembers {
			item.Members = c.Units
			item.Exposed = c.Exposed
			item.Border = c.Border
		}
		export.Communities = append(export.Communities, item)
	}
	for _, e := range cm.Edges {
		export.Edges = append(export.Edges, CommunityEdgeExport{From: e.From, To: e.To, Weight: e.Weight, InCycle: e.InCycle, Back: e.Back, Shared: e.Shared})
	}
	for _, f := range cm.Findings {
		export.Findings = append(export.Findings, CommunityFindingExport{Kind: f.Kind, Title: f.Title, Detail: f.Detail, Communities: f.Communities, Units: f.Units})
	}
	for _, a := range cm.Actions {
		export.Actions = append(export.Actions, CommunityActionExport{Kind: a.Kind, Title: a.Title, Detail: a.Detail, Effort: a.Effort, Gain: a.Gain, Communities: a.Communities, Units: a.Units})
	}
	for _, b := range cm.Blocks {
		export.Blocks = append(export.Blocks, CommunityBlockExport{ID: b.ID, Name: b.Name, Communities: b.Communities, Size: b.Size})
	}
	for _, e := range cm.BlockEdges {
		export.BlockEdges = append(export.BlockEdges, BlockEdgeExport{From: e.From, To: e.To, Weight: e.Weight, InCycle: e.InCycle, Back: e.Back})
	}
	return export
}
