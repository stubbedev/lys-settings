package wmconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type tools struct {
	calls       [][]string
	reject      string
	reloadErr   error
	duringCheck func()
	checked     []string
}

func flagValue(argv []string, name string) (string, bool) {
	i := slices.Index(argv, name)
	if i < 0 || i+1 >= len(argv) {
		return "", false
	}
	return argv[i+1], true
}

func (s *tools) run(_ context.Context, argv ...string) ([]byte, error) {
	s.calls = append(s.calls, argv)
	switch argv[0] + " " + argv[1] {
	case "lys-wm --default-config":
		return []byte("binds: []\n"), nil
	case "lys-wm --check-config":
		candidate, ok := flagValue(argv, "--runtime")
		if !ok {
			return nil, fmt.Errorf("no candidate: %q", argv)
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			return nil, err
		}
		s.checked = append(s.checked, string(data))
		if s.duringCheck != nil {
			s.duringCheck()
		}
		if s.reject != "" {
			return nil, &ToolError{Argv: argv, Exit: 1, Stderr: candidate + ":1: " + s.reject, Err: errors.New("exit status 1")}
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

func (s *tools) check(t *testing.T) []string {
	t.Helper()
	for _, argv := range s.calls {
		if argv[1] == "--check-config" {
			return argv
		}
	}
	t.Fatal("no check")
	return nil
}

func openIn(t *testing.T, s *tools) (f *File, config, runtime string) {
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
	return f, filepath.Join(dir, "lys-wm", "config.yaml"), filepath.Join(dir, "lys-wm", "runtime.yaml")
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

func only(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !slices.Contains(names, e.Name()) {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s: %v", path, err)
	}
}

var setGaps = []Edit{{Path: []string{"gaps", "inner"}, Value: 3}}

func TestAChangeIsCheckedWrittenToTheOverridesAndReloadedInThatOrder(t *testing.T) {
	s := &tools{}
	f, config, runtime := openIn(t, s)
	const declared = "gaps:\n  inner: 1\n"
	write(t, config, declared, 0o644)
	write(t, runtime, "# mine\nunknown_to_me: 1\ngaps:\n  inner: 2 # small\n", 0o640)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != Reloaded {
		t.Fatalf("%v, %v", reload, err)
	}
	want := "# mine\nunknown_to_me: 1\ngaps:\n  inner: 3 # small\n"
	if got := read(t, runtime); got != want {
		t.Fatalf("%q", got)
	}
	if got := read(t, config); got != declared {
		t.Fatalf("the config changed: %q", got)
	}
	if !slices.Equal(s.checked, []string{want}) {
		t.Fatalf("checked %q", s.checked)
	}
	if got, ok := flagValue(s.check(t), "--config"); !ok || got != config {
		t.Fatalf("checked against %q", got)
	}
	if !s.ran("lys-wmctl reload") {
		t.Fatal("no reload")
	}
	info, err := os.Stat(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode %o", info.Mode().Perm())
	}
	only(t, filepath.Dir(runtime), "config.yaml", "runtime.yaml")
}

func TestSeveralEditsAreOneWriteAndOneReload(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "tearing: false\n", 0o644)
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

func TestWithoutAConfigAChangeWritesOnlyThatKeyToTheOverrides(t *testing.T) {
	s := &tools{}
	f, config, runtime := openIn(t, s)
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if got := read(t, runtime); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
	absent(t, config)
	if s.ran("lys-wm --default-config") {
		t.Fatal("a new override file started from the built-in config")
	}
	if _, ok := flagValue(s.check(t), "--config"); ok {
		t.Fatal("named a config that does not exist")
	}
	only(t, filepath.Dir(runtime), "runtime.yaml")
}

func TestARefusedChangeLeavesTheOverridesAndDoesNotReload(t *testing.T) {
	s := &tools{reject: "gaps.inner: -1 is not zero or more"}
	f, _, runtime := openIn(t, s)
	const before = "gaps:\n  inner: 1\n"
	write(t, runtime, before, 0o644)
	_, err := f.Apply([]Edit{{Path: []string{"gaps", "inner"}, Value: -1}})
	if _, ok := errors.AsType[*RejectedError](err); !ok {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), runtime+":1: gaps.inner") || strings.Contains(err.Error(), ".check-") {
		t.Fatalf("the error should name the runtime file: %v", err)
	}
	if got := read(t, runtime); got != before {
		t.Fatalf("%q", got)
	}
	if s.ran("lys-wmctl reload") {
		t.Fatal("reloaded after a refusal")
	}
	only(t, filepath.Dir(runtime), "runtime.yaml")
}

func TestARefusedFirstChangeCreatesNoFile(t *testing.T) {
	s := &tools{reject: "no"}
	f, _, runtime := openIn(t, s)
	if _, err := f.Apply(setGaps); err == nil {
		t.Fatal("accepted")
	}
	absent(t, runtime)
}

func TestAChangeThatChangesNothingIsNotWrittenOrReloaded(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "gaps:\n  inner: 3\n", 0o644)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != Unchanged {
		t.Fatalf("%v, %v", reload, err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
}

func TestResettingAKeyWithNoOverrideDoesNothing(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	unset := []Edit{{Path: []string{"gaps", "inner"}, Unset: true}}
	if reload, err := f.Apply(unset); err != nil || reload != Unchanged {
		t.Fatalf("no file: %v, %v", reload, err)
	}
	absent(t, runtime)
	write(t, runtime, "tearing: true\n", 0o644)
	if reload, err := f.Apply(unset); err != nil || reload != Unchanged {
		t.Fatalf("another key: %v, %v", reload, err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
}

func TestResettingAnOverrideRemovesOnlyIt(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "tearing: true\ngaps:\n  inner: 3\n", 0o644)
	if _, err := f.Apply([]Edit{{Path: []string{"gaps", "inner"}, Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, runtime); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	if !s.ran("lys-wmctl reload") {
		t.Fatal("no reload")
	}
}

func TestResetAllDropsEveryOverride(t *testing.T) {
	s := &tools{}
	f, config, runtime := openIn(t, s)
	write(t, config, "tearing: false\n", 0o644)
	write(t, runtime, "tearing: true\ngaps:\n  inner: 3\ninput: {keyboard: {layout: us}}\n", 0o644)
	reload, err := f.ResetAll()
	if err != nil || reload != Reloaded {
		t.Fatalf("%v, %v", reload, err)
	}
	if over, err := f.Overrides(); err != nil || len(over) != 0 {
		t.Fatalf("%v %v", over, err)
	}
	if got := read(t, config); got != "tearing: false\n" {
		t.Fatalf("the config changed: %q", got)
	}
}

func TestResetAllWithoutOverridesRunsNothing(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	if reload, err := f.ResetAll(); err != nil || reload != Unchanged {
		t.Fatalf("%v, %v", reload, err)
	}
	absent(t, runtime)
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

func TestAHomeManagerConfigTakesOverridesAndIsLeftAlone(t *testing.T) {
	s := &tools{}
	f, config, runtime := openIn(t, s)
	store := homeManagerLink(t, config)
	if err := f.Writable(); err != nil {
		t.Fatalf("a read-only config blocks nothing now: %v", err)
	}
	reload, err := f.Apply(setGaps)
	if err != nil || reload != Reloaded {
		t.Fatalf("%v, %v", reload, err)
	}
	if got := read(t, runtime); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
	if got, ok := flagValue(s.check(t), "--config"); !ok || got != config {
		t.Fatalf("checked against %q", got)
	}
	if target, err := os.Readlink(config); err != nil || target != store {
		t.Fatalf("the link moved: %q, %v", target, err)
	}
	if got := read(t, store); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	if tree, exists, err := f.Read(); err != nil || !exists || tree["tearing"] != true {
		t.Fatalf("a read-only file still reads: %v %v %v", tree, exists, err)
	}
}

func TestAReadOnlyOverrideFileIsRefusedClearly(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "tearing: true\n", 0o444)
	check := func(err error) {
		t.Helper()
		var ro *ReadOnlyError
		if !errors.As(err, &ro) || !strings.Contains(err.Error(), runtime+" is read-only") {
			t.Fatalf("%v", err)
		}
	}
	check(f.Writable())
	_, err := f.Apply(setGaps)
	check(err)
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
	if got := read(t, runtime); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
}

func TestAReadOnlyLinkedOverrideFileNamesBothPaths(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	store := homeManagerLink(t, runtime)
	_, err := f.Apply(setGaps)
	if _, ok := errors.AsType[*ReadOnlyError](err); !ok {
		t.Fatalf("%v", err)
	}
	for _, want := range []string{runtime, store} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %q", err, want)
		}
	}
	if len(s.calls) != 0 {
		t.Fatalf("ran %q", s.calls)
	}
}

func TestAWritableLinkIsWrittenThroughAndStaysALink(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	real := filepath.Join(t.TempDir(), "dotfiles", "wm-runtime.yaml")
	write(t, real, "gaps:\n  inner: 1\n", 0o644)
	if err := os.MkdirAll(filepath.Dir(runtime), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, runtime); err != nil {
		t.Fatal(err)
	}
	if err := f.Writable(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(runtime); err != nil || target != real {
		t.Fatalf("no longer the link: %q, %v", target, err)
	}
	if got := read(t, real); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestADanglingLinkIsAnError(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	if err := os.MkdirAll(filepath.Dir(runtime), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Apply(setGaps); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("%v", err)
	}
}

func TestAnOverrideFileThatDoesNotParseIsRefusedAndKept(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	const broken = "gaps: [\n"
	write(t, runtime, broken, 0o644)
	if _, err := f.Apply(setGaps); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("%v", err)
	}
	if got := read(t, runtime); got != broken {
		t.Fatalf("%q", got)
	}
	if _, err := f.Overrides(); err == nil || !strings.Contains(err.Error(), runtime) {
		t.Fatalf("a read says nothing of it: %v", err)
	}
}

func TestAHandEditDuringTheCheckIsKept(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "gaps:\n  inner: 1\n", 0o644)
	once := true
	s.duringCheck = func() {
		if once {
			once = false
			write(t, runtime, "# by hand\ngaps:\n  inner: 1\n", 0o644)
		}
	}
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if got := read(t, runtime); got != "# by hand\ngaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestAnOverrideFileMadeDuringTheCheckIsKept(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	once := true
	s.duringCheck = func() {
		if once {
			once = false
			write(t, runtime, "tearing: true\n", 0o644)
		}
	}
	if _, err := f.Apply(setGaps); err != nil {
		t.Fatal(err)
	}
	if got := read(t, runtime); got != "tearing: true\ngaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestAFileThatKeepsChangingIsGivenUpOn(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "gaps:\n  inner: 1\n", 0o644)
	n := 0
	s.duringCheck = func() {
		n++
		write(t, runtime, fmt.Sprintf("# %d\ngaps:\n  inner: 1\n", n), 0o644)
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
	f, _, runtime := openIn(t, s)
	write(t, runtime, "gaps:\n  inner: 1\n", 0o644)
	reload, err := f.Apply(setGaps)
	if err != nil || reload != NotRunning {
		t.Fatalf("%v, %v", reload, err)
	}
	if got := read(t, runtime); got != "gaps:\n  inner: 3\n" {
		t.Fatalf("%q", got)
	}
}

func TestAFailedReloadSaysTheChangeWasSaved(t *testing.T) {
	s := &tools{reloadErr: &ToolError{Exit: 1, Stderr: "lys-wmctl: bad layout", Err: errors.New("exit status 1")}}
	f, _, runtime := openIn(t, s)
	write(t, runtime, "gaps:\n  inner: 1\n", 0o644)
	_, err := f.Apply(setGaps)
	var failed *ReloadError
	if !errors.As(err, &failed) || !strings.Contains(err.Error(), "bad layout") {
		t.Fatalf("%v", err)
	}
	if got := read(t, runtime); got != "gaps:\n  inner: 3\n" {
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
	write(t, f.Runtime(), "tearing: true\n", 0o644)
	_, err = f.Apply(setGaps)
	var rejected *RejectedError
	if err == nil || errors.As(err, &rejected) || !strings.Contains(err.Error(), "check the change") {
		t.Fatalf("%v", err)
	}
}

func TestTheOverridesAreLaidOverTheConfigAsLysWMLaysThem(t *testing.T) {
	base := map[string]any{
		"gaps":  map[string]any{"inner": 1, "outer": 2},
		"binds": []any{"a", "b"},
		"input": map[string]any{"keyboard": map[string]any{"layout": "us", "repeat_rate": 25}},
	}
	over := map[string]any{
		"gaps":    map[string]any{"inner": 8},
		"binds":   []any{"c"},
		"input":   nil,
		"tearing": true,
	}
	want := map[string]any{
		"gaps":    map[string]any{"inner": 8, "outer": 2},
		"binds":   []any{"c"},
		"input":   nil,
		"tearing": true,
	}
	if got := Merge(base, over); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if base["gaps"].(map[string]any)["inner"] != 1 {
		t.Fatal("the merge changed the config")
	}
	if got := Merge(nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("%v", got)
	}
	for path, want := range map[string]bool{
		"gaps.inner":              true,
		"gaps.outer":              false,
		"gaps":                    true,
		"binds":                   true,
		"input.keyboard.layout":   true,
		"tearing":                 true,
		"decoration.blur.enabled": false,
	} {
		if got := Overridden(over, strings.Split(path, ".")); got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
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
	"e":{"oneOf":[{"type":"integer"},{"type":"array"}],"default":8},
	"i":{"default":"accent","oneOf":[{"type":"string","format":"lys-wm-color"},{"type":"object","properties":{"angle":{"type":"number"},"colors":{"type":"array","minItems":1,"maxItems":10,"items":{"type":"string"}}}}]},
	"p":{"type":"object","additionalProperties":{"type":"string"},"default":{"red":"#f38ba8"}},
	"f":{"type":"object","properties":{"g":{"type":"string"},"c":{"type":"string","format":"lys-wm-color"}}},
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
	if f := field("e"); f.Kind != Composite || f.Default != 8 || f.MaxItems != unboundedInt {
		t.Errorf("e: %+v", f)
	}
	if f := field("i"); f.Kind != Composite || f.Default != "accent" {
		t.Errorf("i: %+v", f)
	}
	if f := field("i", "colors"); f.Kind != Composite || f.MinItems != 1 || f.MaxItems != 10 {
		t.Errorf("i.colors, in the object form: %+v", f)
	}
	if f := field("p"); !reflect.DeepEqual(f.Default, map[string]any{"red": "#f38ba8"}) {
		t.Errorf("p: %+v", f)
	}
	if f := field("f", "g"); f.Kind != Text || f.Format != "" {
		t.Errorf("f.g: %+v", f)
	}
	if f := field("f", "c"); f.Kind != Text || f.Format != "lys-wm-color" {
		t.Errorf("f.c: %+v", f)
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

func TestSchemaReachesListItemsAndMappingValues(t *testing.T) {
	s, err := ParseSchema([]byte(`{"type":"object","properties":{
	"outputs":{"type":"array","items":{"type":"object","properties":{"scale":{"type":"number"},"match":{"type":"string"}},"additionalProperties":false}},
	"submaps":{"type":"object","additionalProperties":{"type":"array","items":{"type":"object","properties":{"chord":{"type":"string"},"flags":{"type":"array","items":{"type":"string","enum":["locked"]}}}}}},
	"env":{"type":"object","additionalProperties":{"type":"string"}},
	"closed":{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := s.Field([]string{"outputs", Each, "scale"}); !ok || f.Kind != Float {
		t.Errorf("outputs.[].scale: %+v %v", f, ok)
	}
	if keys, ok := s.Keys([]string{"outputs", Each}); !ok || !slices.Equal(keys, []string{"match", "scale"}) {
		t.Errorf("an output's keys: %v %v", keys, ok)
	}
	if f, ok := s.Field([]string{"submaps", Each, Each, "flags", Each}); !ok || f.Kind != Enum || !slices.Equal(f.Variants, []string{"locked"}) {
		t.Errorf("a submap bind's flag: %+v %v", f, ok)
	}
	if f, ok := s.Field([]string{"env", Each}); !ok || f.Kind != Text {
		t.Errorf("an env value: %+v %v", f, ok)
	}
	for _, path := range [][]string{{"closed", Each}, {"env", Each, Each}, {"outputs", "scale"}} {
		if _, ok := s.Field(path); ok {
			t.Errorf("%v names a key", path)
		}
	}
	if _, ok := s.Keys([]string{"env"}); ok {
		t.Error("a mapping without keys of its own lists some")
	}
}

func TestNormalReadsACompositeAsTheFileWould(t *testing.T) {
	f := Field{Kind: Composite}
	from := map[string]any{"colors": []any{"accent"}, "angle": float64(45)}
	file, err := Tree([]byte("x:\n  colors: [accent]\n  angle: 45\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Normal(from); !reflect.DeepEqual(got, f.Normal(file["x"])) || !reflect.DeepEqual(got, file["x"]) {
		t.Fatalf("%#v, the file reads %#v", got, file["x"])
	}
}

func TestACheckAsksLysWMAboutTheEditedOverridesAndWritesNothing(t *testing.T) {
	s := &tools{}
	f, _, runtime := openIn(t, s)
	const before = "gaps:\n  inner: 1\n"
	write(t, runtime, before, 0o644)
	if err := f.Check([]Edit{{Path: []string{"binds"}, Value: []any{map[string]any{"chord": "SUPER + Q", "action": "kill"}}}}); err != nil {
		t.Fatal(err)
	}
	if len(s.checked) != 1 || !strings.Contains(s.checked[0], "inner: 1") || !strings.Contains(s.checked[0], "chord: SUPER + Q") {
		t.Fatalf("checked %q", s.checked)
	}
	s.reject = "binds[0].chord: is empty"
	err := f.Check([]Edit{{Path: []string{"binds"}, Value: []any{map[string]any{}}}})
	if _, ok := errors.AsType[*RejectedError](err); !ok || !strings.Contains(err.Error(), runtime+":1: binds[0].chord") {
		t.Fatalf("%v", err)
	}
	if got := read(t, runtime); got != before {
		t.Fatalf("%q", got)
	}
	if s.ran("lys-wmctl reload") {
		t.Fatal("a check reloaded")
	}
	only(t, filepath.Dir(runtime), "runtime.yaml")
}
