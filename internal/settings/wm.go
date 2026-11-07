package settings

import (
	"fmt"
	"log"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	wmToggleType = "WmToggle"
	wmToggleOn   = "on"
	wmToggleOff  = "off"
)

// wmBackend is lys-wm's config as the rows see it: the config file's keys, the
// runtime overrides over them, the edits still on their way to the overrides,
// and how the last change went. Edits leave the loop one batch at a time, since
// each is checked by lys-wm and reloaded.
type wmBackend struct {
	k         *kit
	file      *wmconfig.File
	schema    *wmconfig.Schema
	builtIn   map[string]any
	tree      map[string]any
	exists    bool
	overrides map[string]any
	pending   []wmconfig.Edit
	busy      bool
	// notice is what the status line says; class colors it.
	notice, class string
}

func (k *kit) wmBackend() (*wmBackend, error) {
	if k.wm != nil {
		k.wm.reread()
		return k.wm, nil
	}
	w, err := openWM(k, wmconfig.Exec)
	if err != nil {
		return nil, err
	}
	k.wm = w
	return w, nil
}

func openWM(k *kit, run wmconfig.Runner) (*wmBackend, error) {
	file, err := wmconfig.Open(os.Getenv, run)
	if err != nil {
		return nil, err
	}
	schema, err := file.Schema()
	if err != nil {
		return nil, err
	}
	data, err := file.BuiltIn()
	if err != nil {
		return nil, err
	}
	builtIn, err := wmconfig.Tree(data)
	if err != nil {
		return nil, fmt.Errorf("lys-wm's built-in config: %w", err)
	}
	w := &wmBackend{k: k, file: file, schema: schema, builtIn: builtIn}
	w.reread()
	return w, nil
}

func wmPath(path string) []string { return strings.Split(path, ".") }

func (w *wmBackend) reread() {
	tree, exists, err := w.file.Read()
	if err != nil {
		w.show(err.Error(), "error")
		return
	}
	overrides, err := w.file.Overrides()
	if err != nil {
		w.show(err.Error(), "error")
		return
	}
	w.tree, w.exists, w.overrides = tree, exists, overrides
	if err := w.file.Writable(); err != nil {
		w.show(err.Error(), "warning")
	}
}

func (w *wmBackend) show(notice, class string) { w.notice, w.class = notice, class }

func (w *wmBackend) fieldOf(path string) (wmconfig.Field, bool) {
	return w.schema.Field(wmPath(path))
}

func (w *wmBackend) field(path string) (config.FieldMeta, bool) {
	f, ok := w.fieldOf(path)
	if !ok {
		return config.FieldMeta{}, false
	}
	meta := config.FieldMeta{Min: f.Min, Max: f.Max, Variants: f.Variants, Optional: !f.HasDefault}
	switch f.Kind {
	case wmconfig.Bool:
		meta.Kind = config.FieldBool
		if meta.Optional {
			meta.Kind, meta.Type, meta.Variants = config.FieldEnum, wmToggleType, []string{wmToggleOn, wmToggleOff}
		}
	case wmconfig.Int:
		meta.Kind = config.FieldInt
	case wmconfig.Float:
		meta.Kind = config.FieldFloat
	case wmconfig.Text:
		meta.Kind, meta.Type = config.FieldText, f.Format
	case wmconfig.Enum:
		meta.Kind = config.FieldEnum
	case wmconfig.Composite:
		meta.Kind = config.FieldOther
	}
	return meta, true
}

// defaultOf is what the key means when no file says: the built-in config's
// value, else the schema's default.
func (w *wmBackend) defaultOf(path string) (any, bool) {
	if v, ok := wmconfig.Lookup(w.builtIn, wmPath(path)); ok {
		return v, true
	}
	if f, ok := w.fieldOf(path); ok && f.HasDefault {
		return f.Default, true
	}
	return nil, false
}

func (w *wmBackend) base() map[string]any {
	if w.exists {
		return w.tree
	}
	return w.builtIn
}

func (w *wmBackend) value(path string) any {
	keys := wmPath(path)
	tree := wmconfig.Merge(w.base(), w.overrides)
	for _, e := range slices.Backward(w.pending) {
		if slices.Equal(e.Path, keys) {
			if !e.Unset {
				return w.normal(path, e.Value)
			}
			tree = w.base()
			break
		}
	}
	if v, ok := wmconfig.Lookup(tree, keys); ok {
		return w.normal(path, v)
	}
	return w.fallback(path)
}

