package settings

import (
	"context"
	"encoding/binary"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-shell/i18n"
	"github.com/stubbedev/lys-shell/service/pulse"
	"github.com/stubbedev/lys-shell/service/pulse/native"
	"github.com/stubbedev/lys-shell/service/pulse/pulsetest"
	"github.com/stubbedev/lys-shell/shell/apptheme"
	"github.com/stubbedev/lys-shell/shell/treetest"
	"github.com/stubbedev/lys-ui/widget"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	headsetSink   = "bluez_sink.00_11"
	headsetCardID = 7
)

func headsetCard(active string) native.CardInfo {
	return native.CardInfo{
		Index: headsetCardID, Name: "bluez_card.00_11", OwnerModule: native.InvalidIndex, Driver: "module-bluez5-device.c",
		Profiles: []native.CardProfile{
			{Name: "a2dp-sink", Description: "High Fidelity Playback (A2DP Sink)", Sinks: 1, Priority: 40, Available: true},
			{Name: "headset-head-unit", Description: "Headset Head Unit (HSP/HFP)", Sinks: 1, Sources: 1, Priority: 30, Available: true},
			{Name: "off", Description: "Off", Available: true},
		},
		ActiveProfile: active,
		Props:         native.PropList{"device.description": "Headset"},
	}
}

func firefox() native.StreamInfo {
	return native.StreamInfo{
		Index: 40, Name: "Playback", OwnerModule: native.InvalidIndex, Client: 12, Device: 1,
		SampleSpec: pulsetest.StereoSpec(), ChannelMap: pulsetest.StereoMap(), Volume: native.CVolume{native.VolumeNorm, native.VolumeNorm},
		Props:     native.PropList{"application.name": "Firefox", "media.title": "Song"},
		HasVolume: true, VolumeWritable: true,
		Format: native.FormatInfo{Encoding: native.EncodingPCM, Props: native.PropList{}},
	}
}

func soundServer(t *testing.T) *pulsetest.Server {
	t.Helper()
	srv := pulsetest.New(t)
	srv.PutSink(pulsetest.Sink(1, "speakers", "Speakers", 2))
	headset := pulsetest.Sink(5, headsetSink, "Headset", 6)
	headset.Card, headset.Ports, headset.ActivePort = headsetCardID, nil, ""
	srv.PutSink(headset)
	srv.PutSource(pulsetest.Source(2, "speakers.monitor", "Monitor of Speakers", 1, "speakers"))
	srv.PutSource(pulsetest.Source(6, headsetSink+".monitor", "Monitor of Headset", 5, headsetSink))
	srv.PutSource(pulsetest.Source(10, "mic", "Microphone", native.InvalidIndex, ""))
	srv.PutCard(headsetCard("a2dp-sink"))
	srv.PutSinkInput(firefox())
	srv.SetDefaults(headsetSink, "mic")
	return srv
}

func useSoundServer(t *testing.T, srv *pulsetest.Server) {
	t.Helper()
	before := openPulse
	openPulse = func(ctx context.Context) (*pulse.Service, error) { return pulse.Connect(ctx, srv.Addr()) }
	t.Cleanup(func() { openPulse = before })
}

type soundRig struct {
	srv *pulsetest.Server
	p   *soundPanel
	l   *loop
}

func startSound(t *testing.T) *soundRig {
	t.Helper()
	srv := soundServer(t)
	useSoundServer(t, srv)
	k := testKit(t, "")
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	p := soundLead(k).(*soundPanel)
	t.Cleanup(p.close)
	return &soundRig{srv: srv, p: p, l: l}
}

func calls(srv *pulsetest.Server, cmd uint32) int {
	n := 0
	for _, c := range srv.Calls() {
		if c == cmd {
			n++
		}
	}
	return n
}

func TestChoosingAProfileIsOneSetCardProfileAndThePageFollowsTheServer(t *testing.T) {
	r := startSound(t)
	profile := r.p.output.profile
	if !r.p.output.profileRow.Visible() || profile.Selection() != "High Fidelity Playback (A2DP Sink)" {
		t.Fatalf("profile row shown %v on %q", r.p.output.profileRow.Visible(), profile.Selection())
	}
	profile.SetSelected(1)
	if n := calls(r.srv, native.CmdSetCardProfile); n != 1 {
		t.Fatalf("%d SET_CARD_PROFILE for one choice", n)
	}
	if card, _ := r.srv.Card(headsetCardID); card.ActiveProfile != "headset-head-unit" {
		t.Fatalf("the server's card is on %q", card.ActiveProfile)
	}
	r.srv.PutCard(headsetCard("off"))
	r.l.until("the server's profile change shown", func() bool { return profile.Selection() == "Off" })
	if n := calls(r.srv, native.CmdSetCardProfile); n != 1 {
		t.Fatalf("following the server sent %d more SET_CARD_PROFILE", n-1)
	}
}

