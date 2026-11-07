package settings

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

var listFixtures = map[string][]choice{
	"Monitor":           {{value: "DP-1", label: "DP-1"}, {value: "HDMI-A-1", label: "HDMI-A-1"}},
	"OsdMonitor":        {{value: "DP-1", label: "DP-1"}},
	"PopupMonitor":      {{value: "DP-1", label: "DP-1"}},
	"IconTheme":         {{value: "Adwaita", label: "Adwaita"}, {value: "Papirus", label: "Papirus"}},
	"lys-wm-icon-theme": {{value: "Adwaita", label: "Adwaita"}},
	"CursorTheme":       {{value: "Bibata", label: "Bibata Modern"}},
	"Program":           {{value: "foot", label: "foot"}, {value: "ssh", label: "ssh"}, {value: "xdg-open", label: "xdg-open"}},
	typeTerminal:        {{value: "foot", label: "Foot"}, {value: "kitty", label: "kitty"}},
	listSoundNames:      {{value: "message-new-instant", label: "message-new-instant"}},
	"NetInterface":      {{value: "eth0", label: "eth0"}, {value: "wlan0", label: "wlan0"}},
	"TempSensor":        {{value: "Tctl", label: "Tctl", group: "k10temp"}},
	"AudioSource":       {{value: "speakers.monitor", label: "Speakers monitor"}},
	"PamService":        {{value: "login", label: "login"}, {value: "lys-lock", label: "lys-lock"}},
	"lys-wm-drm-device": {{value: "card0", label: "card0 (amdgpu)"}},
	wmFontFormat:        {{value: "/fonts/Inter.ttf", label: "Inter"}},
	wmLayoutFormat:      {{value: "dk", label: "Danish"}, {value: "us", label: "English (US)"}},
	wmVariantFormat:     {{value: "nodeadkeys", label: "Danish (no dead keys)", group: "dk"}, {value: "intl", label: "English (US, intl.)", group: "us"}},
	wmOptionsFormat:     {{value: "caps:escape", label: "Caps Lock is Escape", group: "Caps Lock behavior"}, {value: "grp:alt_shift_toggle", label: "Alt+Shift", group: "Switching layout"}},
	wmModelFormat:       {{value: "pc105", label: "Generic 105-key PC"}},
	wmRulesFormat:       {{value: "evdev", label: "evdev"}},
}

func fixedLists() map[string]lister {
	out := make(map[string]lister, len(listFixtures))
	for key, items := range listFixtures {
		out[key] = func() []choice { return slices.Clone(items) }
	}
	return out
}

func TestEveryListerHasATestFixture(t *testing.T) {
	want := slices.Sorted(maps.Keys(hostLists(systemHost)))
	if got := slices.Sorted(maps.Keys(listFixtures)); !slices.Equal(got, want) {
		t.Fatalf("fixtures %v, listers %v", got, want)
	}
}

var freeFormRows = []string{
	"launcher.negate-char",
	"launcher.search.web-engine",
	"launcher.search.web-url",
	"modules.bitwarden.email",
	"modules.mail.query",
	"modules.screenshot.label",
	"modules.weather.location",
}

func isFreeForm(path string) bool {
	return slices.Contains(freeFormRows, path) || strings.HasSuffix(path, "-workspaces.divider")
}

func plainTextRows(t *testing.T, k *kit, be backend, rows []rowSpec) []string {
	t.Helper()
	var out []string
	for _, r := range rows {
		if r.editor != nil {
			continue
		}
		if tx, ok := fieldControl(k, be, r).(*text); ok && tx.Echo() == widget.EchoNormal && !isFreeForm(r.path) {
			out = append(out, r.path)
		}
	}
	return out
}

func shellRows() []rowSpec {
	var rows []rowSpec
	for _, s := range layout(config.Defaults()) {
		for _, p := range s.pages {
			if p.wm {
				continue
			}
			for _, sec := range p.sections {
				for _, r := range sec.rows {
					if !r.wm {
						rows = append(rows, r)
					}
				}
			}
		}
	}
	return rows
}

