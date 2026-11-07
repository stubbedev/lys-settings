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

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const wmSchema = `{"type":"object","properties":{
"gaps":{"type":"object","properties":{"inner":{"type":"integer","minimum":0,"default":8},"outer":{"oneOf":[{"type":"integer"},{"type":"array"}]}}},
"tearing":{"type":"boolean","default":false},
"binds":{"type":"array","items":{"type":"object","properties":{"action":{"type":"string","format":"lys-wm-action","x-lys-wm-actions":[{"name":"kill","summary":"Close"},{"name":"exec","args":"<command>","summary":"Run"},{"name":"workspace","args":"<n>","summary":"Show"}]},"chord":{"type":"string","format":"lys-wm-chord"},"flags":{"type":"array","items":{"type":"string","enum":["locked","repeating"]}}},"additionalProperties":false}},
"submaps":{"type":"object","additionalProperties":{"type":"array","items":{"type":"object","properties":{"action":{"type":"string","format":"lys-wm-action","x-lys-wm-actions":[{"name":"kill","summary":"Close"}]},"chord":{"type":"string","format":"lys-wm-chord"}},"additionalProperties":false}}},
"rules":{"type":"array","items":{"type":"object","properties":{"app_id":{"type":"string"},"float":{"type":"boolean"},"look":{"type":"object","properties":{"grain":{"type":"number"}}}}}},
"borders":{"type":"object","properties":{"active":{"default":"accent","oneOf":[{"type":"string","format":"lys-wm-color"},{"type":"object","properties":{"angle":{"type":"number"},"colors":{"type":"array","minItems":1,"maxItems":3,"items":{"type":"string"}}}}]}}},
"decoration":{"type":"object","properties":{"active_opacity":{"type":"number","minimum":0,"maximum":1,"default":1},"shadow":{"type":"object","properties":{"color":{"type":"string","format":"lys-wm-color","default":"#00000060"}}}}},
"input":{"type":"object","properties":{"touchpad":{"type":"object","properties":{"tap":{"type":"boolean"},"click_method":{"type":"string","enum":["none","clickfinger"]}}},
"keyboard":{"type":"object","properties":{"layout":{"type":"string","format":"lys-wm-xkb-layout"},"variant":{"type":"string","format":"lys-wm-xkb-variant"},"options":{"type":"string","format":"lys-wm-xkb-options"},"model":{"type":"string","format":"lys-wm-xkb-model"},"rules":{"type":"string","format":"lys-wm-xkb-rules"}}}}},
"gpu":{"type":"string","format":"lys-wm-drm-device"},
"palette":{"type":"object","additionalProperties":{"type":"string"},"default":{"red":"#f38ba8","blue":"#89b4fa"}},
"tabs":{"type":"object","properties":{"font":{"type":"string","format":"lys-wm-font-file"},"icon_theme":{"type":"string","format":"lys-wm-icon-theme"},"label":{"type":"string"}}}}}`

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
		candidate := argv[slices.Index(argv, "--runtime")+1]
		if w.reject != "" {
			return nil, &wmconfig.ToolError{Argv: argv, Exit: 1, Stderr: candidate + ":3: " + w.reject, Err: errors.New("exit status 1")}
		}
		return nil, nil
	case "lys-wmctl reload":
		w.reloads++
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected program %q", argv)
}

type wmFiles struct{ config, runtime string }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wmKitWith(t *testing.T, config, runtime string) (*kit, *wmBackend, *fakeWM, wmFiles) {
	t.Helper()
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	files := wmFiles{filepath.Join(dir, "lys-wm", "config.yaml"), filepath.Join(dir, "lys-wm", "runtime.yaml")}
	if config != "" {
		writeFile(t, files.config, config)
	}
	if runtime != "" {
		writeFile(t, files.runtime, runtime)
	}
	fake := &fakeWM{}
	w, err := openWM(k, fake.run)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	return k, w, fake, files
}

