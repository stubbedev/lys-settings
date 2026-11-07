package displays

import (
	"math"
	"slices"
)

// Rect is an output's logical rectangle on the plane.
type Rect struct{ X, Y, W, H int }

func (r Rect) right() int  { return r.X + r.W }
func (r Rect) bottom() int { return r.Y + r.H }

func (r Rect) overlaps(o Rect) bool {
	return r.X < o.right() && o.X < r.right() && r.Y < o.bottom() && o.Y < r.bottom()
}

func spanOverlap(a0, a1, b0, b1 int) int { return min(a1, b1) - max(a0, b0) }

func (r Rect) touches(o Rect) bool {
	if (r.right() == o.X || o.right() == r.X) && spanOverlap(r.Y, r.bottom(), o.Y, o.bottom()) > 0 {
		return true
	}
	return (r.bottom() == o.Y || o.bottom() == r.Y) && spanOverlap(r.X, r.right(), o.X, o.right()) > 0
}

// Logical is the rectangle an enabled setting covers: its mode in
// logical pixels at its position.
func Logical(s Setting) Rect {
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	return Rect{
		X: s.X, Y: s.Y,
		W: int(math.Round(float64(s.Mode.Width) / scale)),
		H: int(math.Round(float64(s.Mode.Height) / scale)),
	}
}

// Snap moves rects[i] so an edge within threshold of another rectangle's
// edge lines up with it, on each axis on its own.
func Snap(rects []Rect, i, threshold int) Rect {
	r := rects[i]
	bestX, bestY := threshold+1, threshold+1
	x, y := r.X, r.Y
	for j, o := range rects {
		if j == i {
			continue
		}
		for _, c := range []int{o.X, o.right(), o.X - r.W, o.right() - r.W} {
			if d := abs(c - r.X); d < bestX {
				bestX, x = d, c
			}
		}
		for _, c := range []int{o.Y, o.bottom(), o.Y - r.H, o.bottom() - r.H} {
			if d := abs(c - r.Y); d < bestY {
				bestY, y = d, c
			}
		}
	}
	return Rect{X: x, Y: y, W: r.W, H: r.H}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Settle places rects[i] where it touches another rectangle without
// overlapping any, the nearest such place to where it is, then does the
// same for any rectangle left floating apart from it, and normalizes.
func Settle(rects []Rect, i int) []Rect {
	out := slices.Clone(rects)
	out[i] = place(out, i)
	for range out {
		floating := disconnected(out, i)
		if floating < 0 {
			break
		}
		out[floating] = place(out, floating)
	}
	return Normalize(out)
}

func place(rects []Rect, i int) Rect {
	r := rects[i]
	if len(rects) < 2 {
		return r
	}
	best, bestDist, found := r, math.MaxInt, false
	free := func(c Rect) bool {
		for j, o := range rects {
			if j != i && c.overlaps(o) {
				return false
			}
		}
		return true
	}
	consider := func(c Rect) {
		d := (c.X-r.X)*(c.X-r.X) + (c.Y-r.Y)*(c.Y-r.Y)
		if d < bestDist && free(c) {
			best, bestDist, found = c, d, true
		}
	}
	for j, o := range rects {
		if j == i {
			continue
		}
		y := min(max(r.Y, o.Y-r.H+1), o.bottom()-1)
		x := min(max(r.X, o.X-r.W+1), o.right()-1)
		consider(Rect{X: o.right(), Y: y, W: r.W, H: r.H})
		consider(Rect{X: o.X - r.W, Y: y, W: r.W, H: r.H})
		consider(Rect{X: x, Y: o.bottom(), W: r.W, H: r.H})
		consider(Rect{X: x, Y: o.Y - r.H, W: r.W, H: r.H})
	}
	if found {
		return best
	}
	edge := math.MinInt
	top := math.MaxInt
	for j, o := range rects {
		if j != i {
			edge, top = max(edge, o.right()), min(top, o.Y)
		}
	}
	return Rect{X: edge, Y: top, W: r.W, H: r.H}
}

func disconnected(rects []Rect, from int) int {
	reached := make([]bool, len(rects))
	reached[from] = true
	queue := []int{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for j, o := range rects {
			if !reached[j] && rects[at].touches(o) {
				reached[j] = true
				queue = append(queue, j)
			}
		}
	}
	return slices.Index(reached, false)
}

// Normalize shifts the rectangles so the top-left one sits at 0,0.
func Normalize(rects []Rect) []Rect {
	if len(rects) == 0 {
		return rects
	}
	minX, minY := rects[0].X, rects[0].Y
	for _, r := range rects[1:] {
		minX, minY = min(minX, r.X), min(minY, r.Y)
	}
	out := make([]Rect, len(rects))
	for i, r := range rects {
		out[i] = Rect{X: r.X - minX, Y: r.Y - minY, W: r.W, H: r.H}
	}
	return out
}

// NextEdge is the nearest position past rects[i] along one axis, in the
// direction of the sign of dx or dy, at which one of its edges meets an
// edge of another rectangle; false when there is none.
func NextEdge(rects []Rect, i, dx, dy int) (Rect, bool) {
	r := rects[i]
	found := false
	best := r
	bestDist := math.MaxInt
	for j, o := range rects {
		if j == i {
			continue
		}
		var cands []int
		var at, sign int
		switch {
		case dx != 0:
			cands, at, sign = []int{o.X, o.right(), o.X - r.W, o.right() - r.W}, r.X, dx
		case dy != 0:
			cands, at, sign = []int{o.Y, o.bottom(), o.Y - r.H, o.bottom() - r.H}, r.Y, dy
		default:
			return r, false
		}
		for _, c := range cands {
			d := (c - at) * sign
			if d > 0 && d < bestDist {
				bestDist, found = d, true
				best = r
				if dx != 0 {
					best.X = c
				} else {
					best.Y = c
				}
			}
		}
	}
	return best, found
}
