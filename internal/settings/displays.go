package settings

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/displays"
	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	displaysID     = "wm-outputs"
	outputsPath    = "outputs"
	identifyFor    = 2 * time.Second
	minCustomScale = 0.25
	maxCustomScale = 10
)

var (
	revertAfter     = 15 * time.Second
	scalePresets    = []float64{1, 1.25, 1.5, 1.75, 2, 2.5, 3}
	arrangedKeys    = []string{"match", "mode", "position", "scale", "disabled"}
	connectDisplays = func(onHeads func([]displays.Head)) (displayManager, error) { return displays.Connect(onHeads) }
)

type displayManager interface {
	Test(settings []displays.Setting) error
	Apply(settings []displays.Setting) error
	Close()
}

func displaysPage() pageSpec {
	return pageSpec{
		id: displaysID, navKey: "settings-nav-wm-outputs", icon: "ld-monitor-symbolic",
		header: "settings-page-wm-outputs", wm: true, lead: displaysLead,
		sections: []sectionSpec{{"wm-section-display", wmFields("color_management", "tearing", "gpu", "lid_suspend")}},
	}
}

type displaysConnection struct {
	mgr displayManager
	err error
}

type displaysPanel struct {
	*widget.Box
	k          *kit
	mgr        displayManager
	heads      []displays.Head
	draft      []displays.Setting
	selected   int
	canvas     *displaysCanvas
	controls   *widget.Box
	none       *widget.Label
	enabled    *widget.Switch
	resolution *widget.Dropdown
	sizes      [][2]int
	refresh    *widget.Dropdown
	rates      []displays.Mode
	scale      *widget.Dropdown
	scales     []float64
	custom     *widget.Entry
	extra      *widget.ItemGroup
	identify   *widget.Button
	apply      *widget.Button
	status     *widget.Label
	confirm    *confirmation
	identified []func()
	closed     bool
	syncing    bool
}

type confirmation struct {
	previous []displays.Setting
	applied  []displays.Setting
	heads    []displays.Head
	deadline time.Time
	timer    *time.Timer
	boxes    []*confirmBox
	overlays []func()
}

type confirmBox struct {
	*widget.Box
	countdown *widget.Label
}

func displaysLead(k *kit) widget.Widget {
	t := locale.T()
	p := &displaysPanel{Box: sectionStack(), k: k}
	p.status = wrapped(k.label("", "setting-description", css.Caption))
	p.status.SetVisible(false)
	p.canvas = newDisplaysCanvas(p)
	p.canvas.SetAccessibleName(t.Get("settings-displays-arrangement"))
	p.none = k.label(t.Get("settings-displays-none"), css.DimLabel)
	p.identify = k.button(k.label(t.Get("settings-displays-identify")), func() { p.identifyOutputs(p.enabledNames()) }, css.Secondary)
	p.apply = k.button(k.label(t.Get("settings-displays-apply")), p.applyLayout, css.Small, css.SuggestedAction)
	p.apply.SetEnabled(false)
	actions := gapBox(widget.Row)
	inset := cardInsets()
	inset.Top = 0
	actions.SetPadding(inset)
	actions.Append(p.identify, false)
	actions.Append(widget.NewBox(widget.Row, 0, 0), true)
	actions.Append(p.apply, false)
	arrangement := gapBox(widget.Column)
	arrangement.Append(p.none, false)
	arrangement.Append(p.canvas, false)
	arrangement.Append(actions, false)
	arrangement.Append(p.status, false)
	p.Append(newSection(k, "settings-section-displays-arrangement", arrangement), false)
	p.controls = gapBox(widget.Column)
	p.buildControls()
	p.Append(newSection(k, "settings-section-displays-output", p.controls), false)
	p.connect()
	return p
}

