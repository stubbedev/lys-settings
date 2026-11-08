package settings

import (
	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	switchInherit = iota
	switchOff
	switchOn
)

// wmSwitch is a lys-wm key that is false or an object: Inherit leaves it to
// lys-wm, Off writes false, and On shows the object's fields, the key taking
// the object once one of them is set.
type wmSwitch struct {
	*widget.Box
	slot    slot
	choice  *widget.Dropdown
	body    *widget.Box
	fields  []control
	labels  []string
	on      bool
	syncing bool
}

func newWMSwitch(k *kit, s slot, fields []cardField) *wmSwitch {
	t := locale.T()
	choices := []string{
		t.Get("settings-inherit"),
		t.Get(config.EnumLabelKey(wmToggleType, wmToggleOff)),
		t.Get(config.EnumLabelKey(wmToggleType, wmToggleOn)),
	}
	c := &wmSwitch{Box: gapBox(widget.Column), slot: s, choice: widget.NewDropdown(k.face, k.textPx(), choices, switchInherit), body: gapBox(widget.Column)}
	c.choice.OnSelect = c.choose
	labels := widget.NewSizeGroup(widget.SizeGroupHorizontal)
	for _, f := range fields {
		ctl := f.item(k, func(key string) slot { return nestedSlot(s, []string{key}) })
		row := gapBox(widget.Row)
		row.AppendAligned(labels.Add(k.label(t.Get(f.label), css.CaptionHeading, css.DimLabel)), !f.fill, widget.AlignCenter)
		row.AppendAligned(ctl, f.fill, widget.AlignCenter)
		c.body.Append(row, false)
		c.fields = append(c.fields, ctl)
		c.labels = append(c.labels, f.label)
	}
	c.AppendAligned(c.choice, false, widget.AlignStart)
	c.Append(c.body, false)
	c.refresh()
	return c
}

func (c *wmSwitch) choose(i int) {
	if c.syncing {
		return
	}
	c.on = i == switchOn
	switch i {
	case switchInherit:
		c.slot.unset()
	case switchOff:
		_ = c.slot.set(false)
	}
	c.refresh()
}

func (c *wmSwitch) refresh() {
	v := c.slot.get()
	_, object := v.(map[string]any)
	selected := switchInherit
	switch {
	case object || c.on:
		selected = switchOn
	case v == false:
		selected = switchOff
	}
	c.syncing = true
	c.choice.SetSelected(selected)
	c.syncing = false
	c.body.SetVisible(selected == switchOn)
	for _, f := range c.fields {
		f.refresh()
	}
}
