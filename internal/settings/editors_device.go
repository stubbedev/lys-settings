package settings

import (
	"context"
	"log"
	"strings"
	"time"

	dbus "github.com/stubbedev/lys-lib/third_party/godbus"
	"github.com/stubbedev/lys-shell/config"
	"github.com/stubbedev/lys-shell/service/pulse"
	"github.com/stubbedev/lys-shell/service/recorder/devicechoice"
	"github.com/stubbedev/lys-ui/widget"
)

// deviceSelect is DeviceSelectControl: a dropdown of devices writing
// the picked id, a saved device the list lacks kept selectable.
type deviceSelect struct {
	*widget.Dropdown
	slot    slot
	choices []devicechoice.DeviceChoice
	syncing bool
}

func newDeviceSelect(k *kit, s slot, choices []devicechoice.DeviceChoice) *deviceSelect {
	c := &deviceSelect{Dropdown: widget.NewDropdown(k.face, k.textPx(), nil, 0), slot: s}
	c.OnSelect = func(i int) {
		if !c.syncing && i >= 0 && i < len(c.choices) {
			_ = c.slot.set(c.choices[i].ID)
		}
	}
	c.setChoices(choices)
	return c
}

// setChoices replaces the list (SetChoices), selecting the saved id.
func (c *deviceSelect) setChoices(choices []devicechoice.DeviceChoice) {
	saved := c.slot.text()
	c.choices = devicechoice.WithSaved(choices, saved)
	c.SetItems(devicechoice.ChoiceLabels(c.choices), devicechoice.ChoiceIndex(c.choices, saved))
}

// refresh selects the saved id without writing.
func (c *deviceSelect) refresh() {
	c.syncing = true
	c.SetSelected(devicechoice.ChoiceIndex(c.choices, c.slot.text()))
	c.syncing = false
}

// cameraChoices lists the cameras (a seam for tests).
var cameraChoices = devicechoice.Cameras

// microphoneQueryTimeout bounds the audio daemon's source listing.
const microphoneQueryTimeout = 5 * time.Second

// microphoneChoices asks the audio daemon for its sources
// (dev.stubbe.lys.Audio1 ListSources), monitors dropped by their name; with
// no daemon the list is the default alone (a seam for tests).
var microphoneChoices = func() []devicechoice.DeviceChoice {
	sources, _ := audioDaemonSources()
	return devicechoice.MicrophoneChoices(sources)
}

func audioDaemonSources() ([]devicechoice.Source, bool) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, false
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), microphoneQueryTimeout)
	defer cancel()
	entries, err := pulse.NewClient(conn).ListSources(ctx)
	if err != nil {
		log.Printf("settings: audio sources: %v", err)
		return nil, false
	}
	sources := make([]devicechoice.Source, len(entries))
	for i, e := range entries {
		sources[i] = devicechoice.Source{Name: e.Name, Description: e.Description, Monitor: strings.HasSuffix(e.Name, ".monitor")}
	}
	return sources, true
}

// webcamDevice is webcam_device_select.
var webcamDevice = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newDeviceSelect(k, s, cameraChoices())
})

// microphoneDevice is microphone_device_select: the default alone at
// first, the daemon's sources once they arrive.
var microphoneDevice = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	c := newDeviceSelect(k, s, devicechoice.MicrophoneChoices(nil))
	async(k, microphoneChoices, c.setChoices)
	return c
})
