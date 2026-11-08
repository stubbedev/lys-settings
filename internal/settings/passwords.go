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
	k          *kit
	c          *client.Client
	release    func()
	stop       func()
	colls      []client.Collection
	coll       dbus.ObjectPath
	paths      []dbus.ObjectPath
	items      []client.Item
	shownTo    int
	seen       map[dbus.ObjectPath]time.Time
	now        func() time.Time
	problem    *widget.Label
	picker     *widget.Dropdown
	lock       *widget.Button
	lockFace   *widget.ButtonContent
	filter     *widget.SearchEntry
	list       *widget.ItemGroup
	rows       []widget.Widget
	more       *widget.Button
	expanded   dbus.ObjectPath
	detail     *widget.Box
	revealed   []byte
	secretLbl  *widget.Label
	revealFace *widget.ButtonContent
}

func passwordsFooter(k *kit) widget.Widget {
	t := locale.T()
	p := &passwordsPanel{Box: sectionStack(), k: k, seen: map[dbus.ObjectPath]time.Time{}, now: time.Now}
	p.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	p.problem.SetVisible(false)
	p.Append(p.problem, false)
	p.picker = widget.NewDropdown(k.face, k.textPx(), []string{t.Get("settings-passwords-loading")}, 0)
	p.picker.SetAccessibleName(t.Get("settings-passwords-collection"))
	p.picker.OnSelect = func(i int) {
		if i >= 0 && i < len(p.colls) && p.colls[i].Path != p.coll {
			p.pick(p.colls[i].Path)
		}
	}
	p.lock, p.lockFace = k.contentButton("ld-lock-symbolic", t.Get("settings-passwords-lock"), p.toggleLock, css.Secondary)
	change, _ := k.contentButton("ld-key-round-symbolic", t.Get("settings-passwords-change"), func() {
		coll := p.coll
		p.act(func(ctx context.Context) error { return p.c.ChangeLock(ctx, coll) })
	}, css.Secondary)
	keyring := widget.NewItemGroup(panelRow(k, t.Get("settings-passwords-collection"), "", p.picker, p.lock, change))
	p.Append(newSection(k, "settings-section-passwords-keyring", keyring), false)
	p.filter = widget.NewSearchEntry(k.face, k.textPx(), t.Get("settings-passwords-filter"))
	p.filter.SetDelay(0)
	p.filter.OnSearchChanged = func(string) { p.showItems() }
	p.list = widget.NewItemGroup(insetRow(p.filter))
	p.more, _ = k.contentButton("ld-chevron-down-symbolic", t.Get("settings-passwords-more"), p.loadMore, css.Secondary)
	p.more.SetHAlign(widget.AlignCenter)
	p.more.SetVisible(false)
	items := newSection(k, "settings-section-passwords-items", p.list)
	items.Append(p.more, false)
	p.Append(items, false)
	p.detail = widget.NewBox(widget.Column, widget.Current().Spacing, 0)
	p.detail.SetElement("item")
	p.detail.AddClass("password-detail")
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
		p.lockFace.SetIconName("ld-lock-open-symbolic")
		p.lockFace.SetLabel(t.Get("settings-passwords-unlock"))
	} else {
		p.lockFace.SetIconName("ld-lock-symbolic")
		p.lockFace.SetLabel(t.Get("settings-passwords-lock"))
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
	p.expanded = ""
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

func (p *passwordsPanel) loadMore() {
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
	for _, r := range p.rows {
		p.list.Remove(r)
	}
	p.rows = p.rows[:0]
	add := func(w widget.Widget) {
		p.list.Append(w)
		p.rows = append(p.rows, w)
	}
	words := strings.Fields(strings.ToLower(p.filter.Text()))
	expandedShown := false
	for _, it := range p.items {
		text := keyringItemText(it)
		if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			continue
		}
		add(p.itemRow(it))
		if it.Path == p.expanded {
			add(p.detail)
			expandedShown = true
		}
	}
	if !expandedShown {
		p.expanded = ""
		p.hideSecret()
	}
	if len(p.rows) == 0 {
		add(p.k.emptyRow("ld-key-round-symbolic", t.Get("settings-passwords-none")))
	}
	p.more.SetVisible(p.shownTo < len(p.paths))
}

func (p *passwordsPanel) itemRow(it client.Item) *widget.Item {
	title := it.Label
	if title == "" {
		title = locale.T().Get("settings-passwords-locked-item")
	}
	row := widget.NewItem(p.k.face, title, itemSummary(it.Attributes))
	row.AddClass("password-item")
	row.SetEllipsize(true)
	icon := "ld-key-round-symbolic"
	if it.Locked {
		icon = "ld-lock-symbolic"
	}
	row.SetMedia(p.k.icon(icon), false)
	chevron := "ld-chevron-right-symbolic"
	if it.Path == p.expanded {
		chevron = "ld-chevron-down-symbolic"
	}
	row.AddAction(p.k.icon(chevron, css.DimLabel))
	row.SetOnActivate(func() { p.toggleDetail(it) })
	return row
}

