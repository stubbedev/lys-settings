package settings

import (
	"context"
	"fmt"
	"time"

	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
)

const (
	propertiesIface   = "org.freedesktop.DBus.Properties"
	propertiesChanged = "PropertiesChanged"
	sysCallTimeout    = 25 * time.Second
)

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
	if err := s.obj().CallWithContext(ctx, propertiesIface+".GetAll", 0, s.iface).Store(&out); err != nil {
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

func (s sysService) watch(changed func(sysProps)) (stop func(), err error) {
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(s.path), dbus.WithMatchInterface(propertiesIface), dbus.WithMatchMember(propertiesChanged)}
	if err := s.conn.AddMatchSignal(match...); err != nil {
		return nil, fmt.Errorf("follow %s's properties: %w", s.name, err)
	}
	ch := make(chan *dbus.Signal, 16)
	s.conn.Signal(ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range ch {
			if sig.Path != s.path || sig.Name != propertiesIface+"."+propertiesChanged || len(sig.Body) < 2 {
				continue
			}
			if iface, _ := sig.Body[0].(string); iface != s.iface {
				continue
			}
			if props, ok := sig.Body[1].(map[string]dbus.Variant); ok {
				changed(props)
			}
		}
	}()
	return func() {
		s.conn.RemoveSignal(ch)
		_ = s.conn.RemoveMatchSignal(match...)
		close(ch)
		<-done
	}, nil
}

func withTimeout[T any](fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sysCallTimeout)
	defer cancel()
	return fn(ctx)
}

func dialSystemBus() (*dbus.Conn, error) {
	return dbusx.DialSystem(context.Background(), sysCallTimeout)
}
