package settings

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/lys-lib/intconv"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-secrets/client"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	passwordsID      = "passwords"
	itemsPerPage     = 50
	copiedSecretLife = 30 * time.Second
	repromptPath     = "passwords.reveal-reprompt-minutes"
	schemaAttribute  = "xdg:schema"
	hiddenText       = "••••••••"
	itemTimeLayout   = "2006-01-02 15:04"
)

var knownSchemas = map[string]string{ //nolint:gosec // secret-service schema names, not credentials
	"chrome_libsecret_os_crypt_password_v2":     "Chromium Safe Storage",
	"chrome_libsecret_os_crypt_password_v1":     "Chromium Safe Storage",
	"org.freedesktop.NetworkManager.Connection": "NetworkManager",
	"org.gnome.keyring.NetworkPassword":         "GNOME network passwords",
	"org.git.Password":                          "git",
	"org.freedesktop.Secret.Generic":            "",
}

func passwordsPage(*config.Config) pageSpec {
	return pageSpec{
		id: passwordsID, navKey: "settings-nav-passwords", icon: "ld-key-round-symbolic",
		header: "settings-page-passwords",
		sections: []sectionSpec{
			{title: "settings-section-passwords-reveal", rows: fields(repromptPath)},
		},
		footer: passwordsFooter,
	}
}

func storedBy(attrs map[string]string) string {
	schema := attrs[schemaAttribute]
	if name, ok := knownSchemas[schema]; ok {
		return name
	}
	return schema
}

func keyringItemText(it client.Item) string {
	parts := []string{strings.ToLower(it.Label)}
	for _, k := range slices.Sorted(maps.Keys(it.Attributes)) {
		parts = append(parts, strings.ToLower(it.Attributes[k]))
	}
	return strings.Join(parts, " ")
}