func (w *wmBackend) fallback(path string) any {
	f, ok := w.fieldOf(path)
	if ok && f.HasDefault {
		return f.Default
	}
	return nil
}

func (w *wmBackend) normal(path string, v any) any {
	if f, ok := w.fieldOf(path); ok {
		return f.Normal(v)
	}
	return v
}

func (w *wmBackend) slot(path string) slot {
	raw := slot{
		get:   func() any { return w.value(path) },
		set:   func(v any) error { return w.edit(wmconfig.Edit{Path: wmPath(path), Value: v}) },
		unset: func() { w.dropOverride(path) },
	}
	f, ok := w.fieldOf(path)
	if ok && f.Kind == wmconfig.Bool && !f.HasDefault {
		return wmToggleSlot(raw)
	}
	return raw
}

func wmToggleSlot(raw slot) slot {
	return slot{
		get: func() any {
			on, set := raw.get().(bool)
			switch {
			case !set:
				return nil
			case on:
				return wmToggleOn
			}
			return wmToggleOff
		},
		set:   func(v any) error { return raw.set(v == wmToggleOn) },
		unset: raw.unset,
	}
}

// reset puts the key back to its default: its override drops, unless the
// config file declares another value, which an override holding the default
// then covers.
func (w *wmBackend) reset(path string) {
	e := wmconfig.Edit{Path: wmPath(path), Unset: true}
	if d, ok := w.defaultOf(path); ok && !w.baseIsDefault(path) {
		e = wmconfig.Edit{Path: wmPath(path), Value: d}
	}
	if err := w.edit(e); err != nil {
		log.Printf("settings: reset %s: %v", path, err)
	}
}

// dropOverride drops the key's override, so the config file's value, else
// the built-in one, applies again.
func (w *wmBackend) dropOverride(path string) {
	if err := w.edit(wmconfig.Edit{Path: wmPath(path), Unset: true}); err != nil {
		log.Printf("settings: unset %s: %v", path, err)
	}
}

// baseIsDefault reports whether the key without its override holds its
// default.
func (w *wmBackend) baseIsDefault(path string) bool {
	v, ok := wmconfig.Lookup(w.base(), wmPath(path))
	if !ok {
		return true
	}
	d, _ := w.defaultOf(path)
	return reflect.DeepEqual(w.normal(path, v), w.normal(path, d))
}

// isDefault reports whether the key holds its default.
func (w *wmBackend) isDefault(path string) bool {
	d, ok := w.defaultOf(path)
	if !ok {
		return w.value(path) == nil
	}
	return reflect.DeepEqual(w.value(path), w.normal(path, d))
}

func (k *kit) resetAll() {
	k.store.resetAll()
	if k.wm != nil {
		k.wm.resetAll()
		return
	}
	if !wmconfig.Installed() {
		return
	}
	file, err := wmconfig.Open(os.Getenv, wmconfig.Exec)
	if err != nil {
		log.Printf("settings: reset-all: %v", err)
		return
	}
	async(k, func() error {
		_, err := file.ResetAll()
		return err
	}, func(err error) {
		if err != nil {
			log.Printf("settings: reset-all: %v", err)
		}
	})
}

func (w *wmBackend) resetAll() {
	keys := make(map[string]any, len(w.overrides)+len(w.pending))
	maps.Copy(keys, w.overrides)
	for _, e := range w.pending {
		keys[e.Path[0]] = nil
	}
	w.pending = wmconfig.ResetEdits(keys)
	w.flush()
}

// source is the row state of the key, the reset live while the key is not
// its default and something can bring it back.
func (w *wmBackend) source(path string) sourceInfo {
	info := w.badge(path)
	_, known := w.defaultOf(path)
	info.reset = !w.isDefault(path) && (known || wmconfig.Overridden(w.overrides, wmPath(path)))
	return info
}

func (w *wmBackend) badge(path string) sourceInfo {
	keys := wmPath(path)
	v, inConfig := wmconfig.Lookup(w.tree, keys)
	inConfig = inConfig && w.exists
	if wmconfig.Overridden(w.overrides, keys) {
		return runtimeSource(inConfig)
	}
	if !inConfig {
		return sourceInfo{}
	}
	if d, ok := w.defaultOf(path); ok && reflect.DeepEqual(w.normal(path, v), w.normal(path, d)) {
		return sourceInfo{}
	}
	t := locale.T()
	return sourceInfo{label: t.Get("settings-source-config"), tooltip: t.Attr("wm-source-file", "description"), class: "info", badge: true}
}

