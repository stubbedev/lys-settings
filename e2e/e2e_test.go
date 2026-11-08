// Package e2e holds lys-settings to a real lys-wm: a headless compositor in a
// private runtime directory, its real parser, its real reload, and the real app
// as its client. It runs under `just test`, and so in `just docker check` and CI,
// never from a plain `go test` on a desktop.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/settingspage"
	"github.com/stubbedev/lys-ui/app"

	"github.com/stubbedev/lys-settings/internal/wmconfig"
	"github.com/stubbedev/lys-settings/internal/wmtest"
)

const (
	bound = wmtest.Bound
	poll  = 50 * time.Millisecond
)

type session struct {
	*wmtest.Session
	t    *testing.T
	file *wmconfig.File
}

func start(t *testing.T) *session {
	t.Helper()
	s := &session{Session: wmtest.Start(t, declared), t: t}
	var err error
	if s.file, err = wmconfig.Open(os.Getenv, wmconfig.Exec); err != nil {
		t.Fatal(err)
	}
	if s.file.Path() != s.Path {
		t.Fatalf("the file is %s, want %s", s.file.Path(), s.Path)
	}
	return s
}

func (s *session) reducedMotion() string {
	s.t.Helper()
	out, err := s.Ctl("reducedmotion")
	if err != nil {
		s.t.Fatal(err)
	}
	return out
}

const declared = "# my config\nxwayland:\n  enabled: false # no X here\n"

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (s *session) content() string {
	s.t.Helper()
	return read(s.t, s.Path)
}

func (s *session) overrides() string {
	s.t.Helper()
	return read(s.t, s.file.Runtime())
}

type effectiveKey struct {
	Path   []string `json:"path"`
	Source string   `json:"source"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Value  any      `json:"value"`
}

func effective(t *testing.T) []effectiveKey {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	out, err := wmconfig.Exec(ctx, "lys-wm", "--effective-config")
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Keys []effectiveKey `json:"keys"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	return info.Keys
}

func jsonValue(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func agreesWithLysWM(t *testing.T, f *wmconfig.File) {
	t.Helper()
	base, exists, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		data, err := f.BuiltIn()
		if err != nil {
			t.Fatal(err)
		}
		if base, err = wmconfig.Tree(data); err != nil {
			t.Fatal(err)
		}
	}
	over, err := f.Overrides()
	if err != nil {
		t.Fatal(err)
	}
	merged := wmconfig.Merge(base, over)
	keys := effective(t)
	if len(keys) == 0 {
		t.Fatal("lys-wm reports no keys")
	}
	for _, k := range keys {
		if got := wmconfig.Overridden(over, k.Path); got != (k.Source == "runtime") {
			t.Errorf("%v: lys-wm says %s, this app says overridden %v", k.Path, k.Source, got)
		}
		v, ok := wmconfig.Lookup(merged, k.Path)
		if !ok || jsonValue(t, v) != jsonValue(t, k.Value) {
			t.Errorf("%v: lys-wm reads %s, this app %s", k.Path, jsonValue(t, k.Value), jsonValue(t, v))
		}
	}
}

func source(t *testing.T, path []string) effectiveKey {
	t.Helper()
	for _, k := range effective(t) {
		if slices.Equal(k.Path, path) {
			return k
		}
	}
	return effectiveKey{}
}

var motion = []string{"animations", "reduced_motion"}

