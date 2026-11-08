package settings

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"

	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

// lys-wm's lists of objects (outputs, rules, binds, gestures) edited as
// cards whose fields come from lys-wm's schema: a key of an item is the
// editor its type calls for, a nested object its keys in turn, a list of
// words a list editor.

// wmObjectSpec names a lys-wm list of objects: where its items' schema
// is, the label prefix its fields' keys share, and the keys whose values
// title a card.
type wmObjectSpec struct {
	schema []string
	labels string
	title  []string
}

// wmObjects is the row of the lys-wm list of objects at path.
func wmObjects(path string, title ...string) rowSpec {
	spec := wmObjectSpec{schema: wmPath(path), labels: wmPath(path)[0], title: title}
	return wmField(path, func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control {
			check := k.wm.checker(path)
			return newWMObjects(k, s, spec, func(items []any) func() error { return check(items) })
		}
	})
}

// newWMObjects is the cards of the list in s; check, nil for a list inside
// another's card, is asked before each write.
func newWMObjects(k *kit, s slot, spec wmObjectSpec, check func(items []any) func() error) *cardList {
	item := append(slices.Clone(spec.schema), wmconfig.Each)
	cards := cardSpec{
		check:      check,
		title:      func(item map[string]any, number int) string { return wmCardTitle(item, number, spec.title) },
		fields:     wmItemFields(k.wm, item, nil, spec.labels),
		blank:      func() map[string]any { return map[string]any{} },
		searchable: true,
		collapsed:  true,
	}
	if f, ok := k.wm.schema.Field(append(item, chordKey)); ok && f.Format == wmChordFormat {
		cards.clashKey = chordKey
		cards.keep = func(m map[string]any) bool { chord, _ := m[chordKey].(string); return chord != "" }
	}
	return newCardList(k, s, cards)
}