// edit queues a change; an edit of the same key still queued is replaced, so
// dragging a value through ten steps writes one file.
func (w *wmBackend) edit(e wmconfig.Edit) error {
	w.pending = slices.DeleteFunc(w.pending, func(old wmconfig.Edit) bool { return slices.Equal(old.Path, e.Path) })
	w.pending = append(w.pending, e)
	w.flush()
	return nil
}

func (w *wmBackend) flush() {
	if w.busy || len(w.pending) == 0 {
		return
	}
	w.busy = true
	batch := slices.Clone(w.pending)
	type applied struct {
		reload wmconfig.Reload
		err    error
	}
	async(w.k, func() applied {
		reload, err := w.file.Apply(batch)
		return applied{reload, err}
	}, func(r applied) {
		w.busy = false
		w.pending = slices.DeleteFunc(w.pending, func(e wmconfig.Edit) bool {
			return slices.ContainsFunc(batch, func(b wmconfig.Edit) bool { return reflect.DeepEqual(b, e) })
		})
		w.settled(r.reload, r.err)
		w.flush()
	})
}

func (w *wmBackend) settled(reload wmconfig.Reload, err error) {
	w.show("", "")
	w.reread()
	t := locale.T()
	switch {
	case err != nil:
		w.show(err.Error(), "error")
	case reload == wmconfig.NotRunning:
		w.show(t.Get("wm-status-not-running"), "info")
	}
	if w.k.onWMChange != nil {
		w.k.onWMChange()
	}
}

// wmStatus is the line over a lys-wm page saying why a change did not land, or
// that the file cannot take any.
type wmStatus struct {
	*widget.Label
	w *wmBackend
}

func newWMStatus(k *kit, w *wmBackend) *wmStatus {
	l := k.label("", "wm-status", css.Caption)
	l.SetWrap(true)
	s := &wmStatus{Label: l, w: w}
	s.refresh()
	return s
}

func (s *wmStatus) refresh() {
	s.SetText(s.w.notice)
	s.SetVisible(s.w.notice != "")
	s.RemoveClass(badgeStatuses...)
	if s.w.class != "" {
		s.AddClass(s.w.class)
	}
}

// wmKey is a key's label: `gaps.inner` reads wm-gaps-inner.
func wmKey(path string) string {
	return "wm-" + strings.NewReplacer(".", "-", "_", "-").Replace(path)
}

func wmField(path string, opts ...rowOpt) rowSpec {
	return field(path, append([]rowOpt{withKey(wmKey(path))}, opts...)...)
}

func wmFields(paths ...string) []rowSpec {
	rows := make([]rowSpec, len(paths))
	for i, p := range paths {
		rows[i] = wmField(p)
	}
	return rows
}

func wmPage(id, icon string, sections ...sectionSpec) pageSpec {
	return pageSpec{id: id, navKey: "settings-nav-" + id, icon: icon, header: "settings-page-" + id, wm: true, sections: sections}
}

