package settings

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

func dockAppsFixture(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	app := func(id, keys string) {
		put(t, filepath.Join(data, "applications", id), "[Desktop Entry]\nType=Application\nExec=true\n"+keys)
	}
	app("firefox.desktop", "Name=Firefox\nIcon=firefox\n")
	app("org.gnome.Nautilus.desktop", "Name=Files\nName[fr]=Fichiers\nIcon=org.gnome.Nautilus\n")
	app("org.gnome.Calculator.desktop", "Name=Calculator\nIcon=accessories-calculator\n")
	app("hidden.desktop", "Name=Hidden\nNoDisplay=true\n")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "none"))
	t.Setenv("XDG_CURRENT_DESKTOP", "lys-wm")
	locale.T()
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
}

func dockPinnedEditor(t *testing.T, k *kit) *listEditor {
	t.Helper()
	for _, sec := range dockPage(nil).sections {
		for _, row := range sec.rows {
			if row.path == "dock.pinned" {
				return fieldControl(k, k.store, row).(*listEditor)
			}
		}
	}
	t.Fatal("the dock page has no dock.pinned row")
	return nil
}

func pinnedValue(t *testing.T, k *kit) []any {
	t.Helper()
	v, _ := k.store.value("dock.pinned").([]any)
	return v
}

func rowIcon(t *testing.T, item listItem) string {
	t.Helper()
	return treetest.First[*widget.Icon](t, item).Name()
}

func openPinPicker(t *testing.T, k *kit, c *listEditor) (*fakePickers, *widget.Entry, *widget.FlowBox) {
	t.Helper()
	f := &fakePickers{}
	k.pickers = f
	c.add.ClickAt(widget.Point{})
	if f.content == nil || f.anchor != widget.Widget(c.add) {
		t.Fatal("the app picker did not open beside the add button")
	}
	search, ok := f.focus.(*widget.Entry)
	if !ok {
		t.Fatalf("the picker focuses %T, want its search entry", f.focus)
	}
	return f, search, treetest.First[*widget.FlowBox](t, f.content)
}

func shownLabels(flow *widget.FlowBox) []string {
	var out []string
	for i := range flow.Len() {
		if child := flow.ChildAt(i); child.Visible() {
			out = append(out, treetest.Labels(child)...)
		}
	}
	return out
}

func TestDockPinnedShowsInstalledAppsByNameAndUninstalledOnesByID(t *testing.T) {
	dockAppsFixture(t)
	k := testKit(t, "dock:\n  pinned: [firefox, gone-app, hidden]\n")
	c := dockPinnedEditor(t, k)
	if len(c.items) != 3 {
		t.Fatalf("%d rows for three pinned ids", len(c.items))
	}
	for i, want := range []struct{ label, icon string }{{"Firefox", "firefox"}, {"gone-app", appFallbackIcon}, {"Hidden", appFallbackIcon}} {
		if got := treetest.Labels(c.items[i]); !slices.Equal(got, []string{want.label}) {
			t.Errorf("row %d reads %q, want %q", i, got, want.label)
		}
		if got := rowIcon(t, c.items[i]); got != want.icon {
			t.Errorf("row %d shows icon %q, want %q", i, got, want.icon)
		}
	}
	if !slices.Equal(pinnedValue(t, k), []any{"firefox", "gone-app", "hidden"}) {
		t.Fatalf("showing the list rewrote it: %v", pinnedValue(t, k))
	}
	c.remove(c.items[1])
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"firefox", "hidden"}) {
		t.Fatalf("removing the uninstalled app wrote %v", got)
	}
}

