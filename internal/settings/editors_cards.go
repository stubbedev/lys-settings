package settings

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// The card lists (card_form, threshold_list, the preset and override
// lists): one card per list item, its fields the ordinary editors over
// slots into the item, a remove button in the card's header and an
// add button below.

// cardField is one field of a card: the item key, its label key, and
// the editor over the field's slot; or, for a control over several
// keys of the item, item over the item's field slots. fill gives the
// control the row's spare width instead of the label.
type cardField struct {
	key, label string
	editor     func(k *kit, s slot) control
	item       func(k *kit, field func(key string) slot) control
	fill       bool
}

// cardChrome is a card list's own card markup: the card, header and
// body classes and the remove button's (card_form's by default).
type cardChrome struct {
	card, header, body string
	remove             []string
	// removeKey is the remove button's tooltip.
	removeKey string
}

var cardFormChrome = cardChrome{
	card: "card-form-card", header: "card-form-header", body: "card-form-body",
	remove: []string{"list-control-button", "list-control-remove", css.Ghost}, removeKey: "settings-list-remove",
}

// cardSpec shapes a card list: the card's header (a title for the
// item, or an identity field beside its label), its body fields, the
// new item, what of the cards is stored (keep filters, clean tidies
// each written item), the key duplicates of which are an error, and
// the container classes.
type cardSpec struct {
	title    func(item map[string]any, number int) string
	identity *cardField
	fields   []cardField
	blank    func() map[string]any
	keep     func(item map[string]any) bool
	clean    func(item map[string]any) map[string]any
	unique   string
	clashKey string
	// searchable heads the cards with a filter over the items' values.
	searchable bool
	// collapsed shows each card as its header alone until it is opened,
	// building its fields then: a long list stays a list of titles.
	collapsed bool
	// check, when set, is asked about every write before it is made: it
	// returns, on the loop, the work that says off it why the target would
	// refuse the items. A refused write is held as a draft, shown with the
	// reason and written once the cards make one the target takes.
	check func(items []any) func() error
	// addKey labels the add button's tooltip (settings-list-add).
	addKey   string
	chrome   *cardChrome
	classes  []string
	listCls  string
	rowClass string
	labelCls string
}

// cardList holds its cards as the source of truth (local), writing the
// kept, cleaned items; an outside change replaces them.
type cardList struct {
	*widget.Box
	k     *kit
	slot  slot
	spec  cardSpec
	list  *widget.Box
	local []map[string]any
	cards []*card
	// filter narrows the cards shown to those matching it; nil when the
	// list is not searchable.
	filter *widget.SearchEntry
	// draft says why the cards are not written while the target would
	// refuse them; drafted is the stored value they were drafted over and
	// checks counts the checks asked, so only the latest answer applies.
	draft    *widget.Label
	drafted  any
	drafting bool
	checks   int
	partners []int
}

type card struct {
	root     *widget.Box
	title    *widget.Label
	identity control
	controls []control
	clash    *widget.Box
	clashMsg *widget.Label
}

func newCardList(k *kit, s slot, spec cardSpec) *cardList {
	c := &cardList{Box: gapBox(widget.Column), k: k, slot: s, spec: spec}
	c.AddClass(append([]string{"card-form-editor"}, spec.classes...)...)
	if spec.searchable {
		c.filter = widget.NewSearchEntry(k.face, k.textPx(), locale.T().Get("settings-list-filter"))
		c.filter.SetDelay(0)
		c.filter.OnSearchChanged = func(string) { c.applyFilter() }
		c.Append(c.filter, false)
	}
	c.list = gapBox(widget.Column)
	c.list.AddClass(spec.listCls)
	c.Append(c.list, false)
	c.draft = k.label("", "setting-problem", css.Caption, css.Error)
	c.draft.SetWrap(true)
	c.draft.SetVisible(false)
	addKey := spec.addKey
	if addKey == "" {
		addKey = "settings-list-add"
	}
	add := listButton(k, "ld-plus-symbolic", addKey, "list-control-add", func() {
		c.local = append(c.local, spec.blank())
		c.rebuild()
		c.commit()
	})
	c.AppendAligned(add, false, widget.AlignStart)
	c.Append(c.draft, false)
	c.local = c.stored()
	c.rebuild()
	return c
}

