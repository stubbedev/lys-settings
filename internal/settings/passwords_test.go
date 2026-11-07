package settings

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/dbustest"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-secrets/client"
	"github.com/stubbedev/lys-secrets/secretstest"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

type callMonitor struct {
	mu    sync.Mutex
	calls map[string]int
}

func monitorCalls(t *testing.T, bus *dbustest.Bus) *callMonitor {
	t.Helper()
	conn, err := dbus.Connect(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	m := &callMonitor{calls: map[string]int{}}
	if err := conn.BusObject().Call("org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, []string{"type='method_call'"}, uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	ch := make(chan *dbus.Message, 256)
	conn.Eavesdrop(ch)
	go func() {
		for msg := range ch {
			if msg.Type != dbus.TypeMethodCall {
				continue
			}
			member, _ := msg.Headers[dbus.FieldMember].Value().(string)
			m.mu.Lock()
			m.calls[member]++
			m.mu.Unlock()
		}
	}()
	return m
}

func (m *callMonitor) count(member string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[member]
}

type passwordsRig struct {
	srv *secretstest.Server
	bus *dbustest.Bus
	p   *passwordsPanel
	l   *loop
	mon *callMonitor
}

func startPasswords(t *testing.T, items int) *passwordsRig { return startPasswordsWith(t, items, "") }

func startPasswordsWith(t *testing.T, items int, yaml string) *passwordsRig {
	t.Helper()
	bus := dbustest.Start(t)
	srv := secretstest.Start(t, bus.Conn(t), "login-pw")
	ctx := context.Background()
	seed, err := client.Open(ctx, bus.Conn(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := range items {
		name := strings.Repeat("x", i+1)
		if err := seed.StoreLogin(ctx, "token "+name, map[string]string{"xdg:schema": "org.git.Password", "user": name}, []byte("secret-"+name)); err != nil {
			t.Fatal(err)
		}
	}
	seed.Close()
	srv.Lock()
	before := openKeyring
	openKeyring = func(ctx context.Context) (*client.Client, func(), error) {
		conn, err := dbus.Connect(bus.Address)
		if err != nil {
			return nil, nil, err
		}
		c, err := client.OpenEncrypted(ctx, conn)
		if err != nil {
			return nil, nil, err
		}
		return c, func() { c.Close(); _ = conn.Close() }, nil
	}
	t.Cleanup(func() { openKeyring = before })
	mon := monitorCalls(t, bus)
	k := testKit(t, yaml)
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	p := passwordsFooter(k).(*passwordsPanel)
	t.Cleanup(p.close)
	return &passwordsRig{srv: srv, bus: bus, p: p, l: l, mon: mon}
}

func (r *passwordsRig) itemButtons() []*widget.Button {
	var out []*widget.Button
	for _, w := range treetest.WithClass(r.p.list, "password-item") {
		out = append(out, w.(*widget.Button))
	}
	return out
}

func TestALockedCollectionIsListedWithoutAPromptAndARevealPromptsOnce(t *testing.T) {
	r := startPasswords(t, 1)
	if len(r.itemButtons()) != 1 || r.srv.Asked() != 0 {
		t.Fatalf("%d items listed, %d prompts", len(r.itemButtons()), r.srv.Asked())
	}
	r.itemButtons()[0].OnClick()
	r.srv.Answer("login-pw")
	treetest.Button(t, r.p.detail, "Reveal").OnClick()
	if r.srv.Asked() != 1 || string(r.p.revealed) != "secret-x" || r.p.secretLbl.Text() != "secret-x" {
		t.Fatalf("asked %d, revealed %q", r.srv.Asked(), r.p.revealed)
	}
	buf := r.p.revealed
	treetest.Button(t, r.p.detail, "Hide").OnClick()
	if !bytes.Equal(buf, make([]byte, len(buf))) || r.p.revealed != nil || r.p.secretLbl.Text() != hiddenText {
		t.Fatalf("after Hide the buffer holds %q", buf)
	}
}

func TestDeletingAnItemFollowsItemDeleted(t *testing.T) {
	r := startPasswords(t, 2)
	r.srv.Answer("login-pw")
	treetest.Button(t, r.p, "Unlock…").OnClick()
	r.itemButtons()[0].OnClick()
	del := treetest.Button(t, r.p.detail, "Delete…")
	del.OnClick()
	treetest.Button(t, r.p.detail, "Delete This Item").OnClick()
	r.l.until("the list following the deletion", func() bool { return len(r.itemButtons()) == 1 })
}

func TestChangePasswordRekeysThroughOnePrompt(t *testing.T) {
	r := startPasswords(t, 1)
	r.srv.Answer("login-pw")
	treetest.Button(t, r.p, "Unlock…").OnClick()
	file := filepath.Join(r.srv.Dir(), "login.keyring")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	asked := r.srv.Asked()
	r.srv.Answer("new-pw")
	treetest.Button(t, r.p, "Change Password…").OnClick()
	after, _ := os.ReadFile(file)
	if r.srv.Asked() != asked+1 || bytes.Equal(before[:64], after[:64]) {
		t.Fatalf("asked %d more, header changed %v", r.srv.Asked()-asked, !bytes.Equal(before[:64], after[:64]))
	}
}

func TestTheItemsLoadWithOneSearchAndOneGetAllAPage(t *testing.T) {
	r := startPasswords(t, itemsPerPage+3)
	r.l.drain(0)
	if r.mon.count("SearchItems") != 1 || r.mon.count("GetAll") != itemsPerPage+1 {
		t.Fatalf("SearchItems %d, GetAll %d; want one search and a GetAll for each of the first %d items plus the collection", r.mon.count("SearchItems"), r.mon.count("GetAll"), itemsPerPage)
	}
	if len(r.itemButtons()) != itemsPerPage {
		t.Fatalf("%d rows on the first page", len(r.itemButtons()))
	}
	treetest.Button(t, r.p.list, "Show More").OnClick()
	if r.mon.count("SearchItems") != 1 || r.mon.count("GetAll") != itemsPerPage+4 || len(r.itemButtons()) != itemsPerPage+3 {
		t.Fatalf("after Show More: SearchItems %d, GetAll %d, rows %d", r.mon.count("SearchItems"), r.mon.count("GetAll"), len(r.itemButtons()))
	}
}

func TestARevealAsksAgainOnceTheCollectionWasOpenTooLongAndACopyIsASecret(t *testing.T) {
	r := startPasswordsWith(t, 1, "passwords:\n  reveal-reprompt-minutes: 5\n")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	r.p.now = func() time.Time { return now }
	var copied []string
	r.p.k.copySecret = func(text string) error { copied = append(copied, text); return nil }
	r.srv.Answer("login-pw")
	treetest.Button(t, r.p, "Unlock…").OnClick()
	r.itemButtons()[0].OnClick()
	asked := r.srv.Asked()
	treetest.Button(t, r.p.detail, "Copy").OnClick()
	if r.srv.Asked() != asked || len(copied) != 1 || copied[0] != "secret-x" {
		t.Fatalf("a copy right after unlocking asked %d times and copied %q", r.srv.Asked()-asked, copied)
	}
	now = now.Add(6 * time.Minute)
	r.srv.Answer("login-pw")
	treetest.Button(t, r.p.detail, "Reveal").OnClick()
	if r.srv.Asked() != asked+1 || string(r.p.revealed) != "secret-x" {
		t.Fatalf("six minutes on, the reveal asked %d times", r.srv.Asked()-asked)
	}
}
