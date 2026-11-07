// Package displays reads and applies the compositor's output layout over
// wlr-output-management, and arranges outputs on a plane: the logical
// rectangles the Displays page drags, snaps and normalizes.
package displays

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/lys-lib/intconv"
	"github.com/stubbedev/lys-lib/third_party/wayland/wl"
	"github.com/stubbedev/lys-lib/third_party/wayland/wlclient"
	"github.com/stubbedev/lys-lib/wlr"
)

const (
	managerIface   = "zwlr_output_manager_v1"
	managerVersion = 4
	answerTimeout  = 5 * time.Second
)

// ErrNoOutputManagement is a compositor without wlr-output-management.
var ErrNoOutputManagement = errors.New("displays: the compositor offers no " + managerIface)

// ErrRefused is a configuration the compositor would not take.
var ErrRefused = errors.New("displays: the compositor refused the layout")

// ErrSuperseded is a configuration the compositor cancelled because the
// outputs changed while it was being made.
var ErrSuperseded = errors.New("displays: the outputs changed before the layout was applied")

// Mode is one mode a head offers.
type Mode struct {
	Width, Height int
	RefreshMHz    int
	Preferred     bool
}

// Head is one output as the compositor describes it.
type Head struct {
	Name, Description   string
	Make, Model, Serial string
	Enabled             bool
	Modes               []Mode
	Current             int
	X, Y                int
	Scale               float64
}

// CurrentMode is the head's current mode, false while it has none.
func (h Head) CurrentMode() (Mode, bool) {
	if h.Current < 0 || h.Current >= len(h.Modes) {
		return Mode{}, false
	}
	return h.Modes[h.Current], true
}

// Setting is what a configuration asks of one head.
type Setting struct {
	Name    string
	Enabled bool
	Mode    Mode
	X, Y    int
	Scale   float64
}

// Settings is the layout the heads have now.
func Settings(heads []Head) []Setting {
	out := make([]Setting, len(heads))
	for i, h := range heads {
		mode, _ := h.CurrentMode()
		out[i] = Setting{Name: h.Name, Enabled: h.Enabled, Mode: mode, X: h.X, Y: h.Y, Scale: h.Scale}
	}
	return out
}

type mode struct {
	m    *Manager
	obj  *wlr.ZwlrOutputModeV1
	mode Mode
}

type head struct {
	m       *Manager
	obj     *wlr.ZwlrOutputHeadV1
	info    Head
	modes   []*mode
	current *mode
}

// Manager is a connection of its own to the compositor that follows its
// heads. One goroutine reads the connection; requests go out from any
// goroutine under mu.
type Manager struct {
	ctx     *wl.Context
	display *wl.Display
	reg     *wl.Registry
	onHeads func([]Head)
	ended   chan struct{}

	mu       sync.Mutex
	manager  *wlr.ZwlrOutputManagerV1
	heads    []*head
	serial   uint32
	closing  bool
	finished bool
}

// Connect connects to the compositor named by WAYLAND_DISPLAY; onHeads
// hears every complete set of heads, the first before Connect returns,
// on the connection's goroutine.
func Connect(onHeads func([]Head)) (*Manager, error) {
	display, err := wl.Connect("")
	if err != nil {
		return nil, fmt.Errorf("displays: connect: %w", err)
	}
	m := &Manager{ctx: display.Context(), display: display, onHeads: onHeads, ended: make(chan struct{})}
	if m.reg, err = display.GetRegistry(); err != nil {
		_ = m.ctx.Close()
		return nil, fmt.Errorf("displays: registry: %w", err)
	}
	wlclient.RegistryAddListener(m.reg, m)
	if err := m.roundtrip(); err != nil {
		_ = m.ctx.Close()
		return nil, fmt.Errorf("displays: initial roundtrip: %w", err)
	}
	if m.manager == nil {
		_ = m.ctx.Close()
		return nil, ErrNoOutputManagement
	}
	if err := m.roundtrip(); err != nil {
		_ = m.ctx.Close()
		return nil, fmt.Errorf("displays: read the heads: %w", err)
	}
	go m.dispatch()
	return m, nil
}

