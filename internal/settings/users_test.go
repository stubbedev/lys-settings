package settings

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/dbustest"
	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	secrets "github.com/stubbedev/lys-secrets/control"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

type fakeAccounts struct {
	*fakeSys
	selfPath dbus.ObjectPath
	cached   []dbus.ObjectPath
}

func (f fakeAccounts) FindUserById(uid int64) (dbus.ObjectPath, *dbus.Error) {
	f.record("FindUserById", uid)
	return f.selfPath, nil
}

func (f fakeAccounts) ListCachedUsers() ([]dbus.ObjectPath, *dbus.Error) {
	return f.cached, nil
}

func (f fakeAccounts) CreateUser(name, real string, kind int32) (dbus.ObjectPath, *dbus.Error) {
	return "/org/freedesktop/Accounts/User3000", f.record("CreateUser", name, real, kind)
}

func (f fakeAccounts) DeleteUser(uid int64, remove bool) *dbus.Error {
	return f.record("DeleteUser", uid, remove)
}

type fakeAccountUser struct{ *fakeSys }

func (f fakeAccountUser) SetRealName(name string) *dbus.Error {
	return f.record("SetRealName", name)
}

func (f fakeAccountUser) SetIconFile(path string) *dbus.Error {
	return f.record("SetIconFile", path)
}

func (f fakeAccountUser) SetAccountType(kind int32) *dbus.Error {
	return f.record("SetAccountType", kind)
}

type fakeAccount struct {
	name, real  string
	uid         uint64
	accountType int32
	icon        string
}

var (
	alice = fakeAccount{name: "alice", real: "Alice", uid: 1000}
	bob   = fakeAccount{name: "bob", real: "Bob", uid: 2000}
)

func (a fakeAccount) path() dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/Accounts/User%d", a.uid))
}

func serveAccounts(t *testing.T, bus *dbustest.Bus, self fakeAccount, others ...fakeAccount) (manager, user *fakeSys) {
	t.Helper()
	conn := bus.Conn(t)
	manager = &fakeSys{props: map[string]any{"DaemonVersion": "23.13"}, conn: conn}
	cached := make([]dbus.ObjectPath, 0, 1+len(others))
	for i, a := range append([]fakeAccount{self}, others...) {
		f := &fakeSys{props: map[string]any{"UserName": a.name, "RealName": a.real, "IconFile": a.icon, "AccountType": a.accountType, "Uid": a.uid}, conn: conn}
		if i == 0 {
			user = f
		}
		if _, err := dbusx.Export(conn, a.path(), dbusx.Interface{Name: accountsUser, Methods: fakeAccountUser{f}, Properties: f.getters()}); err != nil {
			t.Fatal(err)
		}
		cached = append(cached, a.path())
	}
	if _, err := dbusx.Export(conn, accountsPath, dbusx.Interface{Name: accountsName, Methods: fakeAccounts{manager, self.path(), cached}, Properties: manager.getters()}); err != nil {
		t.Fatal(err)
	}
	if _, err := dbusx.Own(conn, accountsName); err != nil {
		t.Fatal(err)
	}
	return manager, user
}

func usersFixture(t *testing.T, bus *dbustest.Bus) (*usersPanel, *loop) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	k, l := systemKit(t, bus)
	p := usersFooter(k).(*usersPanel)
	t.Cleanup(p.close)
	return p, l
}

func TestAStockAvatarIsOneSetIconFileAndTheNameOneSetRealName(t *testing.T) {
	bus := dbustest.Start(t)
	_, user := serveAccounts(t, bus, alice, bob)
	p, l := usersFixture(t, bus)
	l.until("the account", func() bool { return p.realName.Enabled() })
	if p.realName.Text() != "Alice" || p.admin.Visible() {
		t.Fatalf("name %q, admin section shown %v for a standard user", p.realName.Text(), p.admin.Visible())
	}
	treetest.WithClass(p, "avatar-choice")[0].(*widget.Button).OnClick()
	l.until("SetIconFile", func() bool { c, _ := user.snapshot(); return len(c) == 1 })
	calls, _ := user.snapshot()
	if calls[0].method != "SetIconFile" {
		t.Fatalf("%+v", calls)
	}
	path, _ := calls[0].args[0].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != avatarSide || img.Bounds().Dy() != avatarSide {
		t.Fatalf("the avatar handed over is %v, %v; want a %dpx PNG", img.Bounds(), err, avatarSide)
	}
	p.realName.SetText("Alice Liddell")
	p.realName.OnActivate("Alice Liddell")
	l.until("SetRealName", func() bool { c, _ := user.snapshot(); return len(c) == 2 })
	if calls, _ := user.snapshot(); calls[1].method != "SetRealName" || fmt.Sprint(calls[1].args) != "[Alice Liddell]" {
		t.Fatalf("%+v", calls)
	}
}

