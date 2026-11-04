// Package e2e holds lys-settings to a real lys-wm: a headless compositor in a
// private runtime directory, its real parser, its real reload, and the real app
// as its client. It runs under `just test`, and so in `just docker check` and CI,
// never from a plain `go test` on a desktop.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-shell/settingspage"

	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	bound = 60 * time.Second
	poll  = 50 * time.Millisecond
)

type session struct {
	t       *testing.T
	file    *wmconfig.File
	path    string
	logs    bytes.Buffer
	stop    func()
	runtime string
}

func needHeadless(t *testing.T) {
	t.Helper()
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("set by `just test`, which runs away from the desktop session")
	}
	for _, program := range []string{"lys-wm", "lys-wmctl"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Fatalf("the dev shell provides %s: %v", program, err)
		}
	}
}

func private(t *testing.T) (config, runtime string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lys-settings-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	config, runtime = filepath.Join(dir, "config"), filepath.Join(dir, "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("LYS_WM_SOCKET", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "no-bus"))
	return config, runtime
}

func start(t *testing.T) *session {
	t.Helper()
	needHeadless(t)
	config, runtime := private(t)
	s := &session{t: t, runtime: runtime, path: filepath.Join(config, "lys-wm", "config.yaml")}
	var err error
	if s.file, err = wmconfig.Open(os.Getenv, wmconfig.Exec); err != nil {
		t.Fatal(err)
	}
	if s.file.Path() != s.path {
		t.Fatalf("the file is %s, want %s", s.file.Path(), s.path)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, []byte("# my config\nxwayland:\n  enabled: false # no X here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("lys-wm", "-backend", "headless", "-renderer", "soft")
	cmd.Stdout, cmd.Stderr = &s.logs, &s.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	s.stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(bound):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(s.stop)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("lys-wm said:\n%s", s.logs.String())
		}
	})
	s.until("lys-wm answers", func() bool {
		select {
		case err := <-done:
			t.Fatalf("lys-wm exited: %v\n%s", err, s.logs.String())
		default:
		}
		_, err := s.ctl("version")
		return err == nil
	})
	return s
}

func (s *session) until(what string, ok func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(bound)
	for !ok() {
		if time.Now().After(deadline) {
			s.t.Fatalf("no %s in %s", what, bound)
		}
		time.Sleep(poll)
	}
}

func (s *session) ctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	out, err := wmconfig.Exec(ctx, append([]string{"lys-wmctl"}, args...)...)
	return strings.TrimSpace(string(out)), err
}

func (s *session) reducedMotion() string {
	s.t.Helper()
	out, err := s.ctl("reducedmotion")
	if err != nil {
		s.t.Fatal(err)
	}
	return out
}

func (s *session) content() string {
	s.t.Helper()
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
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
	got := s.content()
	if !strings.HasPrefix(got, "# my config\nxwayland:\n  enabled: false # no X here\n") || !strings.Contains(got, "reduced_motion: true") {
		t.Fatalf("%q", got)
	}

	before := got
	_, err = s.file.Apply([]wmconfig.Edit{{Path: []string{"gaps", "inner"}, Value: -1}})
	var rejected *wmconfig.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("lys-wm's parser did not refuse it: %v", err)
	}
	if !strings.Contains(err.Error(), s.path) || !strings.Contains(err.Error(), "gaps.inner") {
		t.Fatalf("the refusal does not name the file and key: %v", err)
	}
	if s.content() != before {
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
		t.Fatalf("after removing the key: %s", got)
	}

	s.stop()
	reload, err = s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}})
	if err != nil || reload != wmconfig.NotRunning {
		t.Fatalf("with lys-wm gone: %v, %v", reload, err)
	}
	if !strings.Contains(s.content(), "reduced_motion: true") {
		t.Fatal("the change did not reach the file for the next lys-wm")
	}
}

func TestAHomeManagerLinkStopsAChangeBeforeItReachesAnything(t *testing.T) {
	s := start(t)
	store := filepath.Join(filepath.Dir(s.path), "..", "store-config.yaml")
	if err := os.WriteFile(store, []byte(s.content()), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, s.path); err != nil {
		t.Fatal(err)
	}
	_, err := s.file.Apply([]wmconfig.Edit{{Path: motion, Value: true}})
	var ro *wmconfig.ReadOnlyError
	if !errors.As(err, &ro) || !strings.Contains(err.Error(), "Home Manager") {
		t.Fatalf("%v", err)
	}
	if got := s.reducedMotion(); got != "false" {
		t.Fatalf("%s", got)
	}
}

func TestTheBuiltInConfigAndSchemaAreWhatThisAppExpects(t *testing.T) {
	needHeadless(t)
	private(t)
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
	after, _, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if binds, ok := after["binds"].([]any); !ok || len(binds) == 0 {
		t.Fatalf("a first change lost the binds: %v", after["binds"])
	}
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

	displays, err := filepath.Glob(filepath.Join(s.runtime, "wayland-*"))
	if err != nil || len(displays) == 0 {
		t.Fatalf("no display in %s: %v", s.runtime, err)
	}
	display := ""
	for _, d := range displays {
		if !strings.HasSuffix(d, ".lock") {
			display = filepath.Base(d)
		}
	}
	t.Setenv("WAYLAND_DISPLAY", display)

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
	s.until("settings window", func() bool {
		windows, err := s.ctl("windows")
		return err == nil && strings.Contains(windows, "dev.stubbe.lys.settings")
	})
}