func (m *Manager) roundtrip() error {
	cb, err := m.display.Sync()
	if err != nil {
		return err
	}
	return m.ctx.RunTill(cb)
}

// Close closes the connection once a final sync comes back.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closing = true
	if m.manager != nil {
		_ = m.manager.Stop()
	}
	cb := wl.NewCallback(m.ctx)
	cb.AddDoneHandler(m)
	_ = m.ctx.SendRequest(m.display, 0, cb)
	m.mu.Unlock()
	<-m.ended
}

// HandleCallbackDone is Close's final sync: the read loop stops.
func (m *Manager) HandleCallbackDone(wl.CallbackDoneEvent) { m.finished = true }

func (m *Manager) dispatch() {
	defer close(m.ended)
	defer func() { _ = m.ctx.Close() }()
	for !m.finished {
		err := m.ctx.Run()
		if errors.Is(err, wl.ErrContextRunProxyNil) {
			continue
		}
		if err != nil {
			return
		}
	}
}

// HandleRegistryGlobal binds the output manager.
func (m *Manager) HandleRegistryGlobal(ev wl.RegistryGlobalEvent) {
	if ev.Interface != managerIface || m.manager != nil {
		return
	}
	mgr := wlr.NewZwlrOutputManagerV1(m.ctx)
	mgr.AddHeadHandler(m)
	mgr.AddDoneHandler(m)
	mgr.AddFinishedHandler(m)
	if m.reg.Bind(ev.Name, ev.Interface, min(ev.Version, managerVersion), mgr) == nil {
		m.manager = mgr
	}
}

// HandleRegistryGlobalRemove ignores globals going away; a head leaving
// is the manager's to tell.
func (m *Manager) HandleRegistryGlobalRemove(wl.RegistryGlobalRemoveEvent) {}

// HandleZwlrOutputManagerV1Head follows a new head.
func (m *Manager) HandleZwlrOutputManagerV1Head(ev wlr.ZwlrOutputManagerV1HeadEvent) {
	h := &head{m: m, obj: ev.Head, info: Head{Current: -1, Scale: 1}}
	o := ev.Head
	o.AddNameHandler(h)
	o.AddDescriptionHandler(h)
	o.AddModeHandler(h)
	o.AddEnabledHandler(h)
	o.AddCurrentModeHandler(h)
	o.AddPositionHandler(h)
	o.AddScaleHandler(h)
	o.AddFinishedHandler(h)
	o.AddMakeHandler(h)
	o.AddModelHandler(h)
	o.AddSerialNumberHandler(h)
	m.mu.Lock()
	m.heads = append(m.heads, h)
	m.mu.Unlock()
}

// HandleZwlrOutputManagerV1Done publishes the heads as they now are.
func (m *Manager) HandleZwlrOutputManagerV1Done(ev wlr.ZwlrOutputManagerV1DoneEvent) {
	m.mu.Lock()
	m.serial = ev.Serial
	heads := m.snapshotLocked()
	m.mu.Unlock()
	m.onHeads(heads)
}

// HandleZwlrOutputManagerV1Finished drops the manager the compositor let go.
func (m *Manager) HandleZwlrOutputManagerV1Finished(wlr.ZwlrOutputManagerV1FinishedEvent) {
	m.mu.Lock()
	m.manager = nil
	m.mu.Unlock()
}

