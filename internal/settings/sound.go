package settings

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"slices"
	"sync/atomic"
	"time"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-shell/service/pulse"
	"github.com/stubbedev/lys-shell/service/pulse/native"
	"github.com/stubbedev/lys-shell/shell/widgets"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	soundID      = "sound"
	soundTimeout = 5 * time.Second
	balanceRange = 100
)

var openPulse = func(ctx context.Context) (*pulse.Service, error) { return pulse.Connect(ctx, "") }

func soundPage(*config.Config) pageSpec {
	return pageSpec{
		id: soundID, navKey: "settings-nav-sound", icon: "ld-audio-lines-symbolic",
		header: "settings-page-sound",
		lead:   soundLead,
		sections: []sectionSpec{{title: "settings-section-sound-alerts", rows: []rowSpec{
			field("modules.notifications.sounds"),
			field("modules.notifications.sound-volume", percentage),
			field("modules.notifications.sound-default"),
			field("modules.notifications.sound-mute-apps", stringList),
		}}},
	}
}

type soundConnection struct {
	svc *pulse.Service
	err error
}

type soundPanel struct {
	*widget.Box
	k       *kit
	svc     *pulse.Service
	stop    func()
	capture *pulse.Capture
	problem *widget.Label
	output  *deviceControls
	input   *deviceControls
	meter   *levelMeter
	apps    *widget.ItemGroup
	noApps  *widget.Label
	appRows map[pulse.StreamKey]*appRow
	appKeys []pulse.StreamKey
	ticked  atomic.Bool
	closed  bool
	syncing bool
}

func soundLead(k *kit) widget.Widget {
	t := locale.T()
	p := &soundPanel{Box: gapBox(widget.Column), k: k, appRows: map[pulse.StreamKey]*appRow{}}
	p.problem = wrapped(k.label("", css.Error, css.Caption))
	p.problem.SetVisible(false)
	p.Append(p.problem, false)
	p.output = newDeviceControls(p, pulse.DeviceOutput)
	p.input = newDeviceControls(p, pulse.DeviceInput)
	p.meter = newLevelMeter(k)
	p.meter.SetAccessibleName(t.Get("settings-sound-level"))
	p.input.rows.Append(panelRow(k, t.Get("settings-sound-level"), "", p.meter))
	p.apps = widget.NewItemGroup()
	p.noApps = k.label(t.Get("settings-sound-no-apps"), css.DimLabel)
	apps := gapBox(widget.Column)
	apps.Append(p.noApps, false)
	apps.Append(p.apps, false)
	p.Append(loginSection(k, "settings-section-sound-output", p.output.body), false)
	p.Append(loginSection(k, "settings-section-sound-input", p.input.body), false)
	p.Append(loginSection(k, "settings-section-sound-apps", apps), false)
	p.connect()
	return p
}

func (p *soundPanel) connect() {
	async(p.k, func() soundConnection {
		ctx, cancel := context.WithTimeout(context.Background(), soundTimeout)
		defer cancel()
		svc, err := openPulse(ctx)
		return soundConnection{svc, err}
	}, func(c soundConnection) {
		if c.err != nil {
			p.fail(fmt.Errorf("%s: %w", locale.T().Get("settings-sound-unreachable"), c.err))
			return
		}
		if p.closed {
			_ = c.svc.Close()
			return
		}
		p.svc = c.svc
		ticks, stop, err := c.svc.Subscribe(context.Background())
		if err != nil {
			p.fail(err)
			return
		}
		p.stop = stop
		go func() {
			for range ticks {
				if p.ticked.CompareAndSwap(false, true) {
					p.k.invoke(p.tick)
				}
			}
		}()
		capture, err := c.svc.Capture(pulse.CaptureDefaultInput(), meterSpec, p.meter.feed)
		if err != nil {
			p.fail(err)
		} else {
			p.capture = capture
		}
		p.render()
	})
}

func (p *soundPanel) tick() {
	p.ticked.Store(false)
	p.render()
}

func (p *soundPanel) close() {
	p.closed = true
	if p.capture != nil {
		p.capture.Close()
		p.capture = nil
	}
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	if p.svc != nil {
		_ = p.svc.Close()
		p.svc = nil
	}
}

func (p *soundPanel) fail(err error) {
	log.Printf("settings: sound: %v", err)
	p.problem.SetText(err.Error())
	p.problem.SetVisible(true)
}

