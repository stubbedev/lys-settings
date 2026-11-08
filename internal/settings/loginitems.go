package settings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/lys-lib/atomicfile"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/desktopentry"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	loginItemsID      = "login-items"
	autostartDirName  = "autostart"
	desktopSuffix     = ".desktop"
	keyHidden         = "Hidden"
	keySkip           = "X-systemd-skip"
	nixStore          = "/nix/store/"
	loginItemsTimeout = 5 * time.Second
)

var sessionTargets = []string{"graphical-session.target", "lys-wm-session.target"}

func loginItemsPage(*config.Config) pageSpec {
	p := pageSpec{
		id: loginItemsID, navKey: "settings-nav-login-items", icon: "ld-power-symbolic",
		header: "settings-page-login-items",
		footer: loginItemsFooter,
	}
	if wmconfig.Installed() {
		p.sections = []sectionSpec{{title: "settings-section-login-commands", rows: []rowSpec{wmField("autostart", commandList)}}}
	}
	return p
}

type autostartDirs struct {
	user   string
	system []string
}

func (h host) autostartDirs() autostartDirs {
	config := h.getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(h.getenv("HOME"), ".config")
	}
	system := h.getenv("XDG_CONFIG_DIRS")
	if system == "" {
		system = "/etc/xdg"
	}
	d := autostartDirs{user: filepath.Join(config, autostartDirName)}
	for _, dir := range filepath.SplitList(system) {
		if dir != "" {
			d.system = append(d.system, filepath.Join(dir, autostartDirName))
		}
	}
	return d
}

type autostartEntry struct {
	id, name, icon string
	user, system   string
	enabled        bool
}

func (e autostartEntry) removable() bool { return e.user != "" && e.system == "" }

type readDirFunc func(string) ([]fs.DirEntry, error)

func readAutostart(d autostartDirs, desktops []string, readDir readDirFunc) ([]autostartEntry, error) {
	byID := map[string]*autostartEntry{}
	var order []string
	scan := func(dir string, user bool) error {
		files, err := readDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the autostart directory %s: %w", dir, err)
		}
		for _, f := range files {
			id := f.Name()
			if f.IsDir() || !strings.HasSuffix(id, desktopSuffix) {
				continue
			}
			e, seen := byID[id]
			if !seen {
				e = &autostartEntry{id: id}
				byID[id] = e
				order = append(order, id)
			}
			path := filepath.Join(dir, id)
			switch {
			case user:
				e.user = path
			case e.system == "":
				e.system = path
			}
		}
		return nil
	}
	if err := scan(d.user, true); err != nil {
		return nil, err
	}
	for _, dir := range d.system {
		if err := scan(dir, false); err != nil {
			return nil, err
		}
	}
	out := make([]autostartEntry, 0, len(order))
	for _, id := range order {
		e := byID[id]
		kf, err := desktopentry.LoadKeyFile(cmp.Or(e.user, e.system))
		if err != nil {
			return nil, fmt.Errorf("read the autostart entry %s: %w", cmp.Or(e.user, e.system), err)
		}
		e.name, _ = kf.LocaleString(desktopentry.DesktopGroup, "Name")
		e.name = cmp.Or(e.name, strings.TrimSuffix(id, desktopSuffix))
		e.icon, _ = kf.String(desktopentry.DesktopGroup, "Icon")
		e.enabled = !kf.Bool(desktopentry.DesktopGroup, keyHidden) && !kf.Bool(desktopentry.DesktopGroup, keySkip) && kf.ShownIn(desktops)
		out = append(out, *e)
	}
	slices.SortStableFunc(out, func(a, b autostartEntry) int { return cmp.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
	return out, nil
}

func setAutostart(d autostartDirs, e autostartEntry, on bool) error {
	source := cmp.Or(e.user, e.system)
	data, err := os.ReadFile(source) //nolint:gosec // an autostart entry the page listed
	if err != nil {
		return fmt.Errorf("read the autostart entry %s: %w", source, err)
	}
	data = desktopentry.SetKey(data, desktopentry.DesktopGroup, keyHidden, strconv.FormatBool(!on))
	return writeUserEntry(d, e.id, data)
}

func writeUserEntry(d autostartDirs, id string, data []byte) error {
	if err := os.MkdirAll(d.user, 0o755); err != nil { //nolint:gosec // ~/.config/autostart, readable as any config dir
		return fmt.Errorf("create %s: %w", d.user, err)
	}
	path := filepath.Join(d.user, id)
	if err := atomicfile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write the autostart entry %s: %w", path, err)
	}
	return nil
}

