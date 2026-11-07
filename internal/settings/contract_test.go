package settings

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

func TestEveryWMRowIsAKeyOfTheInstalledLysWM(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	f, err := wmconfig.Open(os.Getenv, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := f.Schema()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range allWMRows() {
		if _, ok := schema.Field(wmPath(r.path)); !ok {
			t.Errorf("lys-wm has no key %s", r.path)
		}
	}
}

func TestNoWMRowFallsBackToPlainText(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	k := testKit(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	if got := plainTextRows(t, k, w, allWMRows()); len(got) > 0 {
		t.Fatalf("lys-wm rows on the plain text editor: %v", got)
	}
}

func TestEveryWMRowBuildsWithLabelsAgainstTheInstalledLysWM(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	k := testKit(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	tr := locale.T()
	for _, r := range allWMRows() {
		treetest.Walk(fieldControl(k, w, r), func(x widget.Widget) {
			c, ok := x.(*cardList)
			if !ok {
				return
			}
			for _, f := range c.spec.fields {
				if !tr.Has(f.label) {
					t.Errorf("%s: no message %s", r.path, f.label)
				}
			}
		})
	}
}

// systemDefaulted are the lys-wm rows whose value, while unset, lys-wm
// leaves to the system or the device (libinput, xkb, the font and icon
// lookups, the GPU it finds), or which start as an empty list: they read
// "Inherited" or empty, every other row shows the value lys-wm uses.
var systemDefaulted = []string{
	"decoration.blur.tint", "tabs.font", "tabs.icon_theme", "gpu",
	"input.keyboard.layout", "input.keyboard.variant", "input.keyboard.options", "input.keyboard.model", "input.keyboard.rules",
	"input.pointer.natural_scroll", "input.pointer.middle_emulation", "input.pointer.accel_profile", "input.pointer.accel_speed", "input.pointer.scroll_factor",
	"input.touchpad.tap", "input.touchpad.drag_lock", "input.touchpad.natural_scroll", "input.touchpad.disable_while_typing", "input.touchpad.middle_emulation",
	"input.touchpad.click_method", "input.touchpad.scroll_method", "input.touchpad.accel_profile", "input.touchpad.accel_speed", "input.touchpad.scroll_factor",
	"gestures.hold", "outputs", "rules", "autostart",
}

func TestEveryWMRowShowsTheValueLysWMUsesUnlessTheSystemDecides(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	k := testKit(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range allWMRows() {
		inherited := w.value(r.path) == nil
		if want := slices.Contains(systemDefaulted, r.path); inherited != want {
			t.Errorf("%s: reads inherited %v, want %v", r.path, inherited, want)
		}
	}
}

func TestARefusedListItemNamesItsRuntimeLine(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f, err := wmconfig.Open(os.Getenv, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		edit wmconfig.Edit
		want string
	}{
		"bind":    {wmconfig.Edit{Path: []string{"binds"}, Value: []any{map[string]any{"chord": "SUPER + Q"}}}, ":2: binds[0].action: is missing"},
		"output":  {wmconfig.Edit{Path: []string{"outputs"}, Value: []any{map[string]any{"scale": 2}}}, ":2: outputs[0].match: is missing"},
		"gesture": {wmconfig.Edit{Path: []string{"gestures", "hold"}, Value: []any{map[string]any{"action": "kill"}}}, ":3: gestures.hold[0].fingers: is missing"},
	}
	for name, c := range cases {
		var rejected *wmconfig.RejectedError
		if err := f.Check([]wmconfig.Edit{c.edit}); !errors.As(err, &rejected) || !strings.Contains(rejected.Reason, f.Runtime()+c.want) {
			t.Errorf("%s: %v; want a refusal naming %s%s", name, err, f.Runtime(), c.want)
		}
	}
}