func (p *displaysPanel) buildControls() {
	t := locale.T()
	k := p.k
	rows := widget.NewItemGroup()
	p.enabled = widget.NewSwitch(true)
	p.enabled.SetAccessibleName(t.Get("settings-displays-enabled"))
	p.enabled.OnChanged = func(on bool) {
		if !p.syncing {
			p.setEnabled(on)
		}
	}
	rows.Append(panelRow(k, t.Get("settings-displays-enabled"), "", p.enabled))
	p.resolution = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	p.resolution.SetAccessibleName(t.Get("settings-displays-resolution"))
	p.resolution.OnSelect = func(i int) {
		if !p.syncing && i < len(p.sizes) {
			p.setSize(p.sizes[i])
		}
	}
	rows.Append(panelRow(k, t.Get("settings-displays-resolution"), "", p.resolution))
	p.refresh = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	p.refresh.SetAccessibleName(t.Get("settings-displays-refresh"))
	p.refresh.OnSelect = func(i int) {
		if !p.syncing && i < len(p.rates) {
			p.edit(func(s *displays.Setting) { s.Mode = p.rates[i] }, false)
		}
	}
	rows.Append(panelRow(k, t.Get("settings-displays-refresh"), "", p.refresh))
	p.scale = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	p.scale.SetAccessibleName(t.Get("settings-displays-scale"))
	p.scale.OnSelect = func(i int) {
		if !p.syncing && i < len(p.scales) {
			p.setScale(p.scales[i])
		}
	}
	p.custom = k.entry()
	p.custom.SetPlaceholder(t.Get("settings-displays-scale-custom"))
	p.custom.SetAccessibleName(t.Get("settings-displays-scale-custom"))
	p.custom.OnActivate = func(text string) {
		v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || v < minCustomScale || v > maxCustomScale {
			p.say(t.Get("settings-displays-scale-invalid", i18n.Str("value", text)))
			return
		}
		p.setScale(displays.ScaleValue(v))
	}
	rows.Append(panelRow(k, t.Get("settings-displays-scale"), t.Attr("settings-displays-scale", "description"), p.scale, p.custom))
	p.controls.Append(rows, false)
	p.extra = widget.NewItemGroup()
	p.controls.Append(p.extra, false)
}

func (p *displaysPanel) connect() {
	async(p.k, func() displaysConnection {
		mgr, err := connectDisplays(func(heads []displays.Head) { p.k.invoke(func() { p.showHeads(heads) }) })
		return displaysConnection{mgr, err}
	}, func(c displaysConnection) {
		if c.err != nil {
			p.say(fmt.Sprintf("%s: %v", locale.T().Get("settings-displays-unavailable"), c.err))
			return
		}
		if p.closed {
			c.mgr.Close()
			return
		}
		p.mgr = c.mgr
		p.identifyOutputs(p.enabledNames())
	})
}

func (p *displaysPanel) close() {
	p.closed = true
	p.endConfirm()
	for _, done := range p.identified {
		done()
	}
	p.identified = nil
	if p.mgr != nil {
		p.mgr.Close()
		p.mgr = nil
	}
}

func (p *displaysPanel) say(text string) {
	if text != "" {
		log.Printf("settings: displays: %s", text)
	}
	p.status.SetText(text)
	p.status.SetVisible(text != "")
}

func (p *displaysPanel) showHeads(heads []displays.Head) {
	if p.closed {
		return
	}
	p.heads = heads
	if p.confirm != nil {
		return
	}
	p.draft = displays.Settings(heads)
	p.selected = min(max(p.selected, 0), max(len(heads)-1, 0))
	p.render()
	p.showExtraRows()
}

func (p *displaysPanel) enabledNames() []string {
	var out []string
	for _, s := range p.draft {
		if s.Enabled {
			out = append(out, s.Name)
		}
	}
	return out
}

func (p *displaysPanel) dirty() bool {
	return !slices.Equal(p.draft, displays.Settings(p.heads))
}

