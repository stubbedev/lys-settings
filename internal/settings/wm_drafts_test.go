package settings

import (
	"strings"
	"testing"

	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"
)

func TestANewCardIsADraftUntilLysWMTakesIt(t *testing.T) {
	k, w, fake, files := wmKit(t, "")
	row := newFieldRow(k, w, wmObjects("binds", "chord", "action"))
	k.onWMChange = row.refresh
	c := row.ctl.(*cardList)
	fake.reject = "binds[1].chord: is empty"
	treetest.WithClass(c, "list-control-add")[0].(*widget.Button).OnClick()
	if len(c.local) != 2 || !c.draft.Visible() || !strings.Contains(c.draft.Text(), "binds[1].chord: is empty") {
		t.Fatalf("%d cards, draft %v %q", len(c.local), c.draft.Visible(), c.draft.Text())
	}
	noFile(t, files.runtime)
	if w.class == "error" || fake.reloads != 0 {
		t.Fatalf("a draft was tried as a change: %q, %d reloads", w.notice, fake.reloads)
	}
	row.refresh()
	if len(c.local) != 2 {
		t.Fatal("a refresh dropped the draft")
	}
	fake.reject = ""
	if err := c.fieldSlot(1, "chord").set("SUPER + W"); err != nil {
		t.Fatal(err)
	}
	if c.draft.Visible() || !strings.Contains(fileText(t, files.runtime), "SUPER + W") {
		t.Fatalf("an accepted card is not written: %q", fileText(t, files.runtime))
	}
}