func addAutostart(d autostartDirs, app desktopentry.App) error {
	data, err := os.ReadFile(app.Path)
	if err != nil {
		return fmt.Errorf("read the desktop entry %s: %w", app.Path, err)
	}
	return writeUserEntry(d, filepath.Base(app.ID), data)
}

func removeAutostart(e autostartEntry) error {
	if !e.removable() {
		return fmt.Errorf("the autostart entry %s is the system's, not yours to remove", e.id)
	}
	if err := os.Remove(e.user); err != nil {
		return fmt.Errorf("remove the autostart entry %s: %w", e.user, err)
	}
	return nil
}

type userUnit struct {
	name, description, active, fileState, path string
}

func (u userUnit) nix() bool { return strings.HasPrefix(u.path, nixStore) }

func (u userUnit) switchable() bool {
	return !u.nix() && (u.fileState == "enabled" || u.fileState == "disabled")
}

type userManager interface {
	units(ctx context.Context) ([]userUnit, error)
	setEnabled(ctx context.Context, name string, on bool) error
}

const (
	systemdName    = "org.freedesktop.systemd1"
	systemdPath    = dbus.ObjectPath("/org/freedesktop/systemd1")
	systemdManager = systemdName + ".Manager"
	systemdUnit    = systemdName + ".Unit"
)

type systemdUser struct {
	conn    *dbus.Conn
	resolve func(string) (string, error)
}

func (s systemdUser) manager() dbus.BusObject { return s.conn.Object(systemdName, systemdPath) }