func (p *displaysPanel) render() {
	p.syncing = true
	defer func() { p.syncing = false }()
	has := len(p.draft) > 0
	p.none.SetVisible(!has)
	p.canvas.SetVisible(has)
	p.controls.SetVisible(has)
	p.apply.SetEnabled(has && p.dirty() && p.confirm == nil && p.mgr != nil)
	p.canvas.layout()
	if !has {
		return
	}
	s, h := p.draft[p.selected], p.heads[p.selected]
	if p.enabled.On() != s.Enabled {
		p.enabled.SetOn(s.Enabled)
	}
	p.sizes = p.sizes[:0]
	for _, m := range h.Modes {
		size := [2]int{m.Width, m.Height}
		if !slices.Contains(p.sizes, size) {
			p.sizes = append(p.sizes, size)
		}
	}
	slices.SortFunc(p.sizes, func(a, b [2]int) int { return cmp.Compare(b[0]*b[1], a[0]*a[1]) })
	sizeNames := make([]string, len(p.sizes))
	for i, size := range p.sizes {
		sizeNames[i] = strconv.Itoa(size[0]) + " × " + strconv.Itoa(size[1])
	}
	setChoices(p.resolution, sizeNames, slices.Index(p.sizes, [2]int{s.Mode.Width, s.Mode.Height}))
	p.rates = p.rates[:0]
	for _, m := range h.Modes {
		if m.Width == s.Mode.Width && m.Height == s.Mode.Height {
			p.rates = append(p.rates, m)
		}
	}
	slices.SortFunc(p.rates, func(a, b displays.Mode) int { return cmp.Compare(b.RefreshMHz, a.RefreshMHz) })
	rateNames := make([]string, len(p.rates))
	for i, m := range p.rates {
		rate := strconv.FormatFloat(float64(m.RefreshMHz)/1000, 'f', 2, 64)
		key := "settings-displays-rate"
		if m.Preferred {
			key = "settings-displays-rate-preferred"
		}
		rateNames[i] = locale.T().Get(key, i18n.Str("rate", rate))
	}
	setChoices(p.refresh, rateNames, slices.Index(p.rates, s.Mode))
	p.scales = slices.Clone(scalePresets)
	if scale := displays.ScaleValue(s.Scale); !slices.Contains(p.scales, scale) {
		p.scales = append(p.scales, scale)
		slices.Sort(p.scales)
	}
	scaleNames := make([]string, len(p.scales))
	for i, v := range p.scales {
		scaleNames[i] = strconv.FormatFloat(v*100, 'f', -1, 64) + " %"
	}
	setChoices(p.scale, scaleNames, slices.Index(p.scales, displays.ScaleValue(s.Scale)))
	p.resolution.SetEnabled(s.Enabled)
	p.refresh.SetEnabled(s.Enabled)
	p.scale.SetEnabled(s.Enabled)
	p.custom.SetEnabled(s.Enabled)
}

func (p *displaysPanel) selectOutput(i int) {
	if i == p.selected || i < 0 || i >= len(p.draft) {
		return
	}
	p.selected = i
	p.render()
	p.showExtraRows()
	if p.draft[i].Enabled {
		p.identifyOutputs([]string{p.draft[i].Name})
	}
}

func (p *displaysPanel) edit(change func(*displays.Setting), relayout bool) {
	change(&p.draft[p.selected])
	if relayout {
		p.settle(p.selected)
	}
	p.say("")
	p.render()
}

func (p *displaysPanel) setEnabled(on bool) {
	p.edit(func(s *displays.Setting) {
		s.Enabled = on
		if !on || s.Mode.Width > 0 {
			return
		}
		h := p.heads[p.selected]
		s.Mode = h.Modes[max(0, slices.IndexFunc(h.Modes, func(m displays.Mode) bool { return m.Preferred }))]
		s.Scale = cmp.Or(s.Scale, 1)
	}, true)
}

func (p *displaysPanel) setSize(size [2]int) {
	h := p.heads[p.selected]
	current := p.draft[p.selected].Mode
	best := -1
	for i, m := range h.Modes {
		if m.Width != size[0] || m.Height != size[1] {
			continue
		}
		if best < 0 || m.RefreshMHz == current.RefreshMHz || (h.Modes[best].RefreshMHz != current.RefreshMHz && m.RefreshMHz > h.Modes[best].RefreshMHz) {
			best = i
		}
	}
	if best < 0 {
		return
	}
	p.edit(func(s *displays.Setting) { s.Mode = h.Modes[best] }, true)
}

