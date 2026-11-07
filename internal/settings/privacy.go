package settings

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/portal/permissions"
	"github.com/stubbedev/lys-shell/settingspage"
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
	k       *kit
	store   grantStore
	release func()
	stop    func()
	rows    *widget.Box
	view    *widget.Dropdown
	apps    []permissions.App
	failure string
	problem string
}

func grantsFooter(k *kit) widget.Widget {
	t := locale.T()
	l := &grantsList{Box: gapBox(widget.Column), k: k, rows: gapBox(widget.Column)}
	l.AddClass("settings-section")
	l.Append(k.label(t.Get("settings-section-app-permissions"), "settings-section-title"), false)
	l.view = widget.NewDropdown(k.face, k.textPx(), []string{t.Get("settings-privacy-by-app"), t.Get("settings-privacy-by-permission")}, grantsByApp)
	l.view.SetAccessibleName(t.Get("settings-privacy-view"))
	l.view.OnSelect = func(int) { l.render() }
	l.view.SetHAlign(widget.AlignStart)
	l.Append(l.view, false)
	l.Append(l.rows, false)
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
	if l.failure != "" {
		l.rows.Append(wrapped(l.k.label(l.failure, css.Error, css.Caption)), false)
		l.failure = ""
	}
	switch {
	case l.problem != "":
		l.rows.Append(wrapped(l.k.label(l.problem, css.Error, css.Caption)), false)
		return
	case len(l.apps) == 0:
		l.rows.Append(l.k.label(t.Get("settings-privacy-none"), css.DimLabel), false)
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
						l.rows.Append(l.k.label(t.Get("settings-privacy-table-"+string(table)), "settings-privacy-app", css.Heading), false)
						first = false
					}
					l.rows.Append(l.grantRow(app.ID, g, app.ID+" · "+g.ID), false)
				}
			}
		}
		return
	}
	for _, app := range l.apps {
		l.rows.Append(l.k.label(app.ID, "settings-privacy-app", css.Heading), false)
		for _, g := range app.Grants {
			l.rows.Append(l.grantRow(app.ID, g, t.Get("settings-privacy-table-"+string(g.Table))+" · "+g.ID), false)
		}
	}
}

func (l *grantsList) grantRow(app string, g permissions.Grant, title string) widget.Widget {
	t := locale.T()
	text := gapBox(widget.Column)
	text.Append(l.k.label(title), false)
	if g.Value != "" {
		text.Append(wrapped(l.k.label(g.Value, css.Caption, css.DimLabel)), false)
	}
	row := gapBox(widget.Row)
	row.AddClass("settings-privacy-grant")
	row.Append(text, true)
	switch {
	case slices.Contains(readOnlyTables, g.Table):
		row.AppendAligned(l.k.label(t.Get("settings-privacy-read-only"), css.Caption, css.DimLabel), false, widget.AlignCenter)
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
		row.AppendAligned(sw, false, widget.AlignCenter)
	default:
		revoke := l.k.button(l.k.label(t.Get("settings-privacy-revoke")), nil, css.DestructiveAction)
		revoke.OnClick = func() {
			revoke.SetEnabled(false)
			l.act(func(ctx context.Context, s grantStore) error {
				_, err := s.Revoke(ctx, app, g.Table, g.ID)
				return err
			}, "settings-privacy-revoke-failed")
		}
		row.AppendAligned(revoke, false, widget.AlignCenter)
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