func TestWithoutAccountsServiceTheAvatarIsWrittenToFace(t *testing.T) {
	bus := dbustest.Start(t)
	home := t.TempDir()
	before := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = before })
	p, l := usersFixture(t, bus)
	l.drain(100 * time.Millisecond)
	if p.realName.Enabled() || p.admin.Visible() {
		t.Fatal("the name or the admin section without AccountsService")
	}
	if got, want := p.realName.Placeholder(), locale.T().Get("settings-users-name-unavailable"); got != want {
		t.Fatalf("the name without AccountsService reads %q, want %q", got, want)
	}
	treetest.WithClass(p, "avatar-choice")[1].(*widget.Button).OnClick()
	l.until("~/.face", func() bool { _, err := os.Stat(filepath.Join(home, ".face")); return err == nil })
	data, _ := os.ReadFile(filepath.Join(home, ".face"))
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("~/.face is no PNG: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".face?*")); len(left) != 0 {
		t.Fatalf("a temporary file was left: %v", left)
	}
}

func TestAnAdministratorSeesTheOtherUsers(t *testing.T) {
	admin := alice
	admin.accountType = accountAdmin
	bus := dbustest.Start(t)
	manager, _ := serveAccounts(t, bus, admin, bob)
	p, l := usersFixture(t, bus)
	l.until("the admin section", func() bool { return p.admin.Visible() && len(treetest.All[*widget.Dropdown](p.admin)) == 2 })
	bob := dropdownNamed(t, p.admin, "bob")
	if dropdownNamed(t, p.admin, "alice").Enabled() || !bob.Enabled() {
		t.Fatal("an administrator can change their own type, or not another's")
	}
	treetest.WithClass(p.admin, css.Icon)[0].(*widget.Button).OnClick()
	l.until("DeleteUser", func() bool { c, _ := manager.snapshot(); return len(c) == 2 })
	if calls, _ := manager.snapshot(); calls[1].method != "DeleteUser" || fmt.Sprint(calls[1].args) != "[2000 false]" {
		t.Fatalf("%+v", calls)
	}
}

func TestTheUsersPageOfARealAccountFitsTheWindow(t *testing.T) {
	self := fakeAccount{name: "alexanderstage", real: "Alexander Stage", uid: 1000, accountType: accountAdmin, icon: "/var/lib/AccountsService/icons/alexanderstage"}
	others := []fakeAccount{
		{name: "margaret.hamilton", real: "Margaret Hamilton-Whitfield", uid: 1001},
		{name: "jonas.kristoffersen", real: "Jonas Bjørn Kristoffersen", uid: 1002, accountType: accountAdmin},
		{name: "soeren", real: "Søren Østergaard Nielsen", uid: 1003},
		{name: "backup-operator", real: "Nightly Backup Operator Account", uid: 1004},
		{name: "guest", uid: 1005},
	}
	bus := dbustest.Start(t)
	serveAccounts(t, bus, self, others...)
	enrolled := []string{"right-index-finger", "left-index-finger"}
	serveFprint(t, bus, true, &enrolled)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	k, _ := systemKit(t, bus)
	w := newWindow(k, k.store.svc.Config(), func(_ time.Duration, fn func()) { fn() })
	w.show(usersID)
	if got := len(treetest.All[*widget.Dropdown](w.page)); got != 1+len(others) {
		t.Fatalf("the users page lists %d accounts, want %d", got, 1+len(others))
	}
	if got := len(treetest.All[*widget.ToggleButton](w.page)); got != len(fingers) {
		t.Fatalf("the users page offers %d fingers, want %d", got, len(fingers))
	}
	assertPageFits(t, w)
}