func TestDockPinnedPickerOffersUnpinnedShownAppsFilteredByNameOrID(t *testing.T) {
	dockAppsFixture(t)
	k := testKit(t, "dock:\n  pinned: [firefox]\n")
	c := dockPinnedEditor(t, k)
	_, search, flow := openPinPicker(t, k, c)
	if got := shownLabels(flow); !slices.Equal(got, []string{"Calculator", "Fichiers"}) {
		t.Fatalf("offered %q, want the unpinned shown apps by localized name", got)
	}
	search.Insert("fich")
	if got := shownLabels(flow); !slices.Equal(got, []string{"Fichiers"}) {
		t.Fatalf("the name filter shows %q", got)
	}
	search.SetText("")
	search.Insert("nautilus")
	if got := shownLabels(flow); !slices.Equal(got, []string{"Fichiers"}) {
		t.Fatalf("the id filter shows %q", got)
	}
	search.SetText("")
	search.Insert("zzz")
	if got := shownLabels(flow); len(got) != 0 {
		t.Fatalf("a query matching no app shows %q", got)
	}
}

func TestDockPinnedPickAppendsTheIDAndKeepsOrder(t *testing.T) {
	dockAppsFixture(t)
	k := testKit(t, "dock:\n  pinned: [firefox, gone-app]\n")
	c := dockPinnedEditor(t, k)
	f, _, flow := openPinPicker(t, k, c)
	flow.OnActivate(slices.Index(shownLabels(flow), "Fichiers"))
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"firefox", "gone-app", "org.gnome.Nautilus"}) {
		t.Fatalf("picking Files wrote %v", got)
	}
	if f.closed != 1 {
		t.Fatalf("the picker closed %d times after a pick", f.closed)
	}
	if got := treetest.Labels(c.items[2]); !slices.Equal(got, []string{"Fichiers"}) {
		t.Fatalf("the new row reads %q", got)
	}
	c.move(c.items[2], -1)
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"firefox", "org.gnome.Nautilus", "gone-app"}) {
		t.Fatalf("moving Files up wrote %v", got)
	}
	c.remove(c.items[0])
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"org.gnome.Nautilus", "gone-app"}) {
		t.Fatalf("removing Firefox wrote %v", got)
	}
}

func TestDockPinnedFreeTextIsNeverPinned(t *testing.T) {
	dockAppsFixture(t)
	k := testKit(t, "dock:\n  pinned: [firefox]\n")
	c := dockPinnedEditor(t, k)
	f, search, flow := openPinPicker(t, k, c)
	search.Insert("not-an-app")
	search.KeyAction(widget.KeyEnter, 0)
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"firefox"}) {
		t.Fatalf("typing free text and Enter wrote %v", got)
	}
	if f.closed != 0 || len(shownLabels(flow)) != 0 {
		t.Fatalf("free text closed the picker (%d) or matched %q", f.closed, shownLabels(flow))
	}
	if slices.ContainsFunc(c.items, func(it listItem) bool { return it.itemValue() == "not-an-app" }) {
		t.Fatal("free text became a row")
	}
}

func TestDockPinnedScansOffTheLoopAndOffersNoPickUntilItIsDone(t *testing.T) {
	dockAppsFixture(t)
	k := testKit(t, "dock:\n  pinned: [firefox, gone-app]\n")
	var scan func()
	k.background = func(fn func()) { scan = fn }
	c := dockPinnedEditor(t, k)
	if scan == nil {
		t.Fatal("the editor scanned the applications on the loop")
	}
	if c.add.Visible() {
		t.Fatal("the add button shows before the apps are known")
	}
	if got := treetest.Labels(c.items[0]); !slices.Equal(got, []string{"firefox"}) {
		t.Fatalf("before the scan the row reads %q, want the id", got)
	}
	scan()
	if !c.add.Visible() {
		t.Fatal("the add button stays hidden after the scan")
	}
	if got := treetest.Labels(c.items[0]); !slices.Equal(got, []string{"Firefox"}) {
		t.Fatalf("after the scan the row reads %q, want the name", got)
	}
	if got := pinnedValue(t, k); !slices.Equal(got, []any{"firefox", "gone-app"}) {
		t.Fatalf("the scan rewrote the list: %v", got)
	}
}
