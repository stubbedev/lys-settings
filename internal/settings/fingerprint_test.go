package settings

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/lys-desktop/shell/treetest"
	"github.com/stubbedev/lys-lib/dbustest"
	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-ui/widget"
)

const fakeReader = dbus.ObjectPath("/net/reactivated/Fprint/Device/0")

type fakeFprintManager struct {
	*fakeSys
	device bool
}

func (f fakeFprintManager) GetDefaultDevice() (dbus.ObjectPath, *dbus.Error) {
	f.record("GetDefaultDevice")
	if !f.device {
		return "/", dbus.NewError(fprintName+".Error.NoSuchDevice", []any{"No devices available"})
	}
	return fakeReader, nil
}

type fakeFprintDevice struct {
	*fakeSys
	enrolled *[]string
}

func (f fakeFprintDevice) Claim(string) *dbus.Error { return f.record("Claim") }
func (f fakeFprintDevice) Release() *dbus.Error     { return f.record("Release") }
func (f fakeFprintDevice) EnrollStop() *dbus.Error  { return f.record("EnrollStop") }
func (f fakeFprintDevice) EnrollStart(finger string) *dbus.Error {
	return f.record("EnrollStart", finger)
}

func (f fakeFprintDevice) DeleteEnrolledFinger(finger string) *dbus.Error {
	f.mu.Lock()
	*f.enrolled = slices.DeleteFunc(*f.enrolled, func(s string) bool { return s == finger })
	f.mu.Unlock()
	return f.record("DeleteEnrolledFinger", finger)
}

func (f fakeFprintDevice) DeleteEnrolledFingers2() *dbus.Error {
	return f.record("DeleteEnrolledFingers2")
}

func (f fakeFprintDevice) ListEnrolledFingers(string) ([]string, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(*f.enrolled) == 0 {
		return nil, dbus.NewError(errNoEnrolled, []any{"No fingers enrolled"})
	}
	return slices.Clone(*f.enrolled), nil
}

type fprintRig struct {
	bus      *dbustest.Bus
	manager  *fakeSys
	device   *fakeSys
	enrolled []string
	p        *usersPanel
	l        *loop
}

func startFprint(t *testing.T, withDevice bool) *fprintRig {
	t.Helper()
	r := &fprintRig{bus: dbustest.Start(t), enrolled: []string{"right-index-finger"}}
	r.manager, r.device = serveFprint(t, r.bus, withDevice, &r.enrolled)
	t.Setenv("USER", "alice")
	r.p, r.l = usersFixture(t, r.bus)
	return r
}

func serveFprint(t *testing.T, bus *dbustest.Bus, withDevice bool, enrolled *[]string) (manager, device *fakeSys) {
	t.Helper()
	conn := bus.Conn(t)
	manager = &fakeSys{props: map[string]any{}}
	device = &fakeSys{props: map[string]any{"num-enroll-stages": int32(5), "name": "Fake reader", "scan-type": "press"}, conn: conn, path: fakeReader, iface: fprintDevice}
	if _, err := dbusx.Export(conn, fprintPath, dbusx.Interface{Name: fprintManager, Methods: fakeFprintManager{manager, withDevice}}); err != nil {
		t.Fatal(err)
	}
	if _, err := dbusx.Export(conn, fakeReader, dbusx.Interface{Name: fprintDevice, Methods: fakeFprintDevice{device, enrolled}, Properties: device.getters()}); err != nil {
		t.Fatal(err)
	}
	if _, err := dbusx.Own(conn, fprintName); err != nil {
		t.Fatal(err)
	}
	return manager, device
}

func (r *fprintRig) emit(t *testing.T, result string, done bool) {
	t.Helper()
	if err := r.device.conn.Emit(fakeReader, fprintDevice+"."+enrollStatus, result, done); err != nil {
		t.Fatal(err)
	}
}

func (r *fprintRig) methods() []string {
	calls, _ := r.device.snapshot()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.method
	}
	return out
}

func (r *fprintRig) section(t *testing.T) *fingerprintSection {
	t.Helper()
	r.l.until("the fingerprint section", func() bool { return r.p.fingerprint != nil })
	return r.p.fingerprint
}

