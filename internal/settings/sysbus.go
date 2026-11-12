package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
)

const sysCallTimeout = 25 * time.Second

type sysProps map[string]dbus.Variant

func (p sysProps) str(key string) string {
	s, _ := p[key].Value().(string)
	return s
}

func (p sysProps) flag(key string) bool {
	b, _ := p[key].Value().(bool)
	return b
}

func (p sysProps) strs(key string) []string {
	s, _ := p[key].Value().([]string)
	return s
}

type sysService struct {
	conn        *dbus.Conn
	name, iface string
	path        dbus.ObjectPath
}

func (s sysService) obj() dbus.BusObject { return s.conn.Object(s.name, s.path) }

func (s sysService) props(ctx context.Context) (sysProps, error) {
	var out sysProps
	if err := s.obj().CallWithContext(ctx, dbusx.PropertiesInterface+".GetAll", 0, s.iface).Store(&out); err != nil {
		return nil, fmt.Errorf("read %s's properties: %w", s.name, err)
	}
	return out, nil
}

func (s sysService) call(ctx context.Context, method string, args ...any) error {
	if err := s.obj().CallWithContext(ctx, s.iface+"."+method, 0, args...).Err; err != nil {
		return fmt.Errorf("%s.%s: %w", s.iface, method, err)
	}
	return nil
}

func (s sysService) store(ctx context.Context, method string, out any, args ...any) error {
	if err := s.obj().CallWithContext(ctx, s.iface+"."+method, 0, args...).Store(out); err != nil {
		return fmt.Errorf("%s.%s: %w", s.iface, method, err)
	}
	return nil
}

func (s sysService) watch(changed func(sysProps), failed func(error)) (*dbusx.Watch, error) {
	w, err := dbusx.WatchSignals(context.Background(), s.conn, dbusx.FromOwner(s.name), failed,
		dbusx.On(dbusx.PropertiesChanged, s.path, func(c dbusx.ChangedProperties, _ dbus.Sequence) error {
			changed(c.Changed)
			return nil
		}).WithArg0(s.iface))
	if err != nil {
		return nil, fmt.Errorf("follow %s's properties: %w", s.name, err)
	}
	return w, nil
}

func withTimeout[T any](fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
	defer cancel()
	return fn(ctx)
}

func dialSystemBus() (*dbus.Conn, error) {
	return dbusx.DialSystem(context.Background(), sysCallTimeout)
}
