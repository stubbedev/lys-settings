package settings

import (
	"math"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// lys-wm keys that take one of two forms: gaps.outer, one number or one
// per side, and a border, one colour or a gradient.

// sideWidthChars fits a side's spin to a gap of up to four digits, so the
// four sides sit on one line of the narrowest page.
const sideWidthChars = 4

// sideKeys label gaps.outer's sides in the order lys-wm lists them.
var sideKeys = [4]string{"wm-side-top", "wm-side-right", "wm-side-bottom", "wm-side-left"}

// sidesEditor is the override switch and a spin per side: equal sides
// write one number, others the four; off leaves the key to lys-wm.
type sidesEditor struct {
	*widget.Box
	slot    slot
	on      *widget.Switch
	spins   [4]*widget.SpinButton
	syncing bool
}

func newSidesEditor(k *kit, s slot) *sidesEditor {
	t := locale.T()
	c := &sidesEditor{Box: gapBox(widget.Row), slot: s}
	c.on = widget.NewSwitch(false)
	c.on.AddClass("inherit-switch")
	c.on.SetTooltip(t.Get("settings-override"))
	c.on.OnChanged = func(on bool) {
		c.enable(on)
		if c.syncing {
			return
		}
		if on {
			c.write(0)
			return
		}
		c.slot.unset()
	}
	c.AppendAligned(c.on, false, widget.AlignCenter)
	for i := range c.spins {
		spin := widget.NewSpinButton(k.face, k.textPx(), 0, 0, math.MaxInt32, 1, 0)
		spin.SetTooltip(t.Get(sideKeys[i]))
		spin.SetWidthChars(sideWidthChars, sideWidthChars)
		spin.OnValueChanged = c.write
		c.spins[i] = spin
		c.AppendAligned(spin, false, widget.AlignCenter)
	}
	c.refresh()
	return c
}

func (c *sidesEditor) enable(on bool) {
	for _, s := range c.spins {
		s.SetEnabled(on)
	}
}

func (c *sidesEditor) write(float64) {
	if c.syncing {
		return
	}
	sides := make([]any, len(c.spins))
	same := true
	for i, s := range c.spins {
		sides[i] = int64(math.Round(s.Value()))
		same = same && sides[i] == sides[0]
	}
	if same {
		_ = c.slot.set(sides[0])
		return
	}
	_ = c.slot.set(sides)
}

func (c *sidesEditor) refresh() {
	v := c.slot.get()
	sides := [4]float64{}
	switch v := v.(type) {
	case int64:
		sides = [4]float64{float64(v), float64(v), float64(v), float64(v)}
	case int:
		sides = [4]float64{float64(v), float64(v), float64(v), float64(v)}
	case []any:
		for i := range min(len(v), len(sides)) {
			sides[i] = toFloat(v[i])
		}
	}
	c.syncing = true
	c.on.SetOn(v != nil)
	c.enable(v != nil)
	for i, s := range c.spins {
		s.SetValue(sides[i])
	}
	c.syncing = false
}

// toFloat is a decoded YAML number as a float, 0 for anything else.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

// maxBorderAngle bounds a gradient's angle: a full turn.
const maxBorderAngle = 360

// borderEditor is a border's colours, one or a gradient's, and the
// gradient's angle; no colour leaves the key to lys-wm.
type borderEditor struct {
	*widget.Box
	colors *listEditor
	angle  *slider
	angled *widget.Box
	slot   slot
}

func newBorderEditor(k *kit, s slot) *borderEditor {
	c := &borderEditor{Box: gapBox(widget.Column), slot: s}
	c.colors = newListEditor(k, slot{
		get: func() any { colors, _ := c.parts(); return colors },
		set: func(v any) error { colors, _ := v.([]any); _, angle := c.parts(); return c.write(colors, angle) },
	}, listSpec{
		addKey: "wm-border-add-colour", rowClass: "string-list-row", reorder: true, blank: "",
		item: func(k *kit, v any, changed func()) listItem {
			value, _ := v.(string)
			item := &colorItem{value: value}
			item.wmColorEditor = newWMColorEditor(k, slot{
				get: func() any { return item.value },
				set: func(nv any) error { item.value, _ = nv.(string); changed(); return nil },
			})
			return item
		},
	})
	c.angle = newSlider(k, slot{
		get: func() any { _, angle := c.parts(); return angle },
		set: func(v any) error { colors, _ := c.parts(); return c.write(colors, toFloat(v)) },
	}, 0, maxBorderAngle, true, func(v float64) string { return decimals(0)(v) + "°" })
	c.angled = gapBox(widget.Row)
	c.angled.AppendAligned(k.label(locale.T().Get("wm-border-angle"), css.DimLabel), false, widget.AlignCenter)
	c.angled.Append(c.angle, true)
	c.Append(c.colors, false)
	c.Append(c.angled, false)
	c.refresh()
	return c
}

// colorItem is a gradient's colour.
type colorItem struct {
	*wmColorEditor
	value string
}

func (i *colorItem) itemValue() any { return i.value }

// parts are the border's colours and angle, whichever form it takes.
func (c *borderEditor) parts() ([]any, float64) {
	switch v := c.slot.get().(type) {
	case string:
		return []any{v}, 0
	case map[string]any:
		colors, _ := v["colors"].([]any)
		return colors, toFloat(v["angle"])
	}
	return nil, 0
}

// write stores one colour at no angle as the colour itself, more as a
// gradient, and none as no value.
func (c *borderEditor) write(colors []any, angle float64) error {
	switch {
	case len(colors) == 0:
		c.slot.unset()
		return nil
	case len(colors) == 1 && angle == 0:
		return c.slot.set(colors[0])
	}
	return c.slot.set(map[string]any{"colors": colors, "angle": int64(math.Round(angle))})
}

func (c *borderEditor) refresh() {
	colors, _ := c.parts()
	c.angled.SetVisible(len(colors) > 1)
	c.colors.refresh()
	c.angle.refresh()
}

// wmSides is the gaps.outer row.
func wmSides(path string) rowSpec {
	return wmField(path, func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newSidesEditor(k, s) }
	})
}

// wmBorder is a border colour row.
func wmBorder(path string) rowSpec {
	return wmField(path, func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newBorderEditor(k, s) }
	})
}
