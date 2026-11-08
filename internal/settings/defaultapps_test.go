package settings

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/desktopentry"
	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
)

func defaultAppsFixture(t *testing.T) (config string) {
	t.Helper()
	root := t.TempDir()
	data, config := filepath.Join(root, "data"), filepath.Join(root, "config")
	app := func(id, name, mimes string) {
		put(t, filepath.Join(data, "applications", id), "[Desktop Entry]\nType=Application\nName="+name+"\nExec=true\nIcon="+name+"\nMimeType="+mimes+"\n")
	}
	app("firefox.desktop", "Firefox", "x-scheme-handler/http;x-scheme-handler/https;text/html;")
	app("chromium.desktop", "Chromium", "x-scheme-handler/http;x-scheme-handler/https;text/html;image/png;")
	app("evince.desktop", "Evince", "application/pdf;")
	put(t, filepath.Join(config, "mimeapps.list"), "# mine\n[Added Associations]\nimage/png=chromium.desktop;\n\n[Default Applications]\nx-scheme-handler/https=firefox.desktop;\n")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "none"))
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(root, "none"))
	t.Setenv("XDG_CURRENT_DESKTOP", "lys-wm")
	return config
}

func dropdownNamed(t *testing.T, root widget.Widget, name string) *widget.Dropdown {
	t.Helper()
	for _, d := range treetest.All[*widget.Dropdown](root) {
		if d.AccessibleName() == name {
			return d
		}
	}
	t.Fatalf("no dropdown %q", name)
	return nil
}

func TestPickingABrowserWritesItsThreeKeysAtOnceFromOneScan(t *testing.T) {
	config := defaultAppsFixture(t)
	scans := 0
	before := scanApps
	scanApps = func(dirs []string) []desktopentry.App { scans++; return before(dirs) }
	t.Cleanup(func() { scanApps = before })
	var writes [][]string
	beforeSet := setDefaults
	setDefaults = func(id string, types []string) error { writes = append(writes, types); return beforeSet(id, types) }
	t.Cleanup(func() { setDefaults = beforeSet })
	k := testKit(t, "")
	w := newWindow(k, k.store.svc.Config(), func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	w.show(defaultAppsID)
	w.root.Arrange(render.Rect{W: 1400, H: 6000})
	if scans != 1 {
		t.Fatalf("%d scans of the applications for one page, want one", scans)
	}
	web := dropdownNamed(t, w.page, "Web browser")
	if got := web.Items(); !slices.Equal(got, []string{"Chromium", "Firefox"}) || web.Selected() != 1 {
		t.Fatalf("web browsers %q selected %d, want Firefox, the https default", got, web.Selected())
	}
	if pdf := dropdownNamed(t, w.page, "PDF documents"); pdf.Items()[pdf.Selected()] != "Evince" {
		t.Fatalf("the only PDF viewer is not shown as the default: %q", pdf.Items())
	}
	web.SetSelected(0)
	if len(writes) != 1 || !slices.Equal(writes[0], []string{"x-scheme-handler/https", "x-scheme-handler/http", "text/html"}) {
		t.Fatalf("writes %v, want the three browser types in one", writes)
	}
	got := fileText(t, filepath.Join(config, "mimeapps.list"))
	if !strings.HasPrefix(got, "# mine\n[Added Associations]\nimage/png=chromium.desktop;\n") {
		t.Fatalf("the rest of the file changed: %q", got)
	}
	for _, mimeType := range []string{"x-scheme-handler/http", "x-scheme-handler/https", "text/html"} {
		if a, ok := desktopentry.Default(desktopentry.ApplicationDirs(), desktopentry.MimeAppsLists(), mimeType); !ok || a.ID != "chromium.desktop" {
			t.Errorf("%s resolves to %q, want chromium.desktop", mimeType, a.ID)
		}
	}
	if scans != 1 {
		t.Fatalf("a pick rescanned the applications: %d scans", scans)
	}
	filter := treetest.First[*widget.SearchEntry](t, w.page)
	filter.SetText("pdf")
	filter.OnSearchChanged("pdf")
	var types []string
	for _, dd := range treetest.All[*widget.Dropdown](w.page) {
		if strings.Contains(dd.AccessibleName(), "/") {
			types = append(types, dd.AccessibleName())
		}
	}
	if !slices.Equal(types, []string{"application/pdf"}) {
		t.Fatalf("the pdf filter shows the types %q", types)
	}
}
