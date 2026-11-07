package settings

import (
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// pageSpec is LeafEntry with its PageSpec: the sidebar entry and the
// page's sections.
type pageSpec struct {
	id, navKey, icon string
	// header titles the page; its breadcrumb attribute heads it.
	header   string
	sections []sectionSpec
	// footer, when set, builds what follows the sections (the greeter's
	// apply button).
	footer func(k *kit) widget.Widget
	// wm says the rows are lys-wm's, not lys-shell's.
	wm bool
}

// sectionSpec is SectionSpec: a titled group of rows.
type sectionSpec struct {
	title string
	rows  []rowSpec
}

// rowSpec is SettingRowInit, by path: the label key defaults to the
// field's (config.I18nKey), the editor to the one its type calls for.
type rowSpec struct {
	path      string
	key       string
	editor    editorFunc
	fullWidth bool
	unit      string
	// action marks a row that acts rather than holds a value
	// (RowBehavior::Action): no source badge, no reset.
	action bool
}

// labelKey is the row's label key.
func (r rowSpec) labelKey() string {
	if r.key != "" {
		return r.key
	}
	if key, ok := config.I18nKey(r.path); ok {
		return key
	}
	return "missing-i18n-key"
}

// rowOpt adjusts a row.
type rowOpt func(*rowSpec)

// field is a row for the config field at path.
func field(path string, opts ...rowOpt) rowSpec {
	r := rowSpec{path: path}
	for _, o := range opts {
		o(&r)
	}
	return r
}

// fields is a row per path, each with its auto editor.
func fields(paths ...string) []rowSpec {
	rows := make([]rowSpec, len(paths))
	for i, p := range paths {
		rows[i] = field(p)
	}
	return rows
}

// withKey names the row's label key.
func withKey(key string) rowOpt { return func(r *rowSpec) { r.key = key } }

// withEditor names the row's editor.
func withEditor(e editorFunc) rowOpt { return func(r *rowSpec) { r.editor = e } }

// settingsPage is SettingsPage: the header and the sections in a
// scroll, refreshing its rows when the config changes.
type settingsPage struct {
	*widget.Scroll
	rows   []*settingRow
	status *wmStatus
	// revealing is the row the next layout scrolls into view.
	revealing *settingRow
}

// buildPage lays out a page (pages/layout.rs).
func buildPage(k *kit, spec pageSpec) *settingsPage {
	t := locale.T()
	th := widget.Current()
	content := widget.NewBox(widget.Column, 3*th.Spacing, 3*th.Padding)
	content.AddClass("settings-page")
	header := widget.NewBox(widget.Column, th.Spacing/2, 0)
	header.AddClass("settings-page-header")
	header.Append(k.label(t.Attr(spec.header, "breadcrumb"), "settings-breadcrumb", css.Caption, css.DimLabel), false)
	header.Append(k.label(t.Get(spec.header), "settings-page-title", css.H4), false)
	content.Append(header, false)
	content.Append(widget.NewSeparator(widget.Horizontal), false)
	p := &settingsPage{}
	var be backend = k.store
	if spec.wm {
		w, err := k.wmBackend()
		if err != nil {
			content.Append(k.label(err.Error(), "error", css.Caption), false)
			p.Scroll = widget.NewScroll(k.pageColumn(content))
			p.VerticalOnly, p.ShowBars, p.FillY = true, true, true
			return p
		}
		be = w
		p.status = newWMStatus(k, w)
		content.Append(p.status, false)
	}
	for _, s := range spec.sections {
		section := gapBox(widget.Column)
		section.AddClass("settings-section")
		title := k.label(t.Get(s.title), "settings-section-title", css.CaptionHeading, css.DimLabel)
		section.Append(title, false)
		rows := widget.NewItemGroup()
		group := widget.NewBox(widget.Column, 0, 0)
		group.AddClass(css.Card, "settings-group")
		group.Append(rows, false)
		for _, rs := range s.rows {
			row := newFieldRow(k, be, rs)
			rows.Append(row)
			p.rows = append(p.rows, row)
		}
		section.Append(group, false)
		content.Append(section, false)
	}
	if spec.footer != nil {
		content.Append(spec.footer(k), false)
	}
	p.Scroll = widget.NewScroll(k.pageColumn(content))
	p.VerticalOnly, p.ShowBars, p.FillY = true, true, true
	return p
}

// refresh re-reads every row.
func (p *settingsPage) refresh() {
	for _, r := range p.rows {
		r.refresh()
	}
	if p.status != nil {
		p.status.refresh()
	}
}

// spin is a number row with explicit bounds (number_f64,
// number_u32_range, number_newtype).
func spin(lo, hi, step float64, digits int) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newNumber(k, s, lo, hi, step, digits)
	})
}

// pageMaxWidth caps a page's column at the unscaled styling: wider
// windows centre it instead of stretching rows apart from their controls.
const pageMaxWidth = 760

// pageColumn centres a page's content at its capped width.
func (k *kit) pageColumn(content widget.Widget) widget.Widget {
	return widget.NewClamp(int(pageMaxWidth*float64(k.store.svc.Config().Styling.Scale)+0.5), content)
}

// reveal scrolls row i into view at the next layout and hands its
// control the keys through focus.
func (p *settingsPage) reveal(i int, focus func(widget.Widget)) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	p.revealing = p.rows[i]
	p.InvalidateLayout()
	if target := widget.FirstFocusable(p.revealing.ctl); target != nil && focus != nil {
		focus(target)
	}
}

// Arrange lays the page out, then brings a pending reveal into view.
func (p *settingsPage) Arrange(r render.Rect) {
	p.Scroll.Arrange(r)
	if row := p.revealing; row != nil {
		p.revealing = nil
		widget.RevealRect(row, row.Bounds())
	}
}
