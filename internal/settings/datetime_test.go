package settings

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/dbustest"
	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

type fakeSysCall struct {
	method string
	args   []any
}

type fakeSys struct {
	mu     sync.Mutex
	calls  []fakeSysCall
	reads  int
	refuse string
	props  map[string]any
	conn   *dbus.Conn
	path   dbus.ObjectPath
	iface  string
}

func (f *fakeSys) record(method string, args ...any) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeSysCall{method, args})
	if f.refuse != "" {
		return dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{f.refuse})
	}
	return nil
}

func (f *fakeSys) snapshot() ([]fakeSysCall, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls), f.reads
}

func (f *fakeSys) set(key string, v any) {
	f.mu.Lock()
	f.props[key] = v
	f.mu.Unlock()
	_ = f.conn.Emit(f.path, propertiesIface+"."+propertiesChanged, f.iface, map[string]dbus.Variant{key: dbus.MakeVariant(v)}, []string{})
}

func (f *fakeSys) getters() dbusx.Getters {
	g := dbusx.Getters{}
	for key := range f.props {
		g[key] = func() any {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.reads++
			return f.props[key]
		}
	}
	return g
}

type fakeTimedate struct{ *fakeSys }

func (f fakeTimedate) SetNTP(on, interactive bool) *dbus.Error {
	return f.record("SetNTP", on, interactive)
}

func (f fakeTimedate) SetTime(usec int64, relative, interactive bool) *dbus.Error {
	return f.record("SetTime", usec, relative, interactive)
}

func (f fakeTimedate) SetTimezone(zone string, interactive bool) *dbus.Error {
	return f.record("SetTimezone", zone, interactive)
}

func (f fakeTimedate) ListTimezones() ([]string, *dbus.Error) {
	f.mu.Lock()
	f.reads++
	f.mu.Unlock()
	return []string{"Europe/Copenhagen", "UTC"}, nil
}

type fakeLocaled struct{ *fakeSys }

func (f fakeLocaled) SetLocale(values []string, interactive bool) *dbus.Error {
	return f.record("SetLocale", values, interactive)
}

func (f fakeLocaled) SetVConsoleKeyboard(keymap, toggle string, convert, interactive bool) *dbus.Error {
	return f.record("SetVConsoleKeyboard", keymap, toggle, convert, interactive)
}

func (f fakeLocaled) SetX11Keyboard(layout, model, variant, options string, convert, interactive bool) *dbus.Error {
	return f.record("SetX11Keyboard", layout, model, variant, options, convert, interactive)
}

func serveFake(t *testing.T, bus *dbustest.Bus, name string, path dbus.ObjectPath, f *fakeSys, methods any) {
	t.Helper()
	f.conn, f.path, f.iface = bus.Conn(t), path, name
	if _, err := dbusx.Serve(f.conn, dbusx.Service{Name: name, Path: path, Interface: name, Methods: methods, Properties: f.getters()}); err != nil {
		t.Fatal(err)
	}
}

type loop struct {
	t     *testing.T
	queue chan func()
}

func systemKit(t *testing.T, bus *dbustest.Bus) (*kit, *loop) {
	t.Helper()
	before := connectSystemBus
	connectSystemBus = func() (*dbus.Conn, error) { return dbus.Connect(bus.Address) }
	t.Cleanup(func() { connectSystemBus = before })
	k := testKit(t, "")
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	return k, l
}

func (l *loop) until(what string, ok func() bool) {
	l.t.Helper()
	deadline := time.After(10 * time.Second)
	for !ok() {
		select {
		case fn := <-l.queue:
			fn()
		case <-deadline:
			l.t.Fatalf("no %s in 10s", what)
		}
	}
}

func (l *loop) drain(d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case fn := <-l.queue:
			fn()
		case <-deadline:
			return
		}
	}
}

func switchNamed(t *testing.T, root widget.Widget, name string) *widget.Switch {
	t.Helper()
	for _, s := range treetest.All[*widget.Switch](root) {
		if s.AccessibleName() == name {
			return s
		}
	}
	t.Fatalf("no switch %q", name)
	return nil
}

