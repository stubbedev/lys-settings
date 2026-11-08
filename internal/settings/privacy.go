package settings

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/desktopentry"
	"github.com/stubbedev/lys-desktop/portal/permissions"
	"github.com/stubbedev/lys-desktop/settingspage"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

func privacyPage(*config.Config) pageSpec {
	return pageSpec{
		id: settingspage.PrivacyID, navKey: "settings-nav-privacy", icon: "ld-eye-symbolic",
		header: "settings-page-privacy",
		sections: []sectionSpec{
			{title: "settings-section-screen-share", rows: fields("portal.screen-cast.reminder-days")},
		},
		footer: grantsFooter,
	}
}

type grantStore interface {
	List(ctx context.Context) ([]permissions.App, error)
	Revoke(ctx context.Context, app string, table permissions.Table, id string) (int, error)
	Set(ctx context.Context, app string, table permissions.Table, id string, perms []string) error
	Watch(changed func()) (stop func(), err error)
}

const grantsTimeout = 5 * time.Second

var (
	switchedTables = []permissions.Table{permissions.Background, permissions.Notifications}
	readOnlyTables = []permissions.Table{permissions.GameMode, permissions.Realtime}
	permissionYes  = []string{"yes"}
	permissionNo   = []string{"no"}
	grantsByApp    = 0
	grantsByTable  = 1
)

var openGrants = func() (grantStore, func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	return permissions.Store{Conn: conn, ShortcutsPath: permissions.ShortcutsPath()}, func() { _ = conn.Close() }, nil
}

func withStore[T any](store grantStore, fn func(context.Context, grantStore) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), grantsTimeout)
	defer cancel()
	return fn(ctx, store)
}

type grantsListing struct {
	apps []permissions.App
	err  error
}

type grantsList struct {
	*widget.Box
	k         *kit
	store     grantStore
	release   func()
	stop      func()
	rows      *widget.Box
	view      *widget.Dropdown
	apps      []permissions.App
	installed installedApps
	failure   string
	problem   string
}

// unidentifiedApp is the app ID xdg-desktop-portal records for a caller
// whose ID it could not read.
const unidentifiedApp = "[Invalid UTF-8]"

func grantsFooter(k *kit) widget.Widget {
	t := locale.T()
	l := &grantsList{Box: sectionStack(), k: k, rows: widget.NewBox(widget.Column, 0, 0)}
	l.view = widget.NewDropdown(k.face, k.textPx(), []string{t.Get("settings-privacy-by-app"), t.Get("settings-privacy-by-permission")}, grantsByApp)
	l.view.SetAccessibleName(t.Get("settings-privacy-view"))
	l.view.OnSelect = func(int) { l.render() }
	body := widget.NewBox(widget.Column, 0, 0)
	body.Append(panelRow(k, t.Get("settings-privacy-view"), "", l.view), false)
	body.Append(widget.NewSeparator(widget.Horizontal), false)
	body.Append(l.rows, false)
	l.Append(newSection(k, "settings-section-app-permissions", body), false)
	dirs := desktopentry.ApplicationDirs()
	async(k, func() installedApps { return scanInstalled(dirs) }, func(ix installedApps) {
		l.installed = ix
		l.render()
	})
	store, release, err := openGrants()
	if err != nil {
		l.problem = err.Error()
		l.render()
		return l
	}
	l.store, l.release = store, release
	if stop, err := store.Watch(func() { k.invoke(l.reload) }); err != nil {
		log.Printf("settings: follow the permission store: %v", err)
	} else {
		l.stop = stop
	}
	l.reload()
	return l
}

func (l *grantsList) close() {
	if l.stop != nil {
		l.stop()
		l.stop = nil
	}
	if l.release != nil {
		l.release()
		l.release = nil
	}
	l.store = nil
}

func (l *grantsList) reload() {
	if l.store == nil {
		return
	}
	store := l.store
	async(l.k, func() grantsListing {
		apps, err := withStore(store, func(ctx context.Context, s grantStore) ([]permissions.App, error) { return s.List(ctx) })
		return grantsListing{apps, err}
	}, func(r grantsListing) {
		l.apps, l.problem = r.apps, ""
		if r.err != nil {
			log.Printf("settings: list the apps' grants: %v", r.err)
			l.problem = locale.T().Get("settings-privacy-unavailable") + ": " + r.err.Error()
		}
		l.render()
	})
}

