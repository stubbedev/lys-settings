package settings

import (
	"testing"
	"time"

	"github.com/stubbedev/lys-lib/dbustest"
	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
)

const hangGuard = 2 * time.Minute

func flushed(t *testing.T, conn *dbus.Conn) {
	t.Helper()
	if err := conn.BusObject().CallWithContext(t.Context(), "org.freedesktop.DBus.GetId", 0).Err; err != nil {
		t.Fatalf("round trip to the bus: %v", err)
	}
}

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(hangGuard):
		t.Fatalf("no %s in %v", what, hangGuard)
		var zero T
		return zero
	}
}

func TestAPropertiesWatchClosesTheDescriptorsOfEverySignalItGets(t *testing.T) {
	bus := dbustest.Start(t)
	owner := bus.Conn(t)
	if _, err := dbusx.Own(owner, timedateName); err != nil {
		t.Fatal(err)
	}
	changes := make(chan sysProps, 1)
	w, err := sysService{conn: bus.Conn(t), name: timedateName, iface: timedateName, path: timedatePath}.watch(
		func(p sysProps) { changes <- p },
		func(err error) { t.Errorf("the watch failed: %v", err) })
	if err != nil {
		t.Fatal(err)
	}
	pipe := dbustest.NewPipe(t)
	carrying := func(iface string) dbusx.ChangedProperties {
		return dbusx.ChangedProperties{Interface: iface, Changed: map[string]dbus.Variant{"Handle": dbus.MakeVariant(pipe.Write)}, Invalidated: []string{}}
	}
	impostor := bus.Conn(t)
	if err := dbusx.PropertiesChanged.Emit(impostor, timedatePath, carrying(timedateName)); err != nil {
		t.Fatal(err)
	}
	flushed(t, impostor)
	if err := dbusx.PropertiesChanged.Emit(owner, timedatePath, carrying("org.example.Other")); err != nil {
		t.Fatal(err)
	}
	if err := dbusx.PropertiesChanged.Emit(owner, timedatePath, carrying(timedateName)); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, changes, "routed change"); len(got) != 1 {
		t.Fatalf("the change carried %v, want the one property", got)
	}
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-changes:
		t.Fatalf("an unrouted signal was delivered: %v", p)
	default:
	}
	pipe.RequireEveryCopyClosed(t)
}

func TestAnEnrolmentWatchClosesTheDescriptorsOfEverySignalItGets(t *testing.T) {
	bus := dbustest.Start(t)
	owner := bus.Conn(t)
	if _, err := dbusx.Own(owner, fprintName); err != nil {
		t.Fatal(err)
	}
	statuses := make(chan string, 1)
	f := &fprint{conn: bus.Conn(t), device: sysService{name: fprintName, iface: fprintDevice, path: fakeReader}}
	w, err := f.watch(func(result string, _ bool) { statuses <- result }, func(err error) { t.Errorf("the watch failed: %v", err) })
	if err != nil {
		t.Fatal(err)
	}
	pipe := dbustest.NewPipe(t)
	impostor := bus.Conn(t)
	if err := impostor.Emit(fakeReader, enrollStatus.Name(), "enroll-completed", true, pipe.Write); err != nil {
		t.Fatal(err)
	}
	flushed(t, impostor)
	if err := owner.Emit("/net/reactivated/Fprint/Device/1", enrollStatus.Name(), "enroll-completed", true, pipe.Write); err != nil {
		t.Fatal(err)
	}
	if err := owner.Emit(fakeReader, enrollStatus.Name(), statusStage, false, pipe.Write); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, statuses, "routed status"); got != statusStage {
		t.Fatalf("the watch reported %q, want the owner's %q", got, statusStage)
	}
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-statuses:
		t.Fatalf("an unrouted signal was delivered: %q", s)
	default:
	}
	pipe.RequireEveryCopyClosed(t)
}
