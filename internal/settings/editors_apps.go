package settings

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/desktopentry"
	"github.com/stubbedev/lys-ui/widget"
)

const (
	appIconPx       = 16
	appFallbackIcon = "ld-app-window-symbolic"
)

type installedApps struct {
	byID  map[string]desktopentry.App
	shown []desktopentry.App
}

func scanInstalled(dirs []string) installedApps {
	all := scanApps(dirs)
	ix := installedApps{byID: make(map[string]desktopentry.App, len(all))}
	for _, a := range all {
		ix.byID[a.AppID()] = a
		if a.ShouldShow() {
			ix.shown = append(ix.shown, a)
		}
	}
	slices.SortFunc(ix.shown, byDisplayName)
	return ix
}

func (ix installedApps) loaded() bool { return ix.byID != nil }

func byDisplayName(a, b desktopentry.App) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a.DisplayName()), strings.ToLower(b.DisplayName())), cmp.Compare(a.ID, b.ID))
}

func appFace(k *kit, icon, name, tooltip string) *widget.Box {
	row := gapBox(widget.Row)
	row.SetTooltip(tooltip)
	row.AppendAligned(widget.NewThemeIcon(cmp.Or(icon, appFallbackIcon), appIconPx), false, widget.AlignCenter)
	label := k.label(name)
	label.SetEllipsize(widget.EllipsizeEnd)
	row.AppendAligned(label, true, widget.AlignCenter)
	return row
}

func installedFace(k *kit, a desktopentry.App) *widget.Box {
	return appFace(k, a.Icon(), cmp.Or(a.DisplayName(), a.AppID()), a.AppID())
}

func openAppPicker(k *kit, anchor widget.Widget, apps []desktopentry.App, onPick func(desktopentry.App)) {
	ids := make([]string, len(apps))
	byID := make(map[string]desktopentry.App, len(apps))
	for i, a := range apps {
		ids[i] = a.ID
		byID[a.ID] = a
	}
	openSearchPicker(k, anchor, ids, pickerSpec{
		class: "choice-popover", placeholder: "settings-choice-search", maxH: choiceListPx,
		row:  func(k *kit, id string) widget.Widget { return installedFace(k, byID[id]) },
		text: func(id string) string { return byID[id].DisplayName() + "\n" + byID[id].AppID() },
	}, func(id string) { onPick(byID[id]) })
}

type appItem struct {
	*widget.Box
	id string
}

func (a appItem) itemValue() any { return a.id }

var pinnedApps rowOpt = func(r *rowSpec) {
	r.fullWidth = true
	r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newPinnedAppsEditor(k, s) }
}

func newPinnedAppsEditor(k *kit, s slot) *listEditor {
	var apps installedApps
	var c *listEditor
	c = newListEditor(k, s, listSpec{
		addKey: "settings-list-add", rowClass: "string-list-row", reorder: true,
		item: func(k *kit, v any, _ func()) listItem {
			id, _ := v.(string)
			if a, ok := apps.byID[id]; ok {
				return appItem{installedFace(k, a), id}
			}
			return appItem{appFace(k, "", id, id), id}
		},
		pick: func(anchor widget.Widget, choose func(any)) {
			pinned := c.values()
			unpinned := slices.DeleteFunc(slices.Clone(apps.shown), func(a desktopentry.App) bool { return slices.Contains(pinned, any(a.AppID())) })
			openAppPicker(k, anchor, unpinned, func(a desktopentry.App) { choose(a.AppID()) })
		},
		full: func(int) bool { return !apps.loaded() },
	})
	dirs := desktopentry.ApplicationDirs()
	async(k, func() installedApps { return scanInstalled(dirs) }, func(ix installedApps) {
		apps = ix
		c.rebuild(c.values())
	})
	return c
}
