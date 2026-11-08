package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/shell/apptheme"
	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
)

// maxTabStops bounds a page's Tab cycle, far above the busiest page's
// controls, so a cycle that never closes fails instead of hanging.
const maxTabStops = 5000

// Tab, from the search field, reaches every row's control on every page,
// each stop showing its focus, and comes back round to where it began. The
// wallpaper's cycling directory is set, so its revealed rows are shown.
func TestTabReachesEveryControlOnEveryPage(t *testing.T) {
	k := testKit(t, strings.Replace(geometryYAML, "wallpaper:\n", "wallpaper:\n  cycling-directory: /tmp\n", 1))
	cfg := k.store.svc.Config()
	w := newWindow(k, cfg, func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	apptheme.New(cfg).Attach(w.root)
	router := &widget.Router{Root: w.root}
	for _, sec := range w.sections {
		for _, p := range sec.pages {
			w.show(p.id)
			w.root.Arrange(render.Rect{W: 1400, H: 6000})
			if err := router.SetFocus(w.search); err != nil {
				t.Fatalf("%s: %v", p.id, err)
			}
			visited := map[widget.Widget]bool{}
			closed := false
			for range maxTabStops {
				router.FocusNext()
				f := router.Focused()
				if f == widget.Widget(w.search) {
					closed = true
					break
				}
				if !widget.FocusVisible(f) {
					t.Errorf("%s: Tab landed on %T without a visible focus", p.id, f)
				}
				visited[f] = true
			}
			if !closed {
				t.Fatalf("%s: Tab did not come back to the search field in %d stops", p.id, maxTabStops)
			}
			for i, row := range w.page.rows {
				reached := false
				treetest.Walk(row.ctl, func(x widget.Widget) { reached = reached || visited[x] })
				if !reached {
					t.Errorf("%s: row %d (%s), a %T, is out of Tab's reach", p.id, i, row.path, row.ctl)
				}
			}
		}
	}
}