func TestEachDateControlIsOneInteractiveCallAndARefusalRevertsIt(t *testing.T) {
	bus := dbustest.Start(t)
	td := &fakeSys{props: map[string]any{"Timezone": "UTC", "NTP": true, "CanNTP": true, "LocalRTC": false}}
	serveFake(t, bus, timedateName, timedatePath, td, fakeTimedate{td})
	k, l := systemKit(t, bus)
	footer := dateTimeFooter(k).(*closingBox)
	t.Cleanup(footer.close)
	ntp := switchNamed(t, footer, "Set time automatically")
	if !ntp.On() {
		t.Fatal("the switch does not show NTP on")
	}
	_, reads := td.snapshot()
	l.drain(300 * time.Millisecond)
	if calls, again := td.snapshot(); len(calls) != 0 || again != reads {
		t.Fatalf("an idle page made %d calls and %d more reads", len(calls), again-reads)
	}
	ntp.SetOn(false)
	l.until("SetNTP", func() bool { c, _ := td.snapshot(); return len(c) == 1 })
	calls, _ := td.snapshot()
	if calls[0].method != "SetNTP" || fmt.Sprint(calls[0].args) != "[false true]" {
		t.Fatalf("%+v, want one SetNTP(false, interactive)", calls)
	}
	td.set("NTP", false)
	l.until("the manual entry enabled by the change", func() bool { return treetest.First[*widget.Entry](t, footer).Enabled() })
	manual := treetest.First[*widget.Entry](t, footer)
	manual.SetText("2026-03-04 13:05:06")
	treetest.Button(t, footer, "Set").OnClick()
	l.until("SetTime", func() bool { c, _ := td.snapshot(); return len(c) == 2 })
	want := time.Date(2026, time.March, 4, 13, 5, 6, 0, time.Local).UnixMicro()
	if calls, _ := td.snapshot(); calls[1].method != "SetTime" || fmt.Sprint(calls[1].args) != fmt.Sprint([]any{want, false, true}) {
		t.Fatalf("%+v, want one SetTime(%d, absolute, interactive)", calls, want)
	}
	td.mu.Lock()
	td.refuse = "Changing system settings via systemd is not supported on NixOS."
	td.mu.Unlock()
	zone := treetest.First[*choiceEditor](t, footer)
	zone.pick("Europe/Copenhagen")
	l.until("the refusal shown", func() bool {
		return treetest.First[*sysPanel](t, footer).problem.Visible()
	})
	calls, _ = td.snapshot()
	if len(calls) != 3 || calls[2].method != "SetTimezone" || fmt.Sprint(calls[2].args) != "[Europe/Copenhagen true]" {
		t.Fatalf("%+v", calls)
	}
	if p := treetest.First[*sysPanel](t, footer); !strings.Contains(p.problem.Text(), "NixOS") || zone.entry.Text() != "UTC" {
		t.Fatalf("problem %q, zone %q: want the service's message and the old zone", p.problem.Text(), zone.entry.Text())
	}
}

