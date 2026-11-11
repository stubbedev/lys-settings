package settings

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-lib/atomicfile"
	"github.com/stubbedev/lys-lib/intconv"
	"github.com/stubbedev/lys-lib/pam"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-lib/xdg"
	secrets "github.com/stubbedev/lys-secrets/control"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"
	xdraw "golang.org/x/image/draw"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	usersID          = "users"
	accountsName     = "org.freedesktop.Accounts"
	accountsPath     = dbus.ObjectPath("/org/freedesktop/Accounts")
	accountsUser     = accountsName + ".User"
	avatarSide       = 512
	avatarThumb      = 64
	accountAdmin     = int32(1)
	accountStandard  = int32(0)
	passwdService    = "passwd"
	faceFile         = ".face"
	avatarCacheName  = "avatar.png"
	strengthClasses  = 4
	strengthMinScore = 8
)

//go:embed avatars/*.png
var stockAvatars embed.FS

func usersPage(*config.Config) pageSpec {
	return pageSpec{
		id: usersID, navKey: "settings-nav-users", icon: "ld-user-symbolic",
		header: "settings-page-users",
		footer: usersFooter,
	}
}

type avatarCrop struct {
	zoom, panX, panY float64
}

func cropAvatar(src image.Image, c avatarCrop) *image.RGBA {
	b := src.Bounds()
	side := float64(min(b.Dx(), b.Dy())) / max(c.zoom, 1)
	cx := float64(b.Min.X) + float64(float64(b.Dx())/2) + float64(clampUnit(c.panX)*(float64(b.Dx())-side)/2)
	cy := float64(b.Min.Y) + float64(float64(b.Dy())/2) + float64(clampUnit(c.panY)*(float64(b.Dy())-side)/2)
	half := float64(side / 2)
	r := image.Rect(int(cx-half), int(cy-half), int(cx+half), int(cy+half)).Intersect(b)
	out := image.NewRGBA(image.Rect(0, 0, avatarSide, avatarSide))
	xdraw.CatmullRom.Scale(out, out.Bounds(), src, r, xdraw.Src, nil)
	return out
}

func clampUnit(v float64) float64 { return min(max(v, -1), 1) }

func encodeAvatar(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode the avatar: %w", err)
	}
	return buf.Bytes(), nil
}

type accounts struct {
	conn *dbus.Conn
	user sysService
}

func openAccounts(ctx context.Context, conn *dbus.Conn) (accounts, error) {
	var path dbus.ObjectPath
	if err := conn.Object(accountsName, accountsPath).CallWithContext(ctx, accountsName+".FindUserById", 0, int64(os.Getuid())).Store(&path); err != nil {
		return accounts{}, fmt.Errorf("find this user in AccountsService: %w", err)
	}
	return accounts{conn: conn, user: sysService{conn: conn, name: accountsName, iface: accountsUser, path: path}}, nil
}

func (a accounts) manager() sysService {
	return sysService{conn: a.conn, name: accountsName, iface: accountsName, path: accountsPath}
}

func (s sysService) callInteractive(ctx context.Context, method string, args ...any) error {
	if err := s.obj().CallWithContext(ctx, s.iface+"."+method, dbus.FlagAllowInteractiveAuthorization, args...).Err; err != nil {
		return fmt.Errorf("%s.%s: %w", s.iface, method, err)
	}
	return nil
}

func (s sysService) storeInteractive(ctx context.Context, method string, out any, args ...any) error {
	if err := s.obj().CallWithContext(ctx, s.iface+"."+method, dbus.FlagAllowInteractiveAuthorization, args...).Store(out); err != nil {
		return fmt.Errorf("%s.%s: %w", s.iface, method, err)
	}
	return nil
}

type otherUser struct {
	path        dbus.ObjectPath
	uid         uint64
	name, real  string
	accountType int32
	self        bool
}

