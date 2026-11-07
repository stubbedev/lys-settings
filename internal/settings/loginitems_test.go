package settings

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/dbustest"
	"github.com/stubbedev/lys-lib/render"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/desktopentry"
	"github.com/stubbedev/lys-shell/shell/apptheme"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

func autostartFixture(t *testing.T) (autostartDirs, string) {
	t.Helper()
	root := t.TempDir()
	d := autostartDirs{user: filepath.Join(root, "home", "autostart"), system: []string{filepath.Join(root, "etc", "autostart"), filepath.Join(root, "vendor", "autostart")}}
	entry := func(dir, id, extra string) {
		put(t, filepath.Join(dir, id), "[Desktop Entry]\nType=Application\nName="+strings.TrimSuffix(id, desktopSuffix)+"\nExec=true\n"+extra)
	}
	entry(d.system[0], "nm-applet.desktop", "Icon=nm-device-wireless\n")
	entry(d.system[0], "tracker.desktop", "")
	entry(d.system[0], "gnome-only.desktop", "OnlyShowIn=GNOME;\n")
	entry(d.system[1], "nm-applet.desktop", "Name=Shadowed\n")
	entry(d.system[1], "skipped.desktop", "X-systemd-skip=true\n")
	entry(d.user, "tracker.desktop", "Hidden=true\n")
	entry(d.user, "syncthing.desktop", "")
	put(t, filepath.Join(d.user, "notes.txt"), "")
	return d, root
}

func entryByID(t *testing.T, entries []autostartEntry, id string) autostartEntry {
	t.Helper()
	i := slices.IndexFunc(entries, func(e autostartEntry) bool { return e.id == id })
	if i < 0 {
		t.Fatalf("no %s in %+v", id, entries)
	}
	return entries[i]
}