func (p *displaysPanel) setScale(v float64) {
	p.edit(func(s *displays.Setting) { s.Scale = v }, true)
}

func (p *displaysPanel) settle(moved int) {
	rects, index := p.enabledRects()
	at := slices.Index(index, moved)
	if at < 0 {
		return
	}
	p.place(index, displays.Settle(rects, at))
}

func (p *displaysPanel) enabledRects() ([]displays.Rect, []int) {
	var rects []displays.Rect
	var index []int
	for i, s := range p.draft {
		if s.Enabled {
			rects = append(rects, displays.Logical(s))
			index = append(index, i)
		}
	}
	return rects, index
}

func (p *displaysPanel) place(index []int, rects []displays.Rect) {
	for j, i := range index {
		p.draft[i].X, p.draft[i].Y = rects[j].X, rects[j].Y
	}
}

func (p *displaysPanel) moved(index []int, rects []displays.Rect) {
	p.place(index, rects)
	p.say("")
	p.render()
}

func (p *displaysPanel) identifyOutputs(names []string) {
	if p.k.overlays == nil {
		return
	}
	for _, name := range names {
		label := p.k.label(name, "display-identify", css.H1)
		done, err := p.k.overlays(name, label, false)
		if err != nil {
			log.Printf("settings: identify %s: %v", name, err)
			continue
		}
		p.identified = append(p.identified, done)
		time.AfterFunc(identifyFor, func() { p.k.invoke(done) })
	}
}

func (p *displaysPanel) applyLayout() {
	if p.mgr == nil || p.confirm != nil {
		return
	}
	t := locale.T()
	mgr, settings := p.mgr, slices.Clone(p.draft)
	previous, heads := displays.Settings(p.heads), slices.Clone(p.heads)
	p.apply.SetEnabled(false)
	p.say(t.Get("settings-displays-testing"))
	async(p.k, func() error {
		if err := mgr.Test(settings); err != nil {
			return err
		}
		return mgr.Apply(settings)
	}, func(err error) {
		switch {
		case p.closed:
		case errors.Is(err, displays.ErrRefused):
			p.say(t.Get("settings-displays-refused"))
			p.render()
		case err != nil:
			p.say(err.Error())
			p.render()
		default:
			p.say("")
			p.startConfirm(previous, settings, heads)
		}
	})
}

func (p *displaysPanel) startConfirm(previous, applied []displays.Setting, heads []displays.Head) {
	c := &confirmation{previous: previous, applied: applied, heads: heads, deadline: time.Now().Add(revertAfter)}
	p.confirm = c
	inline := p.newConfirmBox()
	c.boxes = append(c.boxes, inline)
	p.InsertAt(0, inline, false)
	if p.k.overlays != nil {
		for _, s := range applied {
			if !s.Enabled {
				continue
			}
			box := p.newConfirmBox()
			done, err := p.k.overlays(s.Name, box, true)
			if err != nil {
				log.Printf("settings: confirm on %s: %v", s.Name, err)
				continue
			}
			c.boxes = append(c.boxes, box)
			c.overlays = append(c.overlays, done)
		}
	}
	p.tick()
	p.render()
}

func (p *displaysPanel) newConfirmBox() *confirmBox {
	t := locale.T()
	k := p.k
	b := &confirmBox{Box: gapBox(widget.Column)}
	b.AddClass(css.Card, "display-confirm")
	b.Append(k.label(t.Get("settings-displays-confirm"), css.Heading), false)
	b.countdown = k.label("", css.DimLabel)
	b.Append(b.countdown, false)
	buttons := gapBox(widget.Row)
	buttons.Append(k.button(k.label(t.Get("settings-displays-revert")), p.revertLayout), false)
	buttons.Append(k.button(k.label(t.Get("settings-displays-keep")), p.keepLayout, css.SuggestedAction), false)
	b.Append(buttons, false)
	return b
}