// stored are the slot's items, plain.
func (c *cardList) stored() []map[string]any {
	v, _ := c.slot.get().([]any)
	out := make([]map[string]any, 0, len(v))
	for _, it := range v {
		m, _ := it.(map[string]any)
		out = append(out, maps.Clone(m))
	}
	return out
}

// written is what the cards store: the kept items, cleaned.
func (c *cardList) written() []any {
	out := make([]any, 0, len(c.local))
	for _, m := range c.local {
		if c.spec.keep != nil && !c.spec.keep(m) {
			continue
		}
		if c.spec.clean != nil {
			m = c.spec.clean(maps.Clone(m))
		}
		out = append(out, m)
	}
	return out
}

func (c *cardList) commit() {
	c.validate()
	items := c.written()
	if c.spec.check == nil {
		c.write(items)
		return
	}
	c.checks++
	asked := c.checks
	async(c.k, c.spec.check(items), func(err error) {
		if asked != c.checks {
			return
		}
		if err != nil {
			c.hold(err)
			return
		}
		c.write(items)
	})
}

func (c *cardList) write(items []any) {
	c.drafting = false
	c.draft.SetVisible(false)
	_ = c.slot.set(items)
}

// discardDraft drops the draft and any answer still on its way, so the
// next refresh shows what is stored.
func (c *cardList) discardDraft() {
	c.checks++
	c.drafting = false
	c.draft.SetVisible(false)
	if c.spec.keep != nil && slices.ContainsFunc(c.local, func(m map[string]any) bool { return !c.spec.keep(m) }) {
		c.local = slices.DeleteFunc(c.local, func(m map[string]any) bool { return !c.spec.keep(m) })
		c.rebuild()
	}
}

// hold keeps the cards as a draft over what is stored, saying why the
// target refuses them.
func (c *cardList) hold(err error) {
	c.drafting, c.drafted = true, c.slot.get()
	c.draft.SetText(locale.T().Get("settings-list-draft", i18n.Str("reason", strings.TrimSpace(err.Error()))))
	c.draft.SetVisible(true)
}

// fieldSlot is the slot of one key of card i: unset removes the key.
func (c *cardList) fieldSlot(i int, key string) slot {
	write := func(v any, remove bool) {
		if i >= len(c.local) {
			return
		}
		if c.local[i] == nil {
			c.local[i] = map[string]any{}
		}
		if remove {
			delete(c.local[i], key)
		} else {
			c.local[i][key] = v
		}
		c.commit()
		c.retitle(i)
	}
	return slot{
		get: func() any {
			if i < len(c.local) {
				return c.local[i][key]
			}
			return nil
		},
		set:   func(v any) error { write(v, false); return nil },
		unset: func() { write(nil, true) },
	}
}

func (c *cardList) rebuild() {
	c.list.Clear()
	c.cards = nil
	for i := range c.local {
		c.appendCard(i)
	}
	c.validate()
	c.applyFilter()
}

// applyFilter shows the cards with a value containing every word of the
// filter, all of them while it is empty.
func (c *cardList) applyFilter() {
	if c.filter == nil {
		return
	}
	c.filter.SetVisible(len(c.cards) > 0)
	words := strings.Fields(strings.ToLower(c.filter.Text()))
	for i, cd := range c.cards {
		text := strings.ToLower(itemText(c.local[i]))
		cd.root.SetVisible(!slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }))
	}
}

