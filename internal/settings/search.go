package settings

import (
	"cmp"
	"slices"
	"strings"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	searchLimit       = 50
	searchPage        = "search"
	searchLabelWeight = 1 << 30
	searchPlaceWeight = 1 << 29
	searchDescWeight  = 1 << 28
)

// settingEntry is one row of the layout as the search finds it: the page
// it is on, its place among that page's rows, and its localized texts.
type settingEntry struct {
	page        string
	row         int
	label       string
	description string
	place       string
	order       int
}

type searchHit struct {
	entry settingEntry
	score int
}

// settingIndex lists every row of every page, in sidebar order.
func settingIndex(sections []navSection) []settingEntry {
	t := locale.T()
	var out []settingEntry
	for _, sec := range sections {
		group := t.Get(sec.key)
		for _, p := range sec.pages {
			place := group + " › " + t.Get(p.navKey)
			row := 0
			for _, s := range p.sections {
				for _, r := range s.rows {
					key := r.labelKey()
					out = append(out, settingEntry{
						page: p.id, row: row, label: t.Get(key), description: description(key),
						place: place + " › " + t.Get(s.title), order: len(out),
					})
					row++
				}
			}
		}
	}
	return out
}

// searchSettings ranks the entries every word of query matches: a word
// matching the label fuzzily outranks one found in the page or section
// name, which outranks one found in the description.
func searchSettings(index []settingEntry, query string) []settingEntry {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var hits []searchHit
	for _, e := range index {
		if score, ok := entryScore(e, words); ok {
			hits = append(hits, searchHit{e, score})
		}
	}
	slices.SortStableFunc(hits, func(a, b searchHit) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.entry.order, b.entry.order))
	})
	out := make([]settingEntry, 0, min(len(hits), searchLimit))
	for _, h := range hits[:min(len(hits), searchLimit)] {
		out = append(out, h.entry)
	}
	return out
}

func entryScore(e settingEntry, words []string) (int, bool) {
	place, desc := strings.ToLower(e.place), strings.ToLower(e.description)
	total := 0
	for _, w := range words {
		switch s, ok := widget.FuzzyScore(w, e.label); {
		case ok:
			total += searchLabelWeight + s
		case strings.Contains(place, w):
			total += searchPlaceWeight
		case strings.Contains(desc, w):
			total += searchDescWeight
		default:
			return 0, false
		}
	}
	return total, true
}

// searchResults is the page a search shows in the content area: one
// activatable row per hit, opening its setting.
type searchResults struct {
	*widget.Scroll
	k      *kit
	list   *widget.Box
	empty  *widget.Label
	open   func(settingEntry)
	hits   []settingEntry
	button []*widget.Button
}

func newSearchResults(k *kit, open func(settingEntry)) *searchResults {
	th := widget.Current()
	r := &searchResults{k: k, open: open}
	r.list = widget.NewBox(widget.Column, 0, 0)
	r.list.AddClass(css.Card, "settings-group", "search-results")
	r.empty = k.label(locale.T().Get("settings-search-empty"), "search-empty", css.DimLabel)
	content := widget.NewBox(widget.Column, 3*th.Spacing, 3*th.Padding)
	content.AddClass("settings-page")
	content.Append(k.label(locale.T().Get("settings-search-title"), "settings-page-title", css.H4), false)
	content.Append(r.empty, false)
	content.Append(r.list, false)
	r.Scroll = widget.NewScroll(k.pageColumn(content))
	r.VerticalOnly, r.ShowBars, r.FillY = true, true, true
	return r
}

// show lists hits.
func (r *searchResults) show(hits []settingEntry) {
	r.hits = hits
	r.list.Clear()
	r.button = r.button[:0]
	r.empty.SetVisible(len(hits) == 0)
	r.list.SetVisible(len(hits) > 0)
	th := widget.Current()
	for i, h := range hits {
		if i > 0 {
			r.list.Append(widget.NewSeparator(widget.Horizontal), false)
		}
		text := widget.NewBox(widget.Column, th.Spacing/4, 0)
		text.SetPadding(render.Insets{Top: th.Padding, Right: 2 * th.Padding, Bottom: th.Padding, Left: 2 * th.Padding})
		text.Append(r.k.label(h.label, "setting-label"), false)
		text.Append(r.k.label(h.place, "search-result-place", css.Caption, css.DimLabel), false)
		if h.description != "" {
			d := r.k.label(h.description, "setting-description", css.Caption, css.DimLabel)
			d.SetEllipsize(widget.EllipsizeEnd)
			text.Append(d, false)
		}
		b := r.k.button(text, func() { r.open(h) }, css.Flat, "search-result")
		b.SetHAlign(widget.AlignFill)
		r.list.Append(b, false)
		r.button = append(r.button, b)
	}
	r.SetOffset(0, 0)
}

// first is the best hit, false while there is none.
func (r *searchResults) first() (settingEntry, bool) {
	if len(r.hits) == 0 {
		return settingEntry{}, false
	}
	return r.hits[0], true
}
