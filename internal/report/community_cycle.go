package report

import (
	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

// cycleView is what the row of a community caught in a cycle shows of it:
// who it is locked with, which of them it reaches and which reach it, and
// the arrows to cut that touch it.
type cycleView struct {
	// Mates are the other communities of the cycle, in its order.
	Mates []cycleMate
	// Reaches and ReachedBy are the links of the community with its mates,
	// heaviest first, with the class-level references behind them.
	Reaches   []analyzer.CommunityLink
	ReachedBy []analyzer.CommunityLink
	// Cuts are the back edges touching the community: the lightest way out
	// of the cycle passes through them.
	Cuts []cycleCut
	// IDs are the ids of the whole cycle, this community included, joined
	// by spaces: what the map lights up.
	IDs string
}

type cycleMate struct {
	ID, Name string
}

type cycleCut struct {
	FromID, FromName, ToID, ToName string
	Weight                         int
	// Outgoing is true when the community is the one to stop depending.
	Outgoing bool
	Details  []analyzer.UnitLink
}

// cycleViewOf builds the view for a community; nil when it is in no cycle.
func cycleViewOf(c *analyzer.Community, cm *analyzer.CommunityMetrics) *cycleView {
	if c == nil || cm == nil || len(c.CycleWith) == 0 {
		return nil
	}
	byID := map[string]*analyzer.Community{}
	for _, other := range cm.Communities {
		byID[other.ID] = other
	}
	inCycle := map[string]bool{c.ID: true}
	v := &cycleView{IDs: c.ID}
	for _, id := range c.CycleWith {
		inCycle[id] = true
		v.IDs += " " + id
		if other := byID[id]; other != nil {
			v.Mates = append(v.Mates, cycleMate{ID: id, Name: other.ShortName})
		}
	}
	for _, l := range c.Uses {
		if inCycle[l.ID] {
			v.Reaches = append(v.Reaches, l)
		}
	}
	for _, l := range c.UsedBy {
		if inCycle[l.ID] {
			v.ReachedBy = append(v.ReachedBy, l)
		}
	}
	for _, e := range cm.Edges {
		if !e.Back || (e.From != c.ID && e.To != c.ID) || !inCycle[e.From] || !inCycle[e.To] {
			continue
		}
		cut := cycleCut{FromID: e.From, ToID: e.To, Weight: e.Weight, Outgoing: e.From == c.ID}
		if from := byID[e.From]; from != nil {
			cut.FromName = from.ShortName
			for _, l := range from.Uses {
				if l.ID == e.To {
					cut.Details = l.Details
				}
			}
		}
		if to := byID[e.To]; to != nil {
			cut.ToName = to.ShortName
		}
		v.Cuts = append(v.Cuts, cut)
	}
	return v
}