func (m *Manager) snapshotLocked() []Head {
	out := make([]Head, 0, len(m.heads))
	for _, h := range m.heads {
		info := h.info
		info.Modes = make([]Mode, len(h.modes))
		info.Current = -1
		for i, md := range h.modes {
			info.Modes[i] = md.mode
			if md == h.current {
				info.Current = i
			}
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b Head) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func (h *head) HandleZwlrOutputHeadV1Name(ev wlr.ZwlrOutputHeadV1NameEvent) {
	h.set(func(i *Head) { i.Name = ev.Name })
}

func (h *head) HandleZwlrOutputHeadV1Description(ev wlr.ZwlrOutputHeadV1DescriptionEvent) {
	h.set(func(i *Head) { i.Description = ev.Description })
}

func (h *head) HandleZwlrOutputHeadV1Make(ev wlr.ZwlrOutputHeadV1MakeEvent) {
	h.set(func(i *Head) { i.Make = ev.Make })
}

func (h *head) HandleZwlrOutputHeadV1Model(ev wlr.ZwlrOutputHeadV1ModelEvent) {
	h.set(func(i *Head) { i.Model = ev.Model })
}

func (h *head) HandleZwlrOutputHeadV1SerialNumber(ev wlr.ZwlrOutputHeadV1SerialNumberEvent) {
	h.set(func(i *Head) { i.Serial = ev.SerialNumber })
}

func (h *head) HandleZwlrOutputHeadV1Enabled(ev wlr.ZwlrOutputHeadV1EnabledEvent) {
	h.set(func(i *Head) { i.Enabled = ev.Enabled != 0 })
}

func (h *head) HandleZwlrOutputHeadV1Position(ev wlr.ZwlrOutputHeadV1PositionEvent) {
	h.set(func(i *Head) { i.X, i.Y = int(ev.X), int(ev.Y) })
}

func (h *head) HandleZwlrOutputHeadV1Scale(ev wlr.ZwlrOutputHeadV1ScaleEvent) {
	h.set(func(i *Head) { i.Scale = float64(ev.Scale) })
}

func (h *head) set(fn func(*Head)) {
	h.m.mu.Lock()
	fn(&h.info)
	h.m.mu.Unlock()
}

func (h *head) HandleZwlrOutputHeadV1Mode(ev wlr.ZwlrOutputHeadV1ModeEvent) {
	md := &mode{m: h.m, obj: ev.Mode}
	ev.Mode.AddSizeHandler(md)
	ev.Mode.AddRefreshHandler(md)
	ev.Mode.AddPreferredHandler(md)
	ev.Mode.AddFinishedHandler(&modeGone{h: h, md: md})
	h.m.mu.Lock()
	h.modes = append(h.modes, md)
	h.m.mu.Unlock()
}

func (h *head) HandleZwlrOutputHeadV1CurrentMode(ev wlr.ZwlrOutputHeadV1CurrentModeEvent) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	for _, md := range h.modes {
		if md.obj == ev.Mode {
			h.current = md
		}
	}
}

func (h *head) HandleZwlrOutputHeadV1Finished(wlr.ZwlrOutputHeadV1FinishedEvent) {
	h.m.mu.Lock()
	h.m.heads = slices.DeleteFunc(h.m.heads, func(x *head) bool { return x == h })
	h.m.mu.Unlock()
	_ = h.obj.Release()
}

func (md *mode) HandleZwlrOutputModeV1Size(ev wlr.ZwlrOutputModeV1SizeEvent) {
	md.set(func(m *Mode) { m.Width, m.Height = int(ev.Width), int(ev.Height) })
}

func (md *mode) HandleZwlrOutputModeV1Refresh(ev wlr.ZwlrOutputModeV1RefreshEvent) {
	md.set(func(m *Mode) { m.RefreshMHz = int(ev.Refresh) })
}

func (md *mode) HandleZwlrOutputModeV1Preferred(wlr.ZwlrOutputModeV1PreferredEvent) {
	md.set(func(m *Mode) { m.Preferred = true })
}

func (md *mode) set(fn func(*Mode)) {
	md.m.mu.Lock()
	fn(&md.mode)
	md.m.mu.Unlock()
}

type modeGone struct {
	h  *head
	md *mode
}

func (g *modeGone) HandleZwlrOutputModeV1Finished(wlr.ZwlrOutputModeV1FinishedEvent) {
	g.h.m.mu.Lock()
	g.h.modes = slices.DeleteFunc(g.h.modes, func(x *mode) bool { return x == g.md })
	if g.h.current == g.md {
		g.h.current = nil
	}
	g.h.m.mu.Unlock()
	_ = g.md.obj.Release()
}

type outcome struct{ result chan error }

func (o outcome) HandleZwlrOutputConfigurationV1Succeeded(wlr.ZwlrOutputConfigurationV1SucceededEvent) {
	o.result <- nil
}

func (o outcome) HandleZwlrOutputConfigurationV1Failed(wlr.ZwlrOutputConfigurationV1FailedEvent) {
	o.result <- ErrRefused
}

func (o outcome) HandleZwlrOutputConfigurationV1Cancelled(wlr.ZwlrOutputConfigurationV1CancelledEvent) {
	o.result <- ErrSuperseded
}

// Test asks whether the compositor would take settings, changing nothing.
func (m *Manager) Test(settings []Setting) error { return m.configure(settings, true) }

// Apply applies settings as one configuration.
func (m *Manager) Apply(settings []Setting) error { return m.configure(settings, false) }

func (m *Manager) configure(settings []Setting, test bool) error {
	m.mu.Lock()
	if m.manager == nil || m.closing {
		m.mu.Unlock()
		return ErrNoOutputManagement
	}
	cfg, err := m.manager.CreateConfiguration(m.serial)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("displays: create a configuration: %w", err)
	}
	o := outcome{result: make(chan error, 1)}
	cfg.AddSucceededHandler(o)
	cfg.AddFailedHandler(o)
	cfg.AddCancelledHandler(o)
	err = m.fillLocked(cfg, settings)
	if err == nil {
		if test {
			err = cfg.Test()
		} else {
			err = cfg.Apply()
		}
	}
	m.mu.Unlock()
	if err != nil {
		_ = cfg.Destroy()
		return err
	}
	defer func() { _ = cfg.Destroy() }()
	select {
	case err := <-o.result:
		return err
	case <-time.After(answerTimeout):
		return fmt.Errorf("displays: the compositor did not answer the configuration in %s", answerTimeout)
	case <-m.ended:
		return errors.New("displays: the compositor connection ended")
	}
}