func (a accounts) others(ctx context.Context) ([]otherUser, error) {
	var paths []dbus.ObjectPath
	if err := a.manager().store(ctx, "ListCachedUsers", &paths); err != nil {
		return nil, err
	}
	out := make([]otherUser, 0, len(paths))
	for _, p := range paths {
		props, err := sysService{conn: a.conn, name: accountsName, iface: accountsUser, path: p}.props(ctx)
		if err != nil {
			return nil, err
		}
		uid, _ := props["Uid"].Value().(uint64)
		t, _ := props["AccountType"].Value().(int32)
		out = append(out, otherUser{path: p, uid: uid, name: props.str("UserName"), real: props.str("RealName"), accountType: t, self: p == a.user.path})
	}
	slices.SortFunc(out, func(x, y otherUser) int { return strings.Compare(x.name, y.name) })
	return out, nil
}

func writeFace(h host, data []byte) error {
	home, ok := h.env.Home()
	if !ok {
		return errors.New("find the home directory for ~/.face: HOME is not an absolute path")
	}
	path := filepath.Join(home, faceFile)
	if err := atomicfile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func setAvatar(ctx context.Context, acc *accounts, data []byte) error {
	if acc == nil {
		return writeFace(systemHost, data)
	}
	cache, ok := xdg.OS.CacheDir("lys-settings")
	if !ok {
		return errors.New("no cache directory to hand AccountsService the avatar from")
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", cache, err)
	}
	path := filepath.Join(cache, avatarCacheName)
	if err := atomicfile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return acc.user.call(ctx, "SetIconFile", path)
}

type passwordChange struct {
	messages []string
	err      error
}

type passwordChanger struct {
	service, confdir string
	rekey            func(old, next []byte) error
}

var defaultPasswordChanger = passwordChanger{service: passwdService, rekey: func(old, next []byte) error {
	path, err := secrets.SocketPath(xdg.OS)
	if err != nil {
		return err
	}
	return secrets.Rekey(path, old, next)
}}

func (pc passwordChanger) change(user string, old, next []byte) passwordChange {
	var res passwordChange
	newAnswers := 0
	txn, err := pam.Start(pc.service, user, pc.confdir, func(ms []pam.Message) ([]pam.Reply, int32) {
		replies := make([]pam.Reply, len(ms))
		for i, m := range ms {
			switch m.Style {
			case pam.ErrorMsg, pam.TextInfo:
				res.messages = append(res.messages, m.Text)
			case pam.PromptEchoOff, pam.PromptEchoOn:
				lower := strings.ToLower(m.Text)
				switch {
				case strings.Contains(lower, "current") || strings.Contains(lower, "old"):
					replies[i] = pam.Reply{Text: string(old), Set: true}
				case newAnswers < 2:
					newAnswers++
					replies[i] = pam.Reply{Text: string(next), Set: true}
				default:
					return nil, pam.ConvErr
				}
			}
		}
		return replies, pam.Success
	})
	if err != nil {
		res.err = err
		return res
	}
	rc := txn.Chauthtok(0)
	if rc != pam.Success {
		res.err = errors.New(txn.Strerror(rc))
	}
	txn.End(rc)
	if res.err != nil {
		return res
	}
	if err := pc.rekey(old, next); err != nil {
		res.err = fmt.Errorf("the password changed, but the keyring did not follow: %w", err)
	}
	return res
}

func passwordStrength(pw string) int {
	if pw == "" {
		return 0
	}
	var classes [strengthClasses]bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			classes[0] = true
		case unicode.IsUpper(r):
			classes[1] = true
		case unicode.IsDigit(r):
			classes[2] = true
		default:
			classes[3] = true
		}
	}
	score := len([]rune(pw))
	for _, c := range classes {
		if c {
			score += 2
		}
	}
	return min(score/strengthMinScore, strengthClasses)
}

type usersPanel struct {
	*widget.Box
	k           *kit
	conn        *dbus.Conn
	acc         *accounts
	props       sysProps
	user        string
	problem     *widget.Label
	avatar      *widget.Box
	realName    *widget.Entry
	admin       *widget.Box
	changer     passwordChanger
	fingerprint *fingerprintSection
}

