package displays

import (
	"reflect"
	"testing"
)

func TestRulesUpdateTheNamingRuleKeepTheRestAndAddOneForANewHead(t *testing.T) {
	heads := []Head{
		{Name: "DP-1", Description: "Dell Inc. U2720Q ABC123"},
		{Name: "HDMI-A-1", Description: "HDMI-A-1"},
	}
	existing := []any{
		map[string]any{"match": "desc:Dell Inc. U2720Q", "vrr": "on", "position": "0x0"},
		map[string]any{"match": "eDP-1", "scale": 2.0},
	}
	settings := []Setting{
		{Name: "DP-1", Enabled: true, Mode: Mode{Width: 3840, Height: 2160, RefreshMHz: 59997}, X: 1280, Scale: 1.5},
		{Name: "HDMI-A-1", Enabled: false, Mode: Mode{Width: 1280, Height: 720, RefreshMHz: 60000}, Scale: 1},
	}
	got := Rules(existing, heads, settings)
	want := []any{
		map[string]any{"match": "desc:Dell Inc. U2720Q ABC123", "vrr": "on", "position": "1280x0", "mode": "3840x2160@59.997", "scale": 1.5},
		map[string]any{"match": "eDP-1", "scale": 2.0},
		map[string]any{"match": "HDMI-A-1", "position": "0x0", "mode": "1280x720@60", "scale": 1.0, "disabled": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules\n%v\nwant\n%v", got, want)
	}
	if existing[0].(map[string]any)["position"] != "0x0" {
		t.Fatal("the existing rules were changed in place")
	}
}