func (s systemdUser) units(ctx context.Context) ([]userUnit, error) {
	var names []string
	for _, target := range sessionTargets {
		var path dbus.ObjectPath
		if err := s.manager().CallWithContext(ctx, systemdManager+".LoadUnit", 0, target).Store(&path); err != nil {
			return nil, fmt.Errorf("load %s: %w", target, err)
		}
		v, err := s.conn.Object(systemdName, path).GetProperty(systemdUnit + ".Wants")
		if err != nil {
			return nil, fmt.Errorf("read what %s wants: %w", target, err)
		}
		wants, _ := v.Value().([]string)
		for _, w := range wants {
			if strings.HasSuffix(w, ".service") && !slices.Contains(names, w) {
				names = append(names, w)
			}
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, nil
	}
	var files []struct {
		Path  string
		State string
	}
	if err := s.manager().CallWithContext(ctx, systemdManager+".ListUnitFilesByPatterns", 0, []string{}, names).Store(&files); err != nil {
		return nil, fmt.Errorf("list the unit files of %v: %w", names, err)
	}
	var listed []struct {
		Name, Description, Load, Active, Sub, Following string
		Path                                            dbus.ObjectPath
		JobID                                           uint32
		JobType                                         string
		JobPath                                         dbus.ObjectPath
	}
	if err := s.manager().CallWithContext(ctx, systemdManager+".ListUnitsByNames", 0, names).Store(&listed); err != nil {
		return nil, fmt.Errorf("list the units %v: %w", names, err)
	}
	out := make([]userUnit, len(names))
	for i, n := range names {
		out[i] = userUnit{name: n, active: "inactive"}
	}
	at := func(name string) *userUnit {
		i, ok := slices.BinarySearch(names, name)
		if !ok {
			return nil
		}
		return &out[i]
	}
	for _, f := range files {
		if u := at(filepath.Base(f.Path)); u != nil {
			u.fileState, u.path = f.State, f.Path
			if resolved, err := s.resolve(f.Path); err == nil {
				u.path = resolved
			}
		}
	}
	for _, l := range listed {
		if u := at(l.Name); u != nil {
			u.description, u.active = l.Description, l.Active
		}
	}
	return out, nil
}

func (s systemdUser) setEnabled(ctx context.Context, name string, on bool) error {
	var call *dbus.Call
	if on {
		call = s.manager().CallWithContext(ctx, systemdManager+".EnableUnitFiles", 0, []string{name}, false, true)
	} else {
		call = s.manager().CallWithContext(ctx, systemdManager+".DisableUnitFiles", 0, []string{name}, false)
	}
	if call.Err != nil {
		return fmt.Errorf("turn %s %s: %w", name, map[bool]string{true: "on", false: "off"}[on], call.Err)
	}
	if err := s.manager().CallWithContext(ctx, systemdManager+".Reload", 0).Err; err != nil {
		return fmt.Errorf("reload the user manager after %s: %w", name, err)
	}
	return nil
}

var openUserManager = func() (userManager, func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	return systemdUser{conn: conn, resolve: filepath.EvalSymlinks}, func() { _ = conn.Close() }, nil
}

func withUserManager[T any](fn func(context.Context, userManager) (T, error)) (T, error) {
	var zero T
	m, closeManager, err := openUserManager()
	if err != nil {
		return zero, err
	}
	defer closeManager()
	ctx, cancel := context.WithTimeout(context.Background(), loginItemsTimeout)
	defer cancel()
	return fn(ctx, m)
}

type loginItems struct {
	k        *kit
	dirs     autostartDirs
	desktops []string
	apps     *widget.Box
	services *widget.Box
	problem  *widget.Label
}

func loginItemsFooter(k *kit) widget.Widget {
	l := &loginItems{k: k, dirs: systemHost.autostartDirs(), desktops: desktopentry.CurrentDesktops()}
	content := gapBox(widget.Column)
	l.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	l.problem.SetVisible(false)
	content.Append(l.problem, false)
	l.apps = gapBox(widget.Column)
	content.Append(loginSection(k, "settings-section-login-apps", l.apps), false)
	l.services = gapBox(widget.Column)
	content.Append(loginSection(k, "settings-section-login-services", l.services), false)
	l.loadApps()
	l.loadServices()
	return content
}

func loginSection(k *kit, title string, body widget.Widget) widget.Widget {
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

func (l *loginItems) fail(err error) {
	l.problem.SetVisible(err != nil)
	if err != nil {
		l.problem.SetText(err.Error())
	}
}

type autostartListing struct {
	entries []autostartEntry
	err     error
}

func (l *loginItems) loadApps() {
	async(l.k, func() autostartListing {
		entries, err := readAutostart(l.dirs, l.desktops, os.ReadDir)
		return autostartListing{entries, err}
	}, l.showApps)
}

func (l *loginItems) showApps(r autostartListing) {
	t := locale.T()
	l.apps.Clear()
	if r.err != nil {
		l.apps.Append(wrapped(l.k.label(r.err.Error(), css.Error, css.Caption)), false)
		return
	}
	rows := widget.NewItemGroup()
	for _, e := range r.entries {
		rows.Append(l.appRow(e))
	}
	if len(r.entries) == 0 {
		rows.Append(l.k.label(t.Get("settings-login-apps-none"), css.DimLabel))
	}
	l.apps.Append(rows, false)
	var add *widget.Button
	add = l.k.button(l.k.label(t.Get("settings-login-apps-add")), func() { l.pickApp(add) }, css.Small)
	add.SetHAlign(widget.AlignStart)
	l.apps.Append(add, false)
}

func (l *loginItems) itemRow(icon, name, detail string) *widget.Box {
	row := gapBox(widget.Row)
	row.SetElement("item")
	row.AddClass(css.Small, "setting-row", "login-item")
	if icon != "" {
		row.AppendAligned(l.k.icon(icon, "login-item-icon"), false, widget.AlignCenter)
	}
	text := widget.NewBox(widget.Column, widget.Current().Spacing/4, 0)
	text.Append(l.k.label(name, "setting-label"), false)
	if detail != "" {
		text.Append(wrapped(l.k.label(detail, "setting-description", css.Caption, css.DimLabel)), false)
	}
	row.AppendAligned(text, true, widget.AlignCenter)
	return row
}

func (l *loginItems) appRow(e autostartEntry) widget.Widget {
	t := locale.T()
	detail := t.Get("settings-login-apps-system")
	if e.user != "" {
		detail = t.Get("settings-login-apps-user")
	}
	row := l.itemRow(e.icon, e.name, detail)
	if e.removable() {
		row.AppendAligned(l.k.iconButton("ld-trash-2-symbolic", "settings-login-apps-remove", func() {
			l.act(func() error { return removeAutostart(e) })
		}, css.Ghost), false, widget.AlignCenter)
	}
	sw := widget.NewSwitch(e.enabled)
	sw.SetAccessibleName(e.name)
	sw.OnChanged = func(on bool) { l.act(func() error { return setAutostart(l.dirs, e, on) }) }
	row.AppendAligned(sw, false, widget.AlignCenter)
	return row
}

func (l *loginItems) act(change func() error) {
	async(l.k, change, func(err error) {
		l.fail(err)
		l.loadApps()
	})
}

func (l *loginItems) pickApp(anchor widget.Widget) {
	openAppPicker(l.k, anchor, scanInstalled(desktopentry.ApplicationDirs()).shown, func(app desktopentry.App) {
		l.act(func() error { return addAutostart(l.dirs, app) })
	})
}

type unitsListing struct {
	units []userUnit
	err   error
}

func (l *loginItems) loadServices() {
	async(l.k, func() unitsListing {
		units, err := withUserManager(func(ctx context.Context, m userManager) ([]userUnit, error) { return m.units(ctx) })
		return unitsListing{units, err}
	}, l.showServices)
}

func (l *loginItems) showServices(r unitsListing) {
	t := locale.T()
	l.services.Clear()
	if r.err != nil {
		l.services.Append(wrapped(l.k.label(t.Get("settings-login-services-unavailable")+": "+r.err.Error(), css.Error, css.Caption)), false)
		return
	}
	rows := widget.NewItemGroup()
	for _, u := range r.units {
		rows.Append(l.serviceRow(u))
	}
	if len(r.units) == 0 {
		rows.Append(l.k.label(t.Get("settings-login-services-none"), css.DimLabel))
	}
	l.services.Append(rows, false)
}

func (l *loginItems) serviceRow(u userUnit) widget.Widget {
	t := locale.T()
	detail := t.Get("settings-login-services-state", i18n.Str("active", u.active), i18n.Str("file", cmp.Or(u.fileState, "-")))
	if u.nix() {
		detail += " · " + t.Get("settings-login-services-nix")
	}
	row := l.itemRow("", cmp.Or(u.description, u.name), u.name+" · "+detail)
	if !u.switchable() {
		return row
	}
	sw := widget.NewSwitch(u.fileState == "enabled")
	sw.SetAccessibleName(u.name)
	sw.OnChanged = func(on bool) {
		async(l.k, func() error {
			_, err := withUserManager(func(ctx context.Context, m userManager) (struct{}, error) {
				return struct{}{}, m.setEnabled(ctx, u.name, on)
			})
			return err
		}, func(err error) {
			l.fail(err)
			l.loadServices()
		})
	}
	row.AppendAligned(sw, false, widget.AlignCenter)
	return row
}

func commandList(r *rowSpec) {
	r.fullWidth = true
	r.editor = func(k *kit, s slot, _ config.FieldMeta) control {
		spec := stringListSpec()
		spec.pick = func(anchor widget.Widget, choose func(any)) {
			programs := systemHost.programs()
			names := make([]string, len(programs))
			for i, p := range programs {
				names[i] = p.value
			}
			openSearchPicker(k, anchor, names, pickerSpec{
				class: "choice-popover", placeholder: "settings-choice-search", maxH: choiceListPx,
				row: func(k *kit, name string) widget.Widget { return k.label(name) },
			}, func(name string) { choose(name) })
		}
		return newListEditor(k, s, spec)
	}
}