func TestNoShellRowFallsBackToPlainTextUnlessItIsFreeForm(t *testing.T) {
	k := testKit(t, "")
	if got := plainTextRows(t, k, k.store, shellRows()); len(got) > 0 {
		t.Fatalf("rows on the plain text editor that are not on the free-form list: %v", got)
	}
	k2 := testKit(t, "")
	rows := append(shellRows(), field("modules.weather.location"))
	if got := plainTextRows(t, k2, k2.store, rows); len(got) != 0 {
		t.Fatalf("a free-form row was flagged: %v", got)
	}
	freeFormRows = slices.DeleteFunc(slices.Clone(freeFormRows), func(p string) bool { return p == "modules.weather.location" })
	defer func() { freeFormRows = append(freeFormRows, "modules.weather.location") }()
	if got := plainTextRows(t, k2, k2.store, shellRows()); !slices.Equal(got, []string{"modules.weather.location"}) {
		t.Fatalf("a text row off the list was not flagged: %v", got)
	}
}

func shellControl(t *testing.T, k *kit, path string, opts ...rowOpt) control {
	t.Helper()
	return fieldControl(k, k.store, field(path, opts...))
}

func openChoices(t *testing.T, k *kit, c *choiceEditor) *searchPicker {
	t.Helper()
	f := &fakePickers{}
	k.pickers = f
	c.open.ClickAt(widget.Point{})
	if c.picker == nil || f.content != widget.Widget(c.picker.root) || f.anchor != widget.Widget(c.open) {
		t.Fatal("the list did not open beside its button")
	}
	return c.picker
}

func pickName(t *testing.T, p *searchPicker, name string) {
	t.Helper()
	i := slices.Index(p.all, name)
	if i < 0 {
		t.Fatalf("%q is not offered: %v", name, p.all)
	}
	p.rows.OnActivate(i)
}

func TestAListedValueIsOfferedAndPickingItWritesExactlyIt(t *testing.T) {
	k := testKit(t, "")
	c := shellControl(t, k, "launcher.icon-theme").(*choiceEditor)
	p := openChoices(t, k, c)
	if !slices.Equal(p.all, []string{"", "Adwaita", "Papirus"}) {
		t.Fatalf("offered %q", p.all)
	}
	pickName(t, p, "Papirus")
	if v := k.store.value("launcher.icon-theme"); v != "Papirus" {
		t.Fatalf("%v", v)
	}
	if c.entry.Text() != "Papirus" || c.unknown.Visible() {
		t.Fatalf("shows %q, unknown %v", c.entry.Text(), c.unknown.Visible())
	}
	p = openChoices(t, k, c)
	pickName(t, p, "")
	if v := k.store.value("launcher.icon-theme"); v != "" {
		t.Fatalf("the default entry wrote %v", v)
	}
	if c.entry.Placeholder() != locale.T().Get("settings-choice-default") {
		t.Fatalf("placeholder %q", c.entry.Placeholder())
	}
}

func TestAValueOutsideTheListIsKeptAndMarkedUnknown(t *testing.T) {
	k := testKit(t, "")
	if err := k.store.set("launcher.icon-theme", "Uninstalled"); err != nil {
		t.Fatal(err)
	}
	c := shellControl(t, k, "launcher.icon-theme").(*choiceEditor)
	if c.entry.Text() != "Uninstalled" || !c.unknown.Visible() {
		t.Fatalf("shows %q, unknown %v", c.entry.Text(), c.unknown.Visible())
	}
	p := openChoices(t, k, c)
	if !slices.Contains(p.all, "Uninstalled") {
		t.Fatalf("the current value is not offered: %v", p.all)
	}
	if v := k.store.value("launcher.icon-theme"); v != "Uninstalled" {
		t.Fatalf("an unknown value was replaced: %v", v)
	}
	if err := k.store.set("launcher.icon-theme", "Adwaita"); err != nil {
		t.Fatal(err)
	}
	c.refresh()
	if c.unknown.Visible() {
		t.Fatal("a listed value is marked unknown")
	}
}

