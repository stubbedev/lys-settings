package settings

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/stubbedev/lys-lib/dbusx"
	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

const (
	fprintName      = "net.reactivated.Fprint"
	fprintManager   = fprintName + ".Manager"
	fprintDevice    = fprintName + ".Device"
	fprintPath      = dbus.ObjectPath("/net/reactivated/Fprint/Manager")
	errNoEnrolled   = fprintName + ".Error.NoEnrolledPrints"
	errPermission   = fprintName + ".Error.PermissionDenied"
	statusStage     = "enroll-stage-passed"
	statusCompleted = "enroll-completed"
	statusDuplicate = "enroll-duplicate"
	statusDataFull  = "enroll-data-full"
	statusFailed    = "enroll-failed"
	statusDisconn   = "enroll-disconnected"
	statusUnknown   = "enroll-unknown-error"
)

var fingers = []string{
	"left-little-finger", "left-ring-finger", "left-middle-finger", "left-index-finger", "left-thumb",
	"right-thumb", "right-index-finger", "right-middle-finger", "right-ring-finger", "right-little-finger",
}

func endsEnrolment(status string) bool {
	return slices.Contains([]string{statusCompleted, statusDuplicate, statusDataFull, statusFailed, statusDisconn, statusUnknown}, status)
}

type fprint struct {
	conn   *dbus.Conn
	device sysService
	user   string
}

func openFprint(ctx context.Context, conn *dbus.Conn, user string) (*fprint, error) {
	var path dbus.ObjectPath
	if err := conn.Object(fprintName, fprintPath).CallWithContext(ctx, fprintManager+".GetDefaultDevice", 0).Store(&path); err != nil {
		return nil, fmt.Errorf("find a fingerprint reader: %w", err)
	}
	return &fprint{conn: conn, device: sysService{conn: conn, name: fprintName, iface: fprintDevice, path: path}, user: user}, nil
}

func (f *fprint) enrolled(ctx context.Context) ([]string, error) {
	var out []string
	err := f.device.store(ctx, "ListEnrolledFingers", &out, f.user)
	var derr dbus.Error
	if errors.As(err, &derr) && derr.Name == errNoEnrolled {
		return nil, nil
	}
	return out, err
}