// summaryAttributes are the attributes an item's row names it by, in order.
var summaryAttributes = []string{"username", "user", "service", "server", "application"}

// itemSummary is the line under an item's name: the account and place its
// attributes name, else the program that stored it.
func itemSummary(attrs map[string]string) string {
	var parts []string
	for _, k := range summaryAttributes {
		if v := attrs[k]; v != "" && !slices.Contains(parts, v) {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return storedBy(attrs)
	}
	return strings.Join(parts, " · ")
}

func (p *passwordsPanel) toggleDetail(it client.Item) {
	if p.expanded == it.Path {
		p.expanded = ""
		p.hideSecret()
	} else {
		p.showDetail(it)
	}
	p.showItems()
}

func (p *passwordsPanel) showDetail(it client.Item) {
	t := locale.T()
	p.hideSecret()
	p.expanded = it.Path
	p.detail.Clear()
	labels := widget.NewSizeGroup(widget.SizeGroupHorizontal)
	field := func(name string, value widget.Widget, more ...widget.Widget) {
		row := gapBox(widget.Row)
		row.AppendAligned(labels.Add(p.k.label(name, css.CaptionHeading, css.DimLabel)), false, widget.AlignCenter)
		row.AppendAligned(value, true, widget.AlignCenter)
		for _, w := range more {
			row.AppendAligned(w, false, widget.AlignCenter)
		}
		p.detail.Append(row, false)
	}
	text := func(name, value string) {
		if value != "" {
			field(name, p.k.label(value))
		}
	}
	label := p.k.entry()
	label.SetText(it.Label)
	label.SetAccessibleName(t.Get("settings-passwords-label"))
	label.SetEnabled(!it.Locked)
	label.OnActivate = func(text string) {
		path := it.Path
		p.act(func(ctx context.Context) error { return p.c.SetLabel(ctx, path, strings.TrimSpace(text)) })
	}
	field(t.Get("settings-passwords-label"), label)
	for _, k := range slices.Sorted(maps.Keys(it.Attributes)) {
		text(k, it.Attributes[k])
	}
	text(t.Get("settings-passwords-stored-by"), storedBy(it.Attributes))
	if it.Created != 0 {
		text(t.Get("settings-passwords-created"), time.Unix(intconv.Must[int64](it.Created), 0).UTC().Format(itemTimeLayout))
	}
	if it.Modified != 0 {
		text(t.Get("settings-passwords-modified"), time.Unix(intconv.Must[int64](it.Modified), 0).UTC().Format(itemTimeLayout))
	}
	p.secretLbl = p.k.label(hiddenText, css.Monospace)
	var reveal *widget.Button
	reveal, p.revealFace = p.k.contentButton("ld-eye-symbolic", t.Get("settings-passwords-reveal"), func() {
		if p.revealed != nil {
			p.hideSecret()
			return
		}
		p.reveal(it.Path, false)
	}, css.Secondary)
	cp, _ := p.k.contentButton("ld-copy-symbolic", t.Get("settings-passwords-copy"), func() { p.reveal(it.Path, true) }, css.Secondary)
	field(t.Get("settings-passwords-secret"), p.secretLbl, reveal, cp)
	var del *widget.Button
	var delFace *widget.ButtonContent
	del, delFace = p.k.contentButton("ld-trash-2-symbolic", t.Get("settings-passwords-delete"), func() {
		if !del.HasClass(css.DestructiveAction) {
			del.AddClass(css.DestructiveAction)
			delFace.SetLabel(t.Get("settings-passwords-delete-confirm"))
			return
		}
		path := it.Path
		p.act(func(ctx context.Context) error { return p.c.DeleteItem(ctx, path) })
		p.expanded = ""
		p.showItems()
	}, css.Secondary)
	del.SetHAlign(widget.AlignEnd)
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
		p.revealFace.SetIconName("ld-eye-off-symbolic")
		p.revealFace.SetLabel(locale.T().Get("settings-passwords-hide"))
	})
}

func (p *passwordsPanel) hideSecret() {
	clear(p.revealed)
	p.revealed = nil
	if p.secretLbl != nil {
		p.secretLbl.SetText(hiddenText)
		p.revealFace.SetIconName("ld-eye-symbolic")
		p.revealFace.SetLabel(locale.T().Get("settings-passwords-reveal"))
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
