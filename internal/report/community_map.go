package report

import (
	"fmt"
	"html"
	"math"
	"slices"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

// The community map is drawn on the server: a layered diagram of the
// communities and the dependencies between them, as an inline SVG. It needs
// no library, works offline, and two runs on the same code draw the same map.
//
// Layout: the communities are layered by their dependencies, the ones nobody
// depends on at the top, the ones everything rests on at the bottom, so that
// the arrows flow downwards and an arrow going up is a cycle. Communities
// depending on each other sit on the same layer. The shared kernel, which by
// definition every community rests on, is a bar under the whole map.

// communityMapMaxBoxes bounds the number of communities drawn: past it the map
// stops being readable, and the table below lists them all anyway.
const communityMapMaxBoxes = 30

// communityPalette colors the communities apart. The same color is used for a
// community on the map and in the table.
var communityPalette = []string{
	"#2563eb", "#7c3aed", "#0891b2", "#059669", "#d97706", "#db2777",
	"#4f46e5", "#0d9488", "#ca8a04", "#9333ea", "#0284c7", "#65a30d",
}

// CommunityColor returns the color of a community by its position, the shared
// kernel in slate.
func communityColor(index int, shared bool) string {
	if shared {
		return "#475569"
	}
	return communityPalette[index%len(communityPalette)]
}

type mapBox struct {
	c     *analyzer.Community
	index int
	layer int
	order float64
	x, y  float64
	w, h  float64
}

// communityMapSVG draws the communities of cm.
func communityMapSVG(cm *analyzer.CommunityMetrics) string {
	return drawCommunityMap(cm, mapOptions{})
}

// mapOptions is what the zoomed-out map changes in the drawing.
type mapOptions struct {
	// blocks is true when the boxes are blocks of communities: they carry
	// the ids of their members, list a few of their names, and lead nowhere
	// but back to the map of the communities.
	blocks bool
	// members lists, per box, the ids of the communities it holds, and
	// memberNames their short names, largest first.
	members     map[string][]string
	memberNames map[string][]string
}

// maxMemberLines is the number of member communities named inside a block
// box; the rest is counted.
const maxMemberLines = 3

// communityBlocksSVG draws the zoomed-out map: the blocks of communities as
// boxes, the dependencies between them added up, the shared kernel under.
// The drawing is the same as the map of the communities, on a coarser
// partition; nothing is recomputed on the page.
func communityBlocksSVG(cm *analyzer.CommunityMetrics) string {
	if cm == nil || len(cm.Blocks) == 0 {
		return ""
	}
	byID := map[string]*analyzer.Community{}
	for _, c := range cm.Communities {
		byID[c.ID] = c
	}
	coarse := &analyzer.CommunityMetrics{
		Granularity:      cm.Granularity,
		CommunitiesCount: len(cm.Blocks),
		Edges:            cm.BlockEdges,
		Shared:           cm.Shared,
		SharedShare:      cm.SharedShare,
	}
	opts := mapOptions{blocks: true, members: map[string][]string{}, memberNames: map[string][]string{}}
	for _, b := range cm.Blocks {
		coarse.Communities = append(coarse.Communities, &analyzer.Community{ID: b.ID, Name: b.Name, ShortName: b.Name, Size: b.Size, Cohesive: true})
		opts.members[b.ID] = b.Communities
		for _, id := range b.Communities {
			if c := byID[id]; c != nil {
				opts.memberNames[b.ID] = append(opts.memberNames[b.ID], c.ShortName)
			}
		}
	}
	if cm.Shared != nil {
		coarse.Communities = append(coarse.Communities, cm.Shared)
	}
	return drawCommunityMap(coarse, opts)
}

// drawCommunityMap draws the boxes of cm, communities or blocks of them.
func drawCommunityMap(cm *analyzer.CommunityMetrics, opts mapOptions) string {
	if cm == nil || cm.CommunitiesCount == 0 {
		return ""
	}
	drawn := make([]*mapBox, 0, cm.CommunitiesCount)
	byID := map[string]*mapBox{}
	for i, c := range cm.Communities {
		if c.Shared {
			continue
		}
		if len(drawn) >= communityMapMaxBoxes {
			break
		}
		b := &mapBox{c: c, index: i}
		drawn = append(drawn, b)
		byID[c.ID] = b
	}
	notDrawn := cm.CommunitiesCount - len(drawn)
	// Past a few dozen edges the arrows hide the boxes: only the strongest
	// dependencies are drawn, and the note under the map says so.
	const maxEdges = 45

	// Edges between drawn communities, the shared kernel left aside.
	type edge struct {
		from, to *mapBox
		weight   int
		back     bool
	}
	edges := []edge{}
	maxWeight := 1
	for _, e := range cm.Edges {
		if e.Shared {
			continue
		}
		from, to := byID[e.From], byID[e.To]
		if from == nil || to == nil {
			continue
		}
		edges = append(edges, edge{from: from, to: to, weight: e.Weight, back: e.Back})
		maxWeight = max(maxWeight, e.Weight)
	}
	edgesTotal := len(edges)
	if len(edges) > maxEdges {
		// cm.Edges come heaviest first: the strongest are kept, and every
		// back edge besides them, since they are what closes the cycles and
		// what the "cycles to cut" view shows
		kept := make([]edge, 0, maxEdges)
		heavy := 0
		for _, e := range edges {
			if e.back || heavy < maxEdges {
				kept = append(kept, e)
			}
			if !e.back {
				heavy++
			}
		}
		edges = kept
	}

	// Layers: longest path from the sources once the back edges are set
	// aside, which leaves a DAG. Communities nobody depends on come first.
	dagOut := map[string]map[string]struct{}{}
	dagIn := map[string]int{}
	for _, b := range drawn {
		dagOut[b.c.ID] = map[string]struct{}{}
	}
	for _, e := range edges {
		if e.back {
			continue
		}
		a, b := e.from.c.ID, e.to.c.ID
		if _, seen := dagOut[a][b]; !seen {
			dagOut[a][b] = struct{}{}
			dagIn[b]++
		}
	}
	layerOf := map[string]int{}
	queue := []string{}
	for _, n := range sortedKeysOfMap(dagOut) {
		if dagIn[n] == 0 {
			queue = append(queue, n)
			layerOf[n] = 0
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, m := range sortedKeysOfMap(dagOut[n]) {
			layerOf[m] = max(layerOf[m], layerOf[n]+1)
			dagIn[m]--
			if dagIn[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	layerCount := 0
	for _, b := range drawn {
		b.layer = layerOf[b.c.ID]
		layerCount = max(layerCount, b.layer+1)
	}

	unitWord := "classes"
	if cm.Granularity == analyzer.GranularityNamespace {
		unitWord = "packages"
	}
	// Box sizes from the label.
	const (
		boxHeight  = 58
		gapX       = 22
		gapY       = 84
		paddingX   = 40
		paddingTop = 24
		charWidth  = 7.6
	)
	metaOf := func(c *analyzer.Community) string {
		meta := fmt.Sprintf("%d %s", c.Size, unitWord)
		if opts.blocks {
			if n := len(opts.members[c.ID]); n > 1 {
				meta += fmt.Sprintf(" in %d communities", n)
			}
			return meta
		}
		if !c.Cohesive && len(c.Namespaces) > 1 {
			meta += fmt.Sprintf(", %d namespaces", len(c.Namespaces))
		}
		return meta
	}
	// The lines written inside a block box: a few of its communities by
	// name, the rest counted. A block of one community names nothing, its
	// title is that community.
	memberLinesOf := func(c *analyzer.Community) []string {
		names := opts.memberNames[c.ID]
		if !opts.blocks || len(names) < 2 {
			return nil
		}
		lines := []string{}
		for i, name := range names {
			if i == maxMemberLines {
				lines = append(lines, fmt.Sprintf("and %d more", len(names)-i))
				break
			}
			lines = append(lines, name)
		}
		return lines
	}
	const memberLineHeight = 15.0
	for _, b := range drawn {
		w := float64(len([]rune(b.c.ShortName)))*charWidth + 44
		w = math.Max(w, float64(len([]rune(metaOf(b.c))))*6.4+44)
		lines := memberLinesOf(b.c)
		for _, line := range lines {
			w = math.Max(w, float64(len([]rune(line)))*6.4+44)
		}
		b.w = math.Min(math.Max(w, 128), 380)
		b.h = boxHeight
		if len(lines) > 0 {
			b.h += 6 + memberLineHeight*float64(len(lines))
		}
	}

	// Order inside each layer: barycenter of the neighbours, a few sweeps.
	layers := make([][]*mapBox, layerCount)
	for _, b := range drawn {
		layers[b.layer] = append(layers[b.layer], b)
	}
	for _, layer := range layers {
		slices.SortFunc(layer, func(x, y *mapBox) int { return x.index - y.index })
		for i, b := range layer {
			b.order = float64(i)
		}
	}
	neighbours := map[*mapBox][]*mapBox{}
	for _, e := range edges {
		neighbours[e.from] = append(neighbours[e.from], e.to)
		neighbours[e.to] = append(neighbours[e.to], e.from)
	}
	// The communities linked to no other are drawn apart, under the layers:
	// they lean on the shared kernel or on nothing, and an arrow-less box
	// among the layers reads as a mistake.
	standalone := []*mapBox{}
	for li := range layers {
		kept := layers[li][:0]
		for _, b := range layers[li] {
			if len(neighbours[b]) == 0 {
				standalone = append(standalone, b)
			} else {
				kept = append(kept, b)
			}
		}
		layers[li] = kept
	}
	slices.SortFunc(standalone, func(x, y *mapBox) int { return x.index - y.index })
	sortLayer := func(layer []*mapBox) {
		slices.SortStableFunc(layer, func(x, y *mapBox) int {
			switch {
			case x.order < y.order:
				return -1
			case x.order > y.order:
				return 1
			}
			return x.index - y.index
		})
		for i, b := range layer {
			b.order = float64(i)
		}
	}
	for sweep := 0; sweep < 4; sweep++ {
		for li := range layers {
			layer := layers[li]
			for _, b := range layer {
				sum, n := 0.0, 0
				for _, other := range neighbours[b] {
					if other.layer != b.layer {
						sum += other.order
						n++
					}
				}
				if n > 0 {
					b.order = sum / float64(n)
				}
			}
			sortLayer(layer)
		}
	}
	// Positions. A layer wider than the canvas wraps onto several rows, so
	// that the boxes keep a readable size whatever their number.
	const maxRowWidth = 1080.0
	type row struct {
		boxes  []*mapBox
		width  float64
		height float64
	}
	rowsOfLayer := make([][]row, layerCount)
	maxRowWidthSeen := 0.0
	for li, layer := range layers {
		// The boxes linked to other layers go to the last row of theirs, the
		// closest to the arrows; the ones linked to nothing fill the rows
		// above, where no arrow has to pass behind them.
		ordered := make([]*mapBox, 0, len(layer))
		for _, b := range layer {
			if len(neighbours[b]) == 0 {
				ordered = append(ordered, b)
			}
		}
		for _, b := range layer {
			if len(neighbours[b]) > 0 {
				ordered = append(ordered, b)
			}
		}
		current := row{}
		for _, b := range ordered {
			extra := b.w
			if len(current.boxes) > 0 {
				extra += gapX
			}
			if len(current.boxes) > 0 && current.width+extra > maxRowWidth {
				rowsOfLayer[li] = append(rowsOfLayer[li], current)
				current = row{}
				extra = b.w
			}
			current.boxes = append(current.boxes, b)
			current.width += extra
			current.height = math.Max(current.height, b.h)
		}
		if len(current.boxes) > 0 {
			rowsOfLayer[li] = append(rowsOfLayer[li], current)
		}
		for _, r := range rowsOfLayer[li] {
			maxRowWidthSeen = math.Max(maxRowWidthSeen, r.width)
		}
	}
	wrap := func(boxes []*mapBox) []row {
		rows := []row{}
		current := row{}
		for _, b := range boxes {
			extra := b.w
			if len(current.boxes) > 0 {
				extra += gapX
			}
			if len(current.boxes) > 0 && current.width+extra > maxRowWidth {
				rows = append(rows, current)
				current = row{}
				extra = b.w
			}
			current.boxes = append(current.boxes, b)
			current.width += extra
			current.height = math.Max(current.height, b.h)
		}
		if len(current.boxes) > 0 {
			rows = append(rows, current)
		}
		return rows
	}
	standaloneRows := wrap(standalone)
	for _, r := range standaloneRows {
		maxRowWidthSeen = math.Max(maxRowWidthSeen, r.width)
	}
	width := math.Max(maxRowWidthSeen, 480) + 2*paddingX
	const rowGap = 26.0
	y := float64(paddingTop)
	place := func(rows []row) {
		if len(rows) == 0 {
			return
		}
		rowY := y
		for _, r := range rows {
			x := paddingX + (width-2*paddingX-r.width)/2
			for _, b := range r.boxes {
				b.x = x
				b.y = rowY
				x += b.w + gapX
			}
			rowY += r.height + rowGap
		}
		y = rowY - rowGap + gapY
	}
	for li := range layers {
		place(rowsOfLayer[li])
	}
	// room under the last layer for the arcs of a cycle drawn below it
	if y > paddingTop {
		y = y - gapY + 40
	}
	standaloneCaptionY := 0.0
	if len(standalone) > 0 {
		standaloneCaptionY = y + 14
		y = standaloneCaptionY + 18
		place(standaloneRows)
		y = y - gapY + 8
	}
	height := y
	sharedBarY := 0.0
	sharedBarHeight := 64.0
	if cm.Shared != nil {
		if len(kernelSegments(cm.Shared)) > 0 {
			sharedBarHeight = 96
		}
		sharedBarY = height + 16
		height = sharedBarY + sharedBarHeight + 16
	}
	if notDrawn > 0 || edgesTotal > len(edges) {
		height += 28
	}

	var sb strings.Builder
	// drawn at its natural size, shrunk by the page when it is wider than it;
	// the "after the cuts" view of the page reads the number of dependencies
	// to cut, the references they carry and the layers left once they are
	// gone from the attributes of the drawing
	cuts := cutsOf(cm)
	ariaLabel, mapClass := "Map of the communities and their dependencies", "community-map"
	if opts.blocks {
		ariaLabel, mapClass = "Map of the blocks of communities and their dependencies", "community-map community-map--blocks"
	}
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" style="max-width:100%%;height:auto" class="%s" role="img" aria-label="%s" data-cuts="%d" data-cut-refs="%d" data-layers="%d">`, width, height, width, height, mapClass, ariaLabel, cuts.edges, cuts.references, layerCount)
	sb.WriteString(`<defs>`)
	sb.WriteString(`<marker id="cm-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#94a3b8"/></marker>`)
	sb.WriteString(`<marker id="cm-arrow-cycle" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#ef4444"/></marker>`)
	sb.WriteString(`</defs>`)

	// Edges. When two boxes depend on each other across layers, the two
	// arrows are shifted apart so that neither hides the other.
	sb.WriteString(`<g class="cm-edges">`)
	reverse := map[[2]*mapBox]bool{}
	for _, e := range edges {
		reverse[[2]*mapBox{e.to, e.from}] = true
	}
	for _, e := range edges {
		x1 := e.from.x + e.from.w/2
		x2 := e.to.x + e.to.w/2
		if reverse[[2]*mapBox{e.from, e.to}] && e.from.layer != e.to.layer {
			shift := 7.0
			if e.from.index > e.to.index {
				shift = -shift
			}
			x1 += shift
			x2 += shift
		}
		var y1, y2 float64
		var d string
		strokeWidth := 1.0 + 3.0*math.Log1p(float64(e.weight))/math.Log1p(float64(maxWeight))
		if e.to.layer > e.from.layer {
			// downwards: from the bottom of the source to the top of the target
			y1 = e.from.y + e.from.h
			y2 = e.to.y
			my := (y1 + y2) / 2
			d = fmt.Sprintf("M %.1f %.1f C %.1f %.1f, %.1f %.1f, %.1f %.1f", x1, y1, x1, my, x2, my, x2, y2)
		} else if e.to.layer < e.from.layer {
			// upwards, against the flow: a cycle
			y1 = e.from.y
			y2 = e.to.y + e.to.h
			my := (y1 + y2) / 2
			d = fmt.Sprintf("M %.1f %.1f C %.1f %.1f, %.1f %.1f, %.1f %.1f", x1, y1, x1, my, x2, my, x2, y2)
		} else {
			// same layer: an arc, above the boxes from left to right and
			// below them from right to left, so that the two directions of
			// a cycle do not overlap
			lift := 28.0 + math.Abs(x2-x1)/12
			if x2 >= x1 {
				y1 = e.from.y
				y2 = e.to.y
				d = fmt.Sprintf("M %.1f %.1f C %.1f %.1f, %.1f %.1f, %.1f %.1f", x1, y1, x1, y1-lift, x2, y2-lift, x2, y2)
			} else {
				y1 = e.from.y + e.from.h
				y2 = e.to.y + e.to.h
				d = fmt.Sprintf("M %.1f %.1f C %.1f %.1f, %.1f %.1f, %.1f %.1f", x1, y1, x1, y1+lift, x2, y2+lift, x2, y2)
			}
		}
		color, marker, dash := "#94a3b8", "cm-arrow", ""
		if e.back {
			color, marker, dash = "#ef4444", "cm-arrow-cycle", ` stroke-dasharray="6 4"`
		}
		fmt.Fprintf(&sb, `<path class="cm-edge%s" data-from="%s" data-to="%s" d="%s" fill="none" stroke="%s" stroke-width="%.1f" stroke-opacity="0.75" marker-end="url(#%s)"%s><title>%s → %s: %d reference%s%s</title></path>`,
			cycleClass(e.back), e.from.c.ID, e.to.c.ID, d, color, strokeWidth, marker, dash,
			html.EscapeString(e.from.c.ShortName), html.EscapeString(e.to.c.ShortName), e.weight, pluralS(e.weight), cycleNote(e.back))
	}
	sb.WriteString(`</g>`)

	// Standalone caption
	if len(standalone) > 0 {
		caption := "Linked to no other community"
		if cm.Shared != nil {
			caption = "Linked to no other community: they lean on the shared kernel alone, or on nothing"
		}
		if opts.blocks {
			caption = strings.Replace(caption, "community", "block", 1)
		}
		fmt.Fprintf(&sb, `<line x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f" stroke="#e2e8f0" stroke-width="1"/>`, float64(paddingX), standaloneCaptionY-10, width-paddingX, standaloneCaptionY-10)
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#64748b" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, float64(paddingX), standaloneCaptionY+4, html.EscapeString(caption))
	}

	// Boxes
	sb.WriteString(`<g class="cm-boxes">`)
	for _, b := range drawn {
		color := communityColor(b.index, false)
		if opts.blocks {
			// a block leads nowhere but back to the map of its communities,
			// which the page handles from the ids it carries
			fmt.Fprintf(&sb, `<g class="cm-box cm-block" role="button" tabindex="0" data-id="%s" data-communities="%s">`, b.c.ID, strings.Join(opts.members[b.c.ID], " "))
		} else {
			fmt.Fprintf(&sb, `<a href="#community-%s" class="cm-box" data-id="%s"><g>`, b.c.ID, b.c.ID)
		}
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="12" fill="white" stroke="%s" stroke-width="1.5"/>`, b.x, b.y, b.w, b.h, "#e2e8f0")
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="4" height="%.1f" rx="2" fill="%s"/>`, b.x+10, b.y+12, b.h-24, color)
		label := fitLabel(b.c.ShortName, int((b.w-40)/charWidth))
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="13" font-weight="600" fill="#0f172a" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, b.x+22, b.y+25, html.EscapeString(label))
		meta := metaOf(b.c)
		fmt.Fprintf(&sb, `<text class="cm-meta" x="%.1f" y="%.1f" font-size="11" fill="#64748b" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, b.x+22, b.y+43, html.EscapeString(meta))
		for i, line := range memberLinesOf(b.c) {
			fmt.Fprintf(&sb, `<text class="cm-member" x="%.1f" y="%.1f" font-size="11" fill="#94a3b8" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, b.x+22, b.y+boxHeight+float64(i)*memberLineHeight+2, html.EscapeString(fitLabel(line, int((b.w-40)/6.4))))
		}
		// the share of the box a folder holds, drawn by the page when the
		// reader zooms on a folder
		fmt.Fprintf(&sb, `<rect class="cm-focus" x="%.1f" y="%.1f" width="0" height="4" rx="2" fill="%s" data-full-width="%.1f" style="display:none"/>`, b.x+22, b.y+b.h-9, color, b.w-34)
		fmt.Fprintf(&sb, `<title>%s: %s</title>`, html.EscapeString(b.c.Name), html.EscapeString(meta))
		if opts.blocks {
			sb.WriteString(`</g>`)
		} else {
			sb.WriteString(`</g></a>`)
		}
	}
	sb.WriteString(`</g>`)

	// Shared kernel bar
	if cm.Shared != nil {
		s := cm.Shared
		barX := paddingX
		barW := width - 2*paddingX
		fmt.Fprintf(&sb, `<a href="#community-%s" class="cm-box cm-shared" data-id="%s"><g>`, s.ID, s.ID)
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.0f" rx="12" fill="#f8fafc" stroke="#cbd5e1" stroke-width="1.5" stroke-dasharray="5 4"/>`, float64(barX), sharedBarY, barW, sharedBarHeight)
		title := s.ShortName
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="13" font-weight="600" fill="#0f172a" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, float64(barX)+22, sharedBarY+26, html.EscapeString(fitLabel(title, int((barW-40)/charWidth))))
		meta := fmt.Sprintf("%d %s used by %d communities, %d%% of all dependencies lead here", s.Size, unitWord, len(s.UsedBy), int(cm.SharedShare*100+0.5))
		if s.Hint != "" {
			meta = fmt.Sprintf("%s, %s", s.Hint, meta)
		}
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#64748b" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, float64(barX)+22, sharedBarY+46, html.EscapeString(fitLabel(meta, int((barW-40)/6.2))))
		// where the kernel comes from: its namespaces, as segments of a bar
		if segments := kernelSegments(s); len(segments) > 0 {
			segY := sharedBarY + 58
			innerX := float64(barX) + 22
			innerW := barW - 44
			x := innerX
			for i, seg := range segments {
				w := innerW * seg.share
				color := communityPalette[(i*5+3)%len(communityPalette)]
				if seg.rest {
					color = "#cbd5e1"
				}
				fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="8" rx="2" fill="%s" fill-opacity="0.85"><title>%s: %d %s</title></rect>`, x, segY, math.Max(w-2, 0), color, html.EscapeString(seg.label), seg.count, unitWord)
				if w > 60 {
					fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="10" fill="#475569" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, x, segY+22, html.EscapeString(fitLabel(fmt.Sprintf("%s %d%%", seg.label, int(seg.share*100+0.5)), int((w-6)/5.8))))
				}
				x += w
			}
		}
		sb.WriteString(`</g></a>`)
	}
	notes := []string{}
	if notDrawn > 0 {
		if opts.blocks {
			notes = append(notes, fmt.Sprintf("%d smaller block%s not drawn.", notDrawn, pluralS(notDrawn)))
		} else {
			notes = append(notes, fmt.Sprintf("%d smaller communit%s not drawn; the table lists them.", notDrawn, pluralIes(notDrawn)))
		}
	}
	if edgesTotal > len(edges) {
		backs := 0
		for _, e := range edges {
			if e.back {
				backs++
			}
		}
		if backs > 0 {
			notes = append(notes, fmt.Sprintf("Only the %d strongest of %d dependencies are drawn, plus the %d closing a cycle.", len(edges)-backs, edgesTotal, backs))
		} else {
			notes = append(notes, fmt.Sprintf("Only the %d strongest of %d dependencies are drawn.", len(edges), edgesTotal))
		}
	}
	if len(notes) > 0 {
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#64748b" font-family="var(--font-sans, system-ui, sans-serif)">%s</text>`, float64(paddingX), height-10, html.EscapeString(strings.Join(notes, " ")))
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// mapCuts counts the dependencies to cut to leave no cycle: the back edges
// between communities, and the references they carry.
type mapCuts struct {
	edges, references int
}

func cutsOf(cm *analyzer.CommunityMetrics) mapCuts {
	out := mapCuts{}
	for _, e := range cm.Edges {
		if e.Back && !e.Shared {
			out.edges++
			out.references += e.Weight
		}
	}
	return out
}

// kernelSegment is one namespace of the shared kernel, for the bar under it.
type kernelSegment struct {
	label string
	count int
	share float64
	rest  bool
}

// kernelSegments splits the shared kernel into the namespaces it comes from:
// the largest ones, and the rest together, so that the bar says which part of
// the code the whole project leans on. Nothing when one namespace holds it
// all: the title already says so.
func kernelSegments(s *analyzer.Community) []kernelSegment {
	if s == nil || len(s.Namespaces) < 2 || s.Size == 0 {
		return nil
	}
	segments := []kernelSegment{}
	shown := 0
	for _, ns := range s.Namespaces {
		if len(segments) == 4 || ns.Share < 0.05 {
			break
		}
		label := ns.Label
		if label == "" {
			label = "(no namespace)"
		}
		segments = append(segments, kernelSegment{label: label, count: ns.Count, share: ns.Share})
		shown += ns.Count
	}
	if rest := s.Size - shown; rest > 0 {
		segments = append(segments, kernelSegment{label: "other namespaces", count: rest, share: float64(rest) / float64(s.Size), rest: true})
	}
	return segments
}

func cycleClass(inCycle bool) string {
	if inCycle {
		return " cm-edge--cycle"
	}
	return ""
}

func cycleNote(back bool) string {
	if back {
		return " (closes a cycle)"
	}
	return ""
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralIes(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// fitLabel shortens a label to a number of characters, an ellipsis in the
// middle so that both ends stay readable.
func fitLabel(label string, maxChars int) string {
	if maxChars < 8 {
		maxChars = 8
	}
	runes := []rune(label)
	if len(runes) <= maxChars {
		return label
	}
	keep := maxChars - 1
	head := keep * 2 / 3
	tail := keep - head
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

func sortedKeysOfMap[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