// itemText is every value of an item, nested ones included, in key order.
func itemText(v any) string {
	switch v := v.(type) {
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range slices.Sorted(maps.Keys(v)) {
			parts = append(parts, itemText(v[key]))
		}
		return strings.Join(parts, " ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, it := range v {
			parts = append(parts, itemText(it))
		}
		return strings.Join(parts, " ")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// appendCard is card or card_titled with the fields in its body, every
// field label of the card, the identity's too, sized alike so the
// controls line up.
func (c *cardList) appendCard(i int) {
	t := locale.T()
	chrome := cardFormChrome
	if c.spec.chrome != nil {
		chrome = *c.spec.chrome
	}
	labels := widget.NewSizeGroup(widget.SizeGroupHorizontal)
	formLabel := func(key string) widget.Widget {
		return labels.Add(c.k.label(t.Get(key), c.spec.labelCls, css.CaptionHeading, css.DimLabel))
	}
	header := gapBox(widget.Row)
	header.AddClass(chrome.header)
	cd := &card{}
	if id := c.spec.identity; id != nil {
		header.AppendAligned(formLabel(id.label), false, widget.AlignCenter)
		cd.identity = id.editor(c.k, c.fieldSlot(i, id.key))
		header.AppendAligned(cd.identity, true, widget.AlignCenter)
	} else {
		cd.title = c.k.label("", "card-form-title", css.Heading)
		header.AppendAligned(cd.title, true, widget.AlignCenter)
	}
	remove := c.k.iconButton("ld-trash-2-symbolic", chrome.removeKey, func() {
		if i < len(c.local) {
			c.local = append(c.local[:i:i], c.local[i+1:]...)
			c.rebuild()
			c.commit()
		}
	}, chrome.remove...)
	body := gapBox(widget.Column)
	body.AddClass(chrome.body)
	fill := func() {
		for _, f := range c.spec.fields {
			var ctl control
			if f.item != nil {
				ctl = f.item(c.k, func(key string) slot { return c.fieldSlot(i, key) })
			} else {
				ctl = f.editor(c.k, c.fieldSlot(i, f.key))
			}
			row := gapBox(widget.Row)
			row.AddClass(c.spec.rowClass)
			row.AppendAligned(formLabel(f.label), !f.fill, widget.AlignCenter)
			row.AppendAligned(ctl, f.fill, widget.AlignCenter)
			body.Append(row, false)
			cd.controls = append(cd.controls, ctl)
		}
	}
	if !c.spec.collapsed {
		header.AppendAligned(remove, false, widget.AlignCenter)
		fill()
		cd.root = sectionCard(header, body, chrome.card)
		c.addClash(cd, i)
		c.list.Append(cd.root, false)
		c.cards = append(c.cards, cd)
		c.retitle(i)
		return
	}
	cd.root = sectionCard(header, body, chrome.card)
	sep, _ := cd.root.Children()[1].(*widget.Separator)
	c.addClash(cd, i)
	show := func(open bool) {
		sep.SetVisible(open)
		body.SetVisible(open)
	}
	var disclose *widget.Button
	disclose = c.k.iconButton("ld-chevron-down-symbolic", "settings-card-expand", func() {
		open := !body.Visible()
		if open && len(cd.controls) == 0 {
			fill()
		}
		show(open)
		tip := "settings-card-expand"
		if open {
			tip = "settings-card-collapse"
		}
		disclose.SetTooltip(t.Get(tip))
	}, "card-disclose", css.Ghost)
	header.AppendAligned(disclose, false, widget.AlignCenter)
	header.AppendAligned(remove, false, widget.AlignCenter)
	show(false)
	c.list.Append(cd.root, false)
	c.cards = append(c.cards, cd)
	c.retitle(i)
}

// sectionCard is a list's card: lys-ui's card, header over body split
// by a separator, each inset a padding from the card's edges and two
// from its sides.
func sectionCard(header, body *widget.Box, class string) *widget.Box {
	th := widget.Current()
	band := render.Insets{Top: th.Padding, Right: 2 * th.Padding, Bottom: th.Padding, Left: 2 * th.Padding}
	header.SetPadding(band)
	body.SetPadding(band)
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass(css.Card, class)
	root.Append(header, false)
	root.Append(widget.NewSeparator(widget.Horizontal), false)
	root.Append(body, false)
	return root
}

func (c *cardList) retitle(i int) {
	if i < len(c.cards) && c.cards[i].title != nil && c.spec.title != nil {
		c.cards[i].title.SetText(c.spec.title(c.local[i], i+1))
	}
}

// validate marks the identity of every card whose unique key repeats
// another's (the duplicate preset id): the error class and tooltip.
func (c *cardList) validate() {
	c.markClashes()
	if c.spec.unique == "" {
		return
	}
	counts := map[string]int{}
	for _, m := range c.local {
		if s, _ := m[c.spec.unique].(string); s != "" {
			counts[s]++
		}
	}
	for i, cd := range c.cards {
		w, ok := cd.identity.(interface {
			AddClass(...string)
			RemoveClass(...string)
			SetTooltip(string)
		})
		if !ok || i >= len(c.local) {
			continue
		}
		s, _ := c.local[i][c.spec.unique].(string)
		if s != "" && counts[s] > 1 {
			w.AddClass("error")
			w.SetTooltip(locale.T().Get("settings-toast-preset-id-duplicate"))
		} else {
			w.RemoveClass("error")
			w.SetTooltip("")
		}
	}
}

// refresh follows the store (the watcher): with as many items as the
// cards keep, those cards take the stored values in place; with
// another count the cards are replaced.
func (c *cardList) refresh() {
	if c.drafting && reflect.DeepEqual(c.slot.get(), c.drafted) {
		return
	}
	c.drafting = false
	c.draft.SetVisible(false)
	stored := c.stored()
	var kept []int
	for i, m := range c.local {
		if c.spec.keep == nil || c.spec.keep(m) {
			kept = append(kept, i)
		}
	}
	if len(kept) != len(stored) {
		c.local = stored
		c.rebuild()
		return
	}
	for j, i := range kept {
		c.local[i] = stored[j]
	}
	for i, cd := range c.cards {
		if cd.identity != nil {
			cd.identity.refresh()
		}
		for _, ctl := range cd.controls {
			ctl.refresh()
		}
		c.retitle(i)
	}
	c.validate()
}

// cardRow makes a full-width row with the card list spec builds.
func cardRow(spec cardSpec) rowOpt {
	return func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newCardList(k, s, spec) }
	}
}

