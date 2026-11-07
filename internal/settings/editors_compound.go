package settings

import (
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// The editors built from several controls (editors/optional,
// editors/size, editors/surface_animation).

// enumLabels are the variants' labels: the Fluent key's text, or the
// raw value where the key is missing (variant_label).
func enumLabels(meta config.FieldMeta) []string {
	t := locale.T()
	labels := make([]string, len(meta.Variants))
	for i, v := range meta.Variants {
		labels[i] = v
		if key := config.EnumLabelKey(meta.Type, v); t.Has(key) {
			labels[i] = t.Get(key)
		}
	}
	return labels
}

// optionalEnum is optional_enum_widget: "Inherit" (unset) first, then
// the variants.
type optionalEnum struct {
	*widget.Dropdown
	k        *kit
	slot     slot
	variants []string
	syncing  bool
}

func newOptionalEnum(k *kit, s slot, meta config.FieldMeta) *optionalEnum {
	labels := append([]string{locale.T().Get("settings-inherit")}, enumLabels(meta)...)
	c := &optionalEnum{Dropdown: widget.NewDropdown(k.face, k.textPx(), labels, 0), k: k, slot: s, variants: meta.Variants}
	c.OnSelect = func(i int) {
		switch {
		case c.syncing:
		case i == 0:
			c.slot.unset()
		case i <= len(c.variants):
			_ = c.slot.set(c.variants[i-1])
		}
	}
	c.refresh()
	return c
}

func (c *optionalEnum) refresh() {
	v, _ := c.slot.get().(string)
	c.syncing = true
	c.SetSelected(slices.Index(c.variants, v) + 1)
	c.syncing = false
}

// optionalNumber is an optional number as one spin button: empty and
// reading "Inherited" while unset, a value typed or stepped (from the
// fallback) setting it, and emptying it inheriting again.
type optionalNumber struct {
	*widget.SpinButton
	k     *kit
	slot  slot
	whole bool
}

func newOptionalNumber(k *kit, s slot, lo, hi, step, fallback float64, digits int) *optionalNumber {
	c := &optionalNumber{SpinButton: widget.NewSpinButton(k.face, k.textPx(), 0, lo, hi, step, digits), k: k, slot: s, whole: digits == 0}
	c.SetOptional(locale.T().Get("settings-inherited"))
	c.SetValue(fallback)
	c.OnValueChanged = c.write
	c.OnCleared = s.unset
	c.refresh()
	return c
}

// write stores a whole number (float fields take it as one), or the
// fraction when the spin shows digits.
func (c *optionalNumber) write(v float64) {
	if c.whole {
		_ = c.slot.set(int64(math.Round(v)))
		return
	}
	_ = c.slot.set(v)
}

func (c *optionalNumber) refresh() {
	switch n := c.slot.get().(type) {
	case int64:
		c.SetValue(float64(n))
	case float64:
		c.SetValue(n)
	default:
		c.Clear()
	}
}

// optionalSpin is number_u32_optional's row: the override switch and a
// spin over [lo, hi] stepping by step, starting at fallback.
func optionalSpin(lo, hi, step, fallback float64) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newOptionalNumber(k, s, lo, hi, step, fallback, 0)
	})
}

// optionalFloat is optionalSpin for a fractional value: digits decimals
// and a start at 0.
func optionalFloat(lo, hi, step float64, digits int) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newOptionalNumber(k, s, lo, hi, step, 0, digits)
	})
}

// Size modes (editors/size).
const (
	sizeScale = iota
	sizePx
	// remBasePx is REM_BASE_PX, the rem a scale multiplies.
	remBasePx = 16.0
	// maxPixels bounds a size in pixels; pixelsFallback is where an optional
	// pixel length starts once stepped.
	maxPixels      = 10000
	pixelsFallback = 400
)

// sizeEditor is size_with_base: a scale-or-pixels dropdown and the
// spin. Switching the mode converts the value through the base (a
// scale of base rem), so the size stays put.
type sizeEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	basePx  float64
	mode    *widget.Dropdown
	spin    *widget.SpinButton
	syncing bool
}

func newSizeEditor(k *kit, s slot, baseRem float64) *sizeEditor {
	t := locale.T()
	c := &sizeEditor{Box: gapBox(widget.Row), k: k, slot: s, basePx: baseRem * remBasePx}
	c.mode = widget.NewDropdown(k.face, k.textPx(), []string{t.Get("settings-size-scale"), t.Get("settings-size-px")}, sizeScale)
	c.spin = widget.NewSpinButton(k.face, k.textPx(), 0, 0, maxPixels, 0.05, 2)
	c.AppendAligned(c.mode, false, widget.AlignCenter)
	c.AppendAligned(c.spin, false, widget.AlignCenter)
	c.mode.OnSelect = func(i int) {
		if c.syncing {
			return
		}
		if c.spin.Empty() {
			c.step(i == sizePx)
			return
		}
		v := c.spin.Value()
		if i == sizePx {
			v = math.Round(v * c.basePx)
		} else if c.basePx > 0 {
			v /= c.basePx
		}
		c.configure(i == sizePx, v)
		c.commit()
	}
	c.spin.OnValueChanged = func(float64) { c.commit() }
	c.refresh()
	return c
}

