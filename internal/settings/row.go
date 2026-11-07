package settings

import (
	"log"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/shell/apptheme"
	"github.com/stubbedev/lys-shell/shell/widgets"
	"github.com/stubbedev/lys-shell/styling"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// hiddenPriority puts the reset button's hidden opacity above every
// stylesheet: Rust sets the widget's opacity, which no CSS overrides.
const hiddenPriority = apptheme.Priority + 1

// Row text limits (row/mod.rs).
const (
	descriptionMaxChars        = 120
	descriptionTooltipMinChars = 80
)

// kit builds the window's widgets: the text face every label starts
// from (the stylesheet then sizes and weighs it) and the store the
// controls write to.
type kit struct {
	face render.Font
	// mono and monoVariants are the code views' monospace face and its
	// bold and italic faces.
	mono         render.Font
	monoVariants widget.VariantFunc
	store        store
	// invoke runs a function on the loop (the slider's trailing commit).
	invoke func(func())
	// pickers opens popovers and dialogs; nil without a window.
	pickers pickers
	// wm is lys-wm's config once a page of it was opened; onWMChange
	// refreshes the window when a change to it settled.
	wm         *wmBackend
	onWMChange func()
	// background runs blocking work off the loop (a D-Bus query);
	// nil runs it in place, the tests' synchronous loop.
	background func(func())
}

// async runs work off the loop and hands its result to done on it.
func async[T any](k *kit, work func() T, done func(T)) {
	if k.background == nil {
		done(work())
		return
	}
	k.background(func() {
		v := work()
		k.invoke(func() { done(v) })
	})
}

// palette is the render palette the stored styling resolves to, the
// colors the token swatches show; a provider that fails is logged and
// leaves the configured palette.
func (k *kit) palette() styling.Palette {
	p, err := styling.ConfigPalette(k.store.svc.Config().Styling)
	if err != nil {
		log.Printf("settings: palette: %v", err)
	}
	return *p
}

// textPx is the body size at the styling scale, which every label and
// control of the window takes.
func (k *kit) textPx() float64 {
	return widgets.TextPx(styling.Body, float64(k.store.svc.Config().Styling.Scale))
}

// label is a styled label; color, size and weight come from the
// stylesheet.
func (k *kit) label(text string, classes ...string) *widget.Label {
	l := widget.NewLabel(k.face, k.textPx(), text, 0)
	l.AddClass(classes...)
	return l
}

// entry is a text field sized the way a GtkEntry is: a fixed width
// the text pans inside, whatever it holds.
func (k *kit) entry() *widget.Entry {
	e := widget.NewEntry(k.face, k.textPx(), 0)
	e.SetTextWidth(widget.GTKTextWidth)
	return e
}

// searchField is a picker's search entry: an entry showing the
// placeholder key's text that hands every edit to onChange.
func (k *kit) searchField(placeholder string, onChange func(query string)) *widget.Entry {
	e := k.entry()
	e.SetPlaceholder(locale.T().Get(placeholder))
	e.OnChanged = onChange
	return e
}

// icon is a theme icon the stylesheet sizes and tints (an image node).
func (k *kit) icon(name string, classes ...string) *widget.Icon {
	ic := widget.NewThemeIcon(name, 16)
	ic.AddClass(classes...)
	return ic
}

// gapBox lines widgets up along axis a theme's spacing apart, the one
// gap between the controls of a group, the fields of a form and the
// rows of a list.
func gapBox(axis widget.Axis) *widget.Box {
	return widget.NewBox(axis, widget.Current().Spacing, 0)
}

// browseButton is the outline folder button asking the file chooser,
// pick getting the chosen path.
func (k *kit) browseButton(pick func(path string)) *widget.Button {
	return k.chooserButton("settings-browse-file", func(p pickers) { p.openFile(pick) })
}

func (k *kit) folderButton(pick func(path string)) *widget.Button {
	return k.chooserButton("settings-browse-folder", func(p pickers) { p.openFolder(pick) })
}

func (k *kit) chooserButton(tooltipKey string, open func(pickers)) *widget.Button {
	return k.iconButton("ld-folder-open-symbolic", tooltipKey, func() {
		if k.pickers != nil {
			open(k.pickers)
		}
	}, "browse", css.Outline)
}

// iconButton is an icon-only button: its tooltip, the tooltipKey message,
// is the only text naming what it does, so it cannot be left out.
func (k *kit) iconButton(icon, tooltipKey string, onClick func(), classes ...string) *widget.Button {
	b := k.button(k.icon(icon), onClick, append(classes, css.Icon)...)
	b.SetTooltip(locale.T().Get(tooltipKey))
	return b
}

// button wraps child in a plain button carrying classes.
func (k *kit) button(child widget.Widget, onClick func(), classes ...string) *widget.Button {
	b := widget.NewButton(child, 0, 0)
	b.AddClass(classes...)
	b.OnClick = onClick
	return b
}

// badge is lys-ui's quiet badge reading text; a status class
// (success, warning, error, info) tints it.
func (k *kit) badge(text string, classes ...string) *widget.Badge {
	b := widget.NewBadge(k.face, text)
	b.SetVariant(widget.BadgeSecondary)
	b.AddClass(classes...)
	return b
}

// badgeStatuses are the status classes a badge's tint comes from.
var badgeStatuses = []string{"success", "warning", "error", "info"}

// control is a row's editor: its widget, and refresh re-reading the
// value after the config changed.
type control interface {
	widget.Widget
	refresh()
}

// settingRow is SettingRow: the label with its unit and source badge,
// the description, the reset button and the control.
type settingRow struct {
	*widget.Box
	kit   *kit
	be    backend
	path  string
	ctl   control
	badge *widget.Badge
	reset *widget.Button
	// problem says why the last value the control wrote was refused.
	problem *widget.Label
	// action is the row's RowBehavior::Action.
	action bool
}

// newFieldRow is the row for spec with its field's control, whose refused
// writes the row reports under its label.
func newFieldRow(k *kit, be backend, spec rowSpec) *settingRow {
	var r *settingRow
	report := func(err error) {
		if r != nil {
			r.showProblem(err)
		}
	}
	r = newSettingRow(k, be, spec, fieldControl(k, reportingBackend{be, report}, spec))
	return r
}

// reportingBackend hands report the outcome of every write through its
// slots: the error a refused value gets, nil once one lands.
type reportingBackend struct {
	backend
	report func(error)
}

func (b reportingBackend) slot(path string) slot {
	s := b.backend.slot(path)
	return slot{
		get: s.get,
		set: func(v any) error {
			err := s.set(v)
			b.report(err)
			return err
		},
		unset: func() {
			s.unset()
			b.report(nil)
		},
	}
}

// showProblem shows why a value was refused, or hides the line for nil.
func (r *settingRow) showProblem(err error) {
	r.problem.SetVisible(err != nil)
	if err != nil {
		r.problem.SetText(err.Error())
	}
}

// newSettingRow lays a row out (row/mod.rs view): a full-width control
// stacks under the text, with the reset button beside the label.
func newSettingRow(k *kit, be backend, spec rowSpec, ctl control) *settingRow {
	t := locale.T()
	axis := widget.Row
	if spec.fullWidth {
		axis = widget.Column
	}
	th := widget.Current()
	r := &settingRow{kit: k, be: be, path: spec.path, ctl: ctl, action: spec.action, Box: gapBox(axis)}
	r.SetElement("item")
	r.AddClass(css.Small, "setting-row")

	info := widget.NewBox(widget.Column, th.Spacing/4, 0)
	labelRow := widget.NewBox(widget.Row, th.Spacing/2, 0)
	title := k.label(t.Get(spec.labelKey()), "setting-label")
	title.SetEllipsize(widget.EllipsizeEnd)
	title.SetTooltip(t.Get(spec.labelKey()))
	labelRow.AppendAligned(title, false, widget.AlignCenter)
	if spec.unit != "" {
		labelRow.AppendAligned(k.label("("+spec.unit+")", "setting-unit", css.Caption, css.DimLabel, css.Monospace), false, widget.AlignCenter)
	}
	r.badge = k.badge("")
	labelRow.Append(r.badge, false)
	if d, ok := ctl.(interface{ dirtyBadge() *widget.Badge }); ok {
		labelRow.Append(d.dirtyBadge(), false)
	}
	info.Append(labelRow, false)
	if desc := t.Attr(spec.labelKey(), "description"); desc != "" {
		dl := k.label(desc, "setting-description", css.Caption, css.DimLabel)
		dl.SetEllipsize(widget.EllipsizeEnd)
		dl.SetMaxWidthChars(descriptionMaxChars)
		if len(desc) > descriptionTooltipMinChars {
			dl.SetTooltip(desc)
		}
		info.Append(dl, false)
	}
	r.problem = k.label("", "setting-problem", css.Caption, css.Error)
	r.problem.SetWrap(true)
	r.problem.SetVisible(false)
	info.Append(r.problem, false)

	r.reset = k.iconButton("ld-rotate-ccw-symbolic", "settings-reset-setting", func() {
		be.reset(spec.path)
		r.showProblem(nil)
	}, "setting-reset", css.Ghost)
	slot := widget.NewBox(widget.Row, 0, 0)
	slot.AddClass("setting-control")
	slot.AppendAligned(ctl, spec.fullWidth, widget.AlignCenter)

	if spec.fullWidth {
		r.AddClass("vertical")
		labelRow.Append(widget.NewSpacer(0, 0), true)
		labelRow.Append(r.reset, false)
		r.Append(info, false)
		r.Append(slot, false)
	} else {
		r.AppendAligned(info, true, widget.AlignCenter)
		r.AppendAligned(r.reset, false, widget.AlignCenter)
		r.AppendAligned(slot, false, widget.AlignCenter)
	}
	r.refreshSource()
	return r
}

// refresh re-reads the source and the control's value.
func (r *settingRow) refresh() {
	r.refreshSource()
	r.ctl.refresh()
}

// refreshSource shows the badge and arms the reset button
// (update_source_info): the button keeps its place, invisible and
// inert, while there is no runtime override.
func (r *settingRow) refreshSource() {
	src := sourceInfo{}
	if !r.action {
		src = r.be.source(r.path)
	}
	r.badge.SetVisible(src.badge)
	r.badge.SetText(src.label)
	r.badge.SetTooltip(src.tooltip)
	r.badge.RemoveClass(badgeStatuses...)
	if src.class != "" {
		r.badge.AddClass(src.class)
	}
	r.reset.SetEnabled(src.reset)
	if src.reset {
		r.reset.SetInlineStyle("")
	} else {
		r.reset.SetInlineStylePriority("opacity: 0;", hiddenPriority)
	}
}