var openKeyring = func(ctx context.Context) (*client.Client, func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	c, err := client.OpenEncrypted(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return c, func() {
		c.Close()
		_ = conn.Close()
	}, nil
}

type passwordsPanel struct {
	*widget.Box
	k         *kit
	c         *client.Client
	release   func()
	stop      func()
	colls     []client.Collection
	coll      dbus.ObjectPath
	paths     []dbus.ObjectPath
	items     []client.Item
	shownTo   int
	seen      map[dbus.ObjectPath]time.Time
	now       func() time.Time
	problem   *widget.Label
	picker    *widget.Dropdown
	lock      *widget.Button
	lockLbl   *widget.Label
	filter    *widget.SearchEntry
	list      *widget.Box
	detail    *widget.Box
	revealed  []byte
	secretLbl *widget.Label
}

func passwordsFooter(k *kit) widget.Widget {
	t := locale.T()
	p := &passwordsPanel{Box: gapBox(widget.Column), k: k, seen: map[dbus.ObjectPath]time.Time{}, now: time.Now}
	p.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	p.problem.SetVisible(false)
	p.Append(p.problem, false)
	head := gapBox(widget.Row)
	p.picker = widget.NewDropdown(k.face, k.textPx(), []string{t.Get("settings-passwords-loading")}, 0)
	p.picker.SetAccessibleName(t.Get("settings-passwords-collection"))
	p.picker.OnSelect = func(i int) {
		if i >= 0 && i < len(p.colls) && p.colls[i].Path != p.coll {
			p.pick(p.colls[i].Path)
		}
	}
	p.lockLbl = k.label(t.Get("settings-passwords-lock"))
	p.lock = k.button(p.lockLbl, p.toggleLock, css.Small)
	change := k.button(k.label(t.Get("settings-passwords-change")), func() {
		coll := p.coll
		p.act(func(ctx context.Context) error { return p.c.ChangeLock(ctx, coll) })
	}, css.Small)
	head.Append(p.picker, true)
	head.Append(p.lock, false)
	head.Append(change, false)
	p.Append(head, false)
	p.filter = widget.NewSearchEntry(k.face, k.textPx(), t.Get("settings-passwords-filter"))
	p.filter.SetDelay(0)
	p.filter.OnSearchChanged = func(string) { p.showItems() }
	p.Append(p.filter, false)
	p.list = gapBox(widget.Column)
	p.Append(p.list, false)
	p.detail = gapBox(widget.Column)
	p.detail.AddClass(css.Card, "password-detail")
	p.detail.SetVisible(false)
	p.Append(p.detail, false)
	p.open()
	return p
}

type keyringOpened struct {
	c       *client.Client
	release func()
	colls   []client.Collection
	err     error
}

func (p *passwordsPanel) open() {
	async(p.k, func() keyringOpened {
		ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
		defer cancel()
		c, release, err := openKeyring(ctx)
		if err != nil {
			return keyringOpened{err: err}
		}
		colls, err := c.Collections(ctx)
		if err != nil {
			release()
			return keyringOpened{err: err}
		}
		return keyringOpened{c: c, release: release, colls: colls}
	}, func(o keyringOpened) {
		if o.err != nil {
			p.fail(fmt.Errorf("%s: %w", locale.T().Get("settings-passwords-unavailable"), o.err))
			return
		}
		p.c, p.release = o.c, o.release
		p.setCollections(o.colls)
		if len(p.colls) > 0 {
			p.pick(p.colls[0].Path)
		}
	})
}

func (p *passwordsPanel) setCollections(colls []client.Collection) {
	p.colls = colls
	labels := make([]string, len(colls))
	selected := 0
	for i, c := range colls {
		labels[i] = c.Label
		if c.Locked {
			labels[i] = locale.T().Get("settings-passwords-locked", i18n.Str("label", c.Label))
		} else if _, ok := p.seen[c.Path]; !ok {
			p.seen[c.Path] = p.now()
		}
		if c.Path == p.coll {
			selected = i
		}
	}
	p.picker.SetItems(labels, selected)
	p.refreshLock()
}

func (p *passwordsPanel) current() (client.Collection, bool) {
	i := slices.IndexFunc(p.colls, func(c client.Collection) bool { return c.Path == p.coll })
	if i < 0 {
		return client.Collection{}, false
	}
	return p.colls[i], true
}

func (p *passwordsPanel) refreshLock() {
	t := locale.T()
	c, ok := p.current()
	p.lock.SetVisible(ok)
	if c.Locked {
		p.lockLbl.SetText(t.Get("settings-passwords-unlock"))
	} else {
		p.lockLbl.SetText(t.Get("settings-passwords-lock"))
	}
}

func (p *passwordsPanel) close() {
	p.hideSecret()
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	if p.release != nil {
		p.release()
		p.release = nil
	}
	p.c = nil
}

func (p *passwordsPanel) fail(err error) {
	p.problem.SetVisible(err != nil)
	if err != nil {
		p.problem.SetText(err.Error())
	}
}

func (p *passwordsPanel) pick(coll dbus.ObjectPath) {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	p.coll = coll
	p.hideSecret()
	p.detail.SetVisible(false)
	p.refreshLock()
	if stop, err := p.c.WatchItems(coll, func() { p.k.invoke(p.reload) }); err != nil {
		p.fail(err)
	} else {
		p.stop = stop
	}
	p.load(false)
}

type itemsListing struct {
	paths []dbus.ObjectPath
	items []client.Item
	colls []client.Collection
	err   error
}

func (p *passwordsPanel) reload() { p.load(true) }

func (p *passwordsPanel) load(collections bool) {
	if p.c == nil {
		return
	}
	c, coll, colls := p.c, p.coll, p.colls
	async(p.k, func() itemsListing {
		ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
		defer cancel()
		if collections {
			var err error
			if colls, err = c.Collections(ctx); err != nil {
				return itemsListing{err: err}
			}
		}
		paths, err := c.Search(ctx, coll)
		if err != nil {
			return itemsListing{err: err}
		}
		items, err := c.Describe(ctx, paths[:min(len(paths), itemsPerPage)])
		return itemsListing{paths: paths, items: items, colls: colls, err: err}
	}, func(l itemsListing) {
		if l.err != nil {
			p.fail(l.err)
			return
		}
		p.fail(nil)
		p.setCollections(l.colls)
		p.paths, p.items, p.shownTo = l.paths, l.items, len(l.items)
		p.showItems()
	})
}

func (p *passwordsPanel) more() {
	c, next := p.c, p.paths[p.shownTo:min(len(p.paths), p.shownTo+itemsPerPage)]
	async(p.k, func() itemsListing {
		ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
		defer cancel()
		items, err := c.Describe(ctx, next)
		return itemsListing{items: items, err: err}
	}, func(l itemsListing) {
		if l.err != nil {
			p.fail(l.err)
			return
		}
		p.items = append(p.items, l.items...)
		p.shownTo += len(l.items)
		p.showItems()
	})
}

func (p *passwordsPanel) showItems() {
	t := locale.T()
	p.list.Clear()
	words := strings.Fields(strings.ToLower(p.filter.Text()))
	rows := widget.NewItemGroup()
	shown := 0
	for _, it := range p.items {
		text := keyringItemText(it)
		if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			continue
		}
		title := it.Label
		if title == "" {
			title = t.Get("settings-passwords-locked-item")
		}
		b := p.k.button(p.k.label(title, "setting-label"), func() { p.showDetail(it) }, css.Flat, "password-item")
		b.SetHAlign(widget.AlignFill)
		rows.Append(b)
		shown++
	}
	if shown == 0 {
		rows.Append(p.k.label(t.Get("settings-passwords-none"), css.DimLabel))
	}
	p.list.Append(rows, false)
	if p.shownTo < len(p.paths) {
		p.list.Append(p.k.button(p.k.label(t.Get("settings-passwords-more")), p.more, css.Small), false)
	}
}