func (f *fprint) stages(ctx context.Context) (int, error) {
	props, err := f.device.props(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := props["num-enroll-stages"].Value().(int32)
	if n <= 0 {
		return 0, fmt.Errorf("the reader reports %d enrolment stages", n)
	}
	return int(n), nil
}

type enrolment struct {
	Result string `dbus:"result"`
	Done   bool   `dbus:"done"`
}

var enrollStatus = dbusx.NewSignal[enrolment](fprintDevice, "EnrollStatus")

func (f *fprint) watch(status func(result string, done bool), failed func(error)) (*dbusx.Watch, error) {
	w, err := dbusx.WatchSignals(context.Background(), f.conn, dbusx.FromOwner(fprintName), failed,
		dbusx.On(enrollStatus, f.device.path, func(e enrolment, _ dbus.Sequence) error {
			status(e.Result, e.Done)
			return nil
		}))
	if err != nil {
		return nil, fmt.Errorf("follow the reader's enrolment: %w", err)
	}
	return w, nil
}

type enrollRing struct {
	*widget.ProgressRing
	stages, passed, updates int
}

func (r *enrollRing) set(passed int) {
	r.passed = passed
	r.updates++
	r.SetValue(float64(passed) / float64(r.stages))
}

type fingerprintSection struct {
	*widget.Box
	k        *kit
	fp       *fprint
	stages   int
	enrolled []string
	fingers  *widget.FlowBox
	ring     *enrollRing
	hint     *widget.Label
	cancel   *widget.Button
	watch    *dbusx.Watch
	busy     bool
}

func (p *usersPanel) loadFingerprint() {
	if p.conn == nil || p.user == "" {
		return
	}
	conn, user := p.conn, p.user
	type opened struct {
		fp       *fprint
		stages   int
		enrolled []string
		err      error
	}
	async(p.k, func() opened {
		return withTimeoutValue(func(ctx context.Context) opened {
			fp, err := openFprint(ctx, conn, user)
			if err != nil {
				return opened{err: err}
			}
			stages, err := fp.stages(ctx)
			if err != nil {
				return opened{err: err}
			}
			enrolled, err := fp.enrolled(ctx)
			return opened{fp, stages, enrolled, err}
		})
	}, func(o opened) {
		if o.err != nil {
			return
		}
		s := newFingerprintSection(p.k, o.fp, o.stages, o.enrolled, p.fail)
		p.fingerprint = s
		p.Append(newSection(p.k, "settings-section-users-fingerprint", s), false)
	})
}

func withTimeoutValue[T any](fn func(context.Context) T) T {
	v, _ := withTimeout(func(ctx context.Context) (T, error) { return fn(ctx), nil })
	return v
}

func newFingerprintSection(k *kit, fp *fprint, stages int, enrolled []string, fail func(error)) *fingerprintSection {
	t := locale.T()
	s := &fingerprintSection{Box: gapBox(widget.Column), k: k, fp: fp, stages: stages, enrolled: enrolled}
	s.fingers = widget.NewWrapBox(widget.Current().Spacing, widget.Current().Spacing, widget.JustifyStart)
	s.fingers.AddClass("fingerprint-fingers")
	s.Append(s.fingers, false)
	s.ring = &enrollRing{ProgressRing: widget.NewProgressRing(0), stages: stages}
	s.ring.SetAccessibleName(t.Get("settings-fingerprint-progress"))
	s.ring.SetVisible(false)
	s.hint = wrapped(k.label("", "setting-description", css.Caption, css.DimLabel))
	s.cancel = k.button(k.label(t.Get("settings-fingerprint-cancel")), func() { s.finish(fail) }, css.Secondary)
	s.cancel.SetVisible(false)
	s.Append(s.ring, false)
	s.Append(s.hint, false)
	s.Append(s.cancel, false)
	deleteAll := k.button(k.label(t.Get("settings-fingerprint-delete-all")), func() {
		s.call(fail, func(ctx context.Context) error {
			if err := fp.device.call(ctx, "Claim", fp.user); err != nil {
				return err
			}
			err := fp.device.call(ctx, "DeleteEnrolledFingers2")
			return errors.Join(err, fp.device.call(ctx, "Release"))
		})
	}, css.Small, css.Destructive)
	s.Append(deleteAll, false)
	s.Append(wrapped(k.label(t.Get("settings-fingerprint-pam"), "setting-description", css.Caption, css.DimLabel)), false)
	s.showFingers(fail)
	return s
}

func (s *fingerprintSection) showFingers(fail func(error)) {
	t := locale.T()
	s.fingers.Clear()
	for _, f := range fingers {
		b := widget.NewToggleButton(s.k.label(t.Get("settings-fingerprint-"+f)), widget.Current().Padding/2, 0)
		b.SetAccessibleName(t.Get("settings-fingerprint-" + f))
		b.SetActive(slices.Contains(s.enrolled, f))
		b.AddClass("fingerprint-finger")
		syncing := false
		b.OnToggled = func(bool) {
			if syncing {
				return
			}
			syncing = true
			b.SetActive(slices.Contains(s.enrolled, f))
			syncing = false
			switch {
			case s.busy:
			case slices.Contains(s.enrolled, f):
				s.deleteFinger(f, fail)
			default:
				s.enroll(f, fail)
			}
		}
		s.fingers.Append(b)
	}
}

func (s *fingerprintSection) call(fail func(error), fn func(context.Context) error) {
	async(s.k, func() error {
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) })
		return err
	}, func(err error) {
		fail(permissionMessage(err))
		s.reload(fail)
	})
}

func permissionMessage(err error) error {
	var derr dbus.Error
	if errors.As(err, &derr) && derr.Name == errPermission {
		return errors.New(locale.T().Get("settings-fingerprint-denied"))
	}
	return err
}

