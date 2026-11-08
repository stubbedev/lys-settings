package settings

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/portal/permissions"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	wmChordFormat  = "lys-wm-chord"
	wmActionFormat = "lys-wm-action"
	chordJoin      = " + "
	chordKey       = "chord"
)

type chordModifier struct {
	accel, chord string
	aliases      []string
}

var chordModifiers = []chordModifier{
	{accel: "Ctrl", chord: "CTRL", aliases: []string{"CTRL", "CONTROL"}},
	{accel: "Shift", chord: "SHIFT", aliases: []string{"SHIFT"}},
	{accel: "Alt", chord: "ALT", aliases: []string{"ALT", "MOD1"}},
	{accel: "Super", chord: "SUPER", aliases: []string{"SUPER", "MOD4", "WIN", "LOGO"}},
}

func modifierByAccel(name string) (chordModifier, bool) {
	i := slices.IndexFunc(chordModifiers, func(m chordModifier) bool { return m.accel == name })
	if i < 0 {
		return chordModifier{}, false
	}
	return chordModifiers[i], true
}

func modifierByChord(name string) (int, bool) {
	upper := strings.ToUpper(name)
	i := slices.IndexFunc(chordModifiers, func(m chordModifier) bool { return slices.Contains(m.aliases, upper) })
	return i, i >= 0
}

var chordOrder = []string{"Super", "Ctrl", "Alt", "Shift"}

func chordFromAccel(accel string) string {
	if accel == "" {
		return ""
	}
	parts := strings.Split(accel, "+")
	held := parts[:len(parts)-1]
	out := make([]string, 0, len(parts))
	for _, name := range chordOrder {
		if m, ok := modifierByAccel(name); ok && slices.Contains(held, name) {
			out = append(out, m.chord)
		}
	}
	return strings.Join(append(out, parts[len(parts)-1]), chordJoin)
}

type parsedChord struct {
	mods uint8
	key  string
}

func parseChord(chord string) (parsedChord, bool) {
	parts := strings.Split(chord, "+")
	var c parsedChord
	for _, p := range parts[:len(parts)-1] {
		i, ok := modifierByChord(strings.TrimSpace(p))
		if !ok {
			return parsedChord{}, false
		}
		c.mods |= 1 << i
	}
	c.key = strings.TrimSpace(parts[len(parts)-1])
	return c, c.key != ""
}

func (c parsedChord) mouse() bool {
	lower := strings.ToLower(c.key)
	return strings.HasPrefix(lower, "mouse:") || lower == "mouse_up" || lower == "mouse_down"
}

func (c parsedChord) accel() string {
	parts := make([]string, 0, len(chordModifiers)+1)
	for i, m := range chordModifiers {
		if c.mods&(1<<i) != 0 {
			parts = append(parts, m.accel)
		}
	}
	key := c.key
	if len([]rune(key)) == 1 {
		key = strings.ToUpper(key)
	}
	return strings.Join(append(parts, key), "+")
}

func normalChord(chord string) string {
	c, ok := parseChord(chord)
	if !ok {
		return strings.ToLower(strings.TrimSpace(chord))
	}
	if len([]rune(c.key)) == 1 {
		c.key = strings.ToLower(c.key)
	}
	return string(rune('0'+c.mods)) + c.key
}

func chordClashes(chords []string, normal func(string) string) []int {
	partner := make([]int, len(chords))
	first := make(map[string]int, len(chords))
	for i, chord := range chords {
		partner[i] = -1
		if chord == "" {
			continue
		}
		n := normal(chord)
		j, seen := first[n]
		if !seen {
			first[n] = i
			continue
		}
		partner[i] = j
		if partner[j] < 0 {
			partner[j] = i
		}
	}
	return partner
}

type chordEditor struct {
	*widget.Box
	k        *kit
	slot     slot
	recorder *widget.ShortcutRecorder
	text     *text
}

func newChordEditor(k *kit, s slot) *chordEditor {
	e := &chordEditor{Box: gapBox(widget.Row), k: k, slot: s}
	e.recorder = widget.NewShortcutRecorder(k.face, "")
	e.recorder.SetAccessibleName(locale.T().Get("wm-binds-chord"))
	e.recorder.OnChanged = func(accel string) {
		if accel == "" {
			e.slot.unset()
			return
		}
		_ = e.slot.set(chordFromAccel(accel))
	}
	e.text = newText(k, s, false)
	e.AppendAligned(e.recorder, true, widget.AlignCenter)
	e.AppendAligned(e.text, true, widget.AlignCenter)
	e.refresh()
	return e
}

func (e *chordEditor) refresh() {
	stored := e.slot.text()
	c, ok := parseChord(stored)
	byText := stored != "" && (!ok || c.mouse())
	e.recorder.SetVisible(!byText)
	e.text.SetVisible(byText)
	if byText {
		e.text.refresh()
		return
	}
	if accel := c.accel(); ok && accel != e.recorder.Accel() {
		e.recorder.SetAccel(accel)
	} else if !ok && e.recorder.Accel() != "" {
		e.recorder.SetAccel("")
	}
}

func actionChoicesOf(actions []wmconfig.Action) lister {
	return func() []choice {
		out := make([]choice, 0, len(actions))
		for _, a := range actions {
			value := a.Name
			if a.Args != "" {
				value += " "
			}
			out = append(out, choice{value: value, label: strings.TrimSpace(a.Name + " " + a.Args)})
		}
		return out
	}
}