func TestAHeadsetSwitchedToHFPAppearsInTheInputList(t *testing.T) {
	r := startSound(t)
	if got := r.p.input.devices.Items(); !slices.Equal(got, []string{"Microphone"}) {
		t.Fatalf("inputs %q", got)
	}
	r.p.output.profile.SetSelected(1)
	source := pulsetest.Source(8, "bluez_source.00_11", "Headset Microphone", native.InvalidIndex, "")
	source.Card = headsetCardID
	r.srv.PutSource(source)
	r.l.until("the headset's microphone listed", func() bool {
		return slices.Contains(r.p.input.devices.Items(), "Headset Microphone")
	})
}

func TestTheLevelMeterRecordsOnlyWhileThePageIsShown(t *testing.T) {
	srv := soundServer(t)
	useSoundServer(t, srv)
	k := testKit(t, "")
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	w := soundWindow(t, k)
	if len(srv.Records()) != 0 {
		t.Fatal("a capture before the Sound page was shown")
	}
	w.show(soundID)
	rec := srv.WaitRecords(t, 1)[0]
	if rec.Source != "mic" || rec.Props["media.name"] != meterSpec.Name {
		t.Fatalf("recording %q as %q", rec.Source, rec.Props["media.name"])
	}
	l.drain(0)
	w.show("general")
	srv.WaitRecords(t, 0)
	l.drain(0)
	w.show(soundID)
	srv.WaitRecords(t, 1)
}

func soundWindow(t *testing.T, k *kit) *window {
	t.Helper()
	w := newWindow(k, k.store.svc.Config(), func(_ time.Duration, fn func()) { fn() })
	w.stack.SetTransition(widget.StackNone, 0)
	t.Cleanup(func() {
		for _, p := range w.pages {
			p.close()
		}
	})
	return w
}

func pcm(amplitude int16, frames int) []byte {
	samples := make([]int16, frames)
	for i := range samples {
		samples[i] = amplitude
		if i%2 == 1 {
			samples[i] = -amplitude
		}
	}
	out := make([]byte, 2*frames)
	if _, err := binary.Encode(out, binary.LittleEndian, samples); err != nil {
		panic(err)
	}
	return out
}

func paintOnce(w widget.Widget) {
	w.Arrange(render.Rect{W: 200, H: 20})
	buf := make([]byte, render.Stride(200)*20)
	w.Paint(render.New(buf, render.Stride(200), 200, 20))
}

func TestTheLevelMeterRedrawsOncePerFrameWhateverTheSampleRate(t *testing.T) {
	r := startSound(t)
	rec := r.srv.WaitRecords(t, 1)[0]
	m := r.p.meter
	const chunks = 200
	for i := range chunks {
		rec.Push(pcm(int16(1000+100*i), 400))
	}
	r.l.until("the first redraw", func() bool { return m.redraws == 1 })
	r.l.drain(200 * time.Millisecond)
	if m.redraws != 1 {
		t.Fatalf("%d chunks with no frame painted redrew %d times", chunks, m.redraws)
	}
	paintOnce(m)
	for i := range chunks {
		rec.Push(pcm(int16(20000+10*i), 400))
	}
	r.l.until("the redraw the painted frame lets through", func() bool { return m.redraws == 2 })
	r.l.drain(200 * time.Millisecond)
	if m.redraws != 2 {
		t.Fatalf("%d chunks after one painted frame redrew %d times, want 2", chunks, m.redraws)
	}
	paintOnce(m)
	r.l.until("the loud level shown", func() bool { return m.Value() > 0.9 })
}

func TestATestTonePlaysOnTheChosenChannelOfTheDefaultOutput(t *testing.T) {
	r := startSound(t)
	treetest.Button(t, r.p.output.tests, locale.T().Get("settings-sound-test-channel", i18n.Str("channel", "Front Right"))).OnClick()
	plays := r.srv.Plays()
	if len(plays) != 1 || plays[0].Sink != headsetSink || plays[0].Volume != native.VolumeNorm {
		t.Fatalf("plays %+v", plays)
	}
	sample := r.srv.Samples()[plays[0].Name]
	samples := make([]int16, len(sample.Data)/2)
	if _, err := binary.Decode(sample.Data, binary.LittleEndian, samples); err != nil {
		t.Fatal(err)
	}
	var left, right int
	for i := 0; i+1 < len(samples); i += 2 {
		left = max(left, int(math.Abs(float64(samples[i]))))
		right = max(right, int(math.Abs(float64(samples[i+1]))))
	}
	if left != 0 || right == 0 || len(samples) != 2*toneFrames {
		t.Fatalf("left peak %d, right peak %d over %d samples", left, right, len(samples))
	}
}

