package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/shell/apptheme"
	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/displays"
	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
	"github.com/stubbedev/lys-settings/internal/wmtest"
)

const secondOutput = "HEADLESS-2"

type wmOutput struct {
	Name string `json:"name"`
	Rect struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"rect"`
}

type displaysRig struct {
	s     *wmtest.Session
	k     *kit
	l     *loop
	p     *displaysPanel
	shown []string
}

func twoOutputs(t *testing.T) (*wmtest.Session, *kit, *loop) {
	t.Helper()
	s := wmtest.Start(t, "xwayland:\n  enabled: false\n")
	if out, err := s.Ctl("output", "create", "--name", secondOutput, "800x600"); err != nil {
		t.Fatalf("create %s: %v %s", secondOutput, err, out)
	}
	s.Display()
	k := testKit(t, "")
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	k.wm = w
	return s, k, l
}

func startDisplays(t *testing.T) *displaysRig {
	t.Helper()
	s, k, l := twoOutputs(t)
	r := &displaysRig{s: s, k: k, l: l}
	k.overlays = func(output string, _ widget.Widget, _ bool) (func(), error) {
		r.shown = append(r.shown, output)
		return func() {}, nil
	}
	r.p = displaysLead(k).(*displaysPanel)
	t.Cleanup(r.p.close)
	l.until("both outputs and the manager", func() bool { return len(r.p.heads) == 2 && r.p.mgr != nil })
	r.p.Arrange(render.Rect{W: 900, H: 1200})
	return r
}

func (r *displaysRig) outputs(t *testing.T) map[string]wmOutput {
	t.Helper()
	out, err := r.s.Ctl("outputs")
	if err != nil {
		t.Fatal(err)
	}
	var list []wmOutput
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	byName := map[string]wmOutput{}
	for _, o := range list {
		byName[o.Name] = o
	}
	return byName
}

func (r *displaysRig) config(t *testing.T) []byte {
	t.Helper()
	var out []byte
	for _, path := range []string{r.s.Path, r.k.wm.file.Runtime()} {
		b, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		out = append(append(out, b...), 0)
	}
	return out
}

func tileCenter(c *displaysCanvas, i int) widget.Point {
	b := c.screen(c.rects[i])
	return widget.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
}

func (r *displaysRig) dragSecondLeft(t *testing.T) []int {
	t.Helper()
	c := r.p.canvas
	i := slices.Index(c.index, slices.IndexFunc(r.p.draft, func(s displays.Setting) bool { return s.Name == secondOutput }))
	widget.CollectDamage(r.p)
	from := tileCenter(c, i)
	c.PressAt(from)
	widget.CollectDamage(r.p)
	var regions []int
	left := c.screen(c.rects[1-i]).X
	for step := 1; step <= 10; step++ {
		c.DragMove(widget.Point{X: from.X - step*(from.X-left+40)/10, Y: from.Y})
		rects, _ := widget.CollectDamage(r.p)
		regions = append(regions, len(rects))
		for _, d := range rects {
			if d != c.Bounds() {
				t.Fatalf("a motion damaged %v, outside the canvas %v", d, c.Bounds())
			}
		}
	}
	c.PressEnd()
	return regions
}

func confirmButton(t *testing.T, p *displaysPanel, key string) *widget.Button {
	t.Helper()
	if p.confirm == nil {
		t.Fatal("no confirmation")
	}
	return treetest.Button(t, p.confirm.boxes[0], locale.T().Get(key))
}

func TestDraggingTheSecondOutputLeftAndKeepingWritesPositionsLysWMTakes(t *testing.T) {
	r := startDisplays(t)
	before := r.config(t)
	regions := r.dragSecondLeft(t)
	for i, n := range regions {
		if n != 1 {
			t.Fatalf("motion %d damaged %d regions, want one", i, n)
		}
	}
	if string(r.config(t)) != string(before) {
		t.Fatal("dragging wrote the config")
	}
	first := slices.IndexFunc(r.p.draft, func(s displays.Setting) bool { return s.Name != secondOutput })
	second := 1 - first
	if r.p.draft[second].X != 0 || r.p.draft[first].X != displays.Logical(r.p.draft[second]).W {
		t.Fatalf("the draft is %+v", r.p.draft)
	}
	r.p.apply.OnClick()
	if string(r.config(t)) != string(before) {
		t.Fatal("applying wrote the config before Keep")
	}
	if len(r.p.confirm.overlays) != 2 {
		t.Fatalf("the confirmation shows on %d outputs, want both", len(r.p.confirm.overlays))
	}
	confirmButton(t, r.p, "settings-displays-keep").OnClick()
	if string(r.config(t)) == string(before) {
		t.Fatal("Keep wrote nothing")
	}
	r.s.Until("lys-wm reporting the kept layout", func() bool {
		o := r.outputs(t)
		return o[secondOutput].Rect.X == 0 && o["HEADLESS-1"].Rect.X == o[secondOutput].Rect.Width
	})
	if _, err := r.s.Ctl("reload"); err != nil {
		t.Fatal(err)
	}
	o := r.outputs(t)
	if o[secondOutput].Rect.X != 0 || o["HEADLESS-1"].Rect.X != o[secondOutput].Rect.Width {
		t.Fatalf("after a reload from the config lys-wm reports %+v", o)
	}
}

func TestATimeoutWithoutKeepRestoresThePositionsAndWritesNothing(t *testing.T) {
	before := revertAfter
	revertAfter = 300 * time.Millisecond
	t.Cleanup(func() { revertAfter = before })
	r := startDisplays(t)
	file := r.config(t)
	original := r.outputs(t)
	r.dragSecondLeft(t)
	r.p.apply.OnClick()
	if r.p.confirm == nil {
		t.Fatalf("no confirmation: %s", r.p.status.Text())
	}
	r.s.Until("the applied layout", func() bool { return r.outputs(t)[secondOutput].Rect.X == 0 })
	r.l.until("the revert", func() bool { return r.p.confirm == nil })
	r.s.Until("the original layout back", func() bool {
		o := r.outputs(t)
		return o[secondOutput].Rect == original[secondOutput].Rect && o["HEADLESS-1"].Rect == original["HEADLESS-1"].Rect
	})
	if got := r.config(t); string(got) != string(file) {
		t.Fatalf("the config changed:\n%s\nwas\n%s", got, file)
	}
}

func TestALayoutLysWMRefusesChangesNothing(t *testing.T) {
	r := startDisplays(t)
	file := r.config(t)
	for i := range r.p.draft {
		r.p.selectOutput(i)
		r.p.enabled.SetOn(false)
	}
	r.p.apply.OnClick()
	if r.p.confirm != nil || r.p.status.Text() != locale.T().Get("settings-displays-refused") {
		t.Fatalf("status %q, confirming %v", r.p.status.Text(), r.p.confirm != nil)
	}
	if got := r.config(t); string(got) != string(file) {
		t.Fatal("a refused layout wrote the config")
	}
	if n := len(r.outputs(t)); n != 2 {
		t.Fatalf("lys-wm reports %d outputs", n)
	}
}

func TestTheKeyboardAloneArrangesTheOutputs(t *testing.T) {
	_, k, l := twoOutputs(t)
	cfg := k.store.svc.Config()
	w := newWindow(k, cfg, func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	t.Cleanup(func() {
		for _, p := range w.pages {
			p.close()
		}
	})
	apptheme.New(cfg).Attach(w.root)
	w.show(displaysID)
	p := treetest.First[*displaysPanel](t, w.page)
	l.until("both outputs", func() bool { return len(p.heads) == 2 && p.mgr != nil })
	w.root.Arrange(render.Rect{W: 1400, H: 6000})
	router := &widget.Router{Root: w.root}
	if err := router.SetFocus(w.search); err != nil {
		t.Fatal(err)
	}
	visited := map[widget.Widget]bool{}
	for range maxTabStops {
		router.FocusNext()
		f := router.Focused()
		if f == widget.Widget(w.search) {
			break
		}
		if !widget.FocusVisible(f) {
			t.Errorf("Tab landed on %T without a visible focus", f)
		}
		visited[f] = true
	}
	for _, c := range append(shownControls(p), p.canvas.Children()...) {
		reached := false
		treetest.Walk(c, func(x widget.Widget) { reached = reached || visited[x] })
		if !reached {
			name, _ := c.(interface{ AccessibleName() string })
			t.Errorf("%T %q is out of Tab's reach", c, name.AccessibleName())
		}
	}
	second := slices.IndexFunc(p.canvas.index, func(i int) bool { return p.draft[i].Name == secondOutput })
	for router.Focused() != widget.Widget(p.canvas.handles[second]) {
		router.FocusNext()
	}
	if p.draft[p.selected].Name != secondOutput {
		t.Fatalf("focusing %s selected %s", secondOutput, p.draft[p.selected].Name)
	}
	router.KeyAction(widget.KeyDown, widget.ModShift)
	if p.draft[p.selected].Y == 0 || !p.apply.Enabled() {
		t.Fatalf("Shift+Down left %+v", p.draft[p.selected])
	}
	before := p.draft[p.selected].Y
	router.KeyAction(widget.KeyUp, 0)
	if p.draft[p.selected].Y != before-1 {
		t.Fatalf("Up moved %s from %d to %d", secondOutput, before, p.draft[p.selected].Y)
	}
}

func TestTheArrangementsActionsSitInsetInItsCard(t *testing.T) {
	r := startDisplays(t)
	if r.p.status.Visible() {
		t.Fatalf("the arrangement says %q", r.p.status.Text())
	}
	actions := r.p.identify.Parent().(*widget.Box)
	column := actions.Parent().(*widget.Box).Bounds()
	canvas, row := r.p.canvas.Bounds(), actions.Bounds()
	identify, apply := r.p.identify.Bounds(), r.p.apply.Bounds()
	inset := cardInsets()
	if got := identify.X - column.X; got != inset.Left {
		t.Errorf("Identify sits %dpx from the card's left, want %d", got, inset.Left)
	}
	if got := column.X + column.W - (apply.X + apply.W); got != inset.Right {
		t.Errorf("Apply sits %dpx from the card's right, want %d", got, inset.Right)
	}
	for name, b := range map[string]render.Rect{"Identify": identify, "Apply": apply} {
		if got := column.Y + column.H - (b.Y + b.H); got < inset.Bottom {
			t.Errorf("%s sits %dpx from the card's bottom, want at least %d", name, got, inset.Bottom)
		}
	}
	if canvas.X != column.X || canvas.W != column.W || canvas.Y != column.Y {
		t.Errorf("the canvas moved to %v inside the card's %v", canvas, column)
	}
	if row.X != column.X || row.W != column.W || row.Y != canvas.Y+canvas.H+widget.Current().Spacing || row.Y+row.H != column.Y+column.H {
		t.Errorf("the action row moved to %v under the canvas %v in the card's %v", row, canvas, column)
	}
}