func TestAnEmptyListStillKeepsTypesAndClears(t *testing.T) {
	k := testKit(t, "")
	systemLists["IconTheme"] = func() []choice { return nil }
	c := shellControl(t, k, "launcher.icon-theme").(*choiceEditor)
	p := openChoices(t, k, c)
	if !slices.Equal(p.all, []string{""}) {
		t.Fatalf("offered %q", p.all)
	}
	c.entry.SelectAll()
	c.entry.Insert("Mine")
	c.entry.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value("launcher.icon-theme"); v != "Mine" {
		t.Fatalf("custom entry wrote %v", v)
	}
	if !c.unknown.Visible() {
		t.Fatal("a value no list holds is not marked")
	}
	p = openChoices(t, k, c)
	pickName(t, p, "")
	if v := k.store.value("launcher.icon-theme"); v != "" || c.unknown.Visible() {
		t.Fatalf("clearing wrote %v, unknown %v", v, c.unknown.Visible())
	}
}

func TestSpecialValuesComeFirstWithTheirLabels(t *testing.T) {
	k := testKit(t, "")
	for path, want := range map[string][]string{
		"osd.monitor":                {"primary", "DP-1"},
		"modules.netstat.interface":  {"auto", "eth0", "wlan0"},
		"modules.cpu.temp-sensor":    {"auto", "Tctl"},
		"modules.cava.source":        {"auto", "speakers.monitor"},
		"launcher.monitor":           {"", "DP-1", "HDMI-A-1"},
		"login.lock.pam-service":     {shellDefault("login.lock.pam-service"), "login", "lys-lock"},
		"login.greeter.cursor-theme": {"", "Bibata"},
		"launcher.ssh.client":        {"", "foot", "ssh", "xdg-open"},
	} {
		c := shellControl(t, k, path).(*choiceEditor)
		p := openChoices(t, k, c)
		if !slices.Equal(p.all, want) {
			t.Errorf("%s offers %q, want %q", path, p.all, want)
		}
	}
	if specialLabel("primary") != locale.T().Get("settings-choice-primary") || specialLabel("auto") != locale.T().Get("settings-choice-auto") {
		t.Error("the special values are not labeled")
	}
	if specialLabel("DP-1") != "DP-1" {
		t.Error("an ordinary value is relabeled")
	}
}

func TestAProgramIsAListedNameOrAPath(t *testing.T) {
	k := testKit(t, "")
	const path = "launcher.ssh.client"
	c := shellControl(t, k, path).(*choiceEditor)
	for value, unknown := range map[string]bool{"ssh": false, "/opt/bin/ssh": false, "not-installed": true} {
		if err := k.store.set(path, value); err != nil {
			t.Fatal(err)
		}
		c.refresh()
		if c.unknown.Visible() != unknown {
			t.Errorf("%s: unknown %v", value, c.unknown.Visible())
		}
	}
}

func TestTheTerminalRowListsTerminalEmulators(t *testing.T) {
	k := testKit(t, "")
	c := shellControl(t, k, "launcher.terminal", terminalRow).(*choiceEditor)
	p := openChoices(t, k, c)
	if !slices.Contains(p.all, "kitty") || slices.Contains(p.all, "xdg-open") {
		t.Fatalf("offered %q", p.all)
	}
	pickName(t, p, "kitty")
	if v := k.store.value("launcher.terminal"); v != "kitty" {
		t.Fatalf("%v", v)
	}
	if c.unknown.Visible() {
		t.Fatal("a terminal command is never unknown")
	}
}