// wmCardTitle joins the values of the title keys an item sets, else
// numbers the card.
func wmCardTitle(item map[string]any, number int, keys []string) string {
	var parts []string
	for _, key := range keys {
		if v, ok := item[key]; ok && v != nil && v != "" {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	if len(parts) == 0 {
		return locale.T().Get("wm-card-number", i18n.Int("number", int64(number)))
	}
	return strings.Join(parts, " · ")
}

// wmItemFields is a card field per key of the object whose schema is at
// schema: within lies the keys from the item down to it, labels the
// prefix of the fields' label keys.
func wmItemFields(w *wmBackend, schema, within []string, labels string) []cardField {
	keys, ok := w.schema.Keys(schema)
	if !ok {
		log.Panicf("settings: lys-wm's %s holds no object", strings.Join(schema, "."))
	}
	var out []cardField
	for _, key := range keys {
		path := append(slices.Clone(schema), key)
		inner := append(slices.Clone(within), key)
		label := labels + "." + key
		if edit, ok := wmItemEditors[strings.Join(path, ".")]; ok {
			out = append(out, cardField{label: wmKey(label), fill: true, item: func(k *kit, field func(key string) slot) control {
				return edit(k, nestedSlot(field(inner[0]), inner[1:]), path)
			}})
			continue
		}
		f, _ := w.schema.Field(path)
		if f.Kind == wmconfig.Switch {
			fields := wmItemFields(w, path, nil, label)
			out = append(out, cardField{label: wmKey(label), fill: true, item: func(k *kit, field func(key string) slot) control {
				return newWMSwitch(k, nestedSlot(field(inner[0]), inner[1:]), fields)
			}})
			continue
		}
		if _, nested := w.schema.Keys(path); nested {
			out = append(out, wmItemFields(w, path, inner, label)...)
			continue
		}
		edit := wmLeafEditor(w, path)
		out = append(out, cardField{label: wmKey(label), fill: f.Kind == wmconfig.Text || f.Kind == wmconfig.Composite, item: func(k *kit, field func(key string) slot) control {
			return edit(k, nestedSlot(field(inner[0]), inner[1:]))
		}})
	}
	return out
}

// wmLeafEditor is the editor of a key of a lys-wm item: a list of
// words is a list editor, anything else the control its type calls for.
func wmLeafEditor(w *wmBackend, path []string) func(k *kit, s slot) control {
	name := strings.Join(path, ".")
	f, ok := w.schema.Field(path)
	if !ok {
		log.Panicf("settings: lys-wm has no %s", name)
	}
	if f.Kind == wmconfig.Composite {
		each := append(slices.Clone(path), wmconfig.Each)
		elem, ok := w.field(strings.Join(each, "."))
		words, _ := w.schema.Field(each)
		switch {
		case ok && words.Kind == wmconfig.Enum && !slices.ContainsFunc(words.Variants, isBool):
			return func(k *kit, s slot) control { return newListEditor(k, s, enumListSpec(elem)) }
		case ok && elem.Kind == config.FieldText:
			return func(k *kit, s slot) control { return newListEditor(k, s, stringListSpec()) }
		}
		log.Panicf("settings: lys-wm's %s needs its own editor", name)
	}
	meta, _ := w.field(name)
	return func(k *kit, s slot) control {
		c, err := autoEditor(k, wmChoiceSlot(f, s), name, meta)
		if err != nil {
			log.Panicf("settings: %s: %v", name, err)
		}
		return c
	}
}

// nestedSlot is the value under keys inside the mapping s holds: setting
// it writes the mapping with that value, unsetting drops the key and
// then the mapping once it holds nothing.
func nestedSlot(s slot, keys []string) slot {
	if len(keys) == 0 {
		return s
	}
	parent := nestedSlot(s, keys[:len(keys)-1])
	key := keys[len(keys)-1]
	current := func() map[string]any {
		m, _ := parent.get().(map[string]any)
		return maps.Clone(m)
	}
	return slot{
		get: func() any {
			m, _ := parent.get().(map[string]any)
			return m[key]
		},
		set: func(v any) error {
			m := current()
			if m == nil {
				m = map[string]any{}
			}
			m[key] = v
			return parent.set(m)
		},
		unset: func() {
			m := current()
			delete(m, key)
			if len(m) == 0 {
				parent.unset()
				return
			}
			_ = parent.set(m)
		},
	}
}

// wmSubmaps is the submaps row: a card per named set of binds, its name
// and its binds, the sets in name order.
func wmSubmaps() rowSpec {
	binds := wmObjectSpec{schema: []string{"submaps", wmconfig.Each}, labels: "binds", title: []string{"chord", "action"}}
	spec := cardSpec{
		identity: &cardField{key: "name", label: "wm-submaps-name", editor: func(k *kit, s slot) control { return newText(k, s, false) }},
		fields: []cardField{{key: "binds", label: "wm-binds", fill: true, editor: func(k *kit, s slot) control {
			return newWMObjects(k, s, binds, nil)
		}}},
		blank:  func() map[string]any { return map[string]any{"name": "", "binds": []any{}} },
		keep:   func(item map[string]any) bool { name, _ := item["name"].(string); return name != "" },
		unique: "name",
	}
	return wmField("submaps", func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control {
			check := k.wm.checker("submaps")
			spec := spec
			spec.check = func(items []any) func() error { return check(namedMap(items)) }
			return newCardList(k, namedLists(s), spec)
		}
	})
}

// namedLists shows a mapping of name to list as a list of {name, binds}
// items in name order, and writes such items back as the mapping.
func namedLists(s slot) slot {
	return slot{
		get: func() any {
			m, _ := s.get().(map[string]any)
			out := make([]any, 0, len(m))
			for _, name := range slices.Sorted(maps.Keys(m)) {
				out = append(out, map[string]any{"name": name, "binds": m[name]})
			}
			return out
		},
		set: func(v any) error {
			items, _ := v.([]any)
			return s.set(namedMap(items))
		},
		unset: s.unset,
	}
}

// namedMap is {name, binds} items as the mapping of name to binds.
func namedMap(items []any) map[string]any {
	out := make(map[string]any, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		name, _ := m["name"].(string)
		binds := m["binds"]
		if binds == nil {
			binds = []any{}
		}
		out[name] = binds
	}
	return out
}
