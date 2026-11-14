package settings

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

// maxSweepActions bounds one phase of a row's sweep, far above the
// busiest row's controls, so a phase that never ends fails.
const maxSweepActions = 5000

// sweepPhase is which of a row's controls a sweep step drives.
type sweepPhase int

const (
	phaseAdd sweepPhase = iota
	phaseEdit
	phaseRemove
	phaseReset
)

type sweepAction struct {
	phase sweepPhase
	name  string
	do    func()
	drove string
}

func shownAndEnabled(w widget.Widget) bool {
	v, ok := w.(interface{ Visible() bool })
	return ok && v.Visible() && widget.IsEnabled(w)
}

func buttonPhase(b *widget.Button) sweepPhase {
	classes := b.Classes()
	switch {
	case slices.Contains(classes, "list-control-add"):
		return phaseAdd
	case slices.Contains(classes, "setting-reset"):
		return phaseReset
	case b.TooltipText() == locale.T().Get("settings-list-remove"):
		return phaseRemove
	}
	return phaseEdit
}

// sweepActions is everything a user can do to the row's controls
// without typing, in tree order: every button, switch, check, segment,
// dropdown row, spin step and clear, slider end and colour pick. With
// driven, a dropdown goes through only its first and last row where its
// rows were driven before or an earlier dropdown in the tree shows the same
// rows; the action picking the last of a full run names the rows it drove.
func sweepActions(root widget.Widget, driven map[string]bool) []sweepAction {
	var out []sweepAction
	edit := func(name string, do func()) { out = append(out, sweepAction{phase: phaseEdit, name: name, do: do}) }
	inTree := map[string]bool{}
	var walk func(w widget.Widget)
	walk = func(w widget.Widget) {
		if !shownAndEnabled(w) {
			return
		}
		if s, ok := w.(*slider); ok {
			for _, end := range []struct {
				name string
				past float64
			}{{"end", math.MaxFloat64}, {"start", -math.MaxFloat64}} {
				edit("slider to its "+end.name, func() {
					s.Set(end.past)
					s.OnCommit(s.Knob.Value())
				})
			}
			return
		}
		switch x := embedded(w).(type) {
		case *widget.ColorButton:
			edit("pick a colour", func() { x.Choose(render.RGBA(0x33, 0x66, 0x99, 0xff)) })
			return
		case *widget.Button:
			if x.OnClick != nil {
				out = append(out, sweepAction{phase: buttonPhase(x), name: fmt.Sprintf("click %q %v", x.TooltipText(), x.Classes()), do: x.OnClick})
			}
		case *widget.Switch:
			edit("toggle", x.Toggle)
		case *widget.CheckButton:
			edit("check", x.Toggle)
		case *widget.ToggleButton:
			edit("toggle the button", func() { x.SetActive(!x.Active()) })
		case *widget.ToggleGroup:
			for i := range x.Len() {
				edit(fmt.Sprintf("segment %d", i), func() { x.SetActive(i) })
			}
		case *widget.Dropdown:
			items := x.Items()
			key := strings.Join(items, "\x00")
			full := driven == nil || !driven[key] && !inTree[key]
			inTree[key] = true
			for i := range items {
				if !full && i != 0 && i != len(items)-1 {
					continue
				}
				a := sweepAction{phase: phaseEdit, name: fmt.Sprintf("dropdown row %d (%s)", i, items[i]), do: func() {
					x.SetSelected(i)
					if x.OnSelect != nil {
						x.OnSelect(i)
					}
				}}
				if full && i == len(items)-1 {
					a.drove = key
				}
				out = append(out, a)
			}
			return
		case *widget.SpinButton:
			edit("spin up", func() { x.KeyAction(widget.KeyUp, 0) })
			edit("spin down", func() { x.KeyAction(widget.KeyDown, 0) })
			for _, end := range []struct {
				name string
				at   func(lo, hi float64) float64
			}{{"least", func(lo, _ float64) float64 { return lo }}, {"most", func(_, hi float64) float64 { return hi }}} {
				edit("spin to its "+end.name, func() {
					x.SetValue(end.at(x.Range()))
					if x.OnValueChanged != nil {
						x.OnValueChanged(x.Value())
					}
				})
			}
			if x.OnCleared != nil {
				edit("clear the spin", func() { x.Clear(); x.OnCleared() })
			}
		case *widget.Slider:
			edit("slider to its end", func() { x.KeyAction(widget.KeyEnd, 0) })
			edit("slider to its start", func() { x.KeyAction(widget.KeyHome, 0) })
		}
		if p, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, c := range p.Children() {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// sweepRow drives the row through its phases: each add there is at the
// start, then every edit, then every remove, then the reset; after runs
// after each step. Steps are taken by position, re-read after each, since
// a step may rebuild the controls. The dropdown rows it drives all of join
// driven once the row is done; without driven, every dropdown is driven
// through every row.
func sweepRow(t *testing.T, row *settingRow, driven map[string]bool, after func(action string)) {
	t.Helper()
	met := map[string]bool{}
	if driven != nil {
		defer maps.Copy(driven, met)
	}
	popovers := &fakePickers{}
	row.kit.pickers = popovers
	step := func(a sweepAction) {
		popovers.content = nil
		a.do()
		if a.drove != "" {
			met[a.drove] = true
		}
		row.refresh()
		after(a.name)
		if popovers.content == nil {
			return
		}
		for _, in := range sweepActions(popovers.content, driven) {
			if in.phase == phaseEdit {
				in.do()
				row.refresh()
				after(a.name + ", then " + in.name + " in what it opened")
				return
			}
		}
	}
	of := func(phase sweepPhase) []sweepAction {
		var out []sweepAction
		for _, a := range sweepActions(row, driven) {
			if a.phase == phase {
				out = append(out, a)
			}
		}
		return out
	}
	for i := range len(of(phaseAdd)) {
		if actions := of(phaseAdd); i < len(actions) {
			step(actions[i])
		}
	}
	for i := range maxSweepActions + 1 {
		actions := of(phaseEdit)
		if i >= len(actions) {
			break
		}
		if i == maxSweepActions {
			t.Fatalf("%s: more than %d steps", row.path, maxSweepActions)
		}
		step(actions[i])
	}
	for range maxSweepActions {
		removes := of(phaseRemove)
		if len(removes) == 0 {
			break
		}
		step(removes[0])
		if len(of(phaseRemove)) >= len(removes) {
			break
		}
	}
	for _, a := range of(phaseReset) {
		step(a)
	}
}

// Every lys-wm control, driven through every add, edit, remove and reset,
// writes only what lys-wm's own --check-config takes: each write goes
// through it before it lands, and a refusal fails the test.
func TestEveryWMControlWritesOnlyWhatLysWMAccepts(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeFile(t, filepath.Join(dir, "lys-wm", "config.yaml"), sweepWMConfig)
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	for _, spec := range allWMRows() {
		row := newFieldRow(k, w, spec)
		k.onWMChange = row.refresh
		sweepRow(t, row, nil, func(action string) {
			if w.class == "error" {
				t.Errorf("%s: %s: %s", spec.path, action, w.notice)
				w.show("", "")
			}
		})
	}
}

// Every lys-shell control, driven through every add, edit, remove and
// reset, writes only values lys-shell's validator takes, and what it saved
// loads back without an error: each write is checked as it is made, the
// saved file once the row's sweep is done. Each list of dropdown rows goes
// through every row once, the first time a control shows it.
func TestEveryShellControlWritesOnlyWhatLysShellAccepts(t *testing.T) {
	base := testKit(t, "")
	driven := map[string]bool{}
	for _, spec := range shellRows() {
		t.Run(spec.path, func(t *testing.T) {
			k := *base
			k.store = testStore(t, "")
			row := newFieldRow(&k, k.store, spec)
			var steps []string
			sweepRow(t, row, driven, func(action string) {
				steps = append(steps, action)
				if row.problem.Visible() {
					t.Errorf("%s: refused: %s", action, row.problem.Text())
					row.showProblem(nil)
				}
			})
			var problems []string
			svc := config.Load(k.store.svc.Dir(), func(d config.Diagnostic) {
				if d.Kind == config.DiagnosticError {
					problems = append(problems, d.Plain())
				}
			})
			svc.Close()
			if len(problems) > 0 {
				t.Errorf("after %q lys-shell reads back %v\nruntime.yaml:\n%s", steps, problems, runtimeFile(t, &k))
			}
		})
	}
}

// sweepWMConfig holds one item of every lys-wm list, so a sweep opens
// and edits each kind of card once instead of every built-in bind.
const sweepWMConfig = `binds:
  - {chord: SUPER + Q, action: kill}
submaps:
  resize:
    - {chord: Return, action: submap reset}
gestures:
  swipe:
    - {fingers: 3, direction: horizontal, drive: workspaces}
  pinch:
    - {fingers: 2, direction: both, drive: zoom}
  hold:
    - {fingers: 4, action: kill}
outputs:
  - {match: DP-1, scale: 1}
rules:
  - {app_id: foot, float: true}
autostart:
  - foot
env:
  GDK_BACKEND: wayland
palette:
  mine: "#123456"
`

// embedded is the lys-ui widget a control wraps by embedding it (a
// number's spin, a toggle's switch), else the widget itself.
func embedded(w widget.Widget) widget.Widget {
	widgetType := reflect.TypeFor[widget.Widget]()
	for {
		v := reflect.ValueOf(w)
		if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct || v.Elem().Type().PkgPath() == reflect.TypeFor[widget.Button]().PkgPath() {
			return w
		}
		s := v.Elem()
		if s.NumField() == 0 || !s.Type().Field(0).Anonymous || !s.Field(0).Type().Implements(widgetType) || s.Field(0).Kind() != reflect.Pointer || s.Field(0).IsNil() || !s.Field(0).CanInterface() {
			return w
		}
		w = s.Field(0).Interface().(widget.Widget)
	}
}

// controlState is what the row's controls show: every shown switch,
// check, segment, dropdown, spin and entry, in tree order.
func controlState(root widget.Widget) []string {
	var out []string
	var walk func(w widget.Widget)
	walk = func(w widget.Widget) {
		if v, ok := w.(interface{ Visible() bool }); !ok || !v.Visible() {
			return
		}
		switch x := embedded(w).(type) {
		case *widget.Switch:
			out = append(out, fmt.Sprintf("switch %v", x.On()))
		case *widget.CheckButton:
			out = append(out, fmt.Sprintf("check %v", x.Checked()))
		case *widget.ToggleGroup:
			out = append(out, fmt.Sprintf("segment %d", x.Active()))
		case *widget.Dropdown:
			out = append(out, fmt.Sprintf("dropdown %d", x.Selected()))
			return
		case *widget.SpinButton:
			if x.Empty() {
				out = append(out, "spin empty")
				return
			}
			out = append(out, fmt.Sprintf("spin %v", x.Value()))
			return
		case *widget.Entry:
			out = append(out, "entry "+x.Text())
			return
		}
		if p, ok := w.(interface{ Children() []widget.Widget }); ok {
			for _, c := range p.Children() {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// Every lys-wm control, when lys-wm refuses what it wrote, shows again
// what a row built from the stored config shows: the last accepted state.
func TestARefusedChangeLeavesEveryWMControlAtTheLastAcceptedState(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	k := testKit(t, "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeFile(t, filepath.Join(dir, "lys-wm", "config.yaml"), sweepWMConfig)
	refusing := func(ctx context.Context, argv ...string) ([]byte, error) {
		if len(argv) > 1 && argv[1] == "--check-config" {
			return nil, &wmconfig.ToolError{Argv: argv, Exit: 1, Stderr: "refused by the test", Err: errors.New("exit status 1")}
		}
		return wmconfig.Exec(ctx, argv...)
	}
	w, err := openWM(k, refusing)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	for _, spec := range allWMRows() {
		row := newFieldRow(k, w, spec)
		k.onWMChange = row.refresh
		sweepRow(t, row, nil, func(action string) {
			if w.class != "error" {
				return
			}
			notice := w.notice
			w.show("", "")
			fresh := newFieldRow(k, w, spec)
			if got, want := controlState(row), controlState(fresh); !slices.Equal(got, want) {
				t.Errorf("%s: after a refused %s (%s) the row shows %q, the stored config %q", spec.path, action, notice, got, want)
			}
		})
		noFile(t, w.file.Runtime())
	}
}
