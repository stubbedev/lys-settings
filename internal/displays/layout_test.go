package displays

import (
	"slices"
	"testing"
)

func TestDraggingTheSecondOutputLeftPutsItBesideTheFirstAtTheOrigin(t *testing.T) {
	rects := []Rect{{0, 0, 1920, 1080}, {1920, 0, 1280, 720}}
	rects[1] = Rect{-1300, 100, 1280, 720}
	got := Settle(rects, 1)
	want := []Rect{{1280, 0, 1920, 1080}, {0, 100, 1280, 720}}
	if !slices.Equal(got, want) {
		t.Fatalf("settled %v, want %v", got, want)
	}
}

func TestAnOverlapOrAGapSettlesOnTheNearestEdge(t *testing.T) {
	a := Rect{0, 0, 1000, 1000}
	for name, dropped := range map[string]Rect{
		"overlapping": {900, 200, 500, 500},
		"floating":    {1300, 200, 500, 500},
	} {
		got := Settle([]Rect{a, dropped}, 1)
		if got[1] != (Rect{1000, 200, 500, 500}) || got[0] != a {
			t.Errorf("%s: settled %v", name, got)
		}
	}
}

func TestAnOutputLeftFloatingByAMoveIsBroughtBack(t *testing.T) {
	rects := []Rect{{0, 0, 100, 100}, {100, 0, 100, 100}, {200, 0, 100, 100}}
	rects[1] = Rect{0, 100, 100, 100}
	got := Settle(rects, 1)
	if disconnected(got, 0) >= 0 {
		t.Fatalf("an output floats apart in %v", got)
	}
	for i := range got {
		for j := range got {
			if i != j && got[i].overlaps(got[j]) {
				t.Fatalf("%v overlaps %v", got[i], got[j])
			}
		}
	}
}

func TestSnapLinesUpEdgesWithinTheThreshold(t *testing.T) {
	rects := []Rect{{0, 0, 1000, 1000}, {1012, 7, 500, 500}}
	if got := Snap(rects, 1, 16); got != (Rect{1000, 0, 500, 500}) {
		t.Fatalf("snapped to %v", got)
	}
	rects[1] = Rect{1040, 40, 500, 500}
	if got := Snap(rects, 1, 16); got != rects[1] {
		t.Fatalf("snapped from beyond the threshold to %v", got)
	}
}

func TestNextEdgeStepsToTheNextAlignment(t *testing.T) {
	rects := []Rect{{0, 0, 1000, 1000}, {1000, 0, 500, 500}}
	got, ok := NextEdge(rects, 1, 0, 1)
	if !ok || got != (Rect{1000, 500, 500, 500}) {
		t.Fatalf("next edge down %v, %v", got, ok)
	}
	if _, ok := NextEdge([]Rect{{0, 0, 10, 10}}, 0, 1, 0); ok {
		t.Fatal("a lone output found an edge")
	}
}

func TestLogicalDividesTheModeByTheScale(t *testing.T) {
	s := Setting{X: 5, Y: 6, Scale: 1.5, Mode: Mode{Width: 3840, Height: 2160}}
	if got := Logical(s); got != (Rect{5, 6, 2560, 1440}) {
		t.Fatalf("logical %v", got)
	}
}
