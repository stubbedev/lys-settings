package settings

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-desktop/styling"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	choiceListPx    = 320
	checkMark       = "✓ "
	wmColorFormat   = "lys-wm-color"
	wmFontFormat    = "lys-wm-font-file"
	wmLayoutFormat  = "lys-wm-xkb-layout"
	wmVariantFormat = "lys-wm-xkb-variant"
	wmOptionsFormat = "lys-wm-xkb-options"
	wmModelFormat   = "lys-wm-xkb-model"
	wmRulesFormat   = "lys-wm-xkb-rules"
	typeTerminal    = "Terminal"
	listSoundNames  = "sound-name"
)

func hostLists(h host) map[string]lister {
	x := sync.OnceValue(h.xkb)
	return map[string]lister{
		"Monitor":           h.outputs,
		"OsdMonitor":        h.outputs,
		"PopupMonitor":      h.outputs,
		"IconTheme":         h.iconThemes,
		"lys-wm-icon-theme": h.iconThemes,
		"CursorTheme":       h.cursorThemes,
		"Program":           h.programs,
		typeTerminal:        h.terminals,
		listSoundNames:      h.soundNames,
		"NetInterface":      h.interfaces,
		"TempSensor":        h.sensors,
		"AudioSource":       audioSources,
		"PamService":        h.pamServices,
		"lys-wm-drm-device": h.drmDevices,
		wmFontFormat:        h.fontFiles,
		wmLayoutFormat:      func() []choice { return x().layouts },
		wmVariantFormat:     func() []choice { return x().variants },
		wmOptionsFormat:     func() []choice { return x().options },
		wmModelFormat:       func() []choice { return x().models },
		wmRulesFormat:       func() []choice { return x().rules },
	}
}

var systemLists = hostLists(systemHost)

var commandPresets = []struct {
	keyword  string
	commands []string
}{
	{"lock", []string{"loginctl lock-session"}},
	{"logout", []string{"loginctl terminate-session self", "loginctl terminate-user $USER"}},
	{"suspend", []string{"systemctl suspend", "systemctl hybrid-sleep"}},
	{"reboot", []string{"systemctl reboot"}},
	{"shutdown", []string{"systemctl poweroff"}},
	{"poweroff", []string{"systemctl poweroff"}},
}

var bitwardenServers = []string{"bitwarden.com", "bitwarden.eu"}

var strftimePresets = []string{"%H:%M", "%H:%M:%S", "%I:%M %p", "%a %d %b", "%A, %B %d", "%Y-%m-%d", "%d/%m/%Y", "%Y-%m-%d_%H-%M-%S"}

func fixed(values ...string) lister {
	return func() []choice {
		out := make([]choice, 0, len(values))
		for _, v := range values {
			out = append(out, choice{value: v, label: v})
		}
		return out
	}
}

func shellDefault(path string) string {
	v, err := config.DefaultValue(path)
	if err != nil {
		return ""
	}
	s, _ := config.Plain(v).(string)
	return s
}

func commandsFor(path string) []string {
	keys := strings.Split(path, ".")
	last := keys[len(keys)-1]
	out := []string{}
	if d := shellDefault(path); d != "" {
		out = append(out, d)
	}
	for _, p := range commandPresets {
		if strings.Contains(last, p.keyword) {
			out = append(out, p.commands...)
		}
	}
	return slices.Compact(out)
}

