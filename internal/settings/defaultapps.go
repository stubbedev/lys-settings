package settings

import (
	"slices"
	"strings"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/desktopentry"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	defaultAppsID  = "default-apps"
	fileTypesShown = 40
)

type appCategory struct {
	key   string
	types []string
}

var appCategories = []appCategory{
	{"settings-default-web", []string{"x-scheme-handler/https", "x-scheme-handler/http", "text/html"}},
	{"settings-default-mail", []string{"x-scheme-handler/mailto"}},
	{"settings-default-calendar", []string{"text/calendar"}},
	{"settings-default-music", []string{"audio/mpeg", "audio/flac", "audio/ogg"}},
	{"settings-default-video", []string{"video/mp4", "video/x-matroska", "video/webm"}},
	{"settings-default-images", []string{"image/png", "image/jpeg", "image/webp"}},
	{"settings-default-pdf", []string{"application/pdf"}},
	{"settings-default-text", []string{"text/plain"}},
	{"settings-default-files", []string{"inode/directory"}},
}

func defaultAppsPage(*config.Config) pageSpec {
	return pageSpec{
		id: defaultAppsID, navKey: "settings-nav-default-apps", icon: "ld-app-window-symbolic",
		header: "settings-page-default-apps",
		sections: []sectionSpec{
			{title: "settings-section-default-terminal", rows: []rowSpec{field("launcher.terminal", terminalRow)}},
		},
		footer: defaultAppsFooter,
	}
}

var scanApps = desktopentry.AllApps

type appIndex struct {
	all    []desktopentry.App
	byType map[string][]desktopentry.App
	types  []string
	mime   desktopentry.MimeApps
}

func indexApps(dirs, lists []string) appIndex {
	all := scanApps(dirs)
	ix := appIndex{byType: map[string][]desktopentry.App{}, mime: desktopentry.ReadMimeApps(lists)}
	for _, a := range all {
		if !a.ShouldShow() && len(a.MimeTypes()) == 0 {
			continue
		}
		ix.all = append(ix.all, a)
		for _, t := range a.MimeTypes() {
			ix.byType[t] = append(ix.byType[t], a)
		}
	}
	for t := range ix.byType {
		ix.types = append(ix.types, t)
	}
	slices.Sort(ix.types)
	return ix
}

func (ix appIndex) candidates(types []string) []desktopentry.App {
	var out []desktopentry.App
	for _, a := range ix.all {
		mimes := a.MimeTypes()
		if slices.ContainsFunc(types, func(t string) bool { return slices.Contains(mimes, t) }) {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, byDisplayName)
	return out
}

func (ix appIndex) current(mimeType string) string {
	if a, ok := ix.mime.Default(ix.all, mimeType); ok {
		return a.ID
	}
	return ""
}

type defaultApps struct {
	k       *kit
	ix      appIndex
	cats    *widget.Box
	types   *widget.Box
	filter  *widget.SearchEntry
	problem *widget.Label
}

var setDefaults = desktopentry.SetDefaults

func defaultAppsFooter(k *kit) widget.Widget {
	d := &defaultApps{k: k}
	content := sectionStack()
	d.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	d.problem.SetVisible(false)
	content.Append(d.problem, false)
	d.cats = gapBox(widget.Column)
	content.Append(newSection(k, "settings-section-default-categories", d.cats), false)
	typesBody := widget.NewBox(widget.Column, 0, 0)
	d.filter = widget.NewSearchEntry(k.face, k.textPx(), locale.T().Get("settings-default-types-filter"))
	d.filter.SetDelay(0)
	d.filter.OnSearchChanged = func(string) { d.showTypes() }
	d.types = gapBox(widget.Column)
	typesBody.Append(insetRow(d.filter), false)
	typesBody.Append(widget.NewSeparator(widget.Horizontal), false)
	typesBody.Append(d.types, false)
	content.Append(newSection(k, "settings-section-default-types", typesBody), false)
	dirs, lists := desktopentry.ApplicationDirs(), desktopentry.MimeAppsLists()
	async(k, func() appIndex { return indexApps(dirs, lists) }, d.show)
	return content
}

func (d *defaultApps) show(ix appIndex) {
	d.ix = ix
	d.cats.Clear()
	rows := widget.NewItemGroup()
	t := locale.T()
	for _, c := range appCategories {
		rows.Append(d.chooser(t.Get(c.key), c.types))
	}
	d.cats.Append(rows, false)
	d.showTypes()
}

func (d *defaultApps) showTypes() {
	d.types.Clear()
	words := strings.Fields(strings.ToLower(d.filter.Text()))
	rows := widget.NewItemGroup()
	d.types.Append(rows, false)
	shown := 0
	for _, mimeType := range d.ix.types {
		if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(mimeType, w) }) {
			continue
		}
		if shown == fileTypesShown {
			rows.Append(widget.NewItem(d.k.face, "", locale.T().Get("settings-default-types-more")))
			break
		}
		rows.Append(d.chooser(mimeType, []string{mimeType}))
		shown++
	}
}

func (d *defaultApps) chooser(title string, types []string) widget.Widget {
	apps := d.ix.candidates(types)
	row := gapBox(widget.Row)
	row.SetElement("item")
	row.AddClass(css.Small, "setting-row", "default-app")
	row.AppendAligned(d.k.label(title, "setting-label"), true, widget.AlignCenter)
	if len(apps) == 0 {
		row.AppendAligned(d.k.label(locale.T().Get("settings-default-none"), css.DimLabel), false, widget.AlignCenter)
		return row
	}
	current := d.ix.current(types[0])
	items := make([]widget.DropdownRow, 0, len(apps)+1)
	selected := 0
	if !slices.ContainsFunc(apps, func(a desktopentry.App) bool { return a.ID == current }) {
		items = append(items, widget.DropdownRow{Label: locale.T().Get("settings-default-unset")})
	}
	for _, a := range apps {
		if a.ID == current {
			selected = len(items)
		}
		icon := a.Icon()
		items = append(items, widget.DropdownRow{Label: a.DisplayName(), Leading: func() widget.Widget { return widget.NewThemeIcon(icon, appIconPx) }})
	}
	offset := len(items) - len(apps)
	dd := widget.NewDropdownRows(d.k.face, d.k.textPx(), items, selected)
	dd.SetAccessibleName(title)
	dd.OnSelect = func(i int) {
		if i < offset {
			return
		}
		id := apps[i-offset].ID
		async(d.k, func() error { return setDefaults(id, types) }, func(err error) {
			d.problem.SetVisible(err != nil)
			if err != nil {
				d.problem.SetText(err.Error())
				return
			}
			d.ix.mime = desktopentry.ReadMimeApps(desktopentry.MimeAppsLists())
		})
	}
	row.AppendAligned(dd, false, widget.AlignCenter)
	return row
}
