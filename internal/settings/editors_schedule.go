package settings

import (
	"slices"

	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-ui/widget"
	"github.com/stubbedev/lys-ui/widget/css"

	"github.com/stubbedev/lys-settings/internal/locale"
)

var weekdays = []config.Weekday{
	config.WeekdayMon, config.WeekdayTue, config.WeekdayWed, config.WeekdayThu,
	config.WeekdayFri, config.WeekdaySat, config.WeekdaySun,
}

const (
	scheduleFrom = "22:00"
	scheduleTo   = "07:00"
)

var dndScheduleList = cardRow(cardSpec{
	listCls: "card-form-list", rowClass: "card-form-row", labelCls: "card-form-label",
	title: scheduleTitle,
	fields: []cardField{
		{key: "days", label: "settings-schedule-days", editor: func(k *kit, s slot) control { return newWeekdaysEditor(k, s) }},
		{key: "from", label: "settings-schedule-from", editor: func(k *kit, s slot) control { return newLiveText(k, s, scheduleFrom) }},
		{key: "to", label: "settings-schedule-to", editor: func(k *kit, s slot) control { return newLiveText(k, s, scheduleTo) }},
	},
	blank: func() map[string]any { return map[string]any{"from": scheduleFrom, "to": scheduleTo} },
	keep: func(m map[string]any) bool {
		from, _ := m["from"].(string)
		to, _ := m["to"].(string)
		return from != "" && to != ""
	},
	clean: func(m map[string]any) map[string]any {
		if days, _ := m["days"].([]any); len(days) == 0 {
			delete(m, "days")
		}
		return m
	},
})

func scheduleTitle(item map[string]any, _ int) string {
	from, _ := item["from"].(string)
	to, _ := item["to"].(string)
	return from + " – " + to
}

type weekdaysEditor struct {
	*widget.Box
	slot    slot
	days    []*widget.ToggleButton
	syncing bool
}

func newWeekdaysEditor(k *kit, s slot) *weekdaysEditor {
	t := locale.T()
	e := &weekdaysEditor{Box: widget.NewBox(widget.Row, widget.Current().Spacing/2, 0), slot: s}
	e.AddClass("weekdays-editor")
	for _, d := range weekdays {
		b := widget.NewToggleButton(k.label(t.Get("settings-weekday-"+string(d))), widget.Current().Padding, 0)
		b.AddClass(css.Small)
		b.OnToggled = func(bool) {
			if !e.syncing {
				_ = e.slot.set(e.selected())
			}
		}
		e.days = append(e.days, b)
		e.Append(b, false)
	}
	e.refresh()
	return e
}

func (e *weekdaysEditor) selected() []any {
	out := []any{}
	for i, b := range e.days {
		if b.Active() {
			out = append(out, string(weekdays[i]))
		}
	}
	return out
}

func (e *weekdaysEditor) refresh() {
	stored, _ := e.slot.get().([]any)
	e.syncing = true
	for i, b := range e.days {
		b.SetActive(slices.Contains(stored, any(string(weekdays[i]))))
	}
	e.syncing = false
}
