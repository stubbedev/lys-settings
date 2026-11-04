package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-shell/i18n"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const wmSchema = `{"type":"object","properties":{
"gaps":{"type":"object","properties":{"inner":{"type":"integer","minimum":0,"default":8},"outer":{"oneOf":[{"type":"integer"},{"type":"array"}]}}},
"tearing":{"type":"boolean","default":false},
"binds":{"type":"array"},
"decoration":{"type":"object","properties":{"active_opacity":{"type":"number","minimum":0,"maximum":1,"default":1}}},
"input":{"type":"object","properties":{"touchpad":{"type":"object","properties":{"tap":{"type":"boolean"},"click_method":{"type":"string","enum":["none","clickfinger"]}}}}},
"tabs":{"type":"object","properties":{"font":{"type":"string"}}}}}`

const wmBuiltIn = "binds:\n  - {chord: SUPER + Q, action: kill}\n"

type fakeWM struct {
	reject  string
	reloads int
	checks  int
}

func (w *fakeWM) run(_ context.Context, argv ...string) ([]byte, error) {
	switch argv[0] + " " + argv[1] {
	case "lys-wm --config-schema":
		return []byte(wmSchema), nil
	case "lys-wm --default-config":
		return []byte(wmBuiltIn), nil
	case "lys-wm --check-config":
		w.checks++
		if w.reject != "" {
			return nil, &wmconfig.ToolError{Argv: argv, Exit: 1, Stderr: argv[3] + ":3: " + w.reject, Err: errors.New("exit status 1")}
		}
		return nil, nil
	case "lys-wmctl reload":
		w.reloads++
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected program %q", argv)
}

func wmKit(t *testing.T, content string) (*kit, *wmBackend, *fakeWM, string) {
	t.Helper()
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "lys-wm", "config.yaml")
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeWM{}
	w, err := openWM(k, fake.run)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	return k, w, fake, path
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWMRowsFollowTheSchemasTypes(t *testing.T) {
	k, w, _, _ := wmKit(t, "gaps: {inner: 3}\ninput: {touchpad: {tap: true}}\n")
	for path, want := range map[string]any{
		"gaps.inner":                  &number{},
		"tearing":                     &toggle{},
		"decoration.active_opacity":   &number{},
		"input.touchpad.tap":          &optionalEnum{},
		"input.touchpad.click_method": &optionalEnum{},
		"tabs.font":                   &text{},
	} {
		c := fieldControl(k, w, wmField(path))
		if fmt.Sprintf("%T", c) != fmt.Sprintf("%T", want) {
			t.Errorf("%s: %T, want %T", path, c, want)
		}
	}
	if got := w.value("gaps.inner"); got != int64(3) {
		t.Errorf("a key in the file: %#v", got)
	}
	if got := w.value("tearing"); got != false {
		t.Errorf("a key the file leaves out takes the schema's default: %#v", got)
	}
	if got := w.value("decoration.active_opacity"); got != float64(1) {
		t.Errorf("a float default: %#v", got)
	}
	if got := w.value("tabs.font"); got != nil {
		t.Errorf("no default and no key: %#v", got)
	}
}

func TestAWMRowNamingNoKeyIsAPageBug(t *testing.T) {
	k, w, _, _ := wmKit(t, "")
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	fieldControl(k, w, wmField("no.such.key"))
}

func TestAToggleWritesTheFileKeepingTheRestAndReloads(t *testing.T) {
	const before = "# mine\nunknown: 1\ntearing: false # why\n"
	k, w, fake, path := wmKit(t, before)
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if got, want := fileText(t, path), "# mine\nunknown: 1\ntearing: true # why\n"; got != want {
		t.Fatalf("%q", got)
	}
	if fake.reloads != 1 || fake.checks != 1 {
		t.Fatalf("%d reloads, %d checks", fake.reloads, fake.checks)
	}
	if got := w.value("tearing"); got != true {
		t.Fatalf("%v", got)
	}
	if w.notice != "" {
		t.Fatalf("a good change says %q", w.notice)
	}
}

func TestARefusedChangeShowsWhyAndTheRowGoesBack(t *testing.T) {
	const before = "gaps:\n  inner: 3\n"
	k, w, fake, path := wmKit(t, before)
	fake.reject = "gaps.inner: nope"
	row := newSettingRow(k, w, wmField("gaps.inner"), fieldControl(k, w, wmField("gaps.inner")))
	n := row.ctl.(*number)
	n.OnValueChanged(9)
	if got := fileText(t, path); got != before {
		t.Fatalf("%q", got)
	}
	if fake.reloads != 0 {
		t.Fatal("reloaded a refused change")
	}
	if got := w.value("gaps.inner"); got != int64(3) {
		t.Fatalf("the row holds a value the file does not: %v", got)
	}
	if !strings.Contains(w.notice, "gaps.inner: nope") || !strings.Contains(w.notice, path) || w.class != "error" {
		t.Fatalf("%q %q", w.notice, w.class)
	}
	fake.reject = ""
	n.OnValueChanged(4)
	if w.notice != "" {
		t.Fatalf("a later good change leaves %q", w.notice)
	}
	if got := fileText(t, path); got != "gaps:\n  inner: 4\n" {
		t.Fatalf("%q", got)
	}
}

func TestAHomeManagerLinkShowsOnThePageAndChangesNothing(t *testing.T) {
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	store := filepath.Join(t.TempDir(), "abc-home-manager-files", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("tearing: false\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "lys-wm", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, path); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWM{}
	w, err := openWM(k, fake.run)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	for _, want := range []string{path, store, "Home Manager"} {
		if !strings.Contains(w.notice, want) {
			t.Errorf("before any change, %q does not say %q", w.notice, want)
		}
	}
	if w.class != "warning" {
		t.Errorf("class %q", w.class)
	}
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if !strings.Contains(w.notice, "Home Manager") || w.class != "error" && w.class != "warning" {
		t.Fatalf("%q %q", w.notice, w.class)
	}
	if fileText(t, store) != "tearing: false\n" || fake.checks != 0 || fake.reloads != 0 {
		t.Fatal("touched a read-only config")
	}
	if got := w.value("tearing"); got != false {
		t.Fatalf("%v", got)
	}
	status := newWMStatus(k, w)
	if !status.Visible() {
		t.Fatal("the page does not show it")
	}
}

func TestResettingAKeyTheBuiltInConfigSetsRestoresIt(t *testing.T) {
	_, w, _, path := wmKit(t, "binds:\n  - {chord: SUPER + X, action: exit}\ntearing: true\n")
	if src := w.source("binds"); !src.reset || !src.badge {
		t.Fatalf("a changed key: %+v", src)
	}
	w.reset("binds")
	got := fileText(t, path)
	if !strings.Contains(got, "SUPER + Q") || strings.Contains(got, "SUPER + X") {
		t.Fatalf("%q", got)
	}
	if src := w.source("binds"); src.badge || src.reset {
		t.Fatalf("back at the default: %+v", src)
	}
}

func TestResettingAnyOtherKeyRemovesIt(t *testing.T) {
	_, w, _, path := wmKit(t, "binds: []\ngaps:\n  inner: 3\ntearing: true\n")
	if src := w.source("gaps.inner"); !src.reset {
		t.Fatalf("%+v", src)
	}
	w.reset("gaps.inner")
	if got := fileText(t, path); got != "binds: []\ntearing: true\n" {
		t.Fatalf("%q", got)
	}
	if got := w.value("gaps.inner"); got != int64(8) {
		t.Fatalf("%v", got)
	}
	if src := w.source("gaps.inner"); src.badge || src.reset {
		t.Fatalf("%+v", src)
	}
}

func TestANewFileKeepsTheBuiltInBinds(t *testing.T) {
	k, w, _, path := wmKit(t, "")
	if src := w.source("tearing"); src.badge || src.reset {
		t.Fatalf("no file: %+v", src)
	}
	binds, ok := w.value("binds").([]any)
	if !ok || len(binds) != 1 {
		t.Fatalf("no file reads the built-in binds: %#v", w.value("binds"))
	}
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	got := fileText(t, path)
	if !strings.Contains(got, "SUPER + Q") || !strings.Contains(got, "tearing: true") {
		t.Fatalf("%q", got)
	}
}

func TestAKeyAFileLeavesOutIsNotTheBuiltInConfigs(t *testing.T) {
	_, w, _, _ := wmKit(t, "tearing: true\n")
	if got := w.value("binds"); got != nil {
		t.Fatalf("a file without binds has none: %#v", got)
	}
}

func TestAnOptionalToggleWritesABoolAndUnsets(t *testing.T) {
	k, w, _, path := wmKit(t, "binds: []\n")
	c := fieldControl(k, w, wmField("input.touchpad.tap")).(*optionalEnum)
	if c.Selected() != 0 {
		t.Fatalf("unset shows %d", c.Selected())
	}
	c.OnSelect(1)
	if got := fileText(t, path); !strings.Contains(got, "tap: true") {
		t.Fatalf("%q", got)
	}
	if got := w.value("input.touchpad.tap"); got != true {
		t.Fatalf("%v", got)
	}
	c.refresh()
	if c.Selected() != 1 {
		t.Fatalf("on shows %d", c.Selected())
	}
	c.OnSelect(2)
	if got := fileText(t, path); !strings.Contains(got, "tap: false") {
		t.Fatalf("%q", got)
	}
	c.OnSelect(0)
	if got := fileText(t, path); strings.Contains(got, "tap") || strings.Contains(got, "touchpad") {
		t.Fatalf("%q", got)
	}
}

func TestAYAMLRowAppliesAWholeValue(t *testing.T) {
	k, w, _, path := wmKit(t, "# keep\nbinds:\n  - {chord: SUPER + Q, action: kill}\ntearing: true\n")
	c := fieldControl(k, w, wmYAML("binds", 4)).(*yamlEditor)
	if !strings.Contains(c.area.Text(), "SUPER + Q") {
		t.Fatalf("shows %q", c.area.Text())
	}
	c.area.SetText("binds:\n  - chord: SUPER + X\n    action: exit\n")
	c.apply()
	got := fileText(t, path)
	if !strings.Contains(got, "SUPER + X") || strings.Contains(got, "SUPER + Q") || !strings.HasPrefix(got, "# keep\n") || !strings.Contains(got, "tearing: true") {
		t.Fatalf("%q", got)
	}
}

func TestChangesMadeWhileOneIsOnItsWayLandAsOneLatest(t *testing.T) {
	k, w, fake, path := wmKit(t, "gaps:\n  inner: 1\n")
	var queued []func()
	k.background = func(fn func()) { queued = append(queued, fn) }
	k.invoke = func(fn func()) { fn() }
	if err := w.slot("gaps.inner").set(int64(2)); err != nil {
		t.Fatal(err)
	}
	for v := int64(3); v <= 9; v++ {
		if err := w.slot("gaps.inner").set(v); err != nil {
			t.Fatal(err)
		}
		if got := w.value("gaps.inner"); got != v {
			t.Fatalf("the row shows %v while %d is on its way", got, v)
		}
	}
	if len(queued) != 1 {
		t.Fatalf("%d writers at once", len(queued))
	}
	for len(queued) > 0 {
		fn := queued[0]
		queued = queued[1:]
		fn()
	}
	if got := fileText(t, path); got != "gaps:\n  inner: 9\n" {
		t.Fatalf("%q", got)
	}
	if fake.checks > 2 {
		t.Fatalf("%d checks for one drag", fake.checks)
	}
}

func TestAChangeBesideTheAppIsRead(t *testing.T) {
	k, w, _, path := wmKit(t, "gaps:\n  inner: 1\n")
	if err := os.WriteFile(path, []byte("gaps:\n  inner: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := k.wmBackend(); err != nil {
		t.Fatal(err)
	}
	if got := w.value("gaps.inner"); got != int64(7) {
		t.Fatalf("%v", got)
	}
}

func TestASchemaThatCannotBeReadFailsThePageNotTheApp(t *testing.T) {
	k := testKit(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := openWM(k, func(context.Context, ...string) ([]byte, error) { return nil, errors.New("lys-wm is too old") }); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("%v", err)
	}
}

func allWMRows() []rowSpec {
	var rows []rowSpec
	for _, s := range wmSections() {
		for _, p := range s.pages {
			for _, section := range p.sections {
				rows = append(rows, section.rows...)
			}
		}
	}
	return rows
}

func TestEveryWMRowAndPageHasItsMessagesInEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en-US", "fr"} {
		l, err := i18n.Select(locale.Assets(), []i18n.LangID{i18n.MustLangID(lang)})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range allWMRows() {
			if !l.Has(r.labelKey()) {
				t.Errorf("%s: no %s", lang, r.labelKey())
			}
		}
		for _, s := range wmSections() {
			if !l.Has(s.key) {
				t.Errorf("%s: no %s", lang, s.key)
			}
			for _, p := range s.pages {
				for _, key := range []string{p.navKey, p.header} {
					if !l.Has(key) {
						t.Errorf("%s: no %s", lang, key)
					}
				}
				if l.Attr(p.header, "breadcrumb") == "" {
					t.Errorf("%s: %s has no breadcrumb", lang, p.header)
				}
				for _, section := range p.sections {
					if !l.Has(section.title) {
						t.Errorf("%s: no %s", lang, section.title)
					}
				}
			}
		}
		for _, key := range []string{"wm-status-not-running", "enum-wm-toggle-on", "enum-wm-toggle-off"} {
			if !l.Has(key) {
				t.Errorf("%s: no %s", lang, key)
			}
		}
	}
}

func TestEveryWMPageIDIsUnique(t *testing.T) {
	seen := []string{}
	for _, s := range wmSections() {
		for _, p := range s.pages {
			if slices.Contains(seen, p.id) {
				t.Errorf("%s twice", p.id)
			}
			seen = append(seen, p.id)
		}
	}
}

func TestTheWMPagesAppearOnlyWithLysWMInstalled(t *testing.T) {
	k := testKit(t, "")
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	ids := func() []string {
		var out []string
		for _, p := range Pages(k.store.svc.Config()) {
			out = append(out, p.ID)
		}
		return out
	}
	if slices.Contains(ids(), "wm-windows") {
		t.Fatal("lys-wm pages without lys-wm")
	}
	if !slices.Contains(ids(), "bar-general") {
		t.Fatal("the shell's pages went too")
	}
	if err := os.WriteFile(filepath.Join(empty, "lys-wm"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(), "wm-windows") {
		t.Fatal("no lys-wm pages with lys-wm")
	}
}

func TestAWMPageBuildsItsRowsAndStatus(t *testing.T) {
	k, _, _, _ := wmKit(t, "")
	spec := wmPage("wm-test", "ld-monitor-symbolic", sectionSpec{"wm-section-gaps", []rowSpec{wmField("gaps.inner"), wmYAML("gaps.outer", 1)}})
	page := buildPage(k, spec)
	if len(page.rows) != 2 || page.status == nil {
		t.Fatalf("%d rows, status %v", len(page.rows), page.status)
	}
	if page.status.Visible() {
		t.Fatal("a status line with nothing to say")
	}
	page.refresh()
}

func TestAWMPageWhoseSchemaFailsSaysSoAndStillBuilds(t *testing.T) {
	k := testKit(t, "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	page := buildPage(k, wmPage("wm-test", "ld-monitor-symbolic"))
	if page.Scroll == nil || len(page.rows) != 0 {
		t.Fatalf("%+v", page)
	}
}
