package settings

import (
	"math"

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
	// lead, when set, builds what precedes the sections (the Sound page's
	// devices).
	lead func(k *kit) widget.Widget
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
	wm        bool
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
	focusing  func()
	releases  []func()
}

// buildPage lays out a page (pages/layout.rs).
func buildPage(k *kit, spec pageSpec) *settingsPage {
	t := locale.T()
	th := widget.Current()
	content := widget.NewBox(widget.Column, sectionGap(), 3*th.Padding)
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
	if spec.lead != nil {
		content.Append(p.own(spec.lead(k)), false)
	}
	for _, s := range spec.sections {
		rows := widget.NewItemGroup()
		section := newSection(k, s.title, rows)
		for _, rs := range s.rows {
			rowBackend := be
			if rs.wm && !spec.wm {
				w, err := k.wmBackend()
				if err != nil {
					section.Append(wrapped(k.label(err.Error(), "error", css.Caption)), false)
					continue
				}
				rowBackend = w
			}
			row := newFieldRow(k, rowBackend, rs)
			rows.Append(row)
			p.rows = append(p.rows, row)
		}
		content.Append(section, false)
	}
	if spec.footer != nil {
		content.Append(p.own(spec.footer(k)), false)
	}
	p.Scroll = widget.NewScroll(k.pageColumn(content))
	p.VerticalOnly, p.ShowBars, p.FillY = true, true, true
	return p
}

// sectionGap is how far apart a page sets its sections.
func sectionGap() int { return 3 * widget.Current().Spacing }

// sectionStack is a column of sections a panel builds itself, as far apart
// as a page's own.
func sectionStack() *widget.Box { return widget.NewBox(widget.Column, sectionGap(), 0) }

// newSection is a page section: its title, its description when it has
// one, and body on a card.
func newSection(k *kit, title string, body widget.Widget) *widget.Box {
	section := gapBox(widget.Column)
	section.AddClass("settings-section")
	section.Append(k.label(locale.T().Get(title), "settings-section-title", css.CaptionHeading, css.DimLabel), false)
	if desc := description(title); desc != "" {
		section.Append(wrapped(k.label(desc, "settings-section-description", css.Caption, css.DimLabel)), false)
	}
	group := widget.NewBox(widget.Column, 0, 0)
	group.AddClass(css.Card, "settings-group")
	group.Append(body, false)
	section.Append(group, false)
	return section
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
	return widget.NewClamp(int(math.Round(pageMaxWidth*float64(k.store.svc.Config().Styling.Scale))), content)
}

// reveal scrolls row i into view at the next layout and hands its
// control the keys through focus.
func (p *settingsPage) reveal(i int, focus func(widget.Widget)) *settingRow {
	if i < 0 || i >= len(p.rows) {
		return nil
	}
	row := p.rows[i]
	give := func() {
		if target := widget.FirstFocusable(row.ctl); target != nil && focus != nil {
			focus(target)
		}
	}
	if row.Bounds().Empty() {
		p.revealing, p.focusing = row, give
		p.InvalidateLayout()
		return row
	}
	widget.RevealRect(row, row.Bounds())
	give()
	return row
}

// Arrange lays the page out, then brings a pending reveal into view.
func (p *settingsPage) Arrange(r render.Rect) {
	p.Scroll.Arrange(r)
	if row := p.revealing; row != nil {
		p.revealing = nil
		widget.RevealRect(row, row.Bounds())
	}
	if give := p.focusing; give != nil {
		p.focusing = nil
		give()
	}
}

// own takes on w's close, run when the page is dropped.
func (p *settingsPage) own(w widget.Widget) widget.Widget {
	if c, ok := w.(interface{ close() }); ok {
		p.releases = append(p.releases, c.close)
	}
	return w
}

func (p *settingsPage) close() {
	for _, release := range p.releases {
		release()
	}
	p.releases = nil
}