func (p *soundPanel) act(change func(context.Context, *pulse.Service) error) {
	svc := p.svc
	if svc == nil {
		return
	}
	async(p.k, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), soundTimeout)
		defer cancel()
		return change(ctx, svc)
	}, func(err error) {
		if err != nil {
			p.fail(err)
		}
	})
}

func (p *soundPanel) write(change func(context.Context, *pulse.Service) error) func() {
	svc := p.svc
	if svc == nil {
		return nil
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), soundTimeout)
		defer cancel()
		if err := change(ctx, svc); err != nil {
			p.k.invoke(func() { p.fail(err) })
		}
	}
}

func (p *soundPanel) render() {
	if p.svc == nil {
		return
	}
	p.syncing = true
	defer func() { p.syncing = false }()
	cards := p.svc.Cards()
	outputs := p.svc.OutputDevices()
	outDevices := make([]pulse.Device, len(outputs))
	for i, d := range outputs {
		outDevices[i] = d.Device
	}
	out, hasOut := p.svc.DefaultOutput()
	p.output.show(outDevices, out.Device, hasOut, cards)
	in, hasIn := p.svc.DefaultInput()
	var inDevices []pulse.Device
	for _, d := range p.svc.InputDevices() {
		if !d.IsMonitor() || (hasIn && d.Key == in.Key) {
			inDevices = append(inDevices, d.Device)
		}
	}
	p.input.show(inDevices, in.Device, hasIn, cards)
	p.showApps(p.svc.PlaybackStreams())
}

func (p *soundPanel) showApps(streams []pulse.AudioStream) {
	keys := make([]pulse.StreamKey, len(streams))
	for i, s := range streams {
		keys[i] = s.Key
	}
	if !slices.Equal(keys, p.appKeys) {
		p.apps.Clear()
		clear(p.appRows)
		for _, s := range streams {
			row := newAppRow(p, s.Key)
			p.appRows[s.Key] = row
			p.apps.Append(row.Box)
		}
		p.appKeys = keys
	}
	p.noApps.SetVisible(len(streams) == 0)
	p.apps.SetVisible(len(streams) > 0)
	for _, s := range streams {
		p.appRows[s.Key].show(s)
	}
}

type deviceControls struct {
	p          *soundPanel
	kind       pulse.DeviceType
	body       *widget.Box
	none       *widget.Label
	rows       *widget.ItemGroup
	devices    *widget.Dropdown
	keys       []pulse.DeviceKey
	port       *widget.Dropdown
	portRow    *widget.Box
	ports      []string
	profile    *widget.Dropdown
	profileRow *widget.Box
	profiles   []string
	card       uint32
	volume     *widgets.DebouncedSlider
	mute       *widget.Switch
	balance    *widgets.DebouncedSlider
	balanceRow *widget.Box
	tests      *widget.Box
	testsRow   *widget.Box
	testMap    native.ChannelMap
	current    pulse.Device
}

