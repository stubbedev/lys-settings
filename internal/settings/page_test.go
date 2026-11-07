package settings

import (
	"testing"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

func TestPageColumnIsCappedAndCentred(t *testing.T) {
	k := testKit(t, "styling:\n  scale: 1.5\n")
	spec, _ := pageByID(layout(k.store.svc.Config()), "general")
	p := buildPage(k, spec)
	p.Arrange(render.Rect{W: 3000, H: 800})
	clamp, ok := p.Children()[0].(*widget.Clamp)
	if !ok {
		t.Fatalf("the page scrolls a %T, want its clamp", p.Children()[0])
	}
	content := clamp.Children()[0].(widget.Boundser).Bounds()
	if want := pageMaxWidth * 3 / 2; content.W != want {
		t.Errorf("the column is %dpx wide, want %dpx at scale 1.5", content.W, want)
	}
	if left, right := content.X, 3000-content.X-content.W; left-right > 1 || right-left > 1 {
		t.Errorf("the column sits %dpx from the left and %dpx from the right", left, right)
	}
	p.Arrange(render.Rect{W: 600, H: 800})
	if w := clamp.Children()[0].(widget.Boundser).Bounds().W; w != 600 {
		t.Errorf("a narrow window gives the column %dpx, want all 600", w)
	}
}

func TestASectionShowsItsDescriptionOnlyWhenItHasOne(t *testing.T) {
	k, _, _, _ := wmKit(t, "")
	spec := wmPage("wm-test", "ld-monitor-symbolic",
		sectionSpec{"wm-section-gaps", []rowSpec{wmField("gaps.inner")}},
		sectionSpec{"settings-title", []rowSpec{wmField("tearing")}})
	notes := treetest.WithClass(buildPage(k, spec), "settings-section-description")
	if len(notes) != 1 {
		t.Fatalf("%d section notes, want the gaps section's alone", len(notes))
	}
	if got := notes[0].(*widget.Label).Text(); got != description("wm-section-gaps") || got == "" {
		t.Errorf("the note reads %q", got)
	}
}