func semanticEditor(k *kit, s slot, path string, meta config.FieldMeta) (control, bool) {
	if meta.Kind != config.FieldText {
		return nil, false
	}
	switch meta.Type {
	case "StrftimeFormat":
		return newFormatEditor(k, s, meta.Optional, strftimeFormat()), true
	case "Secret":
		return newSecretEditor(k, s, meta.Optional), true
	case "Directory":
		return newFolderEditor(k, s), true
	case "Command":
		return newChoiceEditor(k, s, choiceSpec{list: fixed(commandsFor(path)...), free: true, optional: meta.Optional}), true
	case "BitwardenServer":
		return newChoiceEditor(k, s, choiceSpec{list: fixed(bitwardenServers...), free: true}), true
	case "IdleTimeout":
		return newChoiceEditor(k, s, choiceSpec{list: fixed(idleTimeouts...), free: true}), true
	case wmColorFormat:
		return newWMColorEditor(k, s), true
	case wmChordFormat:
		return newChordEditor(k, s), true
	case wmActionFormat:
		f, _ := k.wm.fieldOf(path)
		return newWMActionEditor(k, s, f.Actions), true
	}
	if len(meta.Placeholders) > 0 {
		return newFormatEditor(k, s, meta.Optional, placeholderFormat(meta.Placeholders)), true
	}
	list, ok := systemLists[meta.Type]
	if !ok {
		return nil, false
	}
	spec := choiceSpec{list: list, special: meta.Variants, optional: meta.Optional}
	switch meta.Type {
	case wmLayoutFormat, wmOptionsFormat:
		spec.expand, spec.known = toggleList, allListed
	case wmVariantFormat:
		layouts := func() []string { return splitList(wmSibling(k, path, "layout")) }
		spec.expand, spec.known = variantChoices(layouts), variantsListed(layouts)
	case wmFontFormat:
		spec.browse = (*kit).browseButton
	case "Program":
		spec.known = func(current string, items []choice) bool {
			return strings.Contains(current, "/") || hasValue(items, current)
		}
	}
	return newChoiceEditor(k, s, spec), true
}

func wmSibling(k *kit, path, key string) string {
	if k.wm == nil {
		return ""
	}
	keys := wmPath(path)
	keys[len(keys)-1] = key
	v, _ := k.wm.value(strings.Join(keys, ".")).(string)
	return v
}

var soundNameRow = withEditor(func(k *kit, s slot, meta config.FieldMeta) control {
	return newChoiceEditor(k, s, choiceSpec{list: systemLists[listSoundNames], special: meta.Variants, free: true})
})

var terminalRow = withEditor(func(k *kit, s slot, meta config.FieldMeta) control {
	return newChoiceEditor(k, s, choiceSpec{list: systemLists[typeTerminal], special: meta.Variants, free: true})
})

type choiceSpec struct {
	list     lister
	special  []string
	expand   func(current string, items []choice) []choice
	known    func(current string, items []choice) bool
	browse   func(k *kit, pick func(string)) *widget.Button
	free     bool
	optional bool
}

type choiceEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	spec    choiceSpec
	entry   *text
	open    *widget.Button
	browse  *widget.Button
	unknown *widget.Badge
	items   []choice
	listed  bool
	picker  *searchPicker
}

func specialLabel(v string) string {
	t := locale.T()
	switch v {
	case "":
		return t.Get("settings-choice-default")
	case "auto":
		return t.Get("settings-choice-auto")
	case "primary":
		return t.Get("settings-choice-primary")
	}
	return v
}

func newChoiceEditor(k *kit, s slot, spec choiceSpec) *choiceEditor {
	t := locale.T()
	c := &choiceEditor{Box: gapBox(widget.Row), k: k, slot: s, spec: spec, entry: newText(k, s, spec.optional)}
	if slices.Contains(spec.special, "") || spec.optional {
		c.entry.SetPlaceholder(specialLabel(""))
	}
	c.unknown = k.badge(t.Get("settings-choice-unknown"), "warning")
	c.unknown.SetTooltip(t.Attr("settings-choice-unknown", "description"))
	c.unknown.SetVisible(false)
	c.open = k.iconButton("ld-chevron-down-symbolic", "settings-choice-open", c.show, "choice-open", css.Outline)
	committed := c.entry.OnActivate
	c.entry.OnActivate = func(text string) {
		committed(text)
		c.refreshUnknown()
	}
	c.AppendAligned(c.unknown, false, widget.AlignCenter)
	c.AppendAligned(c.entry, false, widget.AlignCenter)
	c.AppendAligned(c.open, false, widget.AlignCenter)
	if spec.browse != nil {
		c.browse = spec.browse(k, c.pick)
		c.AppendAligned(c.browse, false, widget.AlignCenter)
	}
	c.refresh()
	async(k, spec.list, c.setItems)
	return c
}