func (p *passwordsPanel) showDetail(it client.Item) {
	t := locale.T()
	p.hideSecret()
	p.detail.Clear()
	p.detail.SetVisible(true)
	label := p.k.entry()
	label.SetText(it.Label)
	label.SetAccessibleName(t.Get("settings-passwords-label"))
	label.SetEnabled(!it.Locked)
	label.OnActivate = func(text string) {
		path := it.Path
		p.act(func(ctx context.Context) error { return p.c.SetLabel(ctx, path, strings.TrimSpace(text)) })
	}
	p.detail.Append(panelRow(p.k, t.Get("settings-passwords-label"), "", label), false)
	for _, k := range slices.Sorted(maps.Keys(it.Attributes)) {
		p.detail.Append(panelRow(p.k, k, it.Attributes[k]), false)
	}
	if by := storedBy(it.Attributes); by != "" {
		p.detail.Append(panelRow(p.k, t.Get("settings-passwords-stored-by"), by), false)
	}
	if it.Created != 0 {
		p.detail.Append(panelRow(p.k, t.Get("settings-passwords-created"), time.Unix(intconv.Must[int64](it.Created), 0).UTC().Format(itemTimeLayout)), false)
	}
	if it.Modified != 0 {
		p.detail.Append(panelRow(p.k, t.Get("settings-passwords-modified"), time.Unix(intconv.Must[int64](it.Modified), 0).UTC().Format(itemTimeLayout)), false)
	}
	p.secretLbl = p.k.label(hiddenText, css.Monospace)
	reveal := p.k.button(p.k.label(t.Get("settings-passwords-reveal")), func() { p.reveal(it.Path, false) }, css.Small)
	hide := p.k.button(p.k.label(t.Get("settings-passwords-hide")), p.hideSecret, css.Small)
	cp := p.k.button(p.k.label(t.Get("settings-passwords-copy")), func() { p.reveal(it.Path, true) }, css.Small)
	p.detail.Append(panelRow(p.k, t.Get("settings-passwords-secret"), "", p.secretLbl, reveal, hide, cp), false)
	var del *widget.Button
	delLbl := p.k.label(t.Get("settings-passwords-delete"))
	del = p.k.button(delLbl, func() {
		if !del.HasClass(css.DestructiveAction) {
			del.AddClass(css.DestructiveAction)
			delLbl.SetText(t.Get("settings-passwords-delete-confirm"))
			return
		}
		path := it.Path
		p.act(func(ctx context.Context) error { return p.c.DeleteItem(ctx, path) })
		p.hideSecret()
		p.detail.SetVisible(false)
	}, css.Small)
	p.detail.Append(del, false)
}

func (p *passwordsPanel) repromptDue() bool {
	minutes, _ := p.k.store.value(repromptPath).(int64)
	if minutes == 0 {
		return false
	}
	seen, ok := p.seen[p.coll]
	return !ok || p.now().Sub(seen) > time.Duration(minutes)*time.Minute
}

type revealed struct {
	secret []byte
	err    error
}

func (p *passwordsPanel) reveal(item dbus.ObjectPath, copyIt bool) {
	c, coll, again := p.c, p.coll, p.repromptDue()
	async(p.k, func() revealed {
		ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
		defer cancel()
		if again {
			if err := c.Lock(ctx, []dbus.ObjectPath{coll}); err != nil {
				return revealed{err: err}
			}
		}
		s, err := c.Secret(ctx, item)
		return revealed{s, err}
	}, func(r revealed) {
		if r.err != nil {
			p.fail(r.err)
			return
		}
		p.seen[coll] = p.now()
		if copyIt {
			err := p.k.copySecret(string(r.secret))
			clear(r.secret)
			p.fail(err)
			return
		}
		p.hideSecret()
		p.revealed = r.secret
		p.secretLbl.SetText(string(r.secret))
	})
}

func (p *passwordsPanel) hideSecret() {
	clear(p.revealed)
	p.revealed = nil
	if p.secretLbl != nil {
		p.secretLbl.SetText(hiddenText)
	}
}

func (p *passwordsPanel) toggleLock() {
	c, ok := p.current()
	if !ok {
		return
	}
	p.act(func(ctx context.Context) error {
		if c.Locked {
			return p.c.Unlock(ctx, []dbus.ObjectPath{c.Path})
		}
		return p.c.Lock(ctx, []dbus.ObjectPath{c.Path})
	})
}

func (p *passwordsPanel) act(fn func(context.Context) error) {
	if p.c == nil {
		return
	}
	async(p.k, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
		defer cancel()
		return fn(ctx)
	}, func(err error) {
		p.fail(err)
		p.reload()
	})
}