func newDeviceControls(p *soundPanel, kind pulse.DeviceType) *deviceControls {
	t := locale.T()
	k := p.k
	c := &deviceControls{p: p, kind: kind, body: gapBox(widget.Column), rows: widget.NewItemGroup()}
	noneKey, deviceKey, muteKey := "settings-sound-no-input", "settings-sound-input-device", "settings-sound-mute-input"
	if kind == pulse.DeviceOutput {
		noneKey, deviceKey, muteKey = "settings-sound-no-output", "settings-sound-output-device", "settings-sound-mute-output"
	}
	c.none = k.label(t.Get(noneKey), css.DimLabel)
	c.body.Append(c.none, false)
	c.body.Append(c.rows, false)
	c.devices = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	c.devices.SetAccessibleName(t.Get(deviceKey))
	c.devices.OnSelect = func(i int) {
		if p.syncing || i >= len(c.keys) {
			return
		}
		key := c.keys[i]
		p.act(func(ctx context.Context, svc *pulse.Service) error { return svc.SetDefault(ctx, key) })
	}
	c.rows.Append(panelRow(k, t.Get("settings-sound-device"), "", c.devices))
	c.port = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	c.port.SetAccessibleName(t.Get("settings-sound-port"))
	c.port.OnSelect = func(i int) {
		if p.syncing || i >= len(c.ports) {
			return
		}
		key, port := c.current.Key, c.ports[i]
		p.act(func(ctx context.Context, svc *pulse.Service) error { return svc.SetDevicePort(ctx, key, port) })
	}
	c.portRow = panelRow(k, t.Get("settings-sound-port"), "", c.port)
	c.rows.Append(c.portRow)
	c.profile = widget.NewDropdown(k.face, k.textPx(), nil, 0)
	c.profile.SetAccessibleName(t.Get("settings-sound-profile"))
	c.profile.OnSelect = func(i int) {
		if p.syncing || i >= len(c.profiles) {
			return
		}
		card, profile := c.card, c.profiles[i]
		p.act(func(ctx context.Context, svc *pulse.Service) error { return svc.SetCardProfile(ctx, card, profile) })
	}
	c.profileRow = panelRow(k, t.Get("settings-sound-profile"), t.Attr("settings-sound-profile", "description"), c.profile)
	c.rows.Append(c.profileRow)
	c.volume = widgets.NewDebouncedSlider(0, k.face, k.textPx(), 0, k.invoke)
	c.volume.Knob.SetAccessibleName(t.Get("settings-sound-volume"))
	c.volume.Write = func(v float64) func() {
		key, levels := c.current.Key, scaledTo(c.current.Volume.Levels(), v/100)
		return p.write(func(ctx context.Context, svc *pulse.Service) error {
			return svc.SetDeviceVolume(ctx, key, pulse.NewVolume(levels))
		})
	}
	c.mute = widget.NewSwitch(false)
	c.mute.SetAccessibleName(t.Get(muteKey))
	c.mute.OnChanged = func(on bool) {
		if p.syncing {
			return
		}
		key := c.current.Key
		p.act(func(ctx context.Context, svc *pulse.Service) error { return svc.SetDeviceMute(ctx, key, on) })
	}
	c.rows.Append(panelRow(k, t.Get("settings-sound-volume"), "", c.volume, c.mute))
	c.balance = widgets.NewRangedSlider(-balanceRange, balanceRange, 0, nil, 0, 0, k.invoke)
	c.balance.Knob.SetAccessibleName(t.Get("settings-sound-balance"))
	c.balance.Write = func(b float64) func() {
		key, levels := c.current.Key, withBalance(c.current.Volume.Levels(), c.current.ChannelMap, b/balanceRange)
		return p.write(func(ctx context.Context, svc *pulse.Service) error {
			return svc.SetDeviceVolume(ctx, key, pulse.NewVolume(levels))
		})
	}
	c.balanceRow = panelRow(k, t.Get("settings-sound-balance"), "", c.balance)
	c.rows.Append(c.balanceRow)
	if kind == pulse.DeviceOutput {
		c.tests = gapBox(widget.Row)
		c.testsRow = panelRow(k, t.Get("settings-sound-test"), t.Attr("settings-sound-test", "description"), c.tests)
		c.rows.Append(c.testsRow)
	}
	return c
}

func (c *deviceControls) show(devices []pulse.Device, current pulse.Device, has bool, cards []pulse.Card) {
	t := locale.T()
	c.none.SetVisible(!has)
	c.rows.SetVisible(has)
	if !has {
		c.keys = nil
		return
	}
	c.current = current
	c.keys = make([]pulse.DeviceKey, len(devices))
	names := make([]string, len(devices))
	for i, d := range devices {
		c.keys[i], names[i] = d.Key, cmp.Or(d.Description, d.Name)
	}
	setChoices(c.devices, names, slices.Index(c.keys, current.Key))
	c.ports = c.ports[:0]
	portNames := make([]string, 0, len(current.Ports))
	for _, port := range current.Ports {
		c.ports = append(c.ports, port.Name)
		name := cmp.Or(port.Description, port.Name)
		if !port.Available {
			name = t.Get("settings-sound-unplugged", i18n.Str("name", name))
		}
		portNames = append(portNames, name)
	}
	c.portRow.SetVisible(len(c.ports) > 1)
	setChoices(c.port, portNames, slices.Index(c.ports, current.ActivePort))
	c.showProfiles(current, cards)
	c.volume.Set(peakLevel(current.Volume.Levels()) * 100)
	if c.mute.On() != current.Muted {
		c.mute.SetOn(current.Muted)
	}
	b, stereo := balanceOf(current.Volume.Levels(), current.ChannelMap)
	c.balanceRow.SetVisible(stereo)
	c.balance.Set(b * balanceRange)
	if c.tests != nil {
		c.showTests(current)
	}
}

