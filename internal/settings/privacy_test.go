package settings

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-shell/portal/permissions"
	"github.com/stubbedev/lys-shell/settingspage"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// revoked is one Revoke the page made.
type revoked struct {
	app   string
	table permissions.Table
	id    string
}

// fakeGrants is the grants the page lists; a revoke takes the grant out
// unless failRevoke is set, and listErr fails every listing.
type fakeGrants struct {
	apps       []permissions.App
	revokes    []revoked
	listErr    error
	failRevoke error
	opened     int
	closed     int
}

func (f *fakeGrants) List(context.Context) ([]permissions.App, error) {
	return f.apps, f.listErr
}

func (f *fakeGrants) Revoke(_ context.Context, app string, table permissions.Table, id string) (int, error) {
	f.revokes = append(f.revokes, revoked{app, table, id})
	if f.failRevoke != nil {
		return 0, f.failRevoke
	}
	for i := range f.apps {
		if f.apps[i].ID != app {
			continue
		}
		f.apps[i].Grants = slices.DeleteFunc(f.apps[i].Grants, func(g permissions.Grant) bool { return g.Table == table && g.ID == id })
	}
	f.apps = slices.DeleteFunc(f.apps, func(a permissions.App) bool { return len(a.Grants) == 0 })
	return 1, nil
}

func serveGrants(t *testing.T, f *fakeGrants) {
	t.Helper()
	open := openGrants
	openGrants = func() (grantStore, func(), error) {
		f.opened++
		return f, func() { f.closed++ }, nil
	}
	t.Cleanup(func() { openGrants = open })
}

func twoApps() []permissions.App {
	return []permissions.App{
		{ID: "com.obsproject.Studio", Grants: []permissions.Grant{{Table: permissions.ScreenCast, ID: "token-1", Value: "yes"}}},
		{ID: "org.app", Grants: []permissions.Grant{
			{Table: permissions.Devices, ID: "camera", Value: "yes"},
			{Table: permissions.Shortcuts, ID: "toggle", Value: "Toggle recording"},
		}},
	}
}

func revokeButtons(root widget.Widget) []*widget.Button {
	var out []*widget.Button
	for _, b := range treetest.All[*widget.Button](root) {
		if slices.Contains(treetest.Labels(b), locale.T().Get("settings-privacy-revoke")) {
			out = append(out, b)
		}
	}
	return out
}

// The Privacy page is listed under its settingspage id and holds the
// screen-share reminder; its footer lists every app's grants with a
// revoke button each, and a revoke takes exactly that grant and lists
// again without it.
func TestPrivacyPageListsAndRevokesGrants(t *testing.T) {
	k := testKit(t, "")
	if !slices.ContainsFunc(ListedPages(), func(p settingspage.Page) bool { return p.ID == settingspage.PrivacyID && p.Title != "" }) {
		t.Fatalf("the page list lacks %q", settingspage.PrivacyID)
	}
	spec, _ := pageByID(layout(k.store.svc.Config()), settingspage.PrivacyID)
	if len(spec.sections) != 1 || spec.sections[0].rows[0].path != "portal.screen-cast.reminder-days" {
		t.Errorf("the page's rows = %+v, want the reminder", spec.sections)
	}
	f := &fakeGrants{apps: twoApps()}
	serveGrants(t, f)
	footer := grantsFooter(k)
	labels := strings.Join(treetest.Labels(footer), "\n")
	for _, want := range []string{"com.obsproject.Studio", "org.app", locale.T().Get("settings-privacy-table-devices") + " · camera", "Toggle recording"} {
		if !strings.Contains(labels, want) {
			t.Errorf("the list lacks %q:\n%s", want, labels)
		}
	}
	buttons := revokeButtons(footer)
	if len(buttons) != 3 {
		t.Fatalf("%d revoke buttons, want one per grant", len(buttons))
	}
	buttons[1].ClickAt(widget.Point{})
	if want := []revoked{{"org.app", permissions.Devices, "camera"}}; !slices.Equal(f.revokes, want) {
		t.Errorf("revoked %+v, want %+v", f.revokes, want)
	}
	if labels := strings.Join(treetest.Labels(footer), "\n"); strings.Contains(labels, "· camera") || !strings.Contains(labels, "Toggle recording") {
		t.Errorf("after the revoke the list reads:\n%s", labels)
	}
	if len(revokeButtons(footer)) != 2 {
		t.Error("the revoked grant kept its button")
	}
	if f.opened != f.closed || f.opened != 3 {
		t.Errorf("opened the grants %d times and closed them %d, want 3 and 3", f.opened, f.closed)
	}
}

// A revoke that fails says why and leaves the grant listed; a store that
// cannot be read says so; an empty store says nothing was granted.
func TestPrivacyPageFailuresAreShown(t *testing.T) {
	k := testKit(t, "")
	f := &fakeGrants{apps: twoApps(), failRevoke: errors.New("the store went away")}
	serveGrants(t, f)
	footer := grantsFooter(k)
	revokeButtons(footer)[0].ClickAt(widget.Point{})
	labels := strings.Join(treetest.Labels(footer), "\n")
	if !strings.Contains(labels, locale.T().Get("settings-privacy-revoke-failed")+": the store went away") || !strings.Contains(labels, "com.obsproject.Studio") {
		t.Errorf("a failed revoke shows:\n%s", labels)
	}

	serveGrants(t, &fakeGrants{listErr: errors.New("no permission store")})
	labels = strings.Join(treetest.Labels(grantsFooter(k)), "\n")
	if !strings.Contains(labels, locale.T().Get("settings-privacy-unavailable")+": no permission store") {
		t.Errorf("an unreadable store shows:\n%s", labels)
	}

	serveGrants(t, &fakeGrants{})
	if labels := treetest.Labels(grantsFooter(k)); !slices.Contains(labels, locale.T().Get("settings-privacy-none")) || len(revokeButtons(grantsFooter(k))) != 0 {
		t.Errorf("an empty store shows %q", labels)
	}
}

// Every table the model lists has its label.
func TestEveryGrantTableHasALabel(t *testing.T) {
	for _, table := range permissions.Tables {
		key := "settings-privacy-table-" + string(table)
		if got := locale.T().Get(key); got == "" || got == key {
			t.Errorf("%s has no label", table)
		}
	}
}