func (c *choiceEditor) dirtyBadge() *widget.Badge { return c.entry.badge }

func (c *choiceEditor) setItems(items []choice) {
	c.items, c.listed = items, true
	c.refreshUnknown()
}

func (c *choiceEditor) refresh() {
	c.entry.refresh()
	c.refreshUnknown()
}

func (c *choiceEditor) isKnown(current string) bool {
	switch {
	case c.spec.free, current == "", slices.Contains(c.spec.special, current):
		return true
	case c.spec.known != nil:
		return c.spec.known(current, c.items)
	}
	return hasValue(c.items, current)
}

func hasValue(items []choice, v string) bool {
	return slices.ContainsFunc(items, func(it choice) bool { return it.value == v })
}

func (c *choiceEditor) refreshUnknown() {
	c.unknown.SetVisible(c.listed && !c.isKnown(c.slot.text()))
}

func (c *choiceEditor) options() []choice {
	current := c.slot.text()
	var out []choice
	for _, v := range c.spec.special {
		out = append(out, choice{value: v, label: specialLabel(v)})
	}
	if c.spec.expand != nil {
		return append(out, c.spec.expand(current, c.items)...)
	}
	if !c.isKnown(current) {
		out = append(out, choice{value: current, label: current, group: locale.T().Get("settings-choice-unknown")})
	}
	return append(out, c.items...)
}

func choiceHint(o choice) string {
	if o.group != "" {
		return o.group
	}
	if o.value != o.label {
		return o.value
	}
	return ""
}

func (c *choiceEditor) show() {
	if c.k.pickers != nil {
		c.picker = openChoicePicker(c.k, c.open, c.options(), c.pick)
	}
}

func openChoicePicker(k *kit, anchor widget.Widget, opts []choice, pick func(string)) *searchPicker {
	byValue := make(map[string]choice, len(opts))
	names := make([]string, 0, len(opts))
	for _, o := range opts {
		if _, dup := byValue[o.value]; !dup {
			names, byValue[o.value] = append(names, o.value), o
		}
	}
	return openSearchPicker(k, anchor, names, pickerSpec{
		class: "choice-popover", placeholder: "settings-choice-search", maxH: choiceListPx,
		row:  func(k *kit, name string) widget.Widget { return choiceRow(k, byValue[name]) },
		text: func(name string) string { o := byValue[name]; return o.label + " " + choiceHint(o) },
	}, pick)
}

func choiceRow(k *kit, o choice) widget.Widget {
	row := gapBox(widget.Row)
	label := k.label(o.label, "choice-label")
	label.SetEllipsize(widget.EllipsizeEnd)
	row.AppendAligned(label, true, widget.AlignCenter)
	if hint := choiceHint(o); hint != "" {
		row.AppendAligned(k.label(hint, "choice-hint", css.Caption, css.DimLabel), false, widget.AlignCenter)
	}
	return row
}

