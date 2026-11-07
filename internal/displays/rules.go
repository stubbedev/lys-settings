package displays

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

const (
	descPrefix = "desc:"
	scaleSteps = 120
)

// Match is the outputs-rule match that names h: its description when the
// compositor knows one beyond the connector, so the rule follows the
// monitor across ports, else its connector.
func Match(h Head) string {
	if h.Description != "" && h.Description != h.Name {
		return descPrefix + h.Description
	}
	return h.Name
}

func names(rule string, h Head) bool {
	if desc, ok := strings.CutPrefix(rule, descPrefix); ok {
		return desc != "" && strings.HasPrefix(h.Description, desc)
	}
	return rule == h.Name
}

// ModeText is a mode as an outputs rule writes it.
func ModeText(m Mode) string {
	return strconv.Itoa(m.Width) + "x" + strconv.Itoa(m.Height) + "@" + strconv.FormatFloat(float64(m.RefreshMHz)/1000, 'f', -1, 64)
}

// ScaleValue is a scale on lys-wm's 1/120 grid.
func ScaleValue(s float64) float64 { return math.Round(s*scaleSteps) / scaleSteps }

// Rules is existing with each head's rule carrying its setting's mode,
// position, scale and enabled state: a rule that names the head is
// updated in place and keeps its other keys, a head no rule names gets
// one, and rules for other outputs stay as they are.
func Rules(existing []any, heads []Head, settings []Setting) []any {
	out := slices.Clone(existing)
	for _, s := range settings {
		i := slices.IndexFunc(heads, func(h Head) bool { return h.Name == s.Name })
		if i < 0 {
			continue
		}
		h := heads[i]
		at := slices.IndexFunc(out, func(r any) bool {
			m, ok := r.(map[string]any)
			match, _ := m["match"].(string)
			return ok && names(match, h)
		})
		rule := map[string]any{}
		if at >= 0 {
			found, _ := out[at].(map[string]any)
			rule = maps.Clone(found)
		}
		rule["match"] = Match(h)
		rule["mode"] = ModeText(s.Mode)
		rule["position"] = strconv.Itoa(s.X) + "x" + strconv.Itoa(s.Y)
		rule["scale"] = ScaleValue(s.Scale)
		delete(rule, "disabled")
		if !s.Enabled {
			rule["disabled"] = true
		}
		if at >= 0 {
			out[at] = rule
		} else {
			out = append(out, rule)
		}
	}
	return out
}
