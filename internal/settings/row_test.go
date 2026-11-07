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