// configure sets the spin for a mode (configure_spin_for_mode): whole
// pixels, or a scale in hundredths, then shows v.
func (c *sizeEditor) configure(px bool, v float64) {
	c.step(px)
	c.spin.SetValue(v)
}

// step sets the spin's step and digits for a mode: whole pixels, or a
// scale in hundredths.
func (c *sizeEditor) step(px bool) {
	if px {
		c.spin.SetStep(1)
		c.spin.SetDigits(0)
		return
	}
	c.spin.SetStep(0.05)
	c.spin.SetDigits(2)
}

func (c *sizeEditor) commit() {
	v := c.spin.Value()
	if c.mode.Selected() == sizePx {
		_ = c.slot.set(strconv.FormatFloat(math.Round(v), 'f', 0, 64) + "px")
		return
	}
	_ = c.slot.set(v)
}

// refresh shows the stored size: a number is a scale, a "<n>px" string
// pixels.
func (c *sizeEditor) refresh() {
	c.syncing = true
	defer func() { c.syncing = false }()
	switch v := c.slot.get().(type) {
	case nil:
		c.mode.SetSelected(sizeScale)
		c.configure(false, 1)
		c.spin.Clear()
	case float64:
		c.mode.SetSelected(sizeScale)
		c.configure(false, v)
	case int64:
		c.mode.SetSelected(sizeScale)
		c.configure(false, float64(v))
	case string:
		px, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		if err != nil {
			return
		}
		c.mode.SetSelected(sizePx)
		c.configure(true, math.Round(px))
	}
}

// pixelLength is a pixel length ("400px") as the whole pixels a spin edits.
func pixelLength(s slot) slot {
	return slot{
		get: func() any {
			text, ok := s.get().(string)
			if !ok {
				return nil
			}
			px, err := strconv.ParseFloat(strings.TrimSuffix(text, "px"), 64)
			if err != nil {
				log.Printf("settings: %q is no pixel length", text)
				return nil
			}
			return int64(math.Round(px))
		},
		set: func(v any) error {
			px, ok := v.(int64)
			if !ok {
				return fmt.Errorf("settings: %v is no whole number of pixels", v)
			}
			return s.set(strconv.FormatInt(px, 10) + "px")
		},
		unset: s.unset,
	}
}

// sizeBase is size_with_base for a row: the base rem a scale of 1 is.
func sizeBase(rem float64) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newSizeEditor(k, s, rem)
	})
}

// surfaceAnimationRows are surface_animation_rows for the [animations]
// surface at prefix: the optional enter and exit transitions and
// durations, each its own field.
func surfaceAnimationRows(prefix string) []rowSpec {
	duration := optionalSpin(0, maxDurationMS, durationStepMS, durationFallbackMS)
	return []rowSpec{
		field(prefix+".enter", withKey("settings-animations-enter")),
		field(prefix+".exit", withKey("settings-animations-exit")),
		field(prefix+".enter-duration", withKey("settings-animations-enter-duration"), duration),
		field(prefix+".exit-duration", withKey("settings-animations-exit-duration"), duration),
	}
}

// Animation duration bounds (pages/animations, editors/surface_animation).
const (
	maxDurationMS      = 100_000
	durationStepMS     = 10
	durationFallbackMS = 200
)

// newOptionalSize is dropdown_size's size_control: the size editor over
// an optional size, its spin empty and reading "Inherited" while unset; a
// value typed or stepped (from a scale of 1) sets it, emptying it unsets.
func newOptionalSize(k *kit, s slot, baseRem float64) *sizeEditor {
	c := newSizeEditor(k, s, baseRem)
	c.spin.SetOptional(locale.T().Get("settings-inherited"))
	c.spin.OnCleared = s.unset
	c.refresh()
	return c
}

// dropdownSizeRows are dropdown_size_rows for the dropdown at path:
// its optional width and height.
func dropdownSizeRows(path string) []rowSpec {
	size := withEditor(func(k *kit, s slot, _ config.FieldMeta) control { return newOptionalSize(k, s, 1) })
	return []rowSpec{
		field(path+".width", withKey("settings-dropdown-width"), size),
		field(path+".height", withKey("settings-dropdown-height")),
	}
}