func usersFooter(k *kit) widget.Widget {
	t := locale.T()
	p := &usersPanel{Box: sectionStack(), k: k, changer: defaultPasswordChanger}
	p.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	p.problem.SetVisible(false)
	p.Append(p.problem, false)
	p.avatar = gapBox(widget.Column)
	p.avatar.SetElement("item")
	p.Append(newSection(k, "settings-section-users-avatar", p.avatar), false)
	p.realName = k.entry()
	p.realName.SetAccessibleName(t.Get("settings-users-name"))
	p.realName.SetEnabled(false)
	p.realName.OnActivate = func(text string) { p.callUser("SetRealName", strings.TrimSpace(text)) }
	change := k.button(k.label(t.Get("settings-users-password-change")), p.openPasswordDialog, css.Secondary)
	account := widget.NewItemGroup()
	account.Append(panelRow(k, t.Get("settings-users-name"), t.Attr("settings-users-name", "description"), p.realName))
	account.Append(panelRow(k, t.Get("settings-users-password"), t.Attr("settings-users-password", "description"), change))
	p.Append(newSection(k, "settings-section-users-account", account), false)
	p.admin = gapBox(widget.Column)
	p.admin.SetVisible(false)
	p.Append(p.admin, false)
	p.showAvatars()
	p.load()
	return p
}

type accountsOpened struct {
	conn  *dbus.Conn
	acc   *accounts
	props sysProps
	err   error
}

func (p *usersPanel) load() {
	async(p.k, func() accountsOpened {
		conn, err := p.k.systemBus()
		if err != nil {
			return accountsOpened{err: err}
		}
		res, err := withTimeout(func(ctx context.Context) (accountsOpened, error) {
			acc, err := openAccounts(ctx, conn)
			if err != nil {
				return accountsOpened{}, err
			}
			props, err := acc.user.props(ctx)
			return accountsOpened{conn: conn, acc: &acc, props: props}, err
		})
		if err != nil {
			return accountsOpened{conn: conn, err: err}
		}
		return res
	}, func(o accountsOpened) {
		p.conn = o.conn
		if o.err != nil {
			t := locale.T()
			p.realName.SetPlaceholder(t.Get("settings-users-name-unavailable"))
			p.realName.SetTooltip(t.Attr("settings-users-name-unavailable", "description"))
			if acct, err := currentAccount(); err == nil {
				p.user = acct
			}
			p.loadFingerprint()
			return
		}
		p.acc, p.props = o.acc, o.props
		p.user = o.props.str("UserName")
		p.realName.SetText(o.props.str("RealName"))
		p.realName.SetEnabled(true)
		t, _ := o.props["AccountType"].Value().(int32)
		if t == accountAdmin {
			p.loadAdmin()
		}
		p.loadFingerprint()
	})
}

var currentAccount = func() (string, error) {
	name := os.Getenv("USER")
	if name == "" {
		return "", errors.New("no USER in the environment")
	}
	return name, nil
}

func (p *usersPanel) close() {
	if p.fingerprint != nil {
		p.fingerprint.close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

func (p *usersPanel) fail(err error) {
	p.problem.SetVisible(err != nil)
	if err != nil {
		p.problem.SetText(err.Error())
	}
}

func (p *usersPanel) callUser(method string, args ...any) {
	if p.acc == nil {
		return
	}
	user := p.acc.user
	async(p.k, func() error {
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) { return struct{}{}, user.call(ctx, method, args...) })
		return err
	}, p.fail)
}