func fingerButton(t *testing.T, s *fingerprintSection, name string) *widget.ToggleButton {
	t.Helper()
	for _, b := range treetest.All[*widget.ToggleButton](s) {
		if b.AccessibleName() == name {
			return b
		}
	}
	t.Fatalf("no finger %q", name)
	return nil
}

func TestAFiveStageEnrolmentFillsTheRingFiveTimesAndListsTheFinger(t *testing.T) {
	r := startFprint(t, true)
	s := r.section(t)
	if !fingerButton(t, s, "Right index").Active() || fingerButton(t, s, "Left thumb").Active() {
		t.Fatal("the enrolled finger is not the one marked")
	}
	fingerButton(t, s, "Left thumb").SetActive(true)
	r.l.until("EnrollStart", func() bool { return slices.Contains(r.methods(), "EnrollStart") })
	r.enrolled = append(r.enrolled, "left-thumb")
	for range 4 {
		r.emit(t, statusStage, false)
	}
	r.emit(t, statusCompleted, true)
	r.l.until("the finger listed", func() bool { return fingerButton(t, s, "Left thumb").Active() })
	if s.ring.updates != 5 || s.ring.passed != 5 {
		t.Fatalf("the ring moved %d times to %d stages for 5 signals", s.ring.updates, s.ring.passed)
	}
	if got := r.methods(); !slices.Equal(got, []string{"Claim", "EnrollStart", "EnrollStop", "Release"}) {
		t.Fatalf("calls %v", got)
	}
}

func TestCancellingStopsThenReleases(t *testing.T) {
	r := startFprint(t, true)
	s := r.section(t)
	fingerButton(t, s, "Left index").SetActive(true)
	r.l.until("EnrollStart", func() bool { return slices.Contains(r.methods(), "EnrollStart") })
	r.emit(t, statusStage, false)
	r.l.until("one stage", func() bool { return s.ring.passed == 1 })
	treetest.Button(t, s, "Cancel").OnClick()
	r.l.until("released", func() bool { return slices.Contains(r.methods(), "Release") })
	if got := r.methods(); !slices.Equal(got, []string{"Claim", "EnrollStart", "EnrollStop", "Release"}) {
		t.Fatalf("calls %v, want EnrollStop before Release", got)
	}
	r.emit(t, statusStage, false)
	r.l.drain(100 * time.Millisecond)
	if s.ring.updates != 1 {
		t.Fatalf("the ring moved %d times after a cancel", s.ring.updates)
	}
}

func TestADuplicateOrAFullReaderSaysSoAndReleases(t *testing.T) {
	for result, want := range map[string]string{statusDuplicate: "already enrolled", statusDataFull: "full"} {
		t.Run(result, func(t *testing.T) {
			r := startFprint(t, true)
			s := r.section(t)
			fingerButton(t, s, "Left index").SetActive(true)
			r.l.until("EnrollStart", func() bool { return slices.Contains(r.methods(), "EnrollStart") })
			r.emit(t, result, true)
			r.l.until("released", func() bool { return slices.Contains(r.methods(), "Release") })
			if !strings.Contains(s.hint.Text(), want) {
				t.Fatalf("the hint reads %q, want %q in it", s.hint.Text(), want)
			}
			if got := r.methods(); !slices.Equal(got, []string{"Claim", "EnrollStart", "EnrollStop", "Release"}) {
				t.Fatalf("calls %v", got)
			}
		})
	}
}

func TestWithoutAReaderThereIsNoSectionAndNoClaim(t *testing.T) {
	r := startFprint(t, false)
	r.l.until("the reader asked for", func() bool { c, _ := r.manager.snapshot(); return len(c) == 1 })
	r.l.drain(100 * time.Millisecond)
	if r.p.fingerprint != nil || len(r.methods()) != 0 {
		t.Fatalf("section %v, device calls %v", r.p.fingerprint != nil, r.methods())
	}
}

func TestAnEnrolledFingerIsDeletedAlone(t *testing.T) {
	r := startFprint(t, true)
	s := r.section(t)
	fingerButton(t, s, "Right index").SetActive(false)
	r.l.until("the finger gone", func() bool {
		return !fingerButton(t, s, "Right index").Active() && slices.Contains(r.methods(), "Release")
	})
	if got := r.methods(); !slices.Equal(got, []string{"Claim", "DeleteEnrolledFinger", "Release"}) {
		t.Fatalf("calls %v", got)
	}
}
