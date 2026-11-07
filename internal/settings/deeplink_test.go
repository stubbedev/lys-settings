package settings

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/settingspage"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"
)

const listedPagesBudget = 256 << 10

func TestASettingLaunchOpensItsPageFocusedAndHighlighted(t *testing.T) {
	k := testKit(t, "")
	f := &fakeAfter{}
	w := newWindow(k, k.store.svc.Config(), f.after)
	var focused widget.Widget
	w.focus = func(x widget.Widget) { focused = x }
	w.open(launch{setting: "bar.location"})
	if w.pageID != "bar-general" {
		t.Fatalf("page %q, want bar-general", w.pageID)
	}
	i := slices.IndexFunc(w.page.rows, func(r *settingRow) bool { return r.path == "bar.location" })
	if i < 0 {
		t.Fatal("no bar.location row")
	}
	row := w.page.rows[i]
	if want := widget.FirstFocusable(row.ctl); focused == nil || focused != want {
		t.Fatalf("focused %T, want the row's control %T", focused, want)
	}
	if !row.title.HasClass(css.Accent) {
		t.Fatal("the revealed row is not highlighted")
	}
	f.run()
	if row.title.HasClass(css.Accent) {
		t.Fatal("the highlight stayed")
	}
	w.open(launch{setting: "no.such.setting"})
	if w.pageID != "bar-general" {
		t.Fatalf("an unknown setting moved the window to %q", w.pageID)
	}
	w.open(launch{page: "theme"})
	if w.pageID != "theme" {
		t.Fatalf("a page launch went to %q", w.pageID)
	}
}

func TestAWMSettingResolvesWithAndWithoutItsPrefix(t *testing.T) {
	index := []settingEntry{
		{page: "animations", path: "animations.enabled"},
		{page: "wm-motion", path: "wm:animations.enabled"},
		{page: "wm-input", path: "wm:input.touchpad.natural_scroll"},
	}
	for path, page := range map[string]string{
		"animations.enabled":               "animations",
		"wm:animations.enabled":            "wm-motion",
		"input.touchpad.natural_scroll":    "wm-input",
		"wm:input.touchpad.natural_scroll": "wm-input",
	} {
		if hit, ok := findSetting(index, path); !ok || hit.page != page {
			t.Errorf("%s: %+v %v, want page %s", path, hit, ok, page)
		}
	}
	for _, path := range []string{"", "wm:", "wm:wm:input.touchpad.natural_scroll", "does.not.exist"} {
		if hit, ok := findSetting(index, path); ok {
			t.Errorf("%q found %+v", path, hit)
		}
	}
}

func TestCheckSettingNamesTheUnknownPathAndAWrongPage(t *testing.T) {
	if err := CheckSetting("", "bar.location"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSetting("bar-general", "bar.location"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSetting("", "does.not.exist"); err == nil || !strings.Contains(err.Error(), `"does.not.exist"`) {
		t.Fatalf("%v", err)
	}
	if err := CheckSetting("theme", "bar.location"); err == nil || !strings.Contains(err.Error(), `"bar-general"`) {
		t.Fatalf("%v", err)
	}
}

func TestTheListedPagesCarryEveryRowOnceWithinBudget(t *testing.T) {
	sections := layout(config.Defaults())
	index := settingIndex(sections)
	pages := ListedPages()
	listed := map[string]string{}
	for _, p := range pages {
		for _, s := range p.Settings {
			listed[s.Path] = p.ID
			if s.Title == "" || len(s.Keywords) == 0 {
				t.Errorf("%s: %+v", s.Path, s)
			}
		}
	}
	for _, sec := range sections {
		for _, p := range sec.pages {
			for _, s := range p.sections {
				for _, r := range s.rows {
					path := settingPath(p, r)
					on, ok := listed[path]
					if !ok {
						t.Errorf("row %s of %s is not listed", path, p.id)
						continue
					}
					if hit, _ := findSetting(index, path); hit.page != on {
						t.Errorf("%s is listed on %s but opens %s", path, on, hit.page)
					}
				}
			}
		}
	}
	data, err := settingspage.Encode(pages)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > listedPagesBudget {
		t.Fatalf("the page list is %d bytes, over its %d byte budget", len(data), listedPagesBudget)
	}
	t.Logf("the page list is %d bytes of %d", len(data), listedPagesBudget)
}

func TestSearchFindsARowByItsKeywords(t *testing.T) {
	index := []settingEntry{
		{page: "a", label: "Natural scroll", keywords: []string{"reverse scrolling"}, order: 0},
		{page: "b", label: "Width", order: 1},
	}
	if got := searchSettings(index, "reverse"); len(got) != 1 || got[0].page != "a" {
		t.Fatalf("%+v", got)
	}
}

func TestASettingBelowTheFoldIsScrolledIntoView(t *testing.T) {
	k := testKit(t, "")
	w := newWindow(k, k.store.svc.Config(), func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	w.openPage("bar-general")
	const width, height = 900, 600
	w.root.Arrange(render.Rect{W: width, H: height})
	last := w.page.rows[len(w.page.rows)-1]
	if last.Bounds().Y+last.Bounds().H <= height {
		t.Fatalf("the last row at %+v is not below the fold", last.Bounds())
	}
	w.open(launch{setting: last.path})
	w.root.Arrange(render.Rect{W: width, H: height})
	if b, view := last.Bounds(), w.page.Bounds(); b.Y < view.Y || b.Y+b.H > view.Y+view.H {
		t.Fatalf("the revealed row at %+v is outside the page's view %+v", b, view)
	}
}

func TestTheNewestLaunchBeforeTheWindowShowsIsTheOneItOpens(t *testing.T) {
	k := testKit(t, "")
	w := newWindow(k, k.store.svc.Config(), (&fakeAfter{}).after)
	var focused widget.Widget
	w.focus = func(x widget.Widget) { focused = x }
	w.request(launch{page: "theme"})
	w.request(launch{setting: "bar.location"})
	w.request(launch{})
	if w.pageID != "bar-general" || focused != nil {
		t.Fatalf("before the window shows: page %q, focused %T", w.pageID, focused)
	}
	w.presentedFirst()
	if w.pageID != "bar-general" || focused == nil {
		t.Fatalf("once shown: page %q, focused %T", w.pageID, focused)
	}
	w.request(launch{page: "theme"})
	w.presentedFirst()
	if w.pageID != "theme" {
		t.Fatalf("a launch after the window showed went to %q, or a second presentation moved it", w.pageID)
	}
}

func TestASettingOnAnotherPageBelowTheFoldIsScrolledIntoView(t *testing.T) {
	k := testKit(t, "")
	w := newWindow(k, k.store.svc.Config(), func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	w.openPage("theme")
	const width, height = 900, 600
	w.root.Arrange(render.Rect{W: width, H: height})
	spec, _ := pageByID(w.sections, "bar-general")
	rows := spec.sections[len(spec.sections)-1].rows
	w.open(launch{setting: rows[len(rows)-1].path})
	for range 3 {
		w.root.Arrange(render.Rect{W: width, H: height})
	}
	last := w.page.rows[len(w.page.rows)-1]
	if b, view := last.Bounds(), w.page.Bounds(); b.Y < view.Y || b.Y+b.H > view.Y+view.H {
		t.Fatalf("the revealed row at %+v is outside the page's view %+v", b, view)
	}
}
