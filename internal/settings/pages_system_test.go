package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/sockdir"
	"github.com/stubbedev/lys-login/dmsocket"
	"github.com/stubbedev/lys-login/greeterconfig"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

type fakeDaemon struct {
	requests chan greeterconfig.Change
}

func serveFakeDaemon(t *testing.T, answer dmsocket.Reply) (string, *fakeDaemon) {
	t.Helper()
	dir := sockdir.Dir(t)
	ln, err := net.Listen("unix", dmsocket.Apply.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	d := &fakeDaemon{requests: make(chan greeterconfig.Change, 1)}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var c greeterconfig.Change
		if err := json.NewDecoder(conn).Decode(&c); err != nil {
			t.Errorf("the fake daemon read %v", err)
			return
		}
		d.requests <- c
		_ = json.NewEncoder(conn).Encode(answer)
	}()
	return dir, d
}

func (d *fakeDaemon) request(t *testing.T) greeterconfig.Change {
	t.Helper()
	select {
	case c := <-d.requests:
		return c
	default:
		t.Fatal("nothing reached the display manager")
		return greeterconfig.Change{}
	}
}

func noPrograms(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestGreeterApplySendsTheAllowedKeys(t *testing.T) {
	noPrograms(t)
	k := testKit(t, "")
	_ = k.store.set("login.greeter.cursor-size", int64(32))
	_ = k.store.set("login.lock.pam-service", "secret-service")
	dir, daemon := serveFakeDaemon(t, dmsocket.Answer("/etc/lys-shell/runtime.yaml", nil))
	if err := applyGreeter(k.store, dir); err != nil {
		t.Fatal(err)
	}
	c := daemon.request(t)
	doc, err := config.ParseYAML(c.Staged)
	if err != nil {
		t.Fatalf("staged %q: %v", c.Staged, err)
	}
	login, _ := doc["login"].(map[string]any)
	if v, _ := config.PathValue(login, "greeter.cursor-size"); len(doc) != 1 || v != int64(32) || login["show-clock"] == nil {
		t.Errorf("staged:\n%s", c.Staged)
	}
	if _, ok := login["lock"]; ok {
		t.Errorf("the lock screen's keys were staged for the greeter:\n%s", c.Staged)
	}
	for key, v := range login {
		if sub, ok := v.(map[string]any); ok {
			for inner := range sub {
				if !slices.Contains(config.GreeterApplyKeys, key+"."+inner) {
					t.Errorf("%s.%s is outside the allowlist but was staged", key, inner)
				}
			}
			continue
		}
		if !slices.Contains(config.GreeterApplyKeys, key) {
			t.Errorf("%s is outside the allowlist but was staged", key)
		}
	}
	if c.Image != nil {
		t.Errorf("a color background carried an image: %+v", c.Image)
	}
}

func TestGreeterApplyCarriesTheImageNotItsPath(t *testing.T) {
	noPrograms(t)
	k := testKit(t, "")
	wall := filepath.Join(t.TempDir(), "wall.png")
	if err := os.WriteFile(wall, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = k.store.set("login.background-mode", "image")
	_ = k.store.set("login.background-image", wall)
	dir, daemon := serveFakeDaemon(t, dmsocket.Answer("/etc/lys-shell/runtime.yaml", nil))
	if err := applyGreeter(k.store, dir); err != nil {
		t.Fatal(err)
	}
	if c := daemon.request(t); c.Image == nil || string(c.Image.Data) != "PNGDATA" || c.Image.Ext != ".png" {
		t.Errorf("image = %+v", c.Image)
	}
}

func greeterApplyButton(t *testing.T, footer *widget.Box) *widget.Button {
	t.Helper()
	var buttons []*widget.Button
	for _, kid := range footer.Children() {
		if b, ok := kid.(*widget.Button); ok {
			buttons = append(buttons, b)
		}
	}
	if len(buttons) != 1 {
		t.Fatalf("footer has %d buttons, want the apply button alone", len(buttons))
	}
	return buttons[0]
}

func clickGreeterApply(t *testing.T, runtimeDir string) []string {
	t.Helper()
	k := testKit(t, "")
	saved := loginRuntimeDir
	loginRuntimeDir = runtimeDir
	t.Cleanup(func() { loginRuntimeDir = saved })
	footer := greeterApplyFooter(k).(*widget.Box)
	greeterApplyButton(t, footer).ClickAt(widget.Point{})
	return treetest.Labels(footer)
}

func TestGreeterApplyNamesWhatHappened(t *testing.T) {
	noPrograms(t)
	tr := locale.T()
	for _, c := range []struct {
		name   string
		answer dmsocket.Reply
		want   string
	}{
		{"applied", dmsocket.Answer("/etc/lys-shell/runtime.yaml", nil), tr.Get(greeterApplyDone)},
		{
			"invalid", dmsocket.Answer("", dmsocket.Refuse(dmsocket.Invalid, "no recognised login screen keys")),
			tr.Get(greeterApplyInvalid, i18n.Str("reason", "no recognised login screen keys")),
		},
		{
			"not authorized", dmsocket.Answer("", dmsocket.Refuse(dmsocket.Unauthorized, "polkit does not authorize uid 1000")),
			tr.Get(greeterApplyUnauthorized, i18n.Str("reason", "polkit does not authorize uid 1000")),
		},
		{"dismissed", dmsocket.Answer("", dmsocket.Refuse(dmsocket.Dismissed, "the authentication was dismissed")), tr.Get(greeterApplyDismissed)},
		{
			"failed", dmsocket.Answer("", dmsocket.Refuse(dmsocket.Failed, "write /etc/lys-shell/runtime.yaml: disk full")),
			tr.Get(greeterApplyFailed, i18n.Str("reason", "write /etc/lys-shell/runtime.yaml: disk full")),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, daemon := serveFakeDaemon(t, c.answer)
			labels := clickGreeterApply(t, dir)
			daemon.request(t)
			if want := []string{tr.Get(greeterApplyLabel), c.want}; !slices.Equal(labels, want) {
				t.Errorf("the footer shows %q, want %q", labels, want)
			}
		})
	}
}

func TestGreeterApplyWithoutTheDisplayManager(t *testing.T) {
	noPrograms(t)
	tr := locale.T()
	labels := clickGreeterApply(t, sockdir.Dir(t))
	if want := []string{tr.Get(greeterApplyLabel), tr.Get(greeterApplyNoDaemon)}; !slices.Equal(labels, want) {
		t.Errorf("without a daemon the footer shows %q, want %q", labels, want)
	}
}

func TestGreeterApplyFooterStartsWithWhatApplyingChanges(t *testing.T) {
	tr := locale.T()
	labels := treetest.Labels(greeterApplyFooter(testKit(t, "")))
	if want := []string{tr.Get(greeterApplyLabel), tr.Get(greeterApplyHint)}; !slices.Equal(labels, want) {
		t.Errorf("the footer shows %q, want %q", labels, want)
	}
}

func TestGreeterApplyOutcomesReadApartInEveryLanguage(t *testing.T) {
	const reason = "the daemon's own words"
	invalid := fmt.Errorf("%w: %s", greeterconfig.ErrInvalid, reason)
	outcomes := []struct {
		name   string
		err    error
		id     string
		reason string
	}{
		{"applied", nil, greeterApplyDone, ""},
		{"no daemon", dmsocket.ErrNoDaemon, greeterApplyNoDaemon, ""},
		{"invalid before it is sent", invalid, greeterApplyInvalid, invalid.Error()},
		{"invalid", dmsocket.Refuse(dmsocket.Invalid, reason), greeterApplyInvalid, reason},
		{"not authorized", dmsocket.Refuse(dmsocket.Unauthorized, reason), greeterApplyUnauthorized, reason},
		{"dismissed", dmsocket.Refuse(dmsocket.Dismissed, reason), greeterApplyDismissed, ""},
		{"failed", dmsocket.Refuse(dmsocket.Failed, reason), greeterApplyFailed, reason},
		{"failed before an answer", errors.New(reason), greeterApplyFailed, reason},
	}
	ids := []string{
		greeterApplyLabel, greeterApplyHint, greeterApplyDone, greeterApplyNoDaemon,
		greeterApplyInvalid, greeterApplyUnauthorized, greeterApplyDismissed, greeterApplyFailed,
	}
	read := func(lang i18n.LangID) []string {
		tr, err := i18n.Select(locale.Assets(), []i18n.LangID{lang})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range outcomes {
			got := greeterApplyOutcome(tr, c.err)
			if want := tr.Get(c.id, i18n.Str("reason", c.reason)); got != want {
				t.Errorf("%s: %s says %q, want the %s message %q", lang, c.name, got, c.id, want)
			}
			if carries := strings.Contains(got, reason); carries != (c.reason != "") {
				t.Errorf("%s: %s says %q, carrying the reason = %v", lang, c.name, got, carries)
			}
		}
		texts := make([]string, len(ids))
		for i, id := range ids {
			if !tr.Has(id) {
				t.Errorf("%s: no %s", lang, id)
			}
			texts[i] = tr.Get(id, i18n.Str("reason", reason))
			if same := slices.Index(texts[:i], texts[i]); same >= 0 {
				t.Errorf("%s: %s and %s both read %q", lang, ids[same], id, texts[i])
			}
		}
		return texts
	}
	langs, err := locale.Assets().Available()
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) < 2 {
		t.Fatalf("languages %v, want the fallback and a translation at least", langs)
	}
	fallback := read(i18n.Fallback)
	for _, lang := range langs {
		if lang.Equal(i18n.Fallback) {
			continue
		}
		for i, text := range read(lang) {
			if text == fallback[i] {
				t.Errorf("%s: %s is the %s text, not a translation: %q", lang, ids[i], i18n.Fallback, text)
			}
		}
	}
}