func (l *grantsList) render() {
	t := locale.T()
	l.rows.Clear()
	group := widget.NewItemGroup()
	l.rows.Append(group, false)
	if l.failure != "" {
		group.Append(insetRow(wrapped(l.k.label(l.failure, css.Error, css.Caption))))
		l.failure = ""
	}
	switch {
	case l.problem != "":
		group.Append(insetRow(wrapped(l.k.label(l.problem, css.Error, css.Caption))))
		return
	case len(l.apps) == 0:
		group.Append(l.k.emptyRow("ld-shield-check-symbolic", t.Get("settings-privacy-none")))
		return
	}
	if l.view.Selected() == grantsByTable {
		for _, table := range permissions.Tables {
			first := true
			for _, app := range l.apps {
				for _, g := range app.Grants {
					if g.Table != table {
						continue
					}
					if first {
						header := widget.NewItem(l.k.face, t.Get("settings-privacy-table-"+string(table)), "")
						header.AddClass("settings-privacy-app")
						group.Append(header)
						first = false
					}
					group.Append(l.grantRow(app.ID, g, l.appTitle(app.ID)+" · "+g.ID))
				}
			}
		}
		return
	}
	for _, app := range l.apps {
		group.Append(l.appHeader(app.ID))
		for _, g := range app.Grants {
			group.Append(l.grantRow(app.ID, g, t.Get("settings-privacy-table-"+string(g.Table))+" · "+g.ID))
		}
	}
}

// appTitle names the app an ID stands for: its installed name, the apps
// outside a sandbox for the empty ID, or the ID itself.
func (l *grantsList) appTitle(id string) string {
	t := locale.T()
	switch id {
	case "":
		return t.Get("settings-privacy-host-apps")
	case unidentifiedApp:
		return t.Get("settings-privacy-unknown-app")
	}
	if a, ok := l.installed.byID[id]; ok && a.DisplayName() != "" {
		return a.DisplayName()
	}
	return id
}

func (l *grantsList) appHeader(id string) *widget.Item {
	title := l.appTitle(id)
	detail := ""
	if title != id && id != unidentifiedApp {
		detail = id
	}
	header := widget.NewItem(l.k.face, title, detail)
	header.AddClass("settings-privacy-app")
	icon := appFallbackIcon
	if a, ok := l.installed.byID[id]; ok && a.Icon() != "" {
		icon = a.Icon()
	}
	header.SetMedia(widget.NewThemeIcon(icon, appIconPx), true)
	return header
}

func (l *grantsList) grantRow(app string, g permissions.Grant, title string) widget.Widget {
	t := locale.T()
	row := widget.NewItem(l.k.face, title, g.Value)
	row.AddClass(css.Small, "settings-privacy-grant")
	switch {
	case slices.Contains(readOnlyTables, g.Table):
		row.AddAction(l.k.label(t.Get("settings-privacy-read-only"), css.Caption, css.DimLabel))
	case slices.Contains(switchedTables, g.Table):
		sw := widget.NewSwitch(slices.Contains(strings.Split(g.Value, ","), "yes"))
		sw.SetAccessibleName(app + " " + t.Get("settings-privacy-table-"+string(g.Table)))
		sw.OnChanged = func(on bool) {
			perms := permissionNo
			if on {
				perms = permissionYes
			}
			l.act(func(ctx context.Context, s grantStore) error { return s.Set(ctx, app, g.Table, g.ID, perms) }, "settings-privacy-set-failed")
		}
		row.AddAction(sw)
	default:
		var revoke *widget.Button
		revoke, _ = l.k.contentButton("ld-trash-2-symbolic", t.Get("settings-privacy-revoke"), func() {
			revoke.SetEnabled(false)
			l.act(func(ctx context.Context, s grantStore) error {
				_, err := s.Revoke(ctx, app, g.Table, g.ID)
				return err
			}, "settings-privacy-revoke-failed")
		}, css.Secondary)
		row.AddAction(revoke)
	}
	return row
}

func (l *grantsList) act(change func(context.Context, grantStore) error, failedKey string) {
	store := l.store
	async(l.k, func() error {
		_, err := withStore(store, func(ctx context.Context, s grantStore) (struct{}, error) { return struct{}{}, change(ctx, s) })
		return err
	}, func(err error) {
		if err != nil {
			log.Printf("settings: change a grant: %v", err)
			l.failure = locale.T().Get(failedKey) + ": " + err.Error()
		}
		l.reload()
	})
}

func wrapped(l *widget.Label) *widget.Label {
	l.SetWrap(true)
	return l
}
