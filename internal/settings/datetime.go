package settings

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-desktop/strftime"
	"github.com/stubbedev/lys-lib/atomicfile"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/clocale"
	"github.com/stubbedev/lys-settings/internal/locale"
	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

const (
	dateTimeID       = "date-time"
	regionID         = "region"
	timedateName     = "org.freedesktop.timedate1"
	timedatePath     = dbus.ObjectPath("/org/freedesktop/timedate1")
	localeName       = "org.freedesktop.locale1"
	localePath       = dbus.ObjectPath("/org/freedesktop/locale1")
	manualTimeLayout = "2006-01-02 15:04:05"
	clockFormatPath  = "modules.clock.format"
	clock24          = "%a %b %d %H:%M"
	clock12          = "%a %b %d %I:%M %p"
	localeConfName   = "locale.conf"
	interactive      = true
)

var userFormats = []string{"LC_TIME", "LC_NUMERIC", "LC_MONETARY", "LC_MEASUREMENT", "LC_PAPER"}

func dateTimePage(*config.Config) pageSpec {
	return pageSpec{
		id: dateTimeID, navKey: "settings-nav-date-time", icon: "ld-clock-symbolic",
		header: "settings-page-date-time",
		footer: dateTimeFooter,
	}
}

func regionPage(*config.Config) pageSpec {
	return pageSpec{
		id: regionID, navKey: "settings-nav-region", icon: "ld-globe-symbolic",
		header: "settings-page-region",
		footer: regionFooter,
	}
}

type sysPanel struct {
	*widget.Box
	k       *kit
	svc     sysService
	conn    *dbus.Conn
	props   sysProps
	problem *widget.Label
	body    *widget.Box
	stop    func()
	refresh func()
	closed  bool
}

func newSysPanel(k *kit, name string, path dbus.ObjectPath) *sysPanel {
	p := &sysPanel{Box: gapBox(widget.Column), k: k, svc: sysService{name: name, iface: name, path: path}}
	p.problem = wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	p.problem.SetVisible(false)
	p.body = gapBox(widget.Column)
	p.Append(p.problem, false)
	p.Append(p.body, false)
	return p
}

type sysOpened struct {
	conn  *dbus.Conn
	props sysProps
	extra any
	err   error
}

func (p *sysPanel) open(extra func(context.Context, sysService) (any, error), show func(any)) {
	async(p.k, func() sysOpened {
		conn, err := p.k.systemBus()
		if err != nil {
			return sysOpened{err: fmt.Errorf("connect to the system bus: %w", err)}
		}
		svc := p.svc
		svc.conn = conn
		props, err := withTimeout(svc.props)
		if err != nil {
			_ = conn.Close()
			return sysOpened{err: err}
		}
		var more any
		if extra != nil {
			if more, err = withTimeout(func(ctx context.Context) (any, error) { return extra(ctx, svc) }); err != nil {
				_ = conn.Close()
				return sysOpened{err: err}
			}
		}
		return sysOpened{conn: conn, props: props, extra: more}
	}, func(o sysOpened) {
		if o.err != nil {
			p.body.Clear()
			p.body.Append(wrapped(p.k.label(locale.T().Get("settings-sys-unavailable", i18n.Str("service", p.svc.name))+": "+o.err.Error(), css.Error, css.Caption)), false)
			return
		}
		if p.closed {
			_ = o.conn.Close()
			return
		}
		p.conn, p.svc.conn, p.props = o.conn, o.conn, o.props
		stop, err := p.svc.watch(func(changed sysProps) {
			p.k.invoke(func() { p.changed(changed) })
		})
		if err != nil {
			p.fail(err)
		}
		p.stop = stop
		show(o.extra)
	})
}

func (p *sysPanel) changed(changed sysProps) {
	if p.props == nil {
		return
	}
	maps.Copy(p.props, changed)
	if p.refresh != nil {
		p.refresh()
	}
}

func (p *sysPanel) fail(err error) {
	p.problem.SetVisible(err != nil)
	if err != nil {
		p.problem.SetText(err.Error())
	}
}

func (p *sysPanel) do(method string, args ...any) {
	svc := p.svc
	async(p.k, func() error {
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) { return struct{}{}, svc.call(ctx, method, args...) })
		return err
	}, func(err error) {
		p.fail(err)
		if err != nil && p.refresh != nil {
			p.refresh()
		}
	})
}