// Threshold bounds (threshold_list: MAX_THRESHOLD, THRESHOLD_FALLBACK).
const (
	maxThreshold      = 100_000
	thresholdFallback = 50
)

// optionalColor is color_value_widget: the color-value control over an
// optional color, an unset one showing as auto and every pick setting
// it.
func optionalColor(k *kit, s slot) control {
	return newColorValueEditor(k, slot{
		get: func() any {
			if v := s.get(); v != nil {
				return v
			}
			return colorAuto
		},
		set: s.set, unset: s.unset,
	})
}

// thresholdList is threshold_list::threshold_list.
var thresholdList = cardRow(cardSpec{
	classes: []string{"threshold-editor"}, listCls: "threshold-list", rowClass: "threshold-row", labelCls: "threshold-label",
	blank: func() map[string]any { return map[string]any{} },
	title: thresholdTitle,
	fields: []cardField{
		{key: "above", label: "settings-threshold-above", editor: thresholdBound},
		{key: "below", label: "settings-threshold-below", editor: thresholdBound},
		{key: "icon-color", label: "settings-threshold-icon-color", editor: optionalColor},
		{key: "label-color", label: "settings-threshold-label-color", editor: optionalColor},
		{key: "icon-bg-color", label: "settings-threshold-icon-bg-color", editor: optionalColor},
		{key: "button-bg-color", label: "settings-threshold-button-bg-color", editor: optionalColor},
		{key: "border-color", label: "settings-threshold-border-color", editor: optionalColor},
	},
})

// thresholdBound is the override switch and spin over an optional
// bound (optional_number_f64_widget at whole numbers).
func thresholdBound(k *kit, s slot) control {
	return newOptionalNumber(k, s, 0, maxThreshold, 1, thresholdFallback, 0)
}

