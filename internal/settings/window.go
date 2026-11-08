package settings

import (
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// Window geometry and the page switch (app/mod.rs, app/window.rs).
const (
	defaultSidebarWidth = 220
	// minSidebarWidth is the narrowest the sidebar drags to.
	minSidebarWidth = 160
	windowWidth     = 900
	windowHeight    = 650
	pageTransition  = 100 * time.Millisecond
	// pageCleanup is how long after a switch the old page is dropped:
	// the transition plus a buffer (TRANSITION_CLEANUP_BUFFER_MS).
	pageCleanup = pageTransition + 50*time.Millisecond
	// maxSidebarRem caps the sidebar drag (MAX_SIDEBAR_REM).
	maxSidebarRem   = 25.0
	basePxPerRem    = 16.0
	revealHighlight = 2 * time.Second
)

// window is SettingsApp's widget tree: the sidebar beside the page
// stack, the close button over the pages.
type window struct {
	k        *kit
	root     *widget.Box
	sidebar  *widget.Sidebar
	stack    *widget.Stack
	sections []navSection
	page     *settingsPage
	pageID   string
	serial   int
	// names are the pages in the stack, the current one last.
	names   []string
	pages   map[string]*settingsPage
	search  *widget.SearchEntry
	results *searchResults
	index   []settingEntry
	// focus moves the keyboard focus to a widget of the window; nil
	// before the window maps.
	focus func(widget.Widget)
	// after runs fn on the loop once d passed (the old page's
	// removal).
	after func(d time.Duration, fn func())
	// onClose closes the window; onResetAll asks before every runtime
	// override is dropped.
	onClose, onResetAll func()
	pending             launch
	presented           bool
}

func newWindow(k *kit, cfg *config.Config, after func(time.Duration, func())) *window {
	w := &window{k: k, sections: layout(cfg), after: after, pages: map[string]*settingsPage{}}
	w.index = settingIndex(w.sections)
	w.stack = widget.NewStack()
	w.stack.SetTransition(widget.StackCrossfade, pageTransition)
	w.results = newSearchResults(k, w.jump)
	w.stack.Add(searchPage, w.results)
	first := firstPage(w.sections)

	t := locale.T()
	w.search = widget.NewSearchEntry(k.face, k.textPx(), t.Get("settings-search-placeholder"))
	w.search.AddClass("settings-search")
	w.search.SetDelay(0)
	w.search.OnSearchChanged = w.filter
	w.search.OnActivate = func(string) {
		if hit, ok := w.results.first(); ok {
			w.jump(hit)
		}
	}
	closeBtn := k.iconButton("ld-x-symbolic", "settings-close", func() {
		if w.onClose != nil {
			w.onClose()
		}
	}, "settings-close", css.Ghost)
	th := widget.Current()
	closeBtn.SetMargin(render.Insets{Top: 2 * th.Padding, Right: 2 * th.Padding})
	content := widget.NewOverlay().Append(w.stack).AppendAligned(closeBtn, widget.AlignEnd, widget.AlignStart)

	w.sidebar = newSidebar(k, w.sections, first, content, w.search, w.navigate, func() {
		if w.onResetAll != nil {
			w.onResetAll()
		}
	})
	w.sidebar.SetWidth(defaultSidebarWidth)
	w.sidebar.SetResizable(minSidebarWidth, w.sidebarLimit())
	w.root = widget.NewBox(widget.Column, 0, 0)
	w.root.SetElement("window")
	w.root.AddClass("settings-window")
	w.root.Append(w.sidebar, true)
	if first != "" {
		w.show(first)
	}
	return w
}

// sidebarLimit is setup_paned_clamp's bound on the sidebar's width:
// 25rem at the current styling scale.
func (w *window) sidebarLimit() int {
	return int(float64(maxSidebarRem*basePxPerRem*float64(w.k.store.svc.Config().Styling.Scale)) + 0.5)
}

// show builds the page fresh and cross-fades to it (show_page); the
// previous page is dropped once the transition ended.
func (w *window) show(id string) {
	if id == w.pageID {
		return
	}
	spec, ok := pageByID(w.sections, id)
	if !ok {
		return
	}
	w.serial++
	name := id + "#" + strconv.Itoa(w.serial)
	w.page, w.pageID = buildPage(w.k, spec), id
	w.pages[name] = w.page
	w.stack.Add(name, w.page)
	w.stack.Show(name)
	w.names = append(w.names, name)
	w.after(pageCleanup, func() { w.dropStalePages(name) })
}

// dropStalePages removes every page but the one named keep, unless a
// later switch made another current.
func (w *window) dropStalePages(keep string) {
	if w.stack.Visible() != keep {
		return
	}
	for _, name := range w.names {
		if name != keep {
			w.stack.Remove(name)
			w.pages[name].close()
			delete(w.pages, name)
		}
	}
	w.names = []string{keep}
}

// navigate is a sidebar pick: it ends a search and shows the page.
func (w *window) navigate(id string) {
	w.search.SetText("")
	w.show(id)
}

// filter shows the settings matching query in place of the page, and
// the page again once the query is empty.
func (w *window) filter(query string) {
	if strings.TrimSpace(query) == "" {
		if len(w.names) > 0 {
			w.stack.Show(w.names[len(w.names)-1])
		}
		return
	}
	w.results.show(searchSettings(w.index, query))
	w.stack.Show(searchPage)
}

// jump opens the page a search hit is on, scrolls its row into view and
// gives its control the keys.
func (w *window) jump(hit settingEntry) {
	w.search.SetText("")
	w.openPage(hit.page)
	if w.pageID != hit.page {
		return
	}
	if row := w.page.reveal(hit.row, w.focus); row != nil {
		row.title.AddClass(css.Accent)
		w.after(revealHighlight, func() { row.title.RemoveClass(css.Accent) })
	}
}

func (w *window) openSetting(path string) {
	if hit, ok := findSetting(w.index, path); ok {
		w.jump(hit)
	}
}

// focusSearch gives the search field the keys, selecting what it holds.
func (w *window) focusSearch() {
	if w.focus != nil {
		w.focus(w.search)
	}
	w.search.SelectAll()
}

// openPage shows the page with this id and marks it in the sidebar; an
// id the layout does not hold (a forwarded launch from another version)
// changes nothing.
func (w *window) openPage(id string) {
	if _, ok := pageByID(w.sections, id); !ok {
		return
	}
	w.sidebar.SetActive(id)
	w.show(id)
}

// refresh re-reads the open page and the sidebar cap after a config
// change.
func (w *window) refresh() {
	limit := w.sidebarLimit()
	w.sidebar.SetResizable(minSidebarWidth, limit)
	if w.sidebar.Width() > limit {
		w.sidebar.SetWidth(limit)
	}
	if w.page != nil {
		w.page.refresh()
	}
}

func (w *window) open(l launch) {
	if l.setting != "" {
		w.openSetting(l.setting)
		return
	}
	w.openPage(l.page)
}

func (w *window) pageOf(l launch) string {
	if hit, ok := findSetting(w.index, l.setting); ok {
		return hit.page
	}
	return l.page
}

func (w *window) request(l launch) {
	if l == (launch{}) {
		return
	}
	if !w.presented {
		w.pending = l
		w.openPage(w.pageOf(l))
		return
	}
	w.open(l)
}

func (w *window) presentedFirst() {
	if w.presented {
		return
	}
	w.presented = true
	w.open(w.pending)
}