func TestTheFormatsWriteLocaleConfAndChangeThePreview(t *testing.T) {
	bus := dbustest.Start(t)
	lc := &fakeSys{props: map[string]any{"Locale": []string{"LANG=en_US.UTF-8"}, "VConsoleKeymap": "us", "X11Layout": "us", "X11Model": "", "X11Variant": "", "X11Options": ""}}
	serveFake(t, bus, localeName, localePath, lc, fakeLocaled{lc})
	before := listLocales
	listLocales = func() ([]string, error) { return []string{"en_GB.UTF-8", "en_US.UTF-8"}, nil }
	t.Cleanup(func() { listLocales = before })
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	put(t, filepath.Join(config, "locale.conf"), "# mine\nLC_NUMERIC=en_US.UTF-8\n")
	k, l := systemKit(t, bus)
	footer := regionFooter(k).(*closingBox)
	t.Cleanup(footer.close)
	l.until("the system controls", func() bool { return len(treetest.All[*widget.Dropdown](footer)) == len(userFormats)+1 })
	preview := func() string {
		for _, lbl := range treetest.All[*widget.Label](footer) {
			if strings.Count(lbl.Text(), " · ") == 2 {
				return lbl.Text()
			}
		}
		return ""
	}
	first := preview()
	if !strings.Contains(first, "$1,234.50") {
		t.Fatalf("the preview %q is not the language's money", first)
	}
	dropdownNamed(t, footer, "LC_MONETARY").SetSelected(1)
	if got := fileText(t, filepath.Join(config, "locale.conf")); got != "# mine\nLC_NUMERIC=en_US.UTF-8\nLC_MONETARY=en_GB.UTF-8\n" {
		t.Fatalf("locale.conf %q", got)
	}
	if got := preview(); !strings.Contains(got, "£1,234.50") {
		t.Fatalf("the preview did not follow LC_MONETARY: %q", got)
	}
	dropdownNamed(t, footer, "LC_NUMERIC").SetSelected(0)
	if got := fileText(t, filepath.Join(config, "locale.conf")); got != "# mine\nLC_MONETARY=en_GB.UTF-8\n" {
		t.Fatalf("locale.conf %q after the same-as-language choice", got)
	}
	if calls, _ := lc.snapshot(); len(calls) != 0 {
		t.Fatalf("the user's formats called localed: %+v", calls)
	}
	dropdownNamed(t, footer, "Language").SetSelected(0)
	l.until("SetLocale", func() bool { c, _ := lc.snapshot(); return len(c) == 1 })
	if calls, _ := lc.snapshot(); calls[0].method != "SetLocale" || fmt.Sprint(calls[0].args) != "[[LANG=en_GB.UTF-8] true]" {
		t.Fatalf("%+v", calls)
	}
	keymap := treetest.First[*widget.Entry](t, footer)
	keymap.SetText("dk")
	keymap.OnActivate("dk")
	l.until("SetVConsoleKeyboard", func() bool { c, _ := lc.snapshot(); return len(c) == 2 })
	if calls, _ := lc.snapshot(); calls[1].method != "SetVConsoleKeyboard" || fmt.Sprint(calls[1].args) != "[dk  false true]" {
		t.Fatalf("%+v", calls)
	}
}

func TestCopyMyLayoutSendsLysWMsKeyboardInOneCall(t *testing.T) {
	bus := dbustest.Start(t)
	lc := &fakeSys{props: map[string]any{"Locale": []string{"LANG=en_US.UTF-8"}, "VConsoleKeymap": "us", "X11Layout": "us", "X11Model": "", "X11Variant": "", "X11Options": ""}}
	serveFake(t, bus, localeName, localePath, lc, fakeLocaled{lc})
	before := listLocales
	listLocales = func() ([]string, error) { return []string{"en_US.UTF-8"}, nil }
	t.Cleanup(func() { listLocales = before })
	k, w, _, _ := wmKit(t, "input:\n  keyboard:\n    layout: us,dk\n    variant: \",nodeadkeys\"\n    options: caps:escape\n    model: pc105\n")
	k.wm = w
	beforeBus := connectSystemBus
	connectSystemBus = func() (*dbus.Conn, error) { return dbus.Connect(bus.Address) }
	t.Cleanup(func() { connectSystemBus = beforeBus })
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	footer := regionFooter(k).(*closingBox)
	t.Cleanup(footer.close)
	l.until("the system controls", func() bool { return len(treetest.All[*widget.Dropdown](footer)) == len(userFormats)+1 })
	treetest.Button(t, footer, "Copy my layout").OnClick()
	l.until("SetX11Keyboard", func() bool { c, _ := lc.snapshot(); return len(c) == 1 })
	if calls, _ := lc.snapshot(); calls[0].method != "SetX11Keyboard" || fmt.Sprint(calls[0].args) != "[us,dk pc105 ,nodeadkeys caps:escape false true]" {
		t.Fatalf("%+v", calls)
	}
}

func TestAPageWithoutTheServiceSaysSoAndOffersNothing(t *testing.T) {
	bus := dbustest.Start(t)
	k, l := systemKit(t, bus)
	footer := dateTimeFooter(k).(*closingBox)
	t.Cleanup(footer.close)
	l.drain(100 * time.Millisecond)
	if len(treetest.All[*choiceEditor](footer)) != 0 {
		t.Fatal("controls without timedated")
	}
	found := false
	for _, lbl := range treetest.All[*widget.Label](footer) {
		found = found || (strings.Contains(lbl.Text(), "org.freedesktop.timedate1") && strings.Contains(lbl.Text(), "is not running"))
	}
	if !found {
		t.Fatal("the page does not say timedated is missing")
	}
}
