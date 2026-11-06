package tui

// Screen geometry for mouse support.
//
// The renderers record the rectangles of what they draw (panes, tree rows,
// fold chevrons, field boxes, relations, body lines, filter bar fields,
// dropdown and overlay items) into a hitMap while rendering, so the mouse
// handlers (mouse.go) hit-test against exactly what is on screen instead of
// recomputing the layout a second way.

// rect is a rectangle of terminal cells.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// intersect returns the part of r inside o.
func (r rect) intersect(o rect) rect {
	x0, y0 := max(r.x, o.x), max(r.y, o.y)
	x1, y1 := min(r.x+r.w, o.x+o.w), min(r.y+r.h, o.y+o.h)
	return rect{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
}

// hitKind is the kind of a clickable region.
type hitKind int

const (
	hitTreePane     hitKind = iota + 1 // the tree box
	hitTreeRow                         // index: tree row
	hitViewRow                         // index: row of the Ready/Blocked/Audit list
	hitTreeChevron                     // index: tree row
	hitDetailPane                      // the detail pane
	hitDetailField                     // key: action key of a field box
	hitRelation                        // index: relation (detailModel.links)
	hitBody                            // the body box's content
	hitBodyLine                        // index: rendered body line
	hitBarField                        // index: barField (full or one-line bar)
	hitBarLine                         // the unfocused one-line filter summary
	hitDropdown                        // the open dropdown
	hitDropdownItem                    // index: dropdown option
	hitModal                           // the box of an overlay
	hitModalItem                       // index: overlay item
)

// hitTarget is what a region stands for.
type hitTarget struct {
	kind  hitKind
	index int
	key   string
}

type hitRegion struct {
	r rect
	hitTarget
}

// hitMap holds the regions of the last rendered screen. Regions recorded
// later lie on top of earlier ones.
type hitMap struct {
	regions []hitRegion
}

// find returns the topmost region at (x, y) of one of the given kinds (any
// kind if none are given).
func (h *hitMap) find(x, y int, kinds ...hitKind) (hitRegion, bool) {
	if h == nil {
		return hitRegion{}, false
	}
	for i := len(h.regions) - 1; i >= 0; i-- {
		reg := h.regions[i]
		if !reg.r.contains(x, y) {
			continue
		}
		if len(kinds) == 0 {
			return reg, true
		}
		for _, k := range kinds {
			if reg.kind == k {
				return reg, true
			}
		}
	}
	return hitRegion{}, false
}

// all returns the regions of a kind, in recording order (for tests).
func (h *hitMap) all(kind hitKind) []hitRegion {
	if h == nil {
		return nil
	}
	var out []hitRegion
	for _, reg := range h.regions {
		if reg.kind == kind {
			out = append(out, reg)
		}
	}
	return out
}

// recorder returns a recorder writing into h in screen coordinates.
func (h *hitMap) recorder() hitRecorder {
	return hitRecorder{m: h, clip: rect{0, 0, 1 << 30, 1 << 30}}
}

// hitRecorder records regions into a hitMap relative to an origin, clipped
// to an area. The zero value records nothing, so renderers can be used
// without a hit map.
type hitRecorder struct {
	m      *hitMap
	dx, dy int
	clip   rect // in screen coordinates
}

// at returns a recorder whose origin is moved by (x, y).
func (r hitRecorder) at(x, y int) hitRecorder {
	r.dx += x
	r.dy += y
	return r
}

// within returns a recorder that additionally clips to w x h cells from its
// origin.
func (r hitRecorder) within(w, h int) hitRecorder {
	r.clip = r.clip.intersect(rect{r.dx, r.dy, w, h})
	return r
}

// add records a region of w x h cells at (x, y) relative to the origin.
func (r hitRecorder) add(x, y, w, h int, t hitTarget) {
	if r.m == nil {
		return
	}
	rc := rect{r.dx + x, r.dy + y, w, h}.intersect(r.clip)
	if rc.w <= 0 || rc.h <= 0 {
		return
	}
	r.m.regions = append(r.m.regions, hitRegion{r: rc, hitTarget: t})
}

// place records the regions of o (recorded relative to a block) for the
// block placed at (x, y) relative to the origin.
func (r hitRecorder) place(o *hitMap, x, y int) {
	if o == nil {
		return
	}
	for _, reg := range o.regions {
		r.add(x+reg.r.x, y+reg.r.y, reg.r.w, reg.r.h, reg.hitTarget)
	}
}

// boxGridCells lays out field boxes in rows that fit width (see boxGrid) and
// returns the lines and the rectangle of every box.
func boxGridCells(width int, boxes []func(w int) []string) ([]string, []rect) {
	per := boxesPerRow(width, len(boxes))
	var out []string
	var cells []rect
	for start := 0; start < len(boxes); start += per {
		row := boxes[start:min(start+per, len(boxes))]
		ws := splitWidths(width, len(row), 1)
		blocks := make([][]string, len(row))
		h := 0
		for i, f := range row {
			blocks[i] = f(ws[i])
			h = max(h, len(blocks[i]))
		}
		x := 0
		for _, w := range ws {
			cells = append(cells, rect{x, len(out), w, h})
			x += w + 1
		}
		out = append(out, joinColumns(1, blocks...)...)
	}
	return out, cells
}
