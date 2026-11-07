package settings

import (
	"fmt"
	"strings"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/settingspage"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

// initialPage is the page the window opens on (INITIAL_PAGE_ID), or
// the first page while that one is not ported.
const initialPage = "bar-general"

// navSection is NavSectionLayout: a titled sidebar group of pages.
type navSection struct {
	key   string
	pages []pageSpec
}

// layout is pages/nav.rs layout(): the sidebar's sections in order,
// each page built from the config (a page may list what the config
// holds).
func layout(cfg *config.Config) []navSection {
	sections := []navSection{
		{key: "settings-nav-bar-section", pages: []pageSpec{barGeneralPage(cfg), barButtonPage(cfg), barDropdownPage(cfg), dockPage(cfg)}},
		{key: "settings-nav-appearance", pages: []pageSpec{themePage(cfg), wallpaperPage(cfg), animationsPage(cfg)}},
		{key: "settings-nav-system", pages: []pageSpec{generalPage(cfg)}},
		{key: "settings-nav-overlays", pages: []pageSpec{notificationsPage(cfg), osdPage(cfg), sharePickerPage(cfg), dropdownsPage(cfg)}},
		{key: "settings-nav-launcher", pages: []pageSpec{launcherPage(cfg), launcherModesPage(cfg)}},
		{key: "settings-nav-lock", pages: []pageSpec{lockPage(cfg)}},
		{key: "settings-nav-greeter", pages: []pageSpec{greeterPage(cfg)}},
		{key: "settings-nav-modules", pages: modulePages(cfg)},
	}
	if wmconfig.Installed() {
		sections = append(sections, wmSections()...)
	}
	out := sections[:0]
	for _, s := range sections {
		if len(s.pages) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// pageByID finds a page in the layout.
func pageByID(sections []navSection, id string) (pageSpec, bool) {
	for _, s := range sections {
		for _, p := range s.pages {
			if p.id == id {
				return p, true
			}
		}
	}
	return pageSpec{}, false
}

// firstPage is the page to open: initialPage, else the first listed.
func firstPage(sections []navSection) string {
	if _, ok := pageByID(sections, initialPage); ok {
		return initialPage
	}
	if len(sections) > 0 && len(sections[0].pages) > 0 {
		return sections[0].pages[0].id
	}
	return ""
}

// barDropdownPage is pages/bar/dropdown.rs.
func barDropdownPage(*config.Config) pageSpec {
	return pageSpec{
		id: "bar-dropdown", navKey: "settings-nav-bar-dropdown", icon: "ld-panel-bottom-symbolic",
		header: "settings-page-bar-dropdown",
		sections: []sectionSpec{
			{title: "settings-section-behavior", rows: fields("bar.dropdown-shadow", "bar.dropdown-autohide", "bar.dropdown-freeze-label")},
			{title: "settings-section-appearance", rows: []rowSpec{field("bar.dropdown-opacity", percentage)}},
		},
	}
}

// animationsPage is pages/animations.
func animationsPage(*config.Config) pageSpec {
	duration := optionalSpin(0, maxDurationMS, durationStepMS, durationFallbackMS)
	return pageSpec{
		id: "animations", navKey: "settings-nav-animations", icon: "ld-zap-symbolic",
		header: "settings-page-animations",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("animations.enabled", "animations.reduced-motion", "animations.transition", "animations.duration")},
			{title: "settings-section-direction", rows: []rowSpec{
				field("animations.enter"), field("animations.exit"),
				field("animations.enter-duration", duration), field("animations.exit-duration", duration),
			}},
			{title: "settings-section-speed", rows: fields("animations.ui-duration", "animations.interaction-duration", "animations.indicators")},
		},
	}
}

// sharePickerPage is pages/share_picker.
func sharePickerPage(*config.Config) pageSpec {
	return pageSpec{
		id: "share-picker", navKey: "settings-nav-share-picker", icon: "ld-app-window-symbolic",
		header: "settings-page-share-picker",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("share-picker.default-page", "share-picker.hide-token-restore")},
			{title: "settings-section-display", rows: []rowSpec{
				field("share-picker.width", sizeBase(config.SharePickerWidthBaseRem)),
				field("share-picker.height", sizeBase(config.SharePickerHeightBaseRem)),
				field("share-picker.resize-size"),
				field("share-picker.widget-size", sizeBase(config.SharePickerWidgetBaseRem)),
			}},
			{title: "settings-section-share-picker-windows", rows: []rowSpec{
				field("share-picker.windows-spacing", sizeBase(config.SharePickerWindowsSpacingBaseRem)),
				field("share-picker.windows-min-per-row"), field("share-picker.windows-max-per-row"),
			}},
			{title: "settings-section-share-picker-outputs", rows: []rowSpec{
				field("share-picker.outputs-spacing", sizeBase(config.SharePickerOutputsSpacingBaseRem)),
				field("share-picker.outputs-show-label"), field("share-picker.outputs-respect-scaling"),
			}},
			{title: "settings-section-animation", rows: surfaceAnimationRows("animations.share-picker")},
		},
	}
}

func Pages(cfg *config.Config) []settingspage.Page {
	t := locale.T()
	sections := layout(cfg)
	out := []settingspage.Page{}
	at := map[string]int{}
	for _, s := range sections {
		for _, p := range s.pages {
			at[p.id] = len(out)
			out = append(out, settingspage.Page{ID: p.id, Title: t.Get(p.navKey), Icon: p.icon})
		}
	}
	listed := map[string]bool{}
	for _, e := range settingIndex(sections) {
		if e.path == "" || listed[e.path] {
			continue
		}
		listed[e.path] = true
		page := &out[at[e.page]]
		page.Settings = append(page.Settings, settingspage.Setting{Path: e.path, Title: e.label, Keywords: e.listedKeywords()})
	}
	return out
}

// ListedPages is the page list for a launcher, without a session and without
// touching the config: which pages exist does not depend on it.
func ListedPages() []settingspage.Page { return Pages(config.Defaults()) }

type launch struct {
	page, setting string
}

func parseLaunch(args []string) launch {
	return launch{page: flagValue(args, settingspage.PageFlag), setting: flagValue(args, settingspage.SettingFlag)}
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	return ""
}

func CheckSetting(page, path string) error {
	hit, ok := findSetting(settingIndex(layout(config.Defaults())), path)
	if !ok {
		return fmt.Errorf("no setting %q", path)
	}
	if page != "" && page != hit.page {
		return fmt.Errorf("setting %q is on page %q, not %q", path, hit.page, page)
	}
	return nil
}