func (s *fingerprintSection) reload(fail func(error)) {
	fp := s.fp
	async(s.k, func() fingersListing {
		v, err := withTimeout(fp.enrolled)
		return fingersListing{v, err}
	}, func(l fingersListing) {
		if l.err != nil {
			fail(l.err)
			return
		}
		s.enrolled = l.names
		s.showFingers(fail)
	})
}

type fingersListing struct {
	names []string
	err   error
}

func (s *fingerprintSection) enroll(finger string, fail func(error)) {
	t := locale.T()
	watch, err := s.fp.watch(func(result string, done bool) {
		s.k.invoke(func() { s.status(result, done, fail) })
	}, func(err error) {
		s.k.invoke(func() { fail(err) })
	})
	if err != nil {
		fail(err)
		return
	}
	s.watch, s.busy = watch, true
	s.ring.passed, s.ring.updates = 0, 0
	s.ring.SetValue(0)
	s.ring.SetVisible(true)
	s.cancel.SetVisible(true)
	s.hint.SetText(t.Get("settings-fingerprint-place"))
	fp := s.fp
	async(s.k, func() error {
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) {
			if err := fp.device.call(ctx, "Claim", fp.user); err != nil {
				return struct{}{}, err
			}
			if err := fp.device.call(ctx, "EnrollStart", finger); err != nil {
				return struct{}{}, errors.Join(err, fp.device.call(ctx, "Release"))
			}
			return struct{}{}, nil
		})
		return err
	}, func(err error) {
		if err != nil {
			fail(errors.Join(permissionMessage(err), s.unwatch()))
		}
	})
}

func (s *fingerprintSection) status(result string, done bool, fail func(error)) {
	if !s.busy {
		return
	}
	t := locale.T()
	switch result {
	case statusStage:
		s.ring.set(s.ring.passed + 1)
		s.hint.SetText(t.Get("settings-fingerprint-place"))
	case statusCompleted:
		s.ring.set(s.stages)
	}
	if key := "settings-fingerprint-" + strings.TrimPrefix(result, "enroll-"); t.Has(key) {
		s.hint.SetText(t.Get(key))
	}
	if done || endsEnrolment(result) {
		s.finish(fail)
	}
}

func (s *fingerprintSection) unwatch() error {
	var err error
	if s.watch != nil {
		err = s.watch.Stop()
		s.watch = nil
	}
	s.busy = false
	s.cancel.SetVisible(false)
	return err
}

func (s *fingerprintSection) finish(fail func(error)) {
	if !s.busy {
		return
	}
	if err := s.unwatch(); err != nil {
		fail(err)
	}
	fp := s.fp
	s.call(fail, func(ctx context.Context) error {
		return errors.Join(fp.device.call(ctx, "EnrollStop"), fp.device.call(ctx, "Release"))
	})
}

func (s *fingerprintSection) close() {
	if s.busy {
		unwatched := s.unwatch()
		fp := s.fp
		_, err := withTimeout(func(ctx context.Context) (struct{}, error) {
			return struct{}{}, errors.Join(fp.device.call(ctx, "EnrollStop"), fp.device.call(ctx, "Release"))
		})
		if err := errors.Join(unwatched, err); err != nil {
			log.Printf("settings: end the fingerprint enrolment: %v", err)
		}
	}
}

func (s *fingerprintSection) deleteFinger(finger string, fail func(error)) {
	fp := s.fp
	s.call(fail, func(ctx context.Context) error {
		if err := fp.device.call(ctx, "Claim", fp.user); err != nil {
			return err
		}
		err := fp.device.call(ctx, "DeleteEnrolledFinger", finger)
		var derr dbus.Error
		if errors.As(err, &derr) && derr.Name == "org.freedesktop.DBus.Error.UnknownMethod" {
			err = fp.device.call(ctx, "DeleteEnrolledFingers2")
		}
		return errors.Join(err, fp.device.call(ctx, "Release"))
	})
}
