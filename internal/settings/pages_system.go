package settings

import (
	"errors"
	"fmt"
	"log"

	"github.com/stubbedev/lys-desktop/config"
	"github.com/stubbedev/lys-desktop/i18n"
	"github.com/stubbedev/lys-login/dmsocket"
	"github.com/stubbedev/lys-login/greeterconfig"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

// generalPage is pages/general.
func generalPage(*config.Config) pageSpec {
	return pageSpec{
		id: "general", navKey: "settings-nav-general", icon: "ld-settings-symbolic",
		header: "settings-page-general",
		sections: []sectionSpec{
			{title: "settings-section-fonts", rows: []rowSpec{field("general.font-sans", fontRow), field("general.font-mono", monoFontRow)}},
			{title: "settings-section-scale-rounding", rows: fields("styling.scale", "styling.rounding")},
			{title: "settings-section-display", rows: fields("general.tearing-mode")},
		},
	}
}

// lockPage is pages/lock.
func lockPage(*config.Config) pageSpec {
	return pageSpec{
		id: "lock", navKey: "settings-nav-lock-page", icon: "ld-lock-symbolic",
		header: "settings-page-lock",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("login.lock.enabled", "login.lock.lock-on-start")},
			{title: "settings-section-background", rows: []rowSpec{
				field("login.background-mode"), field("login.background-image", filePath),
				field("login.background-color"), field("login.blur"),
			}},
			{title: "settings-section-clock", rows: fields("login.show-clock", "login.clock-format", "login.date-format")},
			{title: "settings-section-security", rows: fields("login.lock.grace-period-ms", "login.max-attempts",
				"login.show-failed-attempts", "login.lock.blank-timeout-ms", "login.lock.pam-service")},
			{title: "settings-section-animation", rows: surfaceAnimationRows("animations.lock")},
		},
	}
}

// greeterPage is pages/greeter, with the apply footer.
func greeterPage(*config.Config) pageSpec {
	return pageSpec{
		id: "greeter", navKey: "settings-nav-greeter-page", icon: "ld-monitor-symbolic",
		header: "settings-page-greeter",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("login.greeter.show-user-list", "login.greeter.show-power-buttons")},
			{title: "settings-section-cursor", rows: fields("login.greeter.cursor-theme", "login.greeter.cursor-size")},
		},
		footer: greeterApplyFooter,
	}
}

const (
	greeterApplyLabel        = "settings-greeter-apply"
	greeterApplyHint         = "settings-greeter-apply-hint"
	greeterApplyDone         = "settings-greeter-apply-done"
	greeterApplyFailed       = "settings-greeter-apply-failed"
	greeterApplyNoDaemon     = "settings-greeter-apply-no-daemon"
	greeterApplyInvalid      = "settings-greeter-apply-invalid"
	greeterApplyUnauthorized = "settings-greeter-apply-unauthorized"
	greeterApplyDismissed    = "settings-greeter-apply-dismissed"
)

var loginRuntimeDir = dmsocket.DefaultRuntimeDir

func greeterApplyFooter(k *kit) widget.Widget {
	box := gapBox(widget.Column)
	box.AddClass("settings-section")
	t := locale.T()
	hint := k.label(t.Get(greeterApplyHint), "settings-section-title")
	hint.SetWrap(true)
	apply := func() {
		async(k, func() error { return applyGreeter(k.store, loginRuntimeDir) }, func(err error) {
			if err != nil {
				log.Printf("settings: greeter apply: %v", err)
			}
			hint.SetText(greeterApplyOutcome(t, err))
		})
	}
	box.AppendAligned(k.button(k.label(t.Get(greeterApplyLabel)), apply, css.SuggestedAction), false, widget.AlignStart)
	box.Append(hint, false)
	return box
}

func greeterApplyOutcome(t *i18n.Loader, err error) string {
	if err == nil {
		return t.Get(greeterApplyDone)
	}
	if errors.Is(err, dmsocket.ErrNoDaemon) {
		return t.Get(greeterApplyNoDaemon)
	}
	because := func(id, reason string) string { return t.Get(id, i18n.Str("reason", reason)) }
	if errors.Is(err, greeterconfig.ErrInvalid) {
		return because(greeterApplyInvalid, err.Error())
	}
	refused, ok := errors.AsType[*dmsocket.RefusedError](err)
	if !ok {
		return because(greeterApplyFailed, err.Error())
	}
	switch refused.Refusal {
	case dmsocket.Invalid:
		return because(greeterApplyInvalid, refused.Reason)
	case dmsocket.Unauthorized:
		return because(greeterApplyUnauthorized, refused.Reason)
	case dmsocket.Dismissed:
		return t.Get(greeterApplyDismissed)
	default:
		return because(greeterApplyFailed, refused.Reason)
	}
}

func applyGreeter(s store, runtimeDir string) error {
	login := make(map[string]any, len(config.GreeterApplyKeys))
	for _, key := range config.GreeterApplyKeys {
		if v := s.value("login." + key); v != nil {
			if err := config.InsertPath(login, key, v); err != nil {
				return fmt.Errorf("stage login.%s: %w", key, err)
			}
		}
	}
	body, err := config.YAMLDocument(map[string]any{"login": login})
	if err != nil {
		return fmt.Errorf("stage the login screen settings: %w", err)
	}
	c, err := greeterconfig.NewChange(body)
	if err != nil {
		return fmt.Errorf("stage the login screen settings: %w", err)
	}
	if _, err := dmsocket.Apply.Send(runtimeDir, c); err != nil {
		return fmt.Errorf("apply the login screen settings: %w", err)
	}
	return nil
}

func idlePage(*config.Config) pageSpec {
	return pageSpec{
		id: "idle", navKey: "settings-nav-idle", icon: "ld-moon-symbolic",
		header: "settings-page-idle",
		sections: []sectionSpec{
			{title: "settings-section-general", rows: fields("idle.enable", "idle.lock-before-sleep")},
			{title: "settings-section-idle-dim", rows: fields("idle.dim.ac", "idle.dim.battery", "idle.dim-percent")},
			{title: "settings-section-idle-lock", rows: fields("idle.lock.ac", "idle.lock.battery")},
			{title: "settings-section-idle-screen-off", rows: fields("idle.screen-off.ac", "idle.screen-off.battery")},
			{title: "settings-section-idle-suspend", rows: fields("idle.suspend.ac", "idle.suspend.battery")},
		},
	}
}

func focusPage(*config.Config) pageSpec {
	return pageSpec{
		id: "focus", navKey: "settings-nav-focus", icon: "ld-bell-off-symbolic",
		header: "settings-page-focus",
		sections: []sectionSpec{
			{title: "settings-section-focus-automatic", rows: append(fields("modules.notifications.dnd.while-sharing", "modules.notifications.dnd.while-fullscreen"), field("modules.notifications.dnd.schedule", dndScheduleList))},
			{title: "settings-section-focus-allowed", rows: []rowSpec{field("modules.notifications.dnd.allow-critical"), field("modules.notifications.dnd.allow-apps", stringList)}},
		},
	}
}

var idleTimeouts = []string{"1m", "2m", "3m", "5m", "10m", "15m", "30m", "1h", "never"}
