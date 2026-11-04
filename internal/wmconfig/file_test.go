package wmconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const builtIn = "# built in\nbinds:\n  - {chord: SUPER + Q, action: kill}\nenv:\n  A: b\n"

type tools struct {
	calls       [][]string
	reject      string
	reloadErr   error
	duringCheck func()
	checked     []string
}

func (s *tools) run(_ context.Context, argv ...string) ([]byte, error) {
	s.calls = append(s.calls, argv)
	switch argv[0] + " " + argv[1] {
	case "lys-wm --default-config":
		return []byte(builtIn), nil
	case "lys-wm --check-config":
		data, err := os.ReadFile(argv[3])
		if err != nil {
			return nil, err
		}
		s.checked = append(s.checked, string(data))
		if s.duringCheck != nil {
			s.duringCheck()
		}
		if s.reject != "" {
			return nil, &ToolError{Argv: argv, Exit: 1, Stderr: argv[3] + ":1: " + s.reject, Err: errors.New("exit status 1")}
		}
		return nil, nil
	case "lys-wmctl reload":
		return nil, s.reloadErr
	}
	return nil, fmt.Errorf("unexpected program: %q", argv)
}

func (s *tools) ran(name string) bool {
	return slices.ContainsFunc(s.calls, func(argv []string) bool { return argv[0]+" "+argv[1] == name })
}

func openIn(t *testing.T, s *tools) (*File, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := Open(func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	}, s.run)
	if err != nil {
		t.Fatal(err)
	}
	return f, filepath.Join(dir, "lys-wm", "config.yaml")
}

func write(t *testing.T, path, content string, perm fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func onlyConfig(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

var setGaps = []Edit{{Path: []string{"gaps", "inner"}, Value: 3}}

func TestAChangeIsCheckedWrittenAndReloadedInThatOrder(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "# mine\nunknown_to_me: 1\ngaps:\n  inner: 1 # small\n", 0o640)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != Reloaded {
		t.Fatalf("%v, %v", reload, err)
	}
	want := "# mine\nunknown_to_me: 1\ngaps:\n  inner: 3 # small\n"
	if got := read(t, path); got != want {
		t.Fatalf("%q", got)
	}
	if !slices.Equal(s.checked, []string{want}) {
		t.Fatalf("checked %q", s.checked)
	}
	if !s.ran("lys-wmctl reload") {
		t.Fatal("no reload")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode %o", info.Mode().Perm())
	}
	onlyConfig(t, path)
}

func TestSeveralEditsAreOneWriteAndOneReload(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "tearing: false\n", 0o644)
	if _, err := f.Apply([]Edit{{Path: []string{"tearing"}, Value: true}, {Path: []string{"gaps", "inner"}, Value: 3}}); err != nil {
		t.Fatal(err)
	}
	if len(s.checked) != 1 {
		t.Fatalf("%d checks", len(s.checked))
	}
	reloads := 0
	for _, argv := range s.calls {
		if argv[0] == "lys-wmctl" {
			reloads++
		}
	}
	if reloads != 1 {
		t.Fatalf("%d reloads", reloads)
	}
}

func TestANewFileStartsFromTheBuiltInConfigSoTheBindsStay(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.HasPrefix(got, builtIn) || !strings.HasSuffix(got, "gaps:\n  inner: 3\n") {
		t.Fatalf("%q", got)
	}
	onlyConfig(t, path)
}

func TestARefusedChangeLeavesTheFileAndDoesNotReload(t *testing.T) {
	s := &tools{reject: "gaps.inner: -1 is not zero or more"}
	f, path := openIn(t, s)
	const before = "gaps:\n  inner: 1\n"
	write(t, path, before, 0o644)
	_, err := f.Apply([]Edit{{Path: []string{"gaps", "inner"}, Value: -1}})
	if _, ok := errors.AsType[*RejectedError](err); !ok {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), path+":1: gaps.inner") || strings.Contains(err.Error(), ".check-") {
		t.Fatalf("the error should name the real file: %v", err)
	}
	if got := read(t, path); got != before {
		t.Fatalf("%q", got)
	}
	if s.ran("lys-wmctl reload") {
		t.Fatal("reloaded after a refusal")
	}
	onlyConfig(t, path)
}

func TestARefusedFirstChangeCreatesNoFile(t *testing.T) {
	s := &tools{reject: "no"}
	f, path := openIn(t, s)
	if _, err := f.Apply(setGaps); err == nil {
		t.Fatal("accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%v", err)
	}
}