func TestAnAvatarCropZoomsAndPansInsideThePicture(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for x := range 400 {
		for y := range 200 {
			c := color.RGBA{0, 0, 255, 255}
			if x < 200 {
				c = color.RGBA{255, 0, 0, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	left := cropAvatar(src, avatarCrop{zoom: 1, panX: -1})
	right := cropAvatar(src, avatarCrop{zoom: 1, panX: 1})
	if left.RGBAAt(avatarSide/2, avatarSide/2).R != 255 || right.RGBAAt(avatarSide/2, avatarSide/2).B != 255 {
		t.Fatalf("panning left shows %v, right %v", left.RGBAAt(avatarSide/2, avatarSide/2), right.RGBAAt(avatarSide/2, avatarSide/2))
	}
	if b := cropAvatar(src, avatarCrop{zoom: 3, panX: 5, panY: -5}).Bounds(); b.Dx() != avatarSide || b.Dy() != avatarSide {
		t.Fatalf("%v", b)
	}
}

type fakeLogin struct {
	mu         sync.Mutex
	old, next  string
	rekeyCalls int
}

func (f *fakeLogin) UnlockLogin([]byte) error { return nil }

func (f *fakeLogin) RekeyLogin(old, next []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.old, f.next = string(old), string(next)
	f.rekeyCalls++
	return nil
}

func pwqualityStack(t *testing.T) (passwordChanger, *fakeLogin) {
	t.Helper()
	module := os.Getenv("LYS_SETTINGS_TEST_PWQUALITY")
	if module == "" {
		t.Fatal("LYS_SETTINGS_TEST_PWQUALITY names pam_pwquality.so; the dev shell sets it")
	}
	dir := t.TempDir()
	put(t, filepath.Join(dir, "lys-settings-passwd"), "password requisite "+module+" retry=1 minlen=12 dictcheck=0\npassword required pam_permit.so\n")
	sock, err := os.MkdirTemp("", "lys-settings-pw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sock) })
	path := filepath.Join(sock, "control")
	ln, err := secrets.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	login := &fakeLogin{}
	ctx, cancel := context.WithCancel(context.Background())
	go secrets.Serve(ctx, ln, login)
	t.Cleanup(func() { cancel(); _ = ln.Close() })
	pc := passwordChanger{service: "lys-settings-passwd", confdir: dir, rekey: func(old, next []byte) error { return secrets.Rekey(path, old, next) }}
	return pc, login
}

func TestAPasswordChangeShowsPwqualitysRefusalAndRekeysOnSuccess(t *testing.T) {
	pc, login := pwqualityStack(t)
	var logged bytes.Buffer
	before := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(before) })
	k := testKit(t, "")
	p := &usersPanel{Box: gapBox(widget.Column), k: k, changer: pc, user: "nobody"}
	p.openPasswordDialog()
	entries := treetest.All[*widget.Entry](p)
	if len(entries) != 3 {
		t.Fatalf("%d entries", len(entries))
	}
	set := func(old, next, confirm string) {
		entries[0].SetText(old)
		entries[1].SetText(next)
		entries[2].SetText(confirm)
		treetest.Button(t, p, "Change Password").OnClick()
	}
	set("old-secret-1", "short", "short")
	var messages strings.Builder
	for _, lbl := range treetest.All[*widget.Label](p) {
		if lbl.HasClass("setting-problem") {
			messages.WriteString(lbl.Text())
		}
	}
	if !strings.Contains(messages.String(), "BAD PASSWORD") {
		t.Fatalf("the dialog shows %q, want pwquality's refusal", messages.String())
	}
	if login.rekeyCalls != 0 {
		t.Fatal("a refused change rekeyed the keyring")
	}
	set("old-secret-1", "a-much-Longer-pass9", "a-much-Longer-pass9")
	if login.rekeyCalls != 1 || login.old != "old-secret-1" || login.next != "a-much-Longer-pass9" {
		t.Fatalf("rekey %d with %q -> %q", login.rekeyCalls, login.old, login.next)
	}
	for _, pw := range []string{"old-secret-1", "a-much-Longer-pass9", "short"} {
		if strings.Contains(logged.String(), pw) || strings.Contains(strings.Join(os.Args, " "), pw) || strings.Contains(strings.Join(os.Environ(), " "), pw) {
			t.Fatalf("a password reached the log, argv or the environment")
		}
	}
}