func TestACommandOffersPresetsAndAcceptsAnyCommand(t *testing.T) {
	k := testKit(t, "")
	const path = "modules.power.suspend-command"
	c := shellControl(t, k, path).(*choiceEditor)
	p := openChoices(t, k, c)
	if !slices.Contains(p.all, "systemctl suspend") || !slices.Contains(p.all, shellDefault(path)) {
		t.Fatalf("offered %q", p.all)
	}
	if slices.Contains(p.all, "systemctl reboot") {
		t.Fatalf("a suspend command offers a reboot: %q", p.all)
	}
	pickName(t, p, "systemctl suspend")
	if v := k.store.value(path); v != "systemctl suspend" {
		t.Fatalf("%v", v)
	}
	c.entry.SelectAll()
	c.entry.Insert("my-suspend --now")
	c.entry.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value(path); v != "my-suspend --now" || c.unknown.Visible() {
		t.Fatalf("custom %v, unknown %v", v, c.unknown.Visible())
	}
	if got := commandsFor("launcher.run.run-command"); !slices.Equal(got, []string{shellDefault("launcher.run.run-command")}) {
		t.Fatalf("a run command offers %q", got)
	}
}

func TestTheBitwardenServerOffersTheHostedRegions(t *testing.T) {
	k := testKit(t, "")
	c := shellControl(t, k, "modules.bitwarden.server").(*choiceEditor)
	p := openChoices(t, k, c)
	if !slices.Equal(p.all, bitwardenServers) {
		t.Fatalf("offered %q", p.all)
	}
	pickName(t, p, "bitwarden.eu")
	if v := k.store.value("modules.bitwarden.server"); v != "bitwarden.eu" {
		t.Fatalf("%v", v)
	}
}

func TestADirectoryRowPicksAFolder(t *testing.T) {
	k := testKit(t, "")
	f := &fakePickers{}
	k.pickers = f
	c := shellControl(t, k, "modules.screenshot.output-directory").(*fileEditor)
	c.browse.ClickAt(widget.Point{})
	if f.folderFn == nil || f.fileFn != nil {
		t.Fatal("the browse button did not ask for a folder")
	}
	f.folderFn("/home/me/Shots")
	if v := k.store.value("modules.screenshot.output-directory"); v != "/home/me/Shots" {
		t.Fatalf("%v", v)
	}
	if c.entry.Placeholder() != locale.T().Get("settings-folder-path-placeholder") {
		t.Fatalf("placeholder %q", c.entry.Placeholder())
	}
	file := newFileEditor(k, pathSlot(k.store, "login.background-image"))
	file.browse.ClickAt(widget.Point{})
	if f.fileFn == nil {
		t.Fatal("a file row stopped asking for a file")
	}
}

func TestASecretIsMasked(t *testing.T) {
	k := testKit(t, "")
	const path = "modules.weather.weatherapi-key"
	c := shellControl(t, k, path).(*text)
	if c.Echo() != widget.EchoPassword {
		t.Fatal("a secret shows in clear text")
	}
	c.Insert("s3cret")
	c.KeyAction(widget.KeyEnter, 0)
	if v := k.store.value(path); v != "s3cret" {
		t.Fatalf("%v", v)
	}
	if plain := shellControl(t, k, "modules.weather.location").(*text); plain.Echo() != widget.EchoNormal {
		t.Fatal("a free-form row is masked")
	}
}

