package settings

import (
	"testing"

	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

// maxTabStops bounds a page's Tab cycle, far above the busiest page's
// controls, so a cycle that never closes fails instead of hanging.
const maxTabStops = 5000

// Tab, from the search field, reaches every row's control on the page,
// each stop showing its focus, and comes back round to where it began.
func tabReachesEveryControl(w *window) pageCheck {
	router := &widget.Router{Root: w.root}
	return pageCheck{visit: func(t *testing.T, w *window) {
		id := w.pageID
		if err := router.SetFocus(w.search); err != nil {
			t.Fatalf("%s: %v", id, err)
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
				t.Errorf("%s: Tab landed on %T without a visible focus", id, f)
			}
			visited[f] = true
		}
		if !closed {
			t.Fatalf("%s: Tab did not come back to the search field in %d stops", id, maxTabStops)
		}
		for i, row := range w.page.rows {
			reached := false
			treetest.Walk(row.ctl, func(x widget.Widget) { reached = reached || visited[x] })
			if !reached {
				t.Errorf("%s: row %d (%s), a %T, is out of Tab's reach", id, i, row.path, row.ctl)
			}
		}
	}}
}