func (c *choiceEditor) pick(v string) {
	if c.spec.optional && v == "" {
		c.slot.unset()
	} else if c.slot.set(v) != nil {
		return
	}
	c.entry.badge.SetVisible(false)
	c.refresh()
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func toggleList(current string, items []choice) []choice {
	have := splitList(current)
	out := make([]choice, 0, len(items))
	for _, it := range items {
		next := slices.DeleteFunc(slices.Clone(have), func(v string) bool { return v == it.value })
		label := it.label
		if it.group != "" {
			label = it.group + ": " + label
		}
		if len(next) == len(have) {
			next = append(next, it.value)
		} else {
			label = checkMark + label
		}
		out = append(out, choice{value: strings.Join(next, ","), label: label, group: it.value})
	}
	return out
}

func allListed(current string, items []choice) bool {
	for _, v := range splitList(current) {
		if !hasValue(items, v) {
			return false
		}
	}
	return true
}

func joinTrimmed(items []string) string {
	for len(items) > 0 && items[len(items)-1] == "" {
		items = items[:len(items)-1]
	}
	return strings.Join(items, ",")
}

func variantChoices(layouts func() []string) func(current string, items []choice) []choice {
	return func(current string, items []choice) []choice {
		ls := layouts()
		have := splitList(current)
		var out []choice
		for i, layout := range ls {
			for _, it := range items {
				if it.group != layout {
					continue
				}
				next := make([]string, len(ls))
				copy(next, have)
				label := layout + ": " + it.label
				if next[i] == it.value {
					next[i], label = "", checkMark+label
				} else {
					next[i] = it.value
				}
				out = append(out, choice{value: joinTrimmed(next), label: label, group: layout + "(" + it.value + ")"})
			}
		}
		return out
	}
}

func variantsListed(layouts func() []string) func(current string, items []choice) bool {
	return func(current string, items []choice) bool {
		ls := layouts()
		for i, v := range splitList(current) {
			if v == "" {
				continue
			}
			if i >= len(ls) || !slices.ContainsFunc(items, func(it choice) bool { return it.group == ls[i] && it.value == v }) {
				return false
			}
		}
		return true
	}
}

type formatKind struct {
	insert  []choice
	presets []string
	render  func(string) string
}

func placeholderFormat(ps []config.Placeholder) formatKind {
	t := locale.T()
	f := formatKind{}
	for _, p := range ps {
		f.insert = append(f.insert, choice{value: p.Token, label: cmp.Or(t.Get(p.Key), p.Name), group: p.Token})
	}
	f.render = func(s string) string {
		pairs := make([]string, 0, 4*len(ps))
		for _, p := range ps {
			pairs = append(pairs, p.Token, p.Sample, strings.ReplaceAll(p.Token, " ", ""), p.Sample)
		}
		return strings.NewReplacer(pairs...).Replace(s)
	}
	return f
}

func strftimeFormat() formatKind {
	return formatKind{presets: strftimePresets, render: func(s string) string {
		f, err := config.ParseStrftimeFormat(s)
		if err != nil {
			return locale.T().Get("settings-format-invalid", i18n.Str("reason", err.Error()))
		}
		return f.Format(formatSampleTime)
	}}
}

var formatSampleTime = time.Date(2026, time.March, 14, 15, 9, 26, 0, time.Local)

type formatEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	kind    formatKind
	entry   *text
	open    *widget.Button
	preview *widget.Label
	picker  *searchPicker
}

func newFormatEditor(k *kit, s slot, optional bool, kind formatKind) *formatEditor {
	c := &formatEditor{Box: widget.NewBox(widget.Column, widget.Current().Spacing/2, 0), k: k, slot: s, kind: kind, entry: newText(k, s, optional)}
	icon, tip := "ld-plus-symbolic", "settings-format-insert"
	if len(kind.insert) == 0 {
		icon, tip = "ld-chevron-down-symbolic", "settings-format-presets"
	}
	c.open = k.iconButton(icon, tip, c.show, "format-open", css.Outline)
	c.preview = k.label("", "format-preview", css.Caption, css.DimLabel)
	c.preview.SetEllipsize(widget.EllipsizeEnd)
	typed := c.entry.OnChanged
	c.entry.OnChanged = func(text string) {
		typed(text)
		c.showPreview(text)
	}
	line := gapBox(widget.Row)
	line.AppendAligned(c.entry, false, widget.AlignCenter)
	line.AppendAligned(c.open, false, widget.AlignCenter)
	c.Append(line, false)
	c.Append(c.preview, false)
	c.refresh()
	return c
}

func (c *formatEditor) dirtyBadge() *widget.Badge { return c.entry.badge }

func (c *formatEditor) refresh() {
	c.entry.refresh()
	c.showPreview(c.entry.Text())
}

func (c *formatEditor) showPreview(text string) {
	c.preview.SetText(locale.T().Get("settings-format-preview", i18n.Str("text", c.kind.render(text))))
}

