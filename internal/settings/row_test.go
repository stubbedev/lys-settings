package settings

import (
	"testing"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
)

func TestRowSaysWhyAValueWasRefused(t *testing.T) {
	k := testKit(t, "")
	const path = "styling.palette.bg"
	row := newFieldRow(k, k.store, field(path, withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newText(k, s, false)
	})))
	entry := row.ctl.(*text)
	if row.problem.Visible() {
		t.Fatal("a fresh row reports a problem")
	}
	entry.SelectAll()
	entry.Insert("#zz")
	entry.KeyAction(widget.KeyEnter, 0)
	if !row.problem.Visible() || row.problem.Text() == "" {
		t.Fatal("a refused value left no word on the row")
	}
	entry.SelectAll()
	entry.Insert("#102030")
	entry.KeyAction(widget.KeyEnter, 0)
	if row.problem.Visible() || k.store.value(path) != "#102030" {
		t.Errorf("an accepted value: problem shown %v, value %v", row.problem.Visible(), k.store.value(path))
	}
	entry.SelectAll()
	entry.Insert("#zz")
	entry.KeyAction(widget.KeyEnter, 0)
	row.refreshSource()
	row.reset.ClickAt(widget.Point{})
	if row.problem.Visible() {
		t.Error("a reset kept the refusal up")
	}
}

func TestTextCommitsWhenItLosesTheKeys(t *testing.T) {
	k := testKit(t, "")
	const path = "modules.weather.location"
	entry := newText(k, pathSlot(k.store, path), false)
	other := widget.NewSwitch(false)
	root := widget.NewBox(widget.Column, 0, 0)
	root.Append(entry, false)
	root.Append(other, false)
	router := &widget.Router{Root: root}
	if err := router.SetFocus(entry); err != nil {
		t.Fatal(err)
	}
	entry.SelectAll()
	entry.Insert("Aarhus")
	if k.store.value(path) == "Aarhus" {
		t.Fatal("typing wrote before a commit")
	}
	if err := router.SetFocus(other); err != nil {
		t.Fatal(err)
	}
	if v := k.store.value(path); v != "Aarhus" || entry.badge.Visible() {
		t.Errorf("leaving the field: value %v, unsaved badge %v", v, entry.badge.Visible())
	}
	if err := router.SetFocus(entry); err != nil {
		t.Fatal(err)
	}
	if err := router.SetFocus(other); err != nil {
		t.Fatal(err)
	}
	if src := k.store.svc.Source(path); src != config.SourceRuntime {
		t.Errorf("leaving an unedited field changed the source to %v", src)
	}
}

func TestNarrowRangesGetSlidersThatWriteWhatTheyShow(t *testing.T) {
	k := testKit(t, "")
	var ints, floats any
	whole := autoSlider(t, k, config.FieldMeta{Kind: config.FieldInt, Min: 1, Max: 4}, &ints)
	whole.Knob.SetValue(2.6)
	if ints != int64(3) || whole.Label().Text() != "3" {
		t.Errorf("an int slider wrote %#v showing %q", ints, whole.Label().Text())
	}
	frac := autoSlider(t, k, config.FieldMeta{Kind: config.FieldFloat, Min: 0, Max: 1}, &floats)
	frac.Knob.SetValue(0.123456)
	if floats != 0.12 || frac.Label().Text() != "0.12" {
		t.Errorf("a float slider wrote %#v showing %q", floats, frac.Label().Text())
	}
	wide, err := autoEditor(k, slot{get: func() any { return int64(0) }}, "x", config.FieldMeta{Kind: config.FieldInt, Min: 0, Max: sliderSpan + 1})
	if _, ok := wide.(*number); err != nil || !ok {
		t.Errorf("a range wider than a slider takes %T (%v), want the spin button", wide, err)
	}
}

func autoSlider(t *testing.T, k *kit, meta config.FieldMeta, stored *any) *slider {
	t.Helper()
	s := slot{get: func() any { return *stored }, set: func(v any) error { *stored = v; return nil }}
	c, err := autoEditor(k, s, "x", meta)
	sl, ok := c.(*slider)
	if err != nil || !ok {
		t.Fatalf("%+v takes %T (%v), want a slider", meta, c, err)
	}
	return sl
}

func TestAnUnsetOptionalFractionInheritsUntilOverridden(t *testing.T) {
	k := testKit(t, "")
	var stored any
	s := slot{get: func() any { return stored }, set: func(v any) error { stored = v; return nil }, unset: func() { stored = nil }}
	c, err := autoEditor(k, s, "x", config.FieldMeta{Kind: config.FieldFloat, Min: -1, Max: 1, Optional: true})
	opt, ok := c.(*optionalNumber)
	if err != nil || !ok {
		t.Fatalf("an optional fraction takes %T (%v), want the override editor", c, err)
	}
	if opt.on.On() || opt.spin.Enabled() || opt.spin.Value() != 0 {
		t.Fatalf("unset: override %v, spin live %v at %v; want off, inert, at 0", opt.on.On(), opt.spin.Enabled(), opt.spin.Value())
	}
	opt.on.SetOn(true)
	if stored != 0.0 {
		t.Errorf("overriding wrote %#v, want 0.0", stored)
	}
	opt.on.SetOn(false)
	if stored != nil {
		t.Errorf("inheriting again left %#v", stored)
	}
}