func TestAChangeReachesTheRunningCompositorAndARefusedOneDoesNot(t *testing.T) {
	s := start(t)
	if got := s.reducedMotion(); got != "false" {
		t.Fatalf("at the start: %s", got)
	}

	reload, err := s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}})
	if err != nil || reload != wmconfig.Reloaded {
		t.Fatalf("%v, %v", reload, err)
	}
	if got := s.reducedMotion(); got != "true" {
		t.Fatalf("the running lys-wm did not take the change: %s", got)
	}
	if got := s.overrides(); got != "animations:\n  reduced_motion: true\n" {
		t.Fatalf("%q", got)
	}
	if got := s.content(); got != declared {
		t.Fatalf("the config changed: %q", got)
	}
	if k := source(t, motion); k.Source != "runtime" || k.File != s.file.Runtime() || k.Value != true {
		t.Fatalf("lys-wm does not read the override: %+v", k)
	}
	if k := source(t, []string{"xwayland", "enabled"}); k.Source != "config" || k.File != s.Path {
		t.Fatalf("%+v", k)
	}
	agreesWithLysWM(t, s.file)

	before := s.overrides()
	_, err = s.file.Apply([]wmconfig.Edit{{Path: []string{"gaps", "inner"}, Value: -1}})
	var rejected *wmconfig.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("lys-wm's parser did not refuse it: %v", err)
	}
	if !strings.Contains(err.Error(), s.file.Runtime()) || !strings.Contains(err.Error(), "gaps.inner") {
		t.Fatalf("the refusal does not name the file and key: %v", err)
	}
	if s.overrides() != before {
		t.Fatal("a refused change reached the file")
	}
	if got := s.reducedMotion(); got != "true" {
		t.Fatalf("after a refusal: %s", got)
	}

	_, err = s.file.Apply([]wmconfig.Edit{{Path: []string{"no_such_key"}, Value: 1}})
	if !errors.As(err, &rejected) {
		t.Fatalf("an unknown key was not refused: %v", err)
	}

	if _, err := s.file.Apply([]wmconfig.Edit{{Path: motion, Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if got := s.reducedMotion(); got != "false" {
		t.Fatalf("after removing the override: %s", got)
	}
	if k := source(t, motion); k.Source != "" {
		t.Fatalf("a reset key still comes from %+v", k)
	}

	if _, err := s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}, {Path: []string{"gaps", "inner"}, Value: 4}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.file.ResetAll(); err != nil {
		t.Fatal(err)
	}
	if got := s.reducedMotion(); got != "false" {
		t.Fatalf("after a reset of everything: %s", got)
	}
	agreesWithLysWM(t, s.file)

	s.Stop()
	reload, err = s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}})
	if err != nil || reload != wmconfig.NotRunning {
		t.Fatalf("with lys-wm gone: %v, %v", reload, err)
	}
	if !strings.Contains(s.overrides(), "reduced_motion: true") {
		t.Fatal("the change did not reach the file for the next lys-wm")
	}
}

func TestAHomeManagerLinkTakesChangesAsOverridesAndStays(t *testing.T) {
	s := start(t)
	store := filepath.Join(filepath.Dir(s.Path), "..", "store-config.yaml")
	if err := os.WriteFile(store, []byte(s.content()), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, s.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}}); err != nil {
		t.Fatal(err)
	}
	if got := s.reducedMotion(); got != "true" {
		t.Fatalf("%s", got)
	}
	if target, err := os.Readlink(s.Path); err != nil || target != store {
		t.Fatalf("the link moved: %q, %v", target, err)
	}
	if got := read(t, store); got != declared {
		t.Fatalf("%q", got)
	}
	agreesWithLysWM(t, s.file)
}

func TestTheBuiltInConfigAndSchemaAreWhatThisAppExpects(t *testing.T) {
	wmtest.Need(t)
	wmtest.Private(t)
	f, err := wmconfig.Open(os.Getenv, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := f.Schema()
	if err != nil {
		t.Fatal(err)
	}
	if field, ok := schema.Field([]string{"decoration", "shadow", "render_power"}); !ok || field.Kind != wmconfig.Int || field.Min != 1 || field.Max != 4 || !field.HasDefault {
		t.Fatalf("%+v %v", field, ok)
	}
	data, err := f.BuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := wmconfig.Tree(data)
	if err != nil {
		t.Fatal(err)
	}
	if binds, ok := tree["binds"].([]any); !ok || len(binds) == 0 {
		t.Fatalf("no binds: %v", tree["binds"])
	}
	if _, err := f.Apply([]wmconfig.Edit{{Path: []string{"xwayland", "enabled"}, Value: false}}); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := f.Read(); err != nil || exists {
		t.Fatalf("a first change made a config: %v %v", exists, err)
	}
	if got := read(t, f.Runtime()); got != "xwayland:\n  enabled: false\n" {
		t.Fatalf("%q", got)
	}
	if k := source(t, []string{"binds"}); k.Source != "built-in" {
		t.Fatalf("a first change lost the built-in binds: %+v", k)
	}
	agreesWithLysWM(t, f)
}

func buildApp(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lys-settings")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/stubbedev/lys-settings/cmd/lys-settings").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return bin
}

func TestTheAppOpensOnALysWMPageAsAClientOfLysWM(t *testing.T) {
	s := start(t)
	bin := buildApp(t)

	s.Display()

	out, err := exec.Command(bin, settingspage.ListFlag).Output()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := settingspage.Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pages {
		found = found || p.ID == "wm-input"
	}
	if !found {
		t.Fatalf("the launcher would not see the lys-wm pages: %+v", pages)
	}

	if out, err := exec.Command(bin, settingspage.PageFlag, "wm-no-such-page").CombinedOutput(); err == nil || !strings.Contains(string(out), "no page") {
		t.Fatalf("a page that does not exist: %v, %s", err, out)
	}

	var appLog bytes.Buffer
	app := exec.Command(bin, settingspage.PageFlag, "wm-input")
	app.Stdout, app.Stderr = &appLog, &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.Process.Kill()
		_ = app.Wait()
		if t.Failed() {
			t.Logf("the app said:\n%s", appLog.String())
		}
	})
	s.Until("settings window", func() bool {
		windows, err := s.Ctl("windows")
		return err == nil && strings.Contains(windows, "dev.stubbe.lys.settings")
	})
}