func (c *formatEditor) options() []choice {
	if len(c.kind.insert) > 0 {
		return c.kind.insert
	}
	out := make([]choice, 0, len(c.kind.presets)+1)
	if current := c.slot.text(); current != "" && !slices.Contains(c.kind.presets, current) {
		out = append(out, choice{value: current, label: c.kind.render(current), group: current})
	}
	for _, p := range c.kind.presets {
		out = append(out, choice{value: p, label: c.kind.render(p), group: p})
	}
	return out
}

func (c *formatEditor) show() {
	if c.k.pickers != nil {
		c.picker = openChoicePicker(c.k, c.open, c.options(), c.pick)
	}
}

func (c *formatEditor) pick(v string) {
	if len(c.kind.insert) > 0 {
		c.entry.Insert(v)
		v = c.entry.Text()
	}
	if c.entry.commit(v) {
		c.entry.badge.SetVisible(false)
	}
	c.refresh()
}

func newSecretEditor(k *kit, s slot, optional bool) *text {
	c := newText(k, s, optional)
	c.SetEcho(widget.EchoPassword)
	c.SetPlaceholder(locale.T().Get("settings-secret-placeholder"))
	return c
}

// wmAccent is the palette name lys-wm gives the desktop accent, which is
// lys-shell's accent (docs/guide/how-it-works.md, One accent).
const wmAccent = "accent"

// wmPalette is every name a lys-wm colour takes, with its colour: lys-wm's
// built-in palette, the desktop accent, and the palette's own names over
// them.
func wmPalette(k *kit) map[string]render.Color {
	out := map[string]render.Color{}
	if k.wm == nil {
		return out
	}
	builtIn, _ := k.wm.defaultOf("palette")
	own, _ := k.wm.value("palette").(map[string]any)
	add := func(names map[string]any) {
		for name, v := range names {
			hex, _ := v.(string)
			if c, ok := styling.ParseHex(hex); ok {
				out[name] = c
			}
		}
	}
	add(asMap(builtIn))
	shell := k.palette()
	if c, ok := shell.Token(config.TokenAccent); ok {
		out[wmAccent] = c
	}
	add(own)
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// wmColorRGB is the colour a lys-wm colour value names: a hex, or a name
// of the palette.
func wmColorRGB(k *kit, value string) (render.Color, bool) {
	if c, ok := styling.ParseHex(value); ok {
		return c, true
	}
	c, ok := wmPalette(k)[value]
	return c, ok
}

// wmColorEditor is a lys-wm colour: the swatch, showing the colour a
// palette name stands for too, and the palette's names to pick from.
type wmColorEditor struct {
	*widget.Box
	slot   slot
	swatch *colorEditor
	names  *choiceEditor
}

func newWMColorEditor(k *kit, s slot) *wmColorEditor {
	shown := slot{
		get: func() any {
			if c, ok := wmColorRGB(k, s.text()); ok {
				return styling.HexRGBA(c)
			}
			return s.get()
		},
		set: s.set, unset: s.unset,
	}
	c := &wmColorEditor{Box: gapBox(widget.Row), slot: s, swatch: colorButton(k, shown)}
	c.names = newChoiceEditor(k, s, choiceSpec{list: func() []choice { return wmPaletteNames(k) }, free: true})
	c.AppendAligned(c.swatch, false, widget.AlignCenter)
	c.AppendAligned(c.names, false, widget.AlignCenter)
	c.refresh()
	return c
}

func (c *wmColorEditor) dirtyBadge() *widget.Badge { return c.names.dirtyBadge() }

func (c *wmColorEditor) refresh() {
	c.names.refresh()
	c.swatch.refresh()
}

func wmPaletteNames(k *kit) []choice {
	palette := wmPalette(k)
	out := make([]choice, 0, len(palette))
	for name, c := range palette {
		out = append(out, choice{value: name, label: name, group: styling.HexRGBA(c)})
	}
	return sortedChoices(out)
}

func audioSources() []choice {
	sources, ok := audioDaemonSources()
	if !ok {
		return nil
	}
	out := make([]choice, 0, len(sources))
	for _, s := range sources {
		out = append(out, choice{value: s.Name, label: cmp.Or(s.Description, s.Name)})
	}
	return out
}