func TestAutostartIsReadOnceADirectoryAndMergedByName(t *testing.T) {
	d, _ := autostartFixture(t)
	reads := map[string]int{}
	entries, err := readAutostart(d, []string{"lys-wm"}, func(dir string) ([]fs.DirEntry, error) {
		reads[dir]++
		return os.ReadDir(dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range append([]string{d.user}, d.system...) {
		if reads[dir] != 1 {
			t.Errorf("%s read %d times, want once", dir, reads[dir])
		}
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	if want := []string{"gnome-only.desktop", "nm-applet.desktop", "skipped.desktop", "syncthing.desktop", "tracker.desktop"}; !slices.Equal(ids, want) {
		t.Fatalf("%q, want %q", ids, want)
	}
	for id, enabled := range map[string]bool{"nm-applet.desktop": true, "tracker.desktop": false, "gnome-only.desktop": false, "skipped.desktop": false, "syncthing.desktop": true} {
		if e := entryByID(t, entries, id); e.enabled != enabled {
			t.Errorf("%s enabled %v, want %v", id, e.enabled, enabled)
		}
	}
	nm := entryByID(t, entries, "nm-applet.desktop")
	if nm.name != "nm-applet" || nm.icon != "nm-device-wireless" || nm.system != filepath.Join(d.system[0], "nm-applet.desktop") || nm.user != "" || nm.removable() {
		t.Fatalf("the first system dir wins: %+v", nm)
	}
	if tr := entryByID(t, entries, "tracker.desktop"); tr.user == "" || tr.system == "" || tr.removable() {
		t.Fatalf("a user override of a system entry: %+v", tr)
	}
	if !entryByID(t, entries, "syncthing.desktop").removable() {
		t.Fatal("a user's own entry is not removable")
	}
}

func TestTurningASystemEntryOffWritesAHiddenUserCopyAndLeavesTheSystemOne(t *testing.T) {
	d, _ := autostartFixture(t)
	system := filepath.Join(d.system[0], "nm-applet.desktop")
	before := fileText(t, system)
	entries, err := readAutostart(d, nil, os.ReadDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := setAutostart(d, entryByID(t, entries, "nm-applet.desktop"), false); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(d.user, "nm-applet.desktop")
	if got := fileText(t, user); got != before+"Hidden=true\n" {
		t.Fatalf("the user copy is %q", got)
	}
	if fileText(t, system) != before {
		t.Fatal("the system entry changed")
	}
	entries, _ = readAutostart(d, nil, os.ReadDir)
	nm := entryByID(t, entries, "nm-applet.desktop")
	if nm.enabled || nm.user != user {
		t.Fatalf("%+v", nm)
	}
	if err := setAutostart(d, nm, true); err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, user); got != before+"Hidden=false\n" {
		t.Fatalf("turned on again: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(d.user, ".*")); len(left) != 0 {
		t.Fatalf("a temporary file was left: %v", left)
	}
}

func TestAnAppIsAddedAsACopyAndOnlyYourOwnEntriesAreRemoved(t *testing.T) {
	d, root := autostartFixture(t)
	apps := filepath.Join(root, "applications")
	put(t, filepath.Join(apps, "org.example.Notes.desktop"), "[Desktop Entry]\nType=Application\nName=Notes\nExec=true\n")
	app, ok := desktopentry.LoadApp("org.example.Notes.desktop", filepath.Join(apps, "org.example.Notes.desktop"))
	if !ok {
		t.Fatal("the fixture app does not load")
	}
	if err := addAutostart(d, app); err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, filepath.Join(d.user, "org.example.Notes.desktop")); !strings.Contains(got, "Name=Notes") {
		t.Fatalf("%q", got)
	}
	entries, _ := readAutostart(d, nil, os.ReadDir)
	if err := removeAutostart(entryByID(t, entries, "nm-applet.desktop")); err == nil {
		t.Fatal("a system entry was removed")
	}
	if err := removeAutostart(entryByID(t, entries, "org.example.Notes.desktop")); err != nil {
		t.Fatal(err)
	}
	noFile(t, filepath.Join(d.user, "org.example.Notes.desktop"))
}

type fakeSystemd struct {
	mu      sync.Mutex
	calls   map[string]int
	enabled []string
	files   map[string]struct{ path, state string }
	wants   map[string][]string
}

func (f *fakeSystemd) count(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[name]++
}

func (f *fakeSystemd) LoadUnit(name string) (dbus.ObjectPath, *dbus.Error) {
	f.count("LoadUnit")
	return dbus.ObjectPath("/org/freedesktop/systemd1/unit/" + strings.NewReplacer(".", "_2e", "-", "_2d").Replace(name)), nil
}

func (f *fakeSystemd) ListUnitFilesByPatterns(_, patterns []string) ([]struct{ Path, State string }, *dbus.Error) {
	f.count("ListUnitFilesByPatterns")
	var out []struct{ Path, State string }
	for _, p := range patterns {
		if file, ok := f.files[p]; ok {
			out = append(out, struct{ Path, State string }{file.path, file.state})
		}
	}
	return out, nil
}

type listedUnit struct {
	Name, Description, Load, Active, Sub, Following string
	Path                                            dbus.ObjectPath
	JobID                                           uint32
	JobType                                         string
	JobPath                                         dbus.ObjectPath
}

func (f *fakeSystemd) ListUnitsByNames(names []string) ([]listedUnit, *dbus.Error) {
	f.count("ListUnitsByNames")
	out := make([]listedUnit, 0, len(names))
	for _, n := range names {
		out = append(out, listedUnit{Name: n, Description: "the " + n, Load: "loaded", Active: "active", Sub: "running", Path: "/", JobPath: "/"})
	}
	return out, nil
}

func (f *fakeSystemd) EnableUnitFiles(files []string, _, _ bool) (bool, []struct{ Type, File, Dest string }, *dbus.Error) {
	f.count("EnableUnitFiles")
	f.mu.Lock()
	f.enabled = append(f.enabled, files...)
	f.mu.Unlock()
	return true, nil, nil
}

func (f *fakeSystemd) DisableUnitFiles(files []string, _ bool) ([]struct{ Type, File, Dest string }, *dbus.Error) {
	f.count("DisableUnitFiles")
	f.mu.Lock()
	f.enabled = slices.DeleteFunc(f.enabled, func(s string) bool { return slices.Contains(files, s) })
	f.mu.Unlock()
	return nil, nil
}

func (f *fakeSystemd) Reload() *dbus.Error {
	f.count("Reload")
	return nil
}

type fakeUnitProps struct {
	f    *fakeSystemd
	name string
}

func (p fakeUnitProps) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	p.f.count("Get " + p.name)
	if iface != systemdUnit || prop != "Wants" {
		return dbus.Variant{}, dbus.MakeFailedError(os.ErrNotExist)
	}
	return dbus.MakeVariant(p.f.wants[p.name]), nil
}

func startFakeSystemd(t *testing.T) (*fakeSystemd, *dbus.Conn) {
	t.Helper()
	bus := dbustest.Start(t)
	service := bus.Conn(t)
	f := &fakeSystemd{
		calls: map[string]int{},
		wants: map[string][]string{
			"graphical-session.target": {"lys-shell.service", "syncthing.service", "graphical-session-pre.target"},
			"lys-wm-session.target":    {"lys-shell.service", "lys-polkit.service"},
		},
		files: map[string]struct{ path, state string }{
			"lys-shell.service":  {"/nix/store/abc-lys-shell/lib/systemd/user/lys-shell.service", "enabled"},
			"syncthing.service":  {"/home/me/.config/systemd/user/syncthing.service", "enabled"},
			"lys-polkit.service": {"/usr/lib/systemd/user/lys-polkit.service", "static"},
		},
	}
	if err := service.Export(f, systemdPath, systemdManager); err != nil {
		t.Fatal(err)
	}
	for target := range f.wants {
		path, _ := f.LoadUnit(target)
		if err := service.Export(fakeUnitProps{f, target}, path, "org.freedesktop.DBus.Properties"); err != nil {
			t.Fatal(err)
		}
	}
	f.calls = map[string]int{}
	if reply, err := service.RequestName(systemdName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", systemdName, reply, err)
	}
	return f, bus.Conn(t)
}

func TestTheSessionsServicesComeFromOneListingOfTheirUnitFiles(t *testing.T) {
	f, conn := startFakeSystemd(t)
	m := systemdUser{conn: conn, resolve: func(p string) (string, error) { return p, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	units, err := m.units(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(units))
	for i, u := range units {
		names[i] = u.name
	}
	if want := []string{"lys-polkit.service", "lys-shell.service", "syncthing.service"}; !slices.Equal(names, want) {
		t.Fatalf("%q, want %q", names, want)
	}
	if f.calls["ListUnitFilesByPatterns"] != 1 || f.calls["ListUnitsByNames"] != 1 {
		t.Fatalf("calls %v, want one ListUnitFilesByPatterns and one ListUnitsByNames", f.calls)
	}
	for name, switchable := range map[string]bool{"lys-shell.service": false, "syncthing.service": true, "lys-polkit.service": false} {
		u := units[slices.Index(names, name)]
		if u.switchable() != switchable || u.active != "active" {
			t.Errorf("%s: %+v switchable %v, want %v", name, u, u.switchable(), switchable)
		}
	}
	if !units[slices.Index(names, "lys-shell.service")].nix() {
		t.Error("a unit in the Nix store is not marked as Nix's")
	}
	if err := m.setEnabled(ctx, "syncthing.service", false); err != nil {
		t.Fatal(err)
	}
	if err := m.setEnabled(ctx, "syncthing.service", true); err != nil {
		t.Fatal(err)
	}
	if f.calls["DisableUnitFiles"] != 1 || f.calls["EnableUnitFiles"] != 1 || f.calls["Reload"] != 2 || !slices.Equal(f.enabled, []string{"syncthing.service"}) {
		t.Fatalf("calls %v, enabled %v", f.calls, f.enabled)
	}
}

type staticManager struct {
	units_  []userUnit
	toggled []string
}

func (m *staticManager) units(context.Context) ([]userUnit, error) { return m.units_, nil }

func (m *staticManager) setEnabled(_ context.Context, name string, on bool) error {
	m.toggled = append(m.toggled, name+map[bool]string{true: "+", false: "-"}[on])
	return nil
}

func TestTabReachesEveryLoginItemSwitch(t *testing.T) {
	d, root := autostartFixture(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(d.user))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(root, "etc")+":"+filepath.Join(root, "vendor"))
	manager := &staticManager{units_: []userUnit{
		{name: "syncthing.service", active: "active", fileState: "enabled", path: "/home/me/.config/systemd/user/syncthing.service"},
		{name: "lys-shell.service", active: "active", fileState: "enabled", path: "/nix/store/x/lys-shell.service"},
	}}
	before := openUserManager
	openUserManager = func() (userManager, func(), error) { return manager, func() {}, nil }
	t.Cleanup(func() { openUserManager = before })
	k := testKit(t, "")
	cfg := k.store.svc.Config()
	w := newWindow(k, cfg, func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	apptheme.New(cfg).Attach(w.root)
	w.show(loginItemsID)
	w.root.Arrange(render.Rect{W: 1400, H: 6000})
	switches := treetest.All[*widget.Switch](w.page)
	if len(switches) != 6 {
		t.Fatalf("%d switches, want one per app (5) and one for the user's own service", len(switches))
	}
	router := &widget.Router{Root: w.root}
	if err := router.SetFocus(w.search); err != nil {
		t.Fatal(err)
	}
	visited := map[widget.Widget]bool{}
	for range maxTabStops {
		router.FocusNext()
		if router.Focused() == widget.Widget(w.search) {
			break
		}
		visited[router.Focused()] = true
	}
	for _, sw := range switches {
		if !visited[sw] {
			t.Errorf("the switch %q is out of Tab's reach", sw.AccessibleName())
		}
	}
	syncthing := switches[slices.IndexFunc(switches, func(s *widget.Switch) bool { return s.AccessibleName() == "syncthing.service" })]
	syncthing.Toggle()
	if !slices.Equal(manager.toggled, []string{"syncthing.service-"}) {
		t.Fatalf("toggled %v", manager.toggled)
	}
}
