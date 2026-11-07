package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func runCaptured(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func noSession(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("HOME", dir)
	t.Setenv("WAYLAND_DISPLAY", "no-such-wayland")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+dir+"/no-such-bus")
}

func TestHelpAndVersionNeedNoSession(t *testing.T) {
	noSession(t)
	for _, tc := range []struct{ arg, want string }{
		{"-h", usage},
		{"--help", usage},
		{"--version", "lys-settings dev\n"},
		{"-V", "lys-settings dev\n"},
	} {
		stdout, stderr, code := runCaptured(t, tc.arg)
		if code != exitOK || stderr != "" || stdout != tc.want {
			t.Errorf("lys-settings %s: code %d stderr %q stdout\n%s", tc.arg, code, stderr, stdout)
		}
	}
}

func TestWhatTheCommandDoesNotTakeIsAUsageError(t *testing.T) {
	noSession(t)
	for _, args := range [][]string{{"--bogus"}, {"stray"}, {"--page"}} {
		stdout, stderr, code := runCaptured(t, args...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "error: ") {
			t.Errorf("lys-settings %q: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestListPagesPrintsJSONWithoutASession(t *testing.T) {
	noSession(t)
	stdout, stderr, code := runCaptured(t, "--list-pages")
	if code != exitOK || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	var pages []struct{ ID, Title, Icon string }
	if err := json.Unmarshal([]byte(stdout), &pages); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if len(pages) == 0 || pages[0].ID == "" || pages[0].Title == "" {
		t.Fatalf("%+v", pages)
	}
	if entries, err := os.ReadDir(os.Getenv("XDG_CONFIG_HOME")); err == nil && len(entries) != 0 {
		t.Fatalf("listing the pages wrote %v", entries)
	}
}

func TestListPagesDoesNotReadTheConfig(t *testing.T) {
	noSession(t)
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "lys-shell")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("bar: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCaptured(t, "--list-pages")
	if code != exitOK || stderr != "" || !strings.Contains(stdout, `"id"`) {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestAnUnknownSettingIsAUsageErrorNamingIt(t *testing.T) {
	noSession(t)
	for args, want := range map[string][]string{
		`"does.not.exist"`: {"--setting", "does.not.exist"},
		`"bar-general"`:    {"--page", "theme", "--setting", "bar.location"},
	} {
		stdout, stderr, code := runCaptured(t, want...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "error: ") || !strings.Contains(stderr, args) {
			t.Errorf("lys-settings %q: code %d stdout %q stderr %q", want, code, stdout, stderr)
		}
	}
}

func TestListPagesOpensNoConfigFile(t *testing.T) {
	noSession(t)
	home := os.Getenv("XDG_CONFIG_HOME")
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	for _, dir := range []string{home, filepath.Join(home, "lys-shell"), filepath.Join(home, "lys-wm")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := unix.InotifyAddWatch(fd, dir, unix.IN_OPEN|unix.IN_ACCESS); err != nil {
			t.Fatal(err)
		}
	}
	if _, stderr, code := runCaptured(t, "--list-pages"); code != exitOK {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	buf := make([]byte, 4096)
	n, err := unix.Read(fd, buf)
	if !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("listing the pages opened or read %d bytes of inotify events under the config: %v", n, err)
	}
}