func wmKit(t *testing.T, config string) (*kit, *wmBackend, *fakeWM, wmFiles) {
	t.Helper()
	return wmKitWith(t, config, "")
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func noFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestWMRowsFollowTheSchemasTypes(t *testing.T) {
	k, w, _, _ := wmKit(t, "gaps: {inner: 3}\ninput: {touchpad: {tap: true}}\n")
	for path, want := range map[string]any{
		"gaps.inner":                  &number{},
		"tearing":                     &toggle{},
		"decoration.active_opacity":   &slider{},
		"input.touchpad.tap":          &optionalEnum{},
		"input.touchpad.click_method": &optionalEnum{},
		"tabs.font":                   &choiceEditor{},
		"tabs.icon_theme":             &choiceEditor{},
		"tabs.label":                  &text{},
		"gpu":                         &choiceEditor{},
		"input.keyboard.layout":       &choiceEditor{},
		"decoration.shadow.color":     &wmColorEditor{},
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

func TestAToggleWritesAnOverrideLeavingTheConfigAndReloads(t *testing.T) {
	const declared = "# mine\nunknown: 1\ntearing: false # why\n"
	k, w, fake, files := wmKit(t, declared)
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if got := fileText(t, files.runtime); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	if got := fileText(t, files.config); got != declared {
		t.Fatalf("the config changed: %q", got)
	}
	if fake.reloads != 1 || fake.checks != 1 {
		t.Fatalf("%d reloads, %d checks", fake.reloads, fake.checks)
	}
	if got := w.value("tearing"); got != true {
		t.Fatalf("%v", got)
	}
	if src := w.source("tearing"); src.label != locale.T().Get("settings-source-override") || !src.reset {
		t.Fatalf("an override of a config key: %+v", src)
	}
	if w.notice != "" {
		t.Fatalf("a good change says %q", w.notice)
	}
}

func TestARefusedChangeShowsWhyAndTheRowGoesBack(t *testing.T) {
	const before = "gaps:\n  inner: 3\n"
	k, w, fake, files := wmKitWith(t, "", before)
	fake.reject = "gaps.inner: nope"
	row := newFieldRow(k, w, wmField("gaps.inner"))
	n := row.ctl.(*number)
	n.OnValueChanged(9)
	if got := fileText(t, files.runtime); got != before {
		t.Fatalf("%q", got)
	}
	if fake.reloads != 0 {
		t.Fatal("reloaded a refused change")
	}
	if got := w.value("gaps.inner"); got != int64(3) {
		t.Fatalf("the row holds a value the file does not: %v", got)
	}
	if !strings.Contains(w.notice, "gaps.inner: nope") || !strings.Contains(w.notice, files.runtime) || w.class != "error" {
		t.Fatalf("%q %q", w.notice, w.class)
	}
	fake.reject = ""
	n.OnValueChanged(4)
	if w.notice != "" {
		t.Fatalf("a later good change leaves %q", w.notice)
	}
	if got := fileText(t, files.runtime); got != "gaps:\n  inner: 4\n" {
		t.Fatalf("%q", got)
	}
}

func TestAHomeManagerConfigTakesChangesAsOverrides(t *testing.T) {
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
	if w.notice != "" {
		t.Fatalf("a read-only config warns: %q", w.notice)
	}
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if w.notice != "" || fake.checks != 1 || fake.reloads != 1 {
		t.Fatalf("%q, %d checks, %d reloads", w.notice, fake.checks, fake.reloads)
	}
	if got := fileText(t, filepath.Join(dir, "lys-wm", "runtime.yaml")); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	if fileText(t, store) != "tearing: false\n" {
		t.Fatal("touched a read-only config")
	}
	if target, err := os.Readlink(path); err != nil || target != store {
		t.Fatalf("the link moved: %q, %v", target, err)
	}
	if got := w.value("tearing"); got != true {
		t.Fatalf("%v", got)
	}
	w.reset("tearing")
	if got := w.value("tearing"); got != false {
		t.Fatalf("a reset falls back to the declared config: %v", got)
	}
}

func TestAReadOnlyOverrideFileShowsOnThePage(t *testing.T) {
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	runtime := filepath.Join(dir, "lys-wm", "runtime.yaml")
	writeFile(t, runtime, "tearing: false\n")
	if err := os.Chmod(runtime, 0o444); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWM{}
	w, err := openWM(k, fake.run)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	if !strings.Contains(w.notice, runtime) || w.class != "warning" {
		t.Fatalf("%q %q", w.notice, w.class)
	}
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if !strings.Contains(w.notice, "read-only") || fake.checks != 0 || fake.reloads != 0 {
		t.Fatalf("%q, %d checks, %d reloads", w.notice, fake.checks, fake.reloads)
	}
	if got := w.value("tearing"); got != false {
		t.Fatalf("%v", got)
	}
	if !newWMStatus(k, w).Visible() {
		t.Fatal("the page does not show it")
	}
}

func TestResetPutsAKeyBackToItsDefaultOverTheConfig(t *testing.T) {
	_, w, _, files := wmKitWith(t, "gaps:\n  inner: 3\n", "gaps:\n  inner: 5\ntearing: true\n")
	if got := w.value("gaps.inner"); got != int64(5) {
		t.Fatalf("%v", got)
	}
	if src := w.source("gaps.inner"); !src.reset || src.label != locale.T().Get("settings-source-override") {
		t.Fatalf("%+v", src)
	}
	w.reset("gaps.inner")
	if got := fileText(t, files.runtime); !strings.Contains(got, "inner: 8") || !strings.Contains(got, "tearing: true") {
		t.Fatalf("the default must cover the config's 3: %q", got)
	}
	if got := w.value("gaps.inner"); got != int64(8) {
		t.Fatalf("%v", got)
	}
	if src := w.source("gaps.inner"); src.reset {
		t.Fatalf("a key at its default offers a reset: %+v", src)
	}
	if src := w.source("tearing"); src.label != locale.T().Get("settings-source-custom") || !src.reset {
		t.Fatalf("an override of no config key: %+v", src)
	}
	w.reset("tearing")
	if got := fileText(t, files.runtime); strings.Contains(got, "tearing") {
		t.Fatalf("a key the config leaves out resets by dropping its override: %q", got)
	}
}

func TestResettingAnOverrideWithoutAConfigRestoresTheBuiltIn(t *testing.T) {
	_, w, _, files := wmKitWith(t, "", "binds:\n  - {chord: SUPER + X, action: exit}\n")
	if src := w.source("binds"); !src.reset || !src.badge {
		t.Fatalf("an override: %+v", src)
	}
	w.reset("binds")
	if got := fileText(t, files.runtime); strings.Contains(got, "binds") {
		t.Fatalf("%q", got)
	}
	binds, ok := w.value("binds").([]any)
	if !ok || len(binds) != 1 || !strings.Contains(fmt.Sprint(binds), "SUPER + Q") {
		t.Fatalf("%#v", w.value("binds"))
	}
	if src := w.source("binds"); src.badge || src.reset {
		t.Fatalf("back at the default: %+v", src)
	}
	noFile(t, files.config)
}

func TestAConfigValueOffTheDefaultResetsThroughAnOverride(t *testing.T) {
	_, w, _, files := wmKit(t, "gaps:\n  inner: 3\n")
	if src := w.source("gaps.inner"); !src.reset {
		t.Fatalf("a config value off the default offers no reset: %+v", src)
	}
	w.reset("gaps.inner")
	if got := fileText(t, files.runtime); !strings.Contains(got, "inner: 8") {
		t.Fatalf("%q", got)
	}
	if got := w.value("gaps.inner"); got != int64(8) {
		t.Fatalf("%v", got)
	}
}

func TestAKeyAtItsDefaultOffersNoReset(t *testing.T) {
	_, w, fake, files := wmKit(t, "gaps:\n  inner: 8\n")
	if src := w.source("gaps.inner"); src.reset {
		t.Fatalf("%+v", src)
	}
	if src := w.source("tearing"); src.reset {
		t.Fatalf("an unset key with a default: %+v", src)
	}
	if fake.checks != 0 {
		t.Fatalf("%d checks", fake.checks)
	}
	noFile(t, files.runtime)
}

func TestANullOverrideClearsTheMappingBelowIt(t *testing.T) {
	_, w, _, _ := wmKitWith(t, "gaps:\n  inner: 3\n", "gaps:\n")
	if got := w.value("gaps.inner"); got != int64(8) {
		t.Fatalf("%v", got)
	}
	if src := w.source("gaps.inner"); src.reset || !src.badge {
		t.Fatalf("a null override leaves the key at its default, overridden: %+v", src)
	}
}

func TestWithoutAConfigAChangeWritesOnlyThatKeyAndKeepsTheBuiltInBinds(t *testing.T) {
	k, w, _, files := wmKit(t, "")
	if src := w.source("tearing"); src.badge || src.reset {
		t.Fatalf("no file: %+v", src)
	}
	binds, ok := w.value("binds").([]any)
	if !ok || len(binds) != 1 {
		t.Fatalf("no file reads the built-in binds: %#v", w.value("binds"))
	}
	c := fieldControl(k, w, wmField("tearing")).(*toggle)
	c.OnChanged(true)
	if got := fileText(t, files.runtime); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
	noFile(t, files.config)
	if binds, ok := w.value("binds").([]any); !ok || len(binds) != 1 {
		t.Fatalf("an override lost the built-in binds: %#v", w.value("binds"))
	}
}

func TestAKeyAFileLeavesOutIsNotTheBuiltInConfigs(t *testing.T) {
	_, w, _, _ := wmKit(t, "tearing: true\n")
	if got := w.value("binds"); got != nil {
		t.Fatalf("a file without binds has none: %#v", got)
	}
}

func TestAnOptionalToggleWritesABoolAndUnsets(t *testing.T) {
	k, w, _, files := wmKit(t, "binds: []\n")
	c := fieldControl(k, w, wmField("input.touchpad.tap")).(*optionalEnum)
	if c.Selected() != 0 {
		t.Fatalf("unset shows %d", c.Selected())
	}
	c.OnSelect(1)
	if got := fileText(t, files.runtime); !strings.Contains(got, "tap: true") {
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
	if got := fileText(t, files.runtime); !strings.Contains(got, "tap: false") {
		t.Fatalf("%q", got)
	}
	c.OnSelect(0)
	if got := fileText(t, files.runtime); strings.Contains(got, "tap") || strings.Contains(got, "touchpad") {
		t.Fatalf("%q", got)
	}
}

func TestBindsAreCardsWithAFilter(t *testing.T) {
	const declared = "# keep\nbinds:\n  - {chord: SUPER + Q, action: kill}\n  - {chord: SUPER + F, action: fullscreen, flags: [locked]}\ntearing: true\n"
	k, w, _, files := wmKit(t, declared)
	c := fieldControl(k, w, wmObjects("binds", "chord", "action")).(*cardList)
	if len(c.cards) != 2 || c.cards[1].title.Text() != "SUPER + F · fullscreen" {
		t.Fatalf("%d cards, the second titled %q", len(c.cards), c.cards[1].title.Text())
	}
	c.filter.SetText("full")
	if c.cards[0].root.Visible() || !c.cards[1].root.Visible() {
		t.Fatal("the filter shows the kill bind or hides the fullscreen one")
	}
	c.filter.SetText("locked super")
	if c.cards[0].root.Visible() || !c.cards[1].root.Visible() {
		t.Fatal("every word must match some value, a flag included")
	}
	c.filter.SetText("")
	if !c.cards[0].root.Visible() {
		t.Fatal("an empty filter hides cards")
	}
	if err := c.fieldSlot(0, "action").set("exit"); err != nil {
		t.Fatal(err)
	}
	got := fileText(t, files.runtime)
	if !strings.Contains(got, "exit") || !strings.Contains(got, "fullscreen") || strings.Contains(got, "tearing") {
		t.Fatalf("%q", got)
	}
	if fileText(t, files.config) != declared {
		t.Fatal("the config changed")
	}
}

func TestARulesNestedObjectAndToggleWriteInsideTheItem(t *testing.T) {
	k, w, _, files := wmKit(t, "rules:\n  - {app_id: foot}\n")
	c := fieldControl(k, w, wmObjects("rules", "app_id")).(*cardList)
	if len(c.spec.fields) != 3 {
		t.Fatalf("%d fields, want app_id, float and look.grain", len(c.spec.fields))
	}
	if len(c.cards[0].controls) != 0 || c.cards[0].root.Children()[2].(*widget.Box).Visible() {
		t.Fatal("a collapsed card built or shows its fields")
	}
	disclose := treetest.WithClass(c.cards[0].root, "card-disclose")[0].(*widget.Button)
	disclose.ClickAt(widget.Point{})
	if len(c.cards[0].controls) != 3 || !c.cards[0].root.Children()[2].(*widget.Box).Visible() {
		t.Fatalf("opening the card built %d controls", len(c.cards[0].controls))
	}
	if _, ok := c.cards[0].controls[1].(*optionalEnum); !ok {
		t.Fatalf("a rule's float takes %T, want the inherit-or-pick select", c.cards[0].controls[1])
	}
	grain := nestedSlot(c.fieldSlot(0, "look"), []string{"grain"})
	if err := grain.set(0.5); err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, files.runtime); !strings.Contains(got, "grain: 0.5") || !strings.Contains(got, "app_id: foot") {
		t.Fatalf("%q", got)
	}
	grain.unset()
	if _, ok := c.local[0]["look"]; ok {
		t.Error("dropping the last nested key left an empty object")
	}
	disclose.ClickAt(widget.Point{})
	if c.cards[0].root.Children()[2].(*widget.Box).Visible() || len(c.cards[0].controls) != 3 {
		t.Error("closing the card left its fields shown, or rebuilt them")
	}
}

func TestSubmapsAreNamedCardsOfBinds(t *testing.T) {
	k, w, _, files := wmKit(t, "submaps:\n  resize:\n    - {chord: Left, action: resizeactive -20 0}\n  apps:\n    - {chord: F, action: exec firefox}\n")
	c := fieldControl(k, w, wmSubmaps()).(*cardList)
	if len(c.local) != 2 || c.local[0]["name"] != "apps" || c.local[1]["name"] != "resize" {
		t.Fatalf("submaps %v, want apps then resize", c.local)
	}
	if err := c.fieldSlot(0, "name").set("launch"); err != nil {
		t.Fatal(err)
	}
	got := fileText(t, files.runtime)
	if !strings.Contains(got, "launch:") || strings.Contains(got, "apps:") || !strings.Contains(got, "resize:") {
		t.Fatalf("%q", got)
	}
}

func TestOuterGapsWriteOneNumberOrFour(t *testing.T) {
	k, w, _, files := wmKit(t, "")
	c := fieldControl(k, w, wmSides("gaps.outer")).(*sidesEditor)
	for _, s := range c.spins {
		if !s.Empty() {
			t.Fatal("an unset gap shows a side")
		}
	}
	c.spins[1].KeyAction(widget.KeyUp, 0)
	if got := fileText(t, files.runtime); !strings.Contains(got, "outer: 1") || c.spins[3].Empty() {
		t.Fatalf("setting one side of an inherited gap sets all: %q", got)
	}
	c.spins[0].KeyAction(widget.KeyUp, 0)
	if got := fileText(t, files.runtime); !strings.Contains(got, "- 2") {
		t.Fatalf("one side changed: %q", got)
	}
	for _, s := range c.spins {
		s.SetValue(6)
	}
	c.write(0)
	if got := fileText(t, files.runtime); !strings.Contains(got, "outer: 6") {
		t.Fatalf("equal sides: %q", got)
	}
	emptySpin(c.spins[2])
	c.refresh()
	if got := fileText(t, files.runtime); strings.Contains(got, "outer") || !c.spins[0].Empty() {
		t.Fatalf("inheriting kept %q", got)
	}
}

func TestABorderIsAColourOrAGradient(t *testing.T) {
	k, w, _, files := wmKit(t, "")
	c := fieldControl(k, w, wmBorder("borders.active")).(*borderEditor)
	if err := c.write([]any{"blue"}, 0); err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, files.runtime); !strings.Contains(got, "active: blue") {
		t.Fatalf("one colour: %q", got)
	}
	c.refresh()
	if c.angled.Visible() {
		t.Error("one colour shows a gradient's angle")
	}
	if err := c.write([]any{"mauve", "blue"}, 45); err != nil {
		t.Fatal(err)
	}
	c.refresh()
	if got := fileText(t, files.runtime); !strings.Contains(got, "angle: 45") || !strings.Contains(got, "mauve") || !c.angled.Visible() {
		t.Fatalf("a gradient: %q", got)
	}
	if colors, angle := c.parts(); len(colors) != 2 || angle != 45 {
		t.Errorf("parts %v %v", colors, angle)
	}
	if err := c.write(nil, 45); err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, files.runtime); strings.Contains(got, "active") {
		t.Fatalf("no colour kept %q", got)
	}
}

func TestChangesMadeWhileOneIsOnItsWayLandAsOneLatest(t *testing.T) {
	k, w, fake, files := wmKitWith(t, "", "gaps:\n  inner: 1\n")
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
	if got := fileText(t, files.runtime); got != "gaps:\n  inner: 9\n" {
		t.Fatalf("%q", got)
	}
	if fake.checks > 2 {
		t.Fatalf("%d checks for one drag", fake.checks)
	}
}

func TestAChangeBesideTheAppIsRead(t *testing.T) {
	k, w, _, files := wmKit(t, "gaps:\n  inner: 1\n")
	writeFile(t, files.config, "gaps:\n  inner: 7\n")
	if _, err := k.wmBackend(); err != nil {
		t.Fatal(err)
	}
	if got := w.value("gaps.inner"); got != int64(7) {
		t.Fatalf("%v", got)
	}
	writeFile(t, files.runtime, "gaps:\n  inner: 2\n")
	if _, err := k.wmBackend(); err != nil {
		t.Fatal(err)
	}
	if got := w.value("gaps.inner"); got != int64(2) {
		t.Fatalf("an override beside the app: %v", got)
	}
}

func TestResetAllDropsTheWMOverridesToo(t *testing.T) {
	k, w, fake, files := wmKitWith(t, "gaps:\n  inner: 3\n", "gaps:\n  inner: 5\ntearing: true\n")
	if err := k.store.set("osd.enabled", false); err != nil {
		t.Fatal(err)
	}
	cfg := resetAllDialog(k)
	cfg.OnResponse(responseCancel)
	if fake.checks != 0 || w.value("gaps.inner") != int64(5) {
		t.Fatal("cancelling reset the lys-wm overrides")
	}
	cfg.OnResponse(responseConfirm)
	if got := fileText(t, files.runtime); strings.TrimSpace(got) != "" {
		t.Fatalf("%q", got)
	}
	if got := w.value("gaps.inner"); got != int64(3) {
		t.Fatalf("%v", got)
	}
	if fake.reloads != 1 {
		t.Fatalf("%d reloads", fake.reloads)
	}
}

func TestResetAllReachesTheWMOverridesBeforeAnyWMPageOpened(t *testing.T) {
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	bin := filepath.Join(dir, "bin")
	log := filepath.Join(dir, "calls")
	for _, program := range []string{"lys-wm", "lys-wmctl"} {
		writeFile(t, filepath.Join(bin, program), "#!/bin/sh\necho \"$0 $*\" >> "+log+"\n")
		if err := os.Chmod(filepath.Join(bin, program), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runtime := filepath.Join(dir, "config", "lys-wm", "runtime.yaml")
	writeFile(t, runtime, "tearing: true\n")
	k.resetAll()
	if got := fileText(t, runtime); strings.TrimSpace(got) != "" {
		t.Fatalf("%q", got)
	}
	calls := fileText(t, log)
	if !strings.Contains(calls, "lys-wm --check-config --runtime") || !strings.Contains(calls, "lys-wmctl reload") {
		t.Fatalf("%q", calls)
	}
}

func TestResetAllWithoutLysWMTouchesNoWMFile(t *testing.T) {
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PATH", t.TempDir())
	runtime := filepath.Join(dir, "lys-wm", "runtime.yaml")
	writeFile(t, runtime, "tearing: true\n")
	k.resetAll()
	if got := fileText(t, runtime); got != "tearing: true\n" {
		t.Fatalf("%q", got)
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
	for _, section := range loginItemsPage(config.Defaults()).sections {
		rows = append(rows, section.rows...)
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
	spec := wmPage("wm-test", "ld-monitor-symbolic", sectionSpec{"wm-section-gaps", []rowSpec{wmField("gaps.inner"), wmSides("gaps.outer")}})
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

func TestTheKeyboardLayoutsAreToggledInOrderAndTheVariantsFollowThem(t *testing.T) {
	k, w, _, files := wmKit(t, "input:\n  keyboard:\n    layout: us\n")
	layout := fieldControl(k, w, wmField("input.keyboard.layout")).(*choiceEditor)
	p := openChoices(t, k, layout)
	pickName(t, p, "us,dk")
	if got := fileText(t, files.runtime); got != "input:\n  keyboard:\n    layout: us,dk\n" {
		t.Fatalf("%q", got)
	}
	variant := fieldControl(k, w, wmField("input.keyboard.variant")).(*choiceEditor)
	p = openChoices(t, k, variant)
	if !slices.Equal(p.all, []string{"intl", ",nodeadkeys"}) {
		t.Fatalf("offered %q", p.all)
	}
	pickName(t, p, ",nodeadkeys")
	if got := w.value("input.keyboard.variant"); got != ",nodeadkeys" {
		t.Fatalf("%v", got)
	}
	if variant.unknown.Visible() {
		t.Fatal("a listed variant is unknown")
	}
	if err := w.slot("input.keyboard.variant").set("nodeadkeys"); err != nil {
		t.Fatal(err)
	}
	variant.refresh()
	if !variant.unknown.Visible() {
		t.Fatal("a variant of another layout is not marked")
	}
	options := fieldControl(k, w, wmField("input.keyboard.options")).(*choiceEditor)
	p = openChoices(t, k, options)
	pickName(t, p, "caps:escape")
	if got := w.value("input.keyboard.options"); got != "caps:escape" {
		t.Fatalf("%v", got)
	}
}

func TestTheTabFontIsAFontFileListedOrBrowsed(t *testing.T) {
	k, w, _, _ := wmKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	c := fieldControl(k, w, wmField("tabs.font")).(*choiceEditor)
	c.open.ClickAt(widget.Point{})
	if !slices.Contains(c.picker.all, "/fonts/Inter.ttf") {
		t.Fatalf("offered %q", c.picker.all)
	}
	c.browse.ClickAt(widget.Point{})
	if f.fileFn == nil {
		t.Fatal("no file chooser")
	}
	f.fileFn("/home/me/.fonts/Mine.ttf")
	if got := w.value("tabs.font"); got != "/home/me/.fonts/Mine.ttf" {
		t.Fatalf("%v", got)
	}
	if !c.unknown.Visible() {
		t.Fatal("a browsed file outside the list is not marked")
	}
}

func TestAWMColorTakesAHexOrAPaletteName(t *testing.T) {
	k, w, _, _ := wmKit(t, "palette:\n  accent: '#ff0000'\n")
	c := fieldControl(k, w, wmField("decoration.shadow.color")).(*wmColorEditor)
	c.swatch.OnColorSet(render.RGBA(0, 255, 0, 255))
	if got := w.value("decoration.shadow.color"); got != "#00ff00" {
		t.Fatalf("%v", got)
	}
	p := openChoices(t, k, c.names)
	if !slices.Equal(p.all, []string{"blue", "red", "accent"}) {
		t.Fatalf("offered %q", p.all)
	}
	pickName(t, p, "accent")
	if got := w.value("decoration.shadow.color"); got != "accent" {
		t.Fatalf("%v", got)
	}
	c.refresh()
	if c.names.unknown.Visible() {
		t.Fatal("a palette name lys-wm builds in would be marked unknown")
	}
}

func TestAnUnlistedDRMDeviceIsKeptAndMarked(t *testing.T) {
	k, w, _, _ := wmKit(t, "gpu: card7\n")
	c := fieldControl(k, w, wmField("gpu")).(*choiceEditor)
	if c.entry.Text() != "card7" || !c.unknown.Visible() {
		t.Fatalf("%q %v", c.entry.Text(), c.unknown.Visible())
	}
	p := openChoices(t, k, c)
	pickName(t, p, "card0")
	if got := w.value("gpu"); got != "card0" || c.unknown.Visible() {
		t.Fatalf("%v %v", got, c.unknown.Visible())
	}
}