func (p *sysPanel) close() {
	p.closed = true
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

func panelRow(k *kit, title, detail string, controls ...widget.Widget) *widget.Box {
	row := gapBox(widget.Row)
	row.SetElement("item")
	row.AddClass(css.Small, "setting-row")
	text := widget.NewBox(widget.Column, widget.Current().Spacing/4, 0)
	text.Append(k.label(title, "setting-label"), false)
	if detail != "" {
		text.Append(wrapped(k.label(detail, "setting-description", css.Caption, css.DimLabel)), false)
	}
	row.AppendAligned(text, true, widget.AlignCenter)
	for _, c := range controls {
		row.AppendAligned(c, false, widget.AlignCenter)
	}
	return row
}

func timezoneChoices(zones []string) []choice {
	now := time.Now()
	out := make([]choice, 0, len(zones))
	for _, z := range zones {
		label := z
		if loc, err := time.LoadLocation(z); err == nil {
			label = z + " (UTC" + now.In(loc).Format("-07:00") + ")"
		}
		out = append(out, choice{value: z, label: label})
	}
	return out
}

func dateTimeFooter(k *kit) widget.Widget {
	t := locale.T()
	p := newSysPanel(k, timedateName, timedatePath)
	clock := gapBox(widget.Column)
	is24 := func() bool {
		f, _ := k.store.value(clockFormatPath).(string)
		return strings.Contains(f, "%H") || strings.Contains(f, "%R") || strings.Contains(f, "%T")
	}
	preview := k.label("", "setting-description", css.Caption, css.DimLabel)
	showPreview := func() {
		f, _ := k.store.value(clockFormatPath).(string)
		if l, err := strftime.Compile(f); err == nil {
			preview.SetText(l.Format(time.Now()))
		}
	}
	h24 := widget.NewSwitch(is24())
	h24.SetAccessibleName(t.Get("settings-date-24h"))
	h24.OnChanged = func(on bool) {
		format := clock12
		if on {
			format = clock24
		}
		if err := k.store.set(clockFormatPath, format); err != nil {
			p.fail(err)
		}
		showPreview()
	}
	showPreview()
	clockRows := widget.NewItemGroup()
	clockRows.Append(panelRow(k, t.Get("settings-date-24h"), t.Attr("settings-date-24h", "description"), preview, h24))
	clock.Append(clockRows, false)
	p.open(func(ctx context.Context, svc sysService) (any, error) {
		var zones []string
		if err := svc.store(ctx, "ListTimezones", &zones); err != nil {
			return nil, err
		}
		return timezoneChoices(zones), nil
	}, func(extra any) {
		zones, _ := extra.([]choice)
		ntp := widget.NewSwitch(p.props.flag("NTP"))
		ntp.SetAccessibleName(t.Get("settings-date-ntp"))
		syncing := false
		ntp.OnChanged = func(on bool) {
			if !syncing {
				p.do("SetNTP", on, interactive)
			}
		}
		manual := k.entry()
		manual.SetPlaceholder(manualTimeLayout)
		set := k.button(k.label(t.Get("settings-date-set")), nil, css.Secondary)
		set.OnClick = func() {
			at, err := time.ParseInLocation(manualTimeLayout, strings.TrimSpace(manual.Text()), time.Local)
			if err != nil {
				p.fail(fmt.Errorf("%q is not a date and time as %s: %w", manual.Text(), manualTimeLayout, err))
				return
			}
			p.do("SetTime", at.UnixMicro(), false, interactive)
		}
		zone := newChoiceEditor(k, slot{
			get:   func() any { return p.props.str("Timezone") },
			set:   func(v any) error { s, _ := v.(string); p.do("SetTimezone", s, interactive); return nil },
			unset: func() {},
		}, choiceSpec{list: fixedChoices(zones), known: func(current string, items []choice) bool { return hasValue(items, current) }})
		rows := widget.NewItemGroup()
		rows.Append(panelRow(k, t.Get("settings-date-ntp"), t.Attr("settings-date-ntp", "description"), ntp))
		rows.Append(panelRow(k, t.Get("settings-date-manual"), t.Attr("settings-date-manual", "description"), manual, set))
		rows.Append(panelRow(k, t.Get("settings-date-zone"), t.Attr("settings-date-zone", "description"), zone))
		refresh := func() {
			syncing = true
			defer func() { syncing = false }()
			ntp.SetEnabled(p.props.flag("CanNTP"))
			if ntp.On() != p.props.flag("NTP") {
				ntp.SetOn(p.props.flag("NTP"))
			}
			manual.SetEnabled(!p.props.flag("NTP"))
			set.SetEnabled(!p.props.flag("NTP"))
			zone.refresh()
		}
		p.refresh = refresh
		refresh()
		p.body.Append(rows, false)
	})
	content := sectionStack()
	content.Append(newSection(k, "settings-section-date-system", p), false)
	content.Append(newSection(k, "settings-section-date-clock", clock), false)
	return &closingBox{content, p.close}
}

type closingBox struct {
	*widget.Box
	release func()
}

func (c *closingBox) close() { c.release() }

func fixedChoices(items []choice) lister {
	return func() []choice { return slices.Clone(items) }
}

var listLocales = func() ([]string, error) {
	out, err := exec.Command("locale", "-a").Output()
	if err != nil {
		return nil, fmt.Errorf("list the system's locales with locale -a: %w", err)
	}
	var names []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if name == "" || name == "C" || name == "POSIX" || strings.HasPrefix(name, "C.") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func localeConfPath(h host) string {
	config := h.getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(h.getenv("HOME"), ".config")
	}
	return filepath.Join(config, localeConfName)
}

func readLocaleConf(path string) (map[string]string, []string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the user's locale.conf
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	values := map[string]string{}
	for _, l := range lines {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok && !strings.HasPrefix(k, "#") {
			values[k] = strings.Trim(v, `"`)
		}
	}
	return values, lines, nil
}

func writeLocaleConf(path, key, value string) error {
	_, lines, err := readLocaleConf(path)
	if err != nil {
		return err
	}
	at := slices.IndexFunc(lines, func(l string) bool {
		k, _, ok := strings.Cut(strings.TrimSpace(l), "=")
		return ok && k == key
	})
	switch {
	case value == "" && at >= 0:
		lines = slices.Delete(lines, at, at+1)
	case value == "":
	case at >= 0:
		lines[at] = key + "=" + value
	default:
		lines = append(lines, key+"="+value)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // ~/.config
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	text := strings.Join(lines, "\n")
	if text != "" {
		text += "\n"
	}
	if err := atomicfile.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func regionFooter(k *kit) widget.Widget {
	t := locale.T()
	conf := localeConfPath(systemHost)
	preview := wrapped(k.label("", "setting-description", css.Caption, css.DimLabel))
	formatsProblem := wrapped(k.label("", "setting-problem", css.Caption, css.Error))
	formatsProblem.SetVisible(false)
	head := widget.NewBox(widget.Column, widget.Current().Spacing/2, 0)
	head.SetElement("item")
	head.Append(preview, false)
	head.Append(formatsProblem, false)
	formats := widget.NewBox(widget.Column, 0, 0)
	formats.Append(head, false)
	formats.Append(widget.NewSeparator(widget.Horizontal), false)
	p := newSysPanel(k, localeName, localePath)
	lang := func() string {
		for _, kv := range p.props.strs("Locale") {
			if v, ok := strings.CutPrefix(kv, "LANG="); ok {
				return v
			}
		}
		return ""
	}
	showPreview := func() {
		values, _, err := readLocaleConf(conf)
		if err != nil {
			formatsProblem.SetText(err.Error())
			formatsProblem.SetVisible(true)
			return
		}
		pick := func(key string) string {
			if v := values[key]; v != "" {
				return v
			}
			return lang()
		}
		pv, err := clocale.Format(clocale.Formats{Time: pick("LC_TIME"), Numeric: pick("LC_NUMERIC"), Monetary: pick("LC_MONETARY")}, time.Now())
		if err != nil {
			preview.SetText(err.Error())
			return
		}
		preview.SetText(t.Get("settings-region-preview", i18n.Str("date", pv.Date), i18n.Str("number", pv.Number), i18n.Str("price", pv.Price)))
	}
	async(k, func() localesListing {
		names, err := listLocales()
		return localesListing{names, err}
	}, func(l localesListing) {
		if l.err != nil {
			head.Append(wrapped(k.label(l.err.Error(), css.Error, css.Caption)), false)
			return
		}
		values, _, err := readLocaleConf(conf)
		if err != nil {
			formatsProblem.SetText(err.Error())
			formatsProblem.SetVisible(true)
		}
		rows := widget.NewItemGroup()
		labels := append([]string{t.Get("settings-region-same")}, l.names...)
		for _, key := range userFormats {
			dd := widget.NewDropdown(k.face, k.textPx(), labels, max(slices.Index(l.names, values[key])+1, 0))
			dd.SetAccessibleName(key)
			dd.OnSelect = func(i int) {
				value := ""
				if i > 0 {
					value = l.names[i-1]
				}
				err := writeLocaleConf(conf, key, value)
				formatsProblem.SetVisible(err != nil)
				if err != nil {
					formatsProblem.SetText(err.Error())
				}
				showPreview()
			}
			rows.Append(panelRow(k, t.Get("settings-region-"+strings.ToLower(strings.ReplaceAll(key, "_", "-"))), "", dd))
		}
		formats.Append(rows, false)
		showPreview()
	})
	p.open(func(context.Context, sysService) (any, error) { return listLocales() }, func(extra any) {
		names, _ := extra.([]string)
		language := widget.NewDropdown(k.face, k.textPx(), names, max(slices.Index(names, lang()), 0))
		language.SetAccessibleName(t.Get("settings-region-language"))
		syncing := false
		language.OnSelect = func(i int) {
			if !syncing && i >= 0 && i < len(names) {
				p.do("SetLocale", []string{"LANG=" + names[i]}, interactive)
			}
		}
		keymap := k.entry()
		keymap.SetPlaceholder("us")
		keymap.OnActivate = func(text string) {
			p.do("SetVConsoleKeyboard", strings.TrimSpace(text), "", false, interactive)
		}
		layout := k.label("", "setting-description", css.Caption, css.DimLabel)
		copyLayout := k.button(k.label(t.Get("settings-region-copy-layout")), func() {
			kb := wmKeyboard(k)
			p.do("SetX11Keyboard", kb.layout, kb.model, kb.variant, kb.options, false, interactive)
		}, css.Secondary)
		copyLayout.SetEnabled(wmconfig.Installed())
		rows := widget.NewItemGroup()
		rows.Append(panelRow(k, t.Get("settings-region-language"), t.Attr("settings-region-language", "description"), language))
		rows.Append(panelRow(k, t.Get("settings-region-keymap"), t.Attr("settings-region-keymap", "description"), keymap))
		rows.Append(panelRow(k, t.Get("settings-region-layout"), t.Attr("settings-region-layout", "description"), layout, copyLayout))
		refresh := func() {
			syncing = true
			defer func() { syncing = false }()
			if i := slices.Index(names, lang()); i >= 0 && language.Selected() != i {
				language.SetSelected(i)
			}
			if keymap.Text() != p.props.str("VConsoleKeymap") {
				keymap.SetText(p.props.str("VConsoleKeymap"))
			}
			layout.SetText(strings.Join(slices.DeleteFunc([]string{p.props.str("X11Layout"), p.props.str("X11Variant"), p.props.str("X11Options")}, func(s string) bool { return s == "" }), " · "))
			showPreview()
		}
		p.refresh = refresh
		refresh()
		p.body.Append(rows, false)
	})
	content := sectionStack()
	content.Append(newSection(k, "settings-section-region-formats", formats), false)
	content.Append(newSection(k, "settings-section-region-system", p), false)
	return &closingBox{content, p.close}
}

type localesListing struct {
	names []string
	err   error
}

type keyboardNames struct{ layout, model, variant, options string }

func wmKeyboard(k *kit) keyboardNames {
	w, err := k.wmBackend()
	if err != nil {
		return keyboardNames{}
	}
	get := func(key string) string { s, _ := w.value("input.keyboard." + key).(string); return s }
	return keyboardNames{layout: get("layout"), model: get("model"), variant: get("variant"), options: get("options")}
}