func TestAChangeThatChangesNothingIsNotWrittenOrReloaded(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "gaps:\n  inner: 3\n", 0o644)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != Unchanged {
		t.Fatalf("%v, %v", reload, err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
}

func homeManagerLink(t *testing.T, path string) string {
	t.Helper()
	store := filepath.Join(t.TempDir(), "nix", "store", "abc-home-manager-files", "config.yaml")
	write(t, store, "tearing: true\n", 0o444)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, path); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAHomeManagerLinkIsRefusedClearlyAndLeftAlone(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	store := homeManagerLink(t, path)
	check := func(err error) {
		t.Helper()
		if _, ok := errors.AsType[*ReadOnlyError](err); !ok {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{path, store, "Home Manager"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q does not say %q", err, want)
			}
		}
	}
	check(f.Writable())
	_, err := f.Apply(setGaps)
	check(err)
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
	if target, err := os.Readlink(path); err != nil || target != store {
		t.Fatalf("the link moved: %q, %v", target, err)
	}
	if got := read(t, store); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	if tree, exists, err := f.Read(); err != nil || !exists || tree["tearing"] != true {
		t.Fatalf("a read-only file still reads: %v %v %v", tree, exists, err)
	}
}

func TestAReadOnlyPlainFileIsRefused(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "tearing: true\n", 0o444)
	_, err := f.Apply(setGaps)
	var ro *ReadOnlyError
	if !errors.As(err, &ro) || !strings.HasSuffix(err.Error(), "is read-only") {
		t.Fatalf("%v", err)
	}
}

func TestAWritableLinkIsWrittenThroughAndStaysALink(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	real := filepath.Join(t.TempDir(), "dotfiles", "wm.yaml")
	write(t, real, "gaps:\n  inner: 1\n", 0o644)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if err := f.Writable(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(path); err != nil || target != real {
		t.Fatalf("no longer the link: %q, %v", target, err)
	}
	if got := read(t, real); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestADanglingLinkIsAnError(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(setGaps); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("%v", err)
	}
}

func TestAFileThatDoesNotParseIsRefusedAndKept(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	const broken = "gaps: [\n"
	write(t, path, broken, 0o644)
	if _, err := f.Apply(setGaps); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("%v", err)
	}
	if got := read(t, path); got != broken {
		t.Fatalf("%q", got)
	}
	if _, _, err := f.Read(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("a read says nothing of it: %v", err)
	}
}

func TestAHandEditDuringTheCheckIsKept(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "gaps:\n  inner: 1\n", 0o644)
	once := true
	s.duringCheck = func() {
		if once {
			once = false
			write(t, path, "# by hand\ngaps:\n  inner: 1\n", 0o644)
		}
	}
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "# by hand\ngaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestAFileThatKeepsChangingIsGivenUpOn(t *testing.T) {
	s := &tools{}
	f, path := openIn(t, s)
	write(t, path, "gaps:\n  inner: 1\n", 0o644)
	n := 0
	s.duringCheck = func() {
		n++
		write(t, path, fmt.Sprintf("# %d\ngaps:\n  inner: 1\n", n), 0o644)
	}
	if _, err := f.Apply(setGaps); err == nil || !strings.Contains(err.Error(), "keeps changing") {
		t.Fatalf("%v", err)
	}
	if n != maxAttempts {
		t.Fatalf("%d attempts", n)
	}
}

