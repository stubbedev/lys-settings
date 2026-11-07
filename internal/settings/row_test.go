package settings

import (
	"fmt"
	"testing"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
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
		t.Fatalf("an optional fraction takes %T (%v), want the optional spin", c, err)
	}
	if !opt.Empty() || opt.Value() != 0 {
		t.Fatalf("unset: empty %v from %v; want inherited, stepping from 0", opt.Empty(), opt.Value())
	}
	opt.KeyAction(widget.KeyUp, 0)
	if stored != 0.01 {
		t.Errorf("a step wrote %#v, want 0.01", stored)
	}
	emptySpin(opt.SpinButton)
	if stored != nil {
		t.Errorf("inheriting again left %#v", stored)
	}
}

func TestRowResetsAConfigValueToTheDefault(t *testing.T) {
	k := testKit(t, "bar:\n  dropdown-shadow: false\n")
	row := newFieldRow(k, k.store, field("bar.dropdown-shadow"))
	if !row.reset.Enabled() || row.reset.InlineStyle() != "" || row.reset.TooltipText() != locale.T().Get("settings-reset-setting") {
		t.Fatalf("a config value off the default: reset live %v, style %q, tooltip %q", row.reset.Enabled(), row.reset.InlineStyle(), row.reset.TooltipText())
	}
	row.reset.ClickAt(widget.Point{})
	row.refresh()
	if !row.ctl.(*toggle).On() || k.store.value("bar.dropdown-shadow") != true {
		t.Fatal("the reset did not bring the default back over the config")
	}
	if row.reset.Enabled() || row.reset.InlineStyle() == "" {
		t.Error("a row at its default keeps its reset shown")
	}
}

func TestSmallEnumsAreSegmentedControls(t *testing.T) {
	k := testKit(t, "")
	var stored any = "b"
	writes := 0
	s := slot{get: func() any { return stored }, set: func(v any) error { stored = v; writes++; return nil }}
	c, err := autoEditor(k, s, "x", config.FieldMeta{Kind: config.FieldEnum, Variants: []string{"a", "b", "c"}})
	seg, ok := c.(*segmentedEnum)
	if err != nil || !ok {
		t.Fatalf("a three-variant enum takes %T (%v), want segments", c, err)
	}
	if seg.Active() != 1 || writes != 0 {
		t.Fatalf("shows segment %d after %d writes, want the stored b silently", seg.Active(), writes)
	}
	seg.KeyAction(widget.KeyRight, 0)
	if stored != "c" || writes != 1 {
		t.Errorf("Right wrote %v (%d writes)", stored, writes)
	}
	stored = "a"
	seg.refresh()
	if seg.Active() != 0 || writes != 1 {
		t.Errorf("refresh: segment %d, %d writes", seg.Active(), writes)
	}
	many := config.FieldMeta{Kind: config.FieldEnum, Variants: []string{"a", "b", "c", "d", "e"}}
	if c, _ := autoEditor(k, s, "x", many); c == nil || fmt.Sprintf("%T", c) != "*settings.enumSelect" {
		t.Errorf("five variants take %T, want the dropdown", c)
	}
	long := config.FieldMeta{Kind: config.FieldEnum, Variants: []string{"a-very-long-variant", "another-long-variant"}}
	if c, _ := autoEditor(k, s, "x", long); fmt.Sprintf("%T", c) != "*settings.enumSelect" {
		t.Errorf("long labels take %T, want the dropdown", c)
	}
}