func (m *Manager) fillLocked(cfg *wlr.ZwlrOutputConfigurationV1, settings []Setting) error {
	for _, s := range settings {
		i := slices.IndexFunc(m.heads, func(h *head) bool { return h.info.Name == s.Name })
		if i < 0 {
			return fmt.Errorf("displays: no output %s", s.Name)
		}
		h := m.heads[i]
		if !s.Enabled {
			if err := cfg.DisableHead(h.obj); err != nil {
				return fmt.Errorf("displays: disable %s: %w", s.Name, err)
			}
			continue
		}
		j := slices.IndexFunc(h.modes, func(md *mode) bool { return md.mode == s.Mode })
		if j < 0 {
			return fmt.Errorf("displays: %s has no mode %dx%d@%d", s.Name, s.Mode.Width, s.Mode.Height, s.Mode.RefreshMHz)
		}
		ch, err := cfg.EnableHead(h.obj)
		if err != nil {
			return fmt.Errorf("displays: enable %s: %w", s.Name, err)
		}
		x, errX := intconv.Exact[int32](s.X)
		y, errY := intconv.Exact[int32](s.Y)
		if err := errors.Join(errX, errY); err != nil {
			return fmt.Errorf("displays: position %s: %w", s.Name, err)
		}
		if err := errors.Join(ch.SetMode(h.modes[j].obj), ch.SetPosition(x, y), ch.SetScale(float32(s.Scale))); err != nil {
			return fmt.Errorf("displays: configure %s: %w", s.Name, err)
		}
	}
	return nil
}