// thresholdTitle is card_title: the range the entry covers, or its
// number.
func thresholdTitle(item map[string]any, number int) string {
	num := func(v any) (string, bool) {
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case int64:
			f = float64(n)
		default:
			return "", false
		}
		if f == math.Trunc(f) {
			return strconv.FormatInt(int64(f), 10), true
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	above, hasAbove := num(item["above"])
	below, hasBelow := num(item["below"])
	switch {
	case hasAbove && hasBelow:
		return above + " – " + below
	case hasAbove:
		return "≥ " + above
	case hasBelow:
		return "≤ " + below
	}
	return locale.T().Get("settings-threshold-card-title") + " " + strconv.Itoa(number)
}

// liveText is card_form::entry: an entry writing as it is typed.
type liveText struct {
	*widget.Entry
	slot    slot
	syncing bool
}

func newLiveText(k *kit, s slot, placeholder string) *liveText {
	c := &liveText{Entry: k.entry(), slot: s}
	c.SetPlaceholder(placeholder)
	c.OnChanged = func(text string) {
		if !c.syncing {
			_ = c.slot.set(text)
		}
	}
	c.refresh()
	return c
}

func (c *liveText) refresh() {
	if text := c.slot.text(); text != c.Text() {
		c.syncing = true
		c.SetText(text)
		c.syncing = false
	}
}

// toastPresetList is toast_preset_list: a card per preset with its id
// in the header (a duplicate one flagged), the label and the icon; a
// preset without an id is not stored, an empty label or icon is none.
var toastPresetList = cardRow(cardSpec{
	listCls: "card-form-list", rowClass: "card-form-row", labelCls: "card-form-label", unique: "id",
	identity: &cardField{key: "id", label: "settings-toast-preset-id", editor: func(k *kit, s slot) control { return newLiveText(k, s, "id") }},
	fields: []cardField{
		{key: "label", label: "settings-toast-preset-label", editor: func(k *kit, s slot) control { return newLiveText(k, s, "label") }},
		{key: "icon", label: "settings-toast-preset-icon", editor: func(k *kit, s slot) control { return newIconEditor(k, s) }},
	},
	blank: func() map[string]any { return map[string]any{"id": ""} },
	keep:  func(m map[string]any) bool { id, _ := m["id"].(string); return id != "" },
	clean: func(m map[string]any) map[string]any {
		for _, key := range []string{"label", "icon"} {
			if s, _ := m[key].(string); s == "" {
				delete(m, key)
			}
		}
		return m
	},
})

func (c *cardList) addClash(cd *card, i int) {
	if c.spec.clashKey == "" {
		return
	}
	th := widget.Current()
	cd.clash = gapBox(widget.Row)
	cd.clash.AddClass("card-clash")
	cd.clash.SetPadding(render.Insets{Top: th.Padding, Right: 2 * th.Padding, Bottom: th.Padding, Left: 2 * th.Padding})
	cd.clashMsg = c.k.label("", "setting-problem", css.Caption, css.Warning)
	cd.clashMsg.SetWrap(true)
	cd.clash.AppendAligned(cd.clashMsg, true, widget.AlignCenter)
	cd.clash.AppendAligned(c.k.button(c.k.label(locale.T().Get("settings-clash-replace")), func() { c.replace(i) }, css.Small), false, widget.AlignCenter)
	cd.clash.SetVisible(false)
	cd.root.Append(cd.clash, false)
}

func (c *cardList) markClashes() {
	if c.spec.clashKey == "" {
		return
	}
	values := make([]string, len(c.local))
	for i, m := range c.local {
		values[i], _ = m[c.spec.clashKey].(string)
	}
	c.partners = chordClashes(values, normalChord)
	t := locale.T()
	for i, cd := range c.cards {
		if cd.clash == nil || i >= len(c.partners) {
			continue
		}
		p := c.partners[i]
		cd.clash.SetVisible(p >= 0)
		if p >= 0 {
			cd.clashMsg.SetText(t.Get("settings-clash", i18n.Str("value", values[i]), i18n.Str("other", c.spec.title(c.local[p], p+1))))
		}
	}
}

func (c *cardList) replace(i int) {
	if i >= len(c.partners) || c.partners[i] < 0 {
		return
	}
	p := c.partners[i]
	delete(c.local[p], c.spec.clashKey)
	if p < len(c.cards) {
		for _, ctl := range c.cards[p].controls {
			ctl.refresh()
		}
	}
	c.retitle(p)
	c.commit()
}
