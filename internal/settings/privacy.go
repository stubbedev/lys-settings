package settings

import (
	"context"
	"fmt"
	"log"
	"time"

	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/portal/permissions"
	"github.com/stubbedev/lys-shell/settingspage"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// privacyPage lists what apps were granted through the portal, with a
// revoke control per grant, beside the persistent screen-share reminder.
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

// grantStore is what the page reads and revokes: lys-shell's per-app
// model of the permission store and the shortcut store.
type grantStore interface {
	List(ctx context.Context) ([]permissions.App, error)
	Revoke(ctx context.Context, app string, table permissions.Table, id string) (int, error)
}

// grantsTimeout bounds one listing or revoke.
const grantsTimeout = 5 * time.Second

// openGrants is the grants over a session-bus connection of their own,
// and its close (a seam for tests).
var openGrants = func() (grantStore, func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	return permissions.Store{Conn: conn, ShortcutsPath: permissions.ShortcutsPath()}, func() { _ = conn.Close() }, nil
}

// withGrants runs fn over freshly opened grants, bounded.
func withGrants[T any](fn func(context.Context, grantStore) (T, error)) (T, error) {
	var zero T
	store, closeStore, err := openGrants()
	if err != nil {
		return zero, err
	}
	defer closeStore()
	ctx, cancel := context.WithTimeout(context.Background(), grantsTimeout)
	defer cancel()
	return fn(ctx, store)
}

// grantsListing is a listing's outcome.
type grantsListing struct {
	apps []permissions.App
	err  error
}

// grantsList is every app's grants, a revoke button each; it lists them
// again after every revoke, above them why the revoke failed if it did.
type grantsList struct {
	k       *kit
	rows    *widget.Box
	failure string
}

func grantsFooter(k *kit) widget.Widget {
	section := gapBox(widget.Column)
	section.AddClass("settings-section")
	section.Append(k.label(locale.T().Get("settings-section-app-permissions"), "settings-section-title"), false)
	l := &grantsList{k: k, rows: gapBox(widget.Column)}
	section.Append(l.rows, false)
	l.reload()
	return section
}

func (l *grantsList) reload() {
	async(l.k, func() grantsListing {
		apps, err := withGrants(func(ctx context.Context, s grantStore) ([]permissions.App, error) { return s.List(ctx) })
		return grantsListing{apps, err}
	}, l.show)
}

func (l *grantsList) show(r grantsListing) {
	t := locale.T()
	l.rows.Clear()
	if l.failure != "" {
		l.rows.Append(wrapped(l.k.label(l.failure, css.Error, css.Caption)), false)
		l.failure = ""
	}
	switch {
	case r.err != nil:
		log.Printf("settings: list the apps' grants: %v", r.err)
		l.rows.Append(wrapped(l.k.label(t.Get("settings-privacy-unavailable")+": "+r.err.Error(), css.Error, css.Caption)), false)
		return
	case len(r.apps) == 0:
		l.rows.Append(l.k.label(t.Get("settings-privacy-none"), css.DimLabel), false)
		return
	}
	for _, app := range r.apps {
		l.rows.Append(l.k.label(app.ID, "settings-privacy-app", css.Heading), false)
		for _, g := range app.Grants {
			l.rows.Append(l.grantRow(app.ID, g), false)
		}
	}
}

// grantRow is one grant: what it is, what it gives, and its revoke
// button.
func (l *grantsList) grantRow(app string, g permissions.Grant) widget.Widget {
	t := locale.T()
	text := gapBox(widget.Column)
	text.Append(l.k.label(t.Get("settings-privacy-table-"+string(g.Table))+" · "+g.ID), false)
	if g.Value != "" {
		text.Append(wrapped(l.k.label(g.Value, css.Caption, css.DimLabel)), false)
	}
	row := gapBox(widget.Row)
	row.AddClass("settings-privacy-grant")
	row.Append(text, true)
	revoke := l.k.button(l.k.label(t.Get("settings-privacy-revoke")), nil, css.DestructiveAction)
	revoke.OnClick = func() {
		revoke.SetEnabled(false)
		async(l.k, func() error {
			_, err := withGrants(func(ctx context.Context, s grantStore) (int, error) { return s.Revoke(ctx, app, g.Table, g.ID) })
			return err
		}, func(err error) {
			if err != nil {
				log.Printf("settings: revoke %s %s for %s: %v", g.Table, g.ID, app, err)
				l.failure = t.Get("settings-privacy-revoke-failed") + ": " + err.Error()
			}
			l.reload()
		})
	}
	row.AppendAligned(revoke, false, widget.AlignCenter)
	return row
}

func wrapped(l *widget.Label) *widget.Label {
	l.SetWrap(true)
	return l
}