func TestPortsVolumeMuteAndAppsReachTheServer(t *testing.T) {
	r := startSound(t)
	r.p.output.devices.SetSelected(0)
	r.l.until("the speakers as the output", func() bool { return r.p.output.current.Name == "speakers" })
	if !r.p.output.portRow.Visible() || r.p.output.port.Selection() != "Speaker" || r.p.output.port.Items()[1] != locale.T().Get("settings-sound-unplugged", i18n.Str("name", "Headphones")) {
		t.Fatalf("ports %q on %q", r.p.output.port.Items(), r.p.output.port.Selection())
	}
	r.p.output.port.SetSelected(1)
	if d, _ := r.srv.Sink(1); d.ActivePort != "headphones" {
		t.Fatalf("the sink's port is %q", d.ActivePort)
	}
	r.p.output.mute.SetOn(true)
	if d, _ := r.srv.Sink(1); !d.Mute {
		t.Fatal("the sink was not muted")
	}
	r.p.output.balance.Knob.SetValue(-50)
	r.l.until("the balance written", func() bool {
		d, _ := r.srv.Sink(1)
		return d.Volume[0] == 0x8000 && d.Volume[1] == 0x4000
	})
	row := r.p.appRows[pulse.StreamKey{Index: 40, Type: pulse.StreamPlayback}]
	if row.title.Text() != "Firefox" || row.detail.Text() != "Song" {
		t.Fatalf("app row %q, %q", row.title.Text(), row.detail.Text())
	}
	row.mute.SetOn(true)
	if st, _ := r.srv.SinkInput(40); !st.Mute {
		t.Fatal("the app was not muted")
	}
}

func TestBalanceKeepsTheLouderSideAndScalesTheOther(t *testing.T) {
	stereo := pulsetest.StereoMap()
	if got := withBalance([]float64{0.5, 0.5}, stereo, -0.5); !slices.Equal(got, []float64{0.5, 0.25}) {
		t.Fatalf("balance -0.5 = %v", got)
	}
	if b, ok := balanceOf([]float64{0.5, 0.25}, stereo); !ok || b != -0.5 {
		t.Fatalf("balance of 0.5/0.25 = %v, %v", b, ok)
	}
	if _, ok := balanceOf([]float64{1}, native.ChannelMap{native.ChannelMono}); ok {
		t.Fatal("a mono device has a balance")
	}
	if got := scaledTo([]float64{0.5, 0.25}, 1); !slices.Equal(got, []float64{1, 0.5}) {
		t.Fatalf("scaled = %v", got)
	}
}

func shownControls(root widget.Widget) []widget.Widget {
	hidden := map[widget.Widget]bool{}
	treetest.Walk(root, func(w widget.Widget) {
		if v, ok := w.(interface{ Visible() bool }); ok && !v.Visible() {
			treetest.Walk(w, func(x widget.Widget) { hidden[x] = true })
		}
	})
	var out []widget.Widget
	controlRoles := []widget.Role{widget.RoleComboBox, widget.RoleSwitch, widget.RoleButton, widget.RoleSlider}
	treetest.Walk(root, func(w widget.Widget) {
		r, ok := w.(interface{ Role() widget.Role })
		if !ok || !slices.Contains(controlRoles, r.Role()) {
			return
		}
		if !hidden[w] && widget.IsEnabled(w) {
			out = append(out, w)
		}
	})
	return out
}

func TestTabReachesEverySoundControl(t *testing.T) {
	srv := soundServer(t)
	useSoundServer(t, srv)
	k := testKit(t, "")
	l := &loop{t: t, queue: make(chan func(), 64)}
	k.invoke = func(fn func()) { l.queue <- fn }
	cfg := k.store.svc.Config()
	w := soundWindow(t, k)
	apptheme.New(cfg).Attach(w.root)
	w.show(soundID)
	panel := treetest.First[*soundPanel](t, w.page)
	l.until("the devices shown", func() bool { return len(panel.output.devices.Items()) == 2 })
	w.root.Arrange(render.Rect{W: 1400, H: 6000})
	router := &widget.Router{Root: w.root}
	if err := router.SetFocus(w.search); err != nil {
		t.Fatal(err)
	}
	visited := map[widget.Widget]bool{}
	for range maxTabStops {
		router.FocusNext()
		f := router.Focused()
		if f == widget.Widget(w.search) {
			break
		}
		visited[f] = true
	}
	controls := shownControls(panel)
	if len(controls) < 10 {
		t.Fatalf("only %d controls on the Sound page", len(controls))
	}
	for _, c := range controls {
		reached := false
		treetest.Walk(c, func(x widget.Widget) { reached = reached || visited[x] })
		if !reached {
			name, _ := c.(interface{ AccessibleName() string })
			t.Errorf("%T %q is out of Tab's reach", c, name.AccessibleName())
		}
	}
}