func (s *session) settingsWindows() int {
	s.t.Helper()
	out, err := s.Ctl("windows")
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.Count(out, "dev.stubbe.lys.settings")
}

func (s *session) launch(t *testing.T, bin string, args ...string) {
	t.Helper()
	var appLog bytes.Buffer
	app := exec.Command(bin, args...)
	app.Stdout, app.Stderr = &appLog, &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.Process.Kill()
		_ = app.Wait()
		if t.Failed() {
			t.Logf("the app said:\n%s", appLog.String())
		}
	})
}

func press(t *testing.T, key string) {
	t.Helper()
	if out, err := exec.Command("wtype", "-k", key).CombinedOutput(); err != nil {
		t.Fatalf("wtype -k %s: %v\n%s", key, err, out)
	}
}

func TestASettingLinkOpensItsPageWithTheControlFocused(t *testing.T) {
	s := start(t)
	if _, err := exec.LookPath("wtype"); err != nil {
		t.Fatalf("the dev shell provides wtype: %v", err)
	}
	bin := buildApp(t)
	s.Display()

	out, err := exec.Command(bin, settingspage.SettingFlag, "does.not.exist").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), `"does.not.exist"`) {
		t.Fatalf("an unknown setting: %v, %s", err, out)
	}

	s.launch(t, bin, settingspage.SettingFlag, "input.touchpad.natural_scroll")
	s.Until("settings window", func() bool { return s.settingsWindows() == 1 })
	s.chooseOn(t, "natural_scroll: true")
}