func (p *usersPanel) showAvatars() {
	t := locale.T()
	grid := widget.NewFlowBox(widget.Current().Spacing, widget.Current().Spacing)
	grid.SetSelectionMode(widget.SelectionNone)
	entries, err := stockAvatars.ReadDir("avatars")
	if err != nil {
		p.fail(err)
		return
	}
	for _, e := range entries {
		data, err := stockAvatars.ReadFile("avatars/" + e.Name())
		if err != nil {
			p.fail(err)
			return
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			p.fail(fmt.Errorf("decode the stock avatar %s: %w", e.Name(), err))
			return
		}
		name := strings.TrimSuffix(e.Name(), ".png")
		thumb := widget.NewAvatar(p.k.face, avatarThumb, name)
		thumb.SetImage(img)
		b := p.k.button(thumb, func() { p.useAvatar(img, avatarCrop{zoom: 1}) }, css.Flat, "avatar-choice")
		b.SetAccessibleName(t.Get("settings-users-avatar-stock", i18n.Str("name", name)))
		grid.Append(b)
	}
	grid.SetMaxChildrenPerLine(len(entries))
	p.avatar.Append(grid, false)
	choose, _ := p.k.contentButton("ld-image-symbolic", t.Get("settings-users-avatar-file"), func() {
		if p.k.pickers != nil {
			p.k.pickers.openFile(p.cropFile)
		}
	}, css.Secondary)
	choose.SetHAlign(widget.AlignStart)
	p.avatar.Append(choose, false)
}

func (p *usersPanel) useAvatar(img image.Image, c avatarCrop) {
	acc := p.acc
	async(p.k, func() error {
		data, err := encodeAvatar(cropAvatar(img, c))
		if err != nil {
			return err
		}
		_, err = withTimeout(func(ctx context.Context) (struct{}, error) { return struct{}{}, setAvatar(ctx, acc, data) })
		return err
	}, p.fail)
}

func (p *usersPanel) cropFile(path string) {
	f, err := os.Open(path) //nolint:gosec // the file the user picked
	if err != nil {
		p.fail(fmt.Errorf("open %s: %w", path, err))
		return
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		p.fail(fmt.Errorf("decode %s: %w", path, err))
		return
	}
	t := locale.T()
	c := avatarCrop{zoom: 1}
	preview := widget.NewImage(cropAvatar(img, c))
	preview.SetSizeRequest(avatarSide/4, avatarSide/4)
	editor := gapBox(widget.Column)
	editor.AddClass("avatar-crop")
	redraw := func() { preview.SetImage(cropAvatar(img, c)) }
	editor.Append(preview, false)
	slider := func(key string, lo, hi, value float64, set func(float64)) {
		s := widget.NewSlider(lo, hi, 0, value)
		s.SetAccessibleName(t.Get(key))
		s.OnChanged = func(v float64) { set(v); redraw() }
		editor.Append(panelRow(p.k, t.Get(key), "", s), false)
	}
	slider("settings-users-avatar-zoom", 1, 4, 1, func(v float64) { c.zoom = v })
	slider("settings-users-avatar-pan-x", -1, 1, 0, func(v float64) { c.panX = v })
	slider("settings-users-avatar-pan-y", -1, 1, 0, func(v float64) { c.panY = v })
	apply := p.k.button(p.k.label(t.Get("settings-users-avatar-apply")), func() {
		p.useAvatar(img, c)
		p.avatar.Remove(editor)
	}, css.SuggestedAction)
	editor.Append(apply, false)
	p.avatar.Append(editor, false)
}

func (p *usersPanel) loadAdmin() {
	acc := *p.acc
	async(p.k, func() otherUsersListing {
		users, err := withTimeout(acc.others)
		return otherUsersListing{users, err}
	}, p.showAdmin)
}

type otherUsersListing struct {
	users []otherUser
	err   error
}