// wmSections is the sidebar's lys-wm group.
func wmSections() []navSection {
	return []navSection{{key: "settings-nav-wm", pages: []pageSpec{
		wmPage("wm-windows", "ld-app-window-symbolic",
			sectionSpec{"wm-section-gaps", []rowSpec{wmField("gaps.inner"), wmSides("gaps.outer")}},
			sectionSpec{"wm-section-borders", []rowSpec{wmField("borders.width"), wmBorder("borders.active"), wmBorder("borders.inactive")}},
			sectionSpec{"wm-section-corners", wmFields("decoration.rounding", "decoration.active_opacity", "decoration.inactive_opacity", "decoration.fullscreen_opacity", "decoration.dim_inactive", "decoration.dim_strength")},
			sectionSpec{"wm-section-focus", wmFields("focus.on_activate")},
		),
		wmPage("wm-effects", "ld-droplets-symbolic",
			sectionSpec{"wm-section-shadow", wmFields("decoration.shadow.enabled", "decoration.shadow.range", "decoration.shadow.render_power", "decoration.shadow.sharp", "decoration.shadow.scale", "decoration.shadow.color", "decoration.shadow.color_inactive")},
			sectionSpec{"wm-section-blur", wmFields("decoration.blur.enabled", "decoration.blur.size", "decoration.blur.passes", "decoration.blur.xray", "decoration.blur.tint", "decoration.blur.saturation", "decoration.blur.brightness", "decoration.blur.contrast", "decoration.blur.grain")},
		),
		wmPage("wm-motion", "ld-film-symbolic",
			sectionSpec{"wm-section-animations", wmFields("animations.enabled", "animations.reduced_motion")},
			sectionSpec{"wm-section-overview", wmFields("overview.blur", "overview.dim")},
			sectionSpec{"wm-section-drag", wmFields("drag_preview.enabled", "drag_preview.opacity", "drag_preview.blur", "drag_preview.snap_distance")},
		),
		wmPage("wm-tiling", "ld-layout-grid-symbolic",
			sectionSpec{"wm-section-autotile", wmFields("autotile.enabled", "autotile.ephemeral", "autotile.min_trigger", "autotile.divisor")},
			sectionSpec{"wm-section-tabs", wmFields("tabs.height", "tabs.padding", "tabs.text_height", "tabs.text_padding", "tabs.font", "tabs.icon_theme")},
		),
		wmPage("wm-input", "ld-keyboard-symbolic",
			sectionSpec{"wm-section-keyboard", wmFields("input.keyboard.layout", "input.keyboard.variant", "input.keyboard.options", "input.keyboard.model", "input.keyboard.rules", "input.keyboard.repeat_rate", "input.keyboard.repeat_delay")},
			sectionSpec{"wm-section-pointer", wmFields("input.pointer.natural_scroll", "input.pointer.middle_emulation", "input.pointer.accel_profile", "input.pointer.accel_speed", "input.pointer.scroll_factor")},
			sectionSpec{"wm-section-touchpad", wmFields("input.touchpad.tap", "input.touchpad.drag_lock", "input.touchpad.natural_scroll", "input.touchpad.disable_while_typing", "input.touchpad.middle_emulation", "input.touchpad.click_method", "input.touchpad.scroll_method", "input.touchpad.accel_profile", "input.touchpad.accel_speed", "input.touchpad.scroll_factor")},
		),
		wmPage("wm-binds", "ld-key-round-symbolic",
			sectionSpec{"wm-section-binds", []rowSpec{wmObjects("binds", "chord", "action")}},
			sectionSpec{"wm-section-submaps", []rowSpec{wmSubmaps()}},
			sectionSpec{"wm-section-app-shortcuts", []rowSpec{appShortcutsRow()}},
			sectionSpec{"wm-section-gestures", []rowSpec{
				wmObjects("gestures.swipe", "fingers", "direction", "modifiers"), wmObjects("gestures.pinch", "fingers", "direction", "modifiers"),
				wmObjects("gestures.hold", "fingers", "modifiers"), wmField("gestures.distance"),
			}},
		),
		wmPage("wm-outputs", "ld-monitor-symbolic",
			sectionSpec{"wm-section-outputs", []rowSpec{wmObjects("outputs", "match")}},
			sectionSpec{"wm-section-display", wmFields("color_management", "tearing", "gpu", "lid_suspend")},
		),
		wmPage("wm-session", "ld-power-symbolic",
			sectionSpec{"wm-section-rules", []rowSpec{wmObjects("rules", "app_id", "title", "tag")}},
			sectionSpec{"wm-section-start", []rowSpec{wmField("autostart", stringList), wmField("env", stringMap)}},
			sectionSpec{"wm-section-xwayland", wmFields("xwayland.enabled", "xwayland.zero_scaling")},
			sectionSpec{"wm-section-palette", []rowSpec{wmPaletteRow()}},
		),
	}}}
}

// checker asks lys-wm whether it would take path set to a value
// over the overrides and the edits still queued, without writing it: the
// work runs off the loop and says why lys-wm would not.
func (w *wmBackend) checker(path string) func(v any) func() error {
	return func(v any) func() error {
		edit := wmconfig.Edit{Path: wmPath(path), Value: v}
		edits := append(slices.DeleteFunc(slices.Clone(w.pending), func(e wmconfig.Edit) bool { return slices.Equal(e.Path, edit.Path) }), edit)
		return func() error { return w.file.Check(edits) }
	}
}
