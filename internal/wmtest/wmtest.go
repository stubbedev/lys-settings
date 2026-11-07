// Package wmtest runs a headless lys-wm in a private runtime directory for
// tests: its real parser, its real reload and its real Wayland globals,
// never the desktop session's.
package wmtest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

// Bound is how long a test waits for lys-wm before it fails.
const Bound = 60 * time.Second

const poll = 50 * time.Millisecond

// Need skips t outside `just test`, which sets LYS_SETTINGS_HEADLESS away
// from the desktop session, and fails it without lys-wm and lys-wmctl.
func Need(t *testing.T) {
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

// Private points HOME, the config home and the runtime directory at a
// fresh temporary directory and cuts the display and bus variables, so
// nothing reaches the session; it returns the config home and the runtime
// directory.
func Private(t *testing.T) (config, runtime string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lys-settings-wm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	config = filepath.Join(dir, "config")
	runtime = filepath.Join(dir, "run")
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

// Session is a running headless lys-wm.
type Session struct {
	t *testing.T
	// Path is the config file lys-wm reads.
	Path string
	// Runtime is its runtime directory.
	Runtime string
	logs    syncBuffer
	stop    func()
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Start writes config as lys-wm's config file in a private home and runs a
// headless lys-wm there until the test ends.
func Start(t *testing.T, config string) *Session {
	t.Helper()
	Need(t)
	home, runtime := Private(t)
	s := &Session{t: t, Path: filepath.Join(home, "lys-wm", "config.yaml"), Runtime: runtime}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("lys-wm", "-backend", "headless", "-renderer", "soft")
	cmd.Stdout, cmd.Stderr = &s.logs, &s.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	stopped := false
	s.stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		var err error
		select {
		case <-exited:
			err = waitErr
		case <-time.After(Bound):
			err = cmd.Process.Kill()
			<-exited
			err = errors.Join(err, waitErr)
		}
		if err != nil {
			t.Logf("lys-wm ended: %v", err)
		}
	}
	t.Cleanup(s.stop)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("lys-wm said:\n%s", s.logs.String())
		}
	})
	s.Until("lys-wm answers", func() bool {
		select {
		case <-exited:
			t.Fatalf("lys-wm exited: %v\n%s", waitErr, s.logs.String())
		default:
		}
		_, err := s.Ctl("version")
		return err == nil
	})
	return s
}

// Stop ends lys-wm before the test does.
func (s *Session) Stop() { s.stop() }

// Until polls ok until it holds, failing the test after Bound.
func (s *Session) Until(what string, ok func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(Bound)
	for !ok() {
		if time.Now().After(deadline) {
			s.t.Fatalf("no %s in %s", what, Bound)
		}
		time.Sleep(poll)
	}
}

// Ctl runs lys-wmctl against this lys-wm and returns its trimmed output.
func (s *Session) Ctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Bound)
	defer cancel()
	out, err := wmconfig.Exec(ctx, append([]string{"lys-wmctl"}, args...)...)
	return strings.TrimSpace(string(out)), err
}

// Display points WAYLAND_DISPLAY at this lys-wm's socket.
func (s *Session) Display() {
	s.t.Helper()
	displays, err := filepath.Glob(filepath.Join(s.Runtime, "wayland-*"))
	if err != nil {
		s.t.Fatal(err)
	}
	for _, d := range displays {
		if !strings.HasSuffix(d, ".lock") {
			s.t.Setenv("WAYLAND_DISPLAY", filepath.Base(d))
			return
		}
	}
	s.t.Fatalf("no display in %s", s.Runtime)
}