func (p *usersPanel) showAdmin(l otherUsersListing) {
	t := locale.T()
	p.admin.Clear()
	p.admin.SetVisible(true)
	body := gapBox(widget.Column)
	if l.err != nil {
		body.Append(wrapped(p.k.label(l.err.Error(), css.Error, css.Caption)), false)
	}
	rows := widget.NewItemGroup()
	types := []string{t.Get("settings-users-type-standard"), t.Get("settings-users-type-admin")}
	for _, u := range l.users {
		kind := widget.NewDropdown(p.k.face, p.k.textPx(), types, int(u.accountType))
		kind.SetAccessibleName(u.name)
		kind.SetEnabled(!u.self)
		user := sysService{conn: p.conn, name: accountsName, iface: accountsUser, path: u.path}
		kind.OnSelect = func(i int) {
			p.adminCall(func(ctx context.Context) error {
				return user.callInteractive(ctx, "SetAccountType", intconv.Must[int32](i))
			})
		}
		controls := []widget.Widget{kind}
		if !u.self {
			uid := u.uid
			manager := p.acc.manager()
			controls = append(controls, p.k.iconButton("ld-trash-2-symbolic", "settings-users-remove", func() {
				p.adminCall(func(ctx context.Context) error {
					return manager.callInteractive(ctx, "DeleteUser", intconv.Must[int64](uid), false)
				})
			}, css.Ghost))
		}
		rows.Append(panelRow(p.k, cmp.Or(u.real, u.name), u.name, controls...))
	}
	body.Append(rows, false)
	name, full := p.k.entry(), p.k.entry()
	name.SetPlaceholder(t.Get("settings-users-add-name"))
	full.SetPlaceholder(t.Get("settings-users-add-full"))
	add := p.k.button(p.k.label(t.Get("settings-users-add")), func() {
		login, real := strings.TrimSpace(name.Text()), strings.TrimSpace(full.Text())
		manager := p.acc.manager()
		p.adminCall(func(ctx context.Context) error {
			var path dbus.ObjectPath
			return manager.storeInteractive(ctx, "CreateUser", &path, login, real, accountStandard)
		})
	}, css.Secondary)
	adding := gapBox(widget.Row)
	adding.Append(name, true)
	adding.Append(full, true)
	adding.Append(add, false)
	body.Append(adding, false)
	p.admin.Append(newSection(p.k, "settings-section-users-others", body), false)
}

func (p *usersPanel) adminCall(fn func(context.Context) error) {
	async(p.k, func() error {
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) })
		return err
	}, func(err error) {
		p.fail(err)
		p.loadAdmin()
	})
}

func (p *usersPanel) openPasswordDialog() {
	t := locale.T()
	box := gapBox(widget.Column)
	box.AddClass("password-change")
	entry := func(key string) *widget.Entry {
		e := p.k.entry()
		e.SetEcho(widget.EchoPassword)
		e.SetAccessibleName(t.Get(key))
		box.Append(panelRow(p.k, t.Get(key), "", e), false)
		return e
	}
	current, next, confirm := entry("settings-users-password-current"), entry("settings-users-password-new"), entry("settings-users-password-confirm")
	meter := widget.NewLevelBar(0)
	meter.SetAccessibleName(t.Get("settings-users-password-strength"))
	box.Append(panelRow(p.k, t.Get("settings-users-password-strength"), "", meter), false)
	messages := wrapped(p.k.label("", "setting-problem", css.Caption, css.Error))
	messages.SetVisible(false)
	box.Append(messages, false)
	next.OnChanged = func(text string) { meter.SetValue(float64(passwordStrength(text)) / strengthClasses) }
	clear := func() {
		for _, e := range []*widget.Entry{current, next, confirm} {
			e.SetText("")
		}
	}
	var apply *widget.Button
	apply = p.k.button(p.k.label(t.Get("settings-users-password-apply")), func() {
		if next.Text() != confirm.Text() {
			messages.SetText(t.Get("settings-users-password-mismatch"))
			messages.SetVisible(true)
			return
		}
		old, pw := []byte(current.Text()), []byte(next.Text())
		user, changer := p.user, p.changer
		apply.SetEnabled(false)
		async(p.k, func() passwordChange {
			res := changer.change(user, old, pw)
			clear0(old)
			clear0(pw)
			return res
		}, func(r passwordChange) {
			apply.SetEnabled(true)
			text := strings.Join(r.messages, "\n")
			if r.err != nil {
				text = strings.TrimSpace(text + "\n" + r.err.Error())
			}
			messages.SetText(text)
			messages.SetVisible(text != "")
			if r.err == nil {
				clear()
				p.Remove(box)
			}
		})
	}, css.SuggestedAction)
	cancel := p.k.button(p.k.label(t.Get("settings-users-password-cancel")), func() {
		clear()
		p.Remove(box)
	}, css.Secondary)
	buttons := gapBox(widget.Row)
	buttons.Append(cancel, false)
	buttons.Append(apply, false)
	box.Append(buttons, false)
	p.Append(box, false)
}

func clear0(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