func TestASettingLinkMovesTheRunningWindowWithoutASecond(t *testing.T) {
	s := start(t)
	if _, err := exec.LookPath("wtype"); err != nil {
		t.Fatalf("the dev shell provides wtype: %v", err)
	}
	bin := buildApp(t)
	s.Display()

	s.launch(t, bin, settingspage.PageFlag, "wm-windows")
	s.Until("settings window", func() bool { return s.settingsWindows() == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	if out, err := exec.CommandContext(ctx, bin, settingspage.SettingFlag, "wm:input.touchpad.tap").CombinedOutput(); err != nil {
		t.Fatalf("the forwarded launch: %v\n%s", err, out)
	}
	if n := s.settingsWindows(); n != 1 {
		t.Fatalf("%d settings windows after a forwarded launch", n)
	}
	s.chooseOn(t, "tap: true")
	if n := s.settingsWindows(); n != 1 {
		t.Fatalf("%d settings windows", n)
	}
}

const settle = 3 * time.Second

func (s *session) chooseOn(t *testing.T, want string) {
	t.Helper()
	written := func() bool {
		data, err := os.ReadFile(s.file.Runtime())
		return err == nil && strings.Contains(string(data), want)
	}
	s.Until("the focused control's dropdown choosing On, writing "+want, func() bool {
		for _, key := range []string{"Escape", "space", "Home", "Down", "Return"} {
			press(t, key)
		}
		deadline := time.Now().Add(settle)
		for !written() {
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(poll)
		}
		return true
	})
}

func (s *session) inhibiting() bool {
	s.t.Helper()
	out, err := s.Ctl("activewindow")
	if err != nil {
		return false
	}
	var w struct {
		AppID    string `json:"app_id"`
		Inhibits bool   `json:"inhibits_shortcuts"`
	}
	return json.Unmarshal([]byte(out), &w) == nil && w.AppID == "dev.stubbe.lys.settings" && w.Inhibits
}

func within(d time.Duration, ok func() bool) bool {
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(poll)
	}
	return true
}

func TestTheShortcutRecorderHoldsLysWMsBindsOffAndWritesTheChord(t *testing.T) {
	s := start(t)
	if _, err := exec.LookPath("wtype"); err != nil {
		t.Fatalf("the dev shell provides wtype: %v", err)
	}
	if _, err := s.file.Apply([]wmconfig.Edit{{Path: []string{"binds"}, Value: []any{map[string]any{"chord": "SUPER + SHIFT + K", "action": "kill"}}}}); err != nil {
		t.Fatal(err)
	}
	bin := buildApp(t)
	s.Display()
	s.launch(t, bin, settingspage.PageFlag, "wm-input")
	s.Until("settings window", func() bool { return s.settingsWindows() == 1 })
	s.Until("the bind's recorder recording, with lys-wm's shortcuts inhibited", func() bool {
		for _, args := range [][]string{{settingspage.PageFlag, "wm-input"}, {settingspage.SettingFlag, "wm:binds"}} {
			if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
		time.Sleep(settle / 6)
		for _, key := range []string{"Tab", "space", "Tab", "Tab", "Tab", "Tab", "space"} {
			press(t, key)
		}
		if within(settle, s.inhibiting) {
			return true
		}
		press(t, "Escape")
		return false
	})
	if out, err := exec.Command("wtype", "-M", "logo", "-M", "shift", "-k", "j", "-m", "shift", "-m", "logo").CombinedOutput(); err != nil {
		t.Fatalf("wtype: %v\n%s", err, out)
	}
	s.Until("the recorded chord in lys-wm's syntax, taken by lys-wm", func() bool {
		return strings.Contains(s.overrides(), "chord: SUPER + SHIFT + J")
	})
	s.Until("the inhibitor dropped once the chord was recorded", func() bool { return !s.inhibiting() })
	if problems, err := s.Ctl("configproblems"); err != nil || strings.Contains(problems, "binds") {
		t.Fatalf("lys-wm reports %q, %v", problems, err)
	}
}

func registerShortcut(t *testing.T, appID, id string) <-chan bool {
	t.Helper()
	sess, err := app.Connect()
	if err != nil {
		t.Fatal(err)
	}
	a := app.NewApplication(sess)
	release := a.Hold()
	presses := make(chan bool, 16)
	registered := make(chan error, 1)
	a.Invoke(func() {
		a.RegisterGlobalShortcut(id, appID, "a test shortcut", "", func(pressed bool, _ uint64) { presses <- pressed }, func(_ *app.GlobalShortcut, err error) { registered <- err })
	})
	ran := make(chan error, 1)
	go func() { ran <- a.Run() }()
	t.Cleanup(func() {
		a.Invoke(release)
		select {
		case <-ran:
		case <-time.After(bound):
			t.Error("the shortcut client did not stop")
		}
		sess.Close()
	})
	select {
	case err := <-registered:
		if err != nil {
			t.Fatalf("register %s:%s: %v", appID, id, err)
		}
	case <-time.After(bound):
		t.Fatalf("no answer to registering %s:%s in %s", appID, id, bound)
	}
	return presses
}

func TestARecordedAppShortcutPressesWhatTheAppRegistered(t *testing.T) {
	s := start(t)
	if _, err := exec.LookPath("wtype"); err != nil {
		t.Fatalf("the dev shell provides wtype: %v", err)
	}
	state := filepath.Join(os.Getenv("HOME"), "state")
	t.Setenv("XDG_STATE_HOME", state)
	if err := os.MkdirAll(filepath.Join(state, "lys-shell"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "lys-shell", "portal-shortcuts.json"), []byte(`{"org.example.Recorder":[{"id":"start","description":"Start recording"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.file.Apply([]wmconfig.Edit{{Path: []string{"binds"}, Value: []any{map[string]any{"chord": "SUPER + SHIFT + K", "action": "kill"}}}}); err != nil {
		t.Fatal(err)
	}
	bin := buildApp(t)
	s.Display()
	presses := registerShortcut(t, "org.example.Recorder", "start")
	s.launch(t, bin, settingspage.PageFlag, "wm-input")
	s.Until("settings window", func() bool { return s.settingsWindows() == 1 })
	s.Until("the app shortcut's recorder recording", func() bool {
		for _, args := range [][]string{{settingspage.PageFlag, "wm-input"}, {settingspage.SettingFlag, "wm:binds"}} {
			if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
		time.Sleep(settle / 6)
		for _, key := range []string{"Tab", "Tab", "Tab", "Tab", "Tab", "Tab", "space"} {
			press(t, key)
		}
		if within(settle, s.inhibiting) {
			return true
		}
		press(t, "Escape")
		return false
	})
	if out, err := exec.Command("wtype", "-M", "logo", "-M", "alt", "-k", "r", "-m", "alt", "-m", "logo").CombinedOutput(); err != nil {
		t.Fatalf("wtype: %v\n%s", err, out)
	}
	s.Until("a global bind for the app's shortcut, taken by lys-wm", func() bool {
		o := s.overrides()
		return strings.Contains(o, "chord: SUPER + ALT + R") && strings.Contains(o, "action: global org.example.Recorder:start")
	})
	if out, err := s.Ctl("dispatch", "global", "org.example.Recorder:start"); err != nil {
		t.Fatalf("dispatch the bind's action: %v %s", err, out)
	}
	select {
	case pressed := <-presses:
		if !pressed {
			t.Fatal("the app saw a release before any press")
		}
	case <-time.After(bound):
		t.Fatalf("the app's shortcut was not pressed in %s", bound)
	}
}