func TestNoRunningLysWMIsNotAFailure(t *testing.T) {
	s := &tools{reloadErr: &ToolError{Exit: notRunning, Stderr: "lys-wmctl: no socket", Err: errors.New("exit status 3")}}
	f, path := openIn(t, s)
	write(t, path, "gaps:\n  inner: 1\n", 0o644)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != NotRunning {
		t.Fatalf("%v, %v", reload, err)
	}
	if got := read(t, path); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestAFailedReloadSaysTheChangeWasSaved(t *testing.T) {
	s := &tools{reloadErr: &ToolError{Exit: 1, Stderr: "lys-wmctl: bad layout", Err: errors.New("exit status 1")}}
	f, path := openIn(t, s)
	write(t, path, "gaps:\n  inner: 1\n", 0o644)
	_, err := f.Apply(setGaps)
	var failed *ReloadError
	if !errors.As(err, &failed) || !strings.Contains(err.Error(), "bad layout") {
		t.Fatalf("%v", err)
	}
	if got := read(t, path); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestACheckThatCouldNotRunIsNotReportedAsARefusal(t *testing.T) {
	run := func(_ context.Context, argv ...string) ([]byte, error) {
		return nil, &ToolError{Argv: argv, Exit: -1, Err: errors.New("killed by the timeout")}
	}
	dir := t.TempDir()
	f, err := Open(func(string) string { return dir }, run)
	if err != nil {
		t.Fatal(err)
	}
	path := f.Path()
	write(t, path, "tearing: true\n", 0o644)
	_, err = f.Apply(setGaps)
	var rejected *RejectedError
	if err == nil || errors.As(err, &rejected) || !strings.Contains(err.Error(), "check the change") {
		t.Fatalf("%v", err)
	}
}

func TestExecReportsTheProgramsExitAndStderr(t *testing.T) {
	_, err := Exec(context.Background(), "sh", "-c", "echo nope >&2; exit 3")
	var tool *ToolError
	if !errors.As(err, &tool) || tool.Exit != 3 || tool.Stderr != "nope" {
		t.Fatalf("%v", err)
	}
	if out, err := Exec(context.Background(), "sh", "-c", "echo yes"); err != nil || string(out) != "yes\n" {
		t.Fatalf("%q, %v", out, err)
	}
	if _, err := Exec(context.Background(), "no-such-program-at-all"); !errors.As(err, &tool) || tool.Exit != -1 {
		t.Fatalf("%v", err)
	}
}

func TestOpenNeedsAnAbsoluteConfigDir(t *testing.T) {
	if _, err := Open(func(string) string { return "" }, Exec); err == nil {
		t.Fatal("no home, no error")
	}
}

func TestSchemaReadsKindsBoundsAndDefaults(t *testing.T) {
	s, err := ParseSchema([]byte(`{"type":"object","properties":{
	"a":{"type":"integer","minimum":1,"maximum":4,"default":2},
	"b":{"type":"number","exclusiveMinimum":0,"maximum":1,"default":1},
	"c":{"type":"string","enum":["x","y"]},
	"d":{"type":"boolean","default":true},
	"e":{"oneOf":[{"type":"integer"},{"type":"array"}]},
	"f":{"type":"object","properties":{"g":{"type":"string"}}},
	"h":{"type":"array"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	field := func(path ...string) Field {
		t.Helper()
		f, ok := s.Field(path)
		if !ok {
			t.Fatalf("no %v", path)
		}
		return f
	}
	if f := field("a"); f.Kind != Int || f.Min != 1 || f.Max != 4 || f.Default != int64(2) || !f.HasDefault {
		t.Errorf("a: %+v", f)
	}
	if f := field("b"); f.Kind != Float || f.Min != exclusiveStep || f.Default != float64(1) {
		t.Errorf("b: %+v", f)
	}
	if f := field("c"); f.Kind != Enum || !slices.Equal(f.Variants, []string{"x", "y"}) || f.HasDefault {
		t.Errorf("c: %+v", f)
	}
	if f := field("d"); f.Kind != Bool || f.Default != true {
		t.Errorf("d: %+v", f)
	}
	if f := field("e"); f.Kind != Composite {
		t.Errorf("e: %+v", f)
	}
	if f := field("f", "g"); f.Kind != Text {
		t.Errorf("f.g: %+v", f)
	}
	if f := field("h"); f.Kind != Composite {
		t.Errorf("h: %+v", f)
	}
	if _, ok := s.Field([]string{"nope"}); ok {
		t.Error("a key the schema lacks")
	}
	if _, ok := s.Field([]string{"a", "b"}); ok {
		t.Error("a key under a scalar")
	}
	for name, data := range map[string]string{"not json": "lys-wm 1", "no keys": `{"type":"object"}`, "not an object": `{"type":"string"}`} {
		if _, err := ParseSchema([]byte(data)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestNormalGivesEditorsTheTypeTheyExpect(t *testing.T) {
	for _, tc := range []struct {
		f    Field
		in   any
		want any
	}{
		{Field{Kind: Int}, 3, int64(3)},
		{Field{Kind: Int}, 3.0, int64(3)},
		{Field{Kind: Float}, 1, 1.0},
		{Field{Kind: Float}, int64(1), 1.0},
		{Field{Kind: Text}, 1, "1"},
		{Field{Kind: Text}, 1.5, "1.5"},
		{Field{Kind: Bool}, true, true},
		{Field{Kind: Int}, "x", "x"},
	} {
		if got := tc.f.Normal(tc.in); got != tc.want {
			t.Errorf("%+v %v: %v, want %v", tc.f, tc.in, got, tc.want)
		}
	}
}