func knownAction(actions []wmconfig.Action) func(current string, _ []choice) bool {
	return func(current string, _ []choice) bool {
		name, _, _ := strings.Cut(strings.TrimSpace(current), " ")
		return slices.ContainsFunc(actions, func(a wmconfig.Action) bool { return a.Name == name })
	}
}

func newWMActionEditor(k *kit, s slot, actions []wmconfig.Action) *choiceEditor {
	return newChoiceEditor(k, s, choiceSpec{list: actionChoicesOf(actions), known: knownAction(actions)})
}

const globalAction = "global"

type appShortcut struct {
	app string
	permissions.Shortcut
}

func (a appShortcut) action() string { return globalAction + " " + a.app + ":" + a.ID }

var readAppShortcuts = func() ([]appShortcut, error) {
	path := permissions.ShortcutsPath()
	if path == "" {
		return nil, nil
	}
	all, err := permissions.ReadShortcuts(path)
	if err != nil {
		return nil, err
	}
	var out []appShortcut
	for _, app := range slices.Sorted(maps.Keys(all)) {
		for _, sc := range all[app] {
			out = append(out, appShortcut{app: app, Shortcut: sc})
		}
	}
	return out, nil
}

type appShortcutsListing struct {
	shortcuts []appShortcut
	err       error
}

type appShortcutsEditor struct {
	*widget.Box
	k         *kit
	binds     slot
	list      *widget.Box
	empty     *widget.Label
	recorders []*widget.ShortcutRecorder
	shortcuts []appShortcut
}

func appShortcutsRow() rowSpec {
	return wmField("binds", withKey("wm-app-shortcuts"), func(r *rowSpec) {
		r.fullWidth, r.action = true, true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newAppShortcutsEditor(k, s) }
	})
}

func newAppShortcutsEditor(k *kit, binds slot) *appShortcutsEditor {
	e := &appShortcutsEditor{Box: gapBox(widget.Column), k: k, binds: binds}
	e.list = gapBox(widget.Column)
	e.empty = k.label(locale.T().Get("wm-app-shortcuts-none"), "setting-description", css.Caption, css.DimLabel)
	e.Append(e.empty, false)
	e.Append(e.list, false)
	reload := k.button(k.label(locale.T().Get("wm-app-shortcuts-reload")), e.load, css.Secondary)
	reload.SetHAlign(widget.AlignStart)
	e.Append(reload, false)
	e.load()
	return e
}

func (e *appShortcutsEditor) load() {
	async(e.k, func() appShortcutsListing {
		s, err := readAppShortcuts()
		return appShortcutsListing{s, err}
	}, e.show)
}

func (e *appShortcutsEditor) show(l appShortcutsListing) {
	t := locale.T()
	e.list.Clear()
	e.recorders, e.shortcuts = e.recorders[:0], l.shortcuts
	switch {
	case l.err != nil:
		e.empty.SetText(l.err.Error())
	case len(l.shortcuts) == 0:
		e.empty.SetText(t.Get("wm-app-shortcuts-none"))
	}
	e.empty.SetVisible(l.err != nil || len(l.shortcuts) == 0)
	for i, sc := range l.shortcuts {
		row := gapBox(widget.Row)
		row.AddClass("app-shortcut")
		text := widget.NewBox(widget.Column, widget.Current().Spacing/4, 0)
		text.Append(e.k.label(cmp.Or(sc.Description, sc.ID), "setting-label"), false)
		text.Append(e.k.label(sc.app, "setting-description", css.Caption, css.DimLabel), false)
		row.AppendAligned(text, true, widget.AlignCenter)
		if sc.Trigger != "" {
			row.AppendAligned(widget.NewShortcutLabel(e.k.face, sc.Trigger), false, widget.AlignCenter)
			row.SetTooltip(t.Get("wm-app-shortcuts-hotkey"))
			e.recorders = append(e.recorders, nil)
		} else {
			rec := widget.NewShortcutRecorder(e.k.face, "")
			rec.SetAccessibleName(cmp.Or(sc.Description, sc.ID))
			rec.OnChanged = func(accel string) { e.bind(i, chordFromAccel(accel)) }
			row.AppendAligned(rec, false, widget.AlignCenter)
			e.recorders = append(e.recorders, rec)
		}
		e.list.Append(row, false)
	}
	e.refresh()
}

func (e *appShortcutsEditor) items() []any {
	items, _ := e.binds.get().([]any)
	return slices.Clone(items)
}

func bindAction(item any) string {
	m, _ := item.(map[string]any)
	a, _ := m["action"].(string)
	return strings.Join(strings.Fields(a), " ")
}

func (e *appShortcutsEditor) bind(i int, chord string) {
	action := e.shortcuts[i].action()
	items := slices.DeleteFunc(e.items(), func(it any) bool { return bindAction(it) == action })
	if chord != "" {
		items = append(items, map[string]any{chordKey: chord, "action": action})
	}
	_ = e.binds.set(items)
}

func (e *appShortcutsEditor) refresh() {
	items := e.items()
	for i, rec := range e.recorders {
		if rec == nil || rec.Recording() {
			continue
		}
		action := e.shortcuts[i].action()
		accel := ""
		if j := slices.IndexFunc(items, func(it any) bool { return bindAction(it) == action }); j >= 0 {
			m, _ := items[j].(map[string]any)
			chord, _ := m[chordKey].(string)
			if c, ok := parseChord(chord); ok {
				accel = c.accel()
			}
		}
		if rec.Accel() != accel {
			rec.SetAccel(accel)
		}
	}
}