func TestAStrftimeFieldOffersPresetsWithAPreview(t *testing.T) {
	k := testKit(t, "")
	const path = "login.clock-format"
	c := shellControl(t, k, path).(*formatEditor)
	current, _ := k.store.value(path).(string)
	f, err := config.ParseStrftimeFormat(current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.preview.Text(), f.Format(formatSampleTime)) {
		t.Fatalf("preview %q", c.preview.Text())
	}
	fp := &fakePickers{}
	k.pickers = fp
	c.open.ClickAt(widget.Point{})
	if !slices.Contains(c.picker.all, "%Y-%m-%d") {
		t.Fatalf("offered %q", c.picker.all)
	}
	pickName(t, c.picker, "%Y-%m-%d")
	if v := k.store.value(path); v != "%Y-%m-%d" || !strings.Contains(c.preview.Text(), "2026-03-14") {
		t.Fatalf("%v, preview %q", v, c.preview.Text())
	}
	c.entry.SelectAll()
	c.entry.Insert("%Q")
	if !strings.Contains(c.preview.Text(), strings.SplitN(locale.T().Get("settings-format-invalid"), " ", 2)[0]) {
		t.Fatalf("a bad pattern previews %q", c.preview.Text())
	}
	if v := k.store.value(path); v != "%Y-%m-%d" {
		t.Fatalf("typing wrote %v", v)
	}
}

func TestAFormatFieldInsertsItsPlaceholdersWithAPreview(t *testing.T) {
	k := testKit(t, "")
	const path = "modules.battery.format"
	ps := config.Placeholders(path)
	if len(ps) == 0 {
		t.Fatal("lys-shell declares no placeholders")
	}
	c := shellControl(t, k, path).(*formatEditor)
	fp := &fakePickers{}
	k.pickers = fp
	c.open.ClickAt(widget.Point{})
	if !slices.Equal(c.picker.all, []string{ps[0].Token}) {
		t.Fatalf("offered %q", c.picker.all)
	}
	c.entry.SelectAll()
	c.entry.Insert("Bat ")
	pickName(t, c.picker, ps[0].Token)
	if v := k.store.value(path); v != "Bat "+ps[0].Token {
		t.Fatalf("%v", v)
	}
	if want := "Bat " + ps[0].Sample; !strings.Contains(c.preview.Text(), want) {
		t.Fatalf("preview %q, want %q", c.preview.Text(), want)
	}
	if got := placeholderFormat(ps).render("{{percent}}%"); got != ps[0].Sample+"%" {
		t.Fatalf("a compact token renders %q", got)
	}
}

func TestToggleListAddsAndRemovesInOrder(t *testing.T) {
	items := listFixtures[wmLayoutFormat]
	got := toggleList("us", items)
	if got[0].value != "us,dk" || strings.HasPrefix(got[0].label, checkMark) {
		t.Fatalf("%+v", got[0])
	}
	if got[1].value != "" || !strings.HasPrefix(got[1].label, checkMark) {
		t.Fatalf("%+v", got[1])
	}
	if !allListed("us,dk", items) || allListed("us,xx", items) || !allListed("", items) {
		t.Fatal("known")
	}
	opts := toggleList("", listFixtures[wmOptionsFormat])
	if opts[0].label != "Caps Lock behavior: Caps Lock is Escape" || opts[0].value != "caps:escape" {
		t.Fatalf("%+v", opts[0])
	}
}

func TestVariantsFollowTheChosenLayouts(t *testing.T) {
	items := listFixtures[wmVariantFormat]
	layouts := func() []string { return []string{"us", "dk"} }
	got := variantChoices(layouts)("", items)
	values := make([]string, len(got))
	for i, o := range got {
		values[i] = o.value
	}
	if !slices.Equal(values, []string{"intl", ",nodeadkeys"}) {
		t.Fatalf("%q", values)
	}
	got = variantChoices(layouts)("intl", items)
	if got[0].value != "" || !strings.HasPrefix(got[0].label, checkMark) || got[1].value != "intl,nodeadkeys" {
		t.Fatalf("%+v", got)
	}
	if got := variantChoices(func() []string { return []string{"dk"} })("", items); len(got) != 1 || got[0].value != "nodeadkeys" {
		t.Fatalf("a variant of a layout not chosen is offered: %+v", got)
	}
	known := variantsListed(layouts)
	if !known(",nodeadkeys", items) || known("nodeadkeys", items) || known(",,intl", items) {
		t.Fatal("known")
	}
}