func (c *deviceControls) showProfiles(current pulse.Device, cards []pulse.Card) {
	t := locale.T()
	c.profiles = c.profiles[:0]
	index, ok := current.Card.Get()
	var card pulse.Card
	if ok {
		i := slices.IndexFunc(cards, func(card pulse.Card) bool { return card.Index == index })
		ok = i >= 0
		if ok {
			card = cards[i]
		}
	}
	c.profileRow.SetVisible(ok && len(card.Profiles) > 1)
	if !ok {
		return
	}
	c.card = card.Index
	names := make([]string, 0, len(card.Profiles))
	for _, profile := range card.Profiles {
		c.profiles = append(c.profiles, profile.Name)
		name := cmp.Or(profile.Description, profile.Name)
		if !profile.Available {
			name = t.Get("settings-sound-unavailable", i18n.Str("name", name))
		}
		names = append(names, name)
	}
	setChoices(c.profile, names, slices.Index(c.profiles, card.ActiveProfile))
}

func (c *deviceControls) showTests(current pulse.Device) {
	c.testsRow.SetVisible(len(current.ChannelMap) > 0)
	if slices.Equal(c.testMap, current.ChannelMap) {
		return
	}
	t := locale.T()
	c.testMap = slices.Clone(current.ChannelMap)
	c.tests.Clear()
	for i, position := range c.testMap {
		name := t.Get("settings-sound-channel-" + position.String())
		b := c.p.k.button(c.p.k.label(name), nil, css.Small)
		b.SetAccessibleName(t.Get("settings-sound-test-channel", i18n.Str("channel", name)))
		channels, channel := c.testMap, i
		b.OnClick = func() {
			sink := c.current.Name
			c.p.act(func(ctx context.Context, svc *pulse.Service) error {
				return playTestTone(ctx, svc, sink, channels, channel)
			})
		}
		c.tests.Append(b, false)
	}
}

func setChoices(d *widget.Dropdown, items []string, selected int) {
	if slices.Equal(d.Items(), items) && d.Selected() == selected {
		return
	}
	d.SetItems(items, selected)
}

type appRow struct {
	*widget.Box
	title  *widget.Label
	detail *widget.Label
	volume *widgets.DebouncedSlider
	mute   *widget.Switch
	levels []float64
}

func newAppRow(p *soundPanel, key pulse.StreamKey) *appRow {
	k := p.k
	r := &appRow{}
	r.title = k.label("", "setting-label")
	r.detail = k.label("", "setting-description", css.Caption, css.DimLabel)
	r.volume = widgets.NewDebouncedSlider(0, k.face, k.textPx(), 0, k.invoke)
	r.volume.Write = func(v float64) func() {
		levels := scaledTo(r.levels, v/100)
		return p.write(func(ctx context.Context, svc *pulse.Service) error {
			return svc.SetStreamVolume(ctx, key, pulse.NewVolume(levels))
		})
	}
	r.mute = widget.NewSwitch(false)
	r.mute.OnChanged = func(on bool) {
		if p.syncing {
			return
		}
		p.act(func(ctx context.Context, svc *pulse.Service) error { return svc.SetStreamMute(ctx, key, on) })
	}
	text := widget.NewBox(widget.Column, widget.Current().Spacing/4, 0)
	text.Append(r.title, false)
	text.Append(r.detail, false)
	r.Box = gapBox(widget.Row)
	r.SetElement("item")
	r.AddClass(css.Small, "setting-row", "sound-app")
	r.AppendAligned(text, true, widget.AlignCenter)
	r.AppendAligned(r.volume, false, widget.AlignCenter)
	r.AppendAligned(r.mute, false, widget.AlignCenter)
	return r
}

func (r *appRow) show(s pulse.AudioStream) {
	t := locale.T()
	name := cmp.Or(s.ApplicationName, s.Binary, s.Name)
	r.title.SetText(name)
	r.detail.SetText(cmp.Or(s.Media.Title, s.Name))
	r.volume.Knob.SetAccessibleName(t.Get("settings-sound-app-volume", i18n.Str("app", name)))
	r.mute.SetAccessibleName(t.Get("settings-sound-app-mute", i18n.Str("app", name)))
	r.levels = s.Volume.Levels()
	r.volume.Knob.SetEnabled(s.HasVolume && s.VolumeWritable)
	r.volume.Set(peakLevel(r.levels) * 100)
	if r.mute.On() != s.Muted {
		r.mute.SetOn(s.Muted)
	}
}