func (p *displaysPanel) tick() {
	c := p.confirm
	if c == nil {
		return
	}
	left := time.Until(c.deadline)
	if left <= 0 {
		p.revertLayout()
		return
	}
	seconds := int64((left + time.Second - 1) / time.Second)
	text := locale.T().Get("settings-displays-reverting", i18n.Int("seconds", seconds))
	for _, b := range c.boxes {
		b.countdown.SetText(text)
	}
	c.timer = time.AfterFunc(min(left, time.Second), func() { p.k.invoke(p.tick) })
}

func (p *displaysPanel) endConfirm() *confirmation {
	c := p.confirm
	if c == nil {
		return nil
	}
	p.confirm = nil
	if c.timer != nil {
		c.timer.Stop()
	}
	for _, done := range c.overlays {
		done()
	}
	p.Remove(c.boxes[0])
	return c
}

func (p *displaysPanel) keepLayout() {
	c := p.endConfirm()
	if c == nil {
		return
	}
	w, err := p.k.wmBackend()
	if err != nil {
		p.say(err.Error())
		return
	}
	existing, _ := w.value(outputsPath).([]any)
	if err := w.edit(wmconfig.Edit{Path: []string{outputsPath}, Value: displays.Rules(existing, c.heads, c.applied)}); err != nil {
		p.say(err.Error())
	}
	p.showHeads(p.heads)
}

func (p *displaysPanel) revertLayout() {
	c := p.endConfirm()
	if c == nil {
		return
	}
	mgr := p.mgr
	if mgr == nil {
		return
	}
	async(p.k, func() error { return mgr.Apply(c.previous) }, func(err error) {
		if err != nil && !p.closed {
			p.say(err.Error())
		}
		p.showHeads(p.heads)
	})
}

func (p *displaysPanel) showExtraRows() {
	p.extra.Clear()
	if len(p.heads) == 0 {
		return
	}
	w, err := p.k.wmBackend()
	if err != nil {
		p.say(err.Error())
		return
	}
	t := locale.T()
	h := p.heads[p.selected]
	rule := func() slot { return outputRuleSlot(w, h) }
	for _, f := range wmItemFields(w, []string{outputsPath, wmconfig.Each}, nil, outputsPath) {
		if arranged(f) {
			continue
		}
		ctl := f.item(p.k, func(key string) slot { return nestedSlot(rule(), []string{key}) })
		p.extra.Append(panelRow(p.k, t.Get(f.label), t.Attr(f.label, "description"), ctl))
	}
}

// arranged reports whether f is a key the arrangement above edits, which
// the selected display's extra rows leave out.
func arranged(f cardField) bool {
	return slices.ContainsFunc(arrangedKeys, func(key string) bool { return f.label == wmKey(outputsPath+"."+key) })
}

func outputRuleSlot(w *wmBackend, h displays.Head) slot {
	match := displays.Match(h)
	find := func(rules []any) int {
		return slices.IndexFunc(rules, func(r any) bool {
			m, _ := r.(map[string]any)
			text, _ := m["match"].(string)
			return text == match || text == h.Name
		})
	}
	write := func(rule map[string]any) error {
		rules, _ := w.value(outputsPath).([]any)
		rules = slices.Clone(rules)
		if i := find(rules); i >= 0 {
			rules[i] = rule
		} else {
			rules = append(rules, rule)
		}
		return w.edit(wmconfig.Edit{Path: []string{outputsPath}, Value: rules})
	}
	return slot{
		get: func() any {
			rules, _ := w.value(outputsPath).([]any)
			if i := find(rules); i >= 0 {
				return rules[i]
			}
			return nil
		},
		set: func(v any) error {
			rule, _ := v.(map[string]any)
			rule = maps.Clone(rule)
			if rule == nil {
				rule = map[string]any{}
			}
			if _, ok := rule["match"]; !ok {
				rule["match"] = match
			}
			return write(rule)
		},
		unset: func() {
			rules, _ := w.value(outputsPath).([]any)
			if i := find(rules); i >= 0 {
				_ = w.edit(wmconfig.Edit{Path: []string{outputsPath}, Value: slices.Delete(slices.Clone(rules), i, i+1)})
			}
		},
	}
}
