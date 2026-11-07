package settings

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
)

func desktopAccent(t *testing.T, k *kit) render.Color {
	t.Helper()
	p := k.palette()
	c, ok := p.Token(config.TokenAccent)
	if !ok {
		t.Fatal("the shell palette has no accent")
	}
	return c
}

func borderPicker(t *testing.T, f *fakePickers, c *borderEditor) *widget.SwatchPicker {
	t.Helper()
	f.content = nil
	c.colors.add.OnClick()
	p, ok := f.content.(*widget.SwatchPicker)
	if !ok || f.anchor != widget.Widget(c.colors.add) {
		t.Fatalf("+ opened %T beside %T, not the colour picker beside itself", f.content, f.anchor)
	}
	return p
}

func TestABorderAtItsDefaultShowsTheAccentItInherits(t *testing.T) {
	k, w, _, files := wmKit(t, "")
	c := fieldControl(k, w, wmBorder("borders.active")).(*borderEditor)
	if colors := c.colors.values(); !slices.Equal(colors, []any{"accent"}) {
		t.Fatalf("shows %v, not the accent lys-wm uses", colors)
	}
	item := c.colors.items[0].(*colorItem)
	if got := item.names.entry.Text(); got != "accent" {
		t.Errorf("the colour reads %q", got)
	}
	if got, want := item.swatch.Color(), desktopAccent(t, k); got != want {
		t.Errorf("the swatch shows %v, not the desktop accent %v", got, want)
	}
	if src := w.source("borders.active"); src.badge || src.reset {
		t.Errorf("the default reads as changed: %+v", src)
	}
	noFile(t, files.runtime)
}

func TestAddingABorderColourPicksAValidOneAndCancellingAddsNothing(t *testing.T) {
	k, w, fake, files := wmKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	c := fieldControl(k, w, wmBorder("borders.active")).(*borderEditor)
	if p := borderPicker(t, f, c); p.Color() != desktopAccent(t, k) {
		t.Errorf("the picker opens at %v, not the border's colour", p.Color())
	}
	noFile(t, files.runtime)
	if fake.checks != 0 || len(c.colors.items) != 1 {
		t.Fatalf("a dismissed pick wrote or added: %d checks, %d colours", fake.checks, len(c.colors.items))
	}
	p := borderPicker(t, f, c)
	p.OnEdited(render.RGBA(0x11, 0x22, 0x33, 0xff))
	p.OnPicked(render.RGBA(0x44, 0x55, 0x66, 0xff))
	if f.closed != 1 {
		t.Errorf("a pick closed the picker %d times", f.closed)
	}
	got, _ := w.value("borders.active").(map[string]any)
	if colors, _ := got["colors"].([]any); !slices.Equal(colors, []any{"accent", "#445566"}) {
		t.Fatalf("wrote %#v; the edit and the pick are one new colour", got)
	}
	if text := fileText(t, files.runtime); strings.Contains(text, `""`) {
		t.Fatalf("an empty colour reached the overrides: %q", text)
	}
	c.refresh()
	if len(c.colors.items) != 2 || !c.angled.Visible() {
		t.Fatalf("%d colours, angle shown %v", len(c.colors.items), c.angled.Visible())
	}
}

func TestABorderTakesNoMoreColoursThanLysWM(t *testing.T) {
	k, w, _, _ := wmKitWith(t, "", "borders:\n  active:\n    colors: [red, blue, accent]\n    angle: 0\n")
	c := fieldControl(k, w, wmBorder("borders.active")).(*borderEditor)
	if c.colors.add.Visible() {
		t.Fatal("+ is offered at lys-wm's most colours")
	}
	c.colors.remove(c.colors.items[2])
	if !c.colors.add.Visible() {
		t.Fatal("+ stays hidden below the most colours")
	}
}

func TestARefusedBorderColourPutsTheRowBack(t *testing.T) {
	k, w, fake, files := wmKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	row := newFieldRow(k, w, wmBorder("borders.active"))
	k.onWMChange = row.refresh
	c := row.ctl.(*borderEditor)
	fake.reject = "borders.active: refused"
	borderPicker(t, f, c).OnPicked(render.RGBA(0x44, 0x55, 0x66, 0xff))
	if colors := c.colors.values(); !slices.Equal(colors, []any{"accent"}) {
		t.Fatalf("after the refusal the row shows %v", colors)
	}
	if w.class != "error" || !strings.Contains(w.notice, "refused") {
		t.Fatalf("the refusal is not shown: %q %q", w.class, w.notice)
	}
	noFile(t, files.runtime)
}

func TestThePaletteRowHoldsOnlyTheUsersNames(t *testing.T) {
	k, w, _, files := wmKit(t, "")
	c := fieldControl(k, w, wmPaletteRow()).(*listEditor)
	if len(c.items) != 0 {
		t.Fatalf("the built-in names fill the row: %d", len(c.items))
	}
	c.add.OnClick()
	item := c.items[0].(mapItem)
	item.key.SetText("mine")
	item.value.SetText("#123456")
	c.commit()
	if got := fileText(t, files.runtime); got != "palette:\n  mine: '#123456'\n" {
		t.Fatalf("wrote %q", got)
	}
	var names []string
	for _, ch := range wmPaletteNames(k) {
		names = append(names, ch.value)
	}
	for _, want := range []string{"accent", "blue", "red", "mine"} {
		if !slices.Contains(names, want) {
			t.Errorf("a colour cannot take %q: %v", want, names)
		}
	}
}
