package settings

import (
	"bufio"
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/lys-lib/xcursor"
	"github.com/stubbedev/lys-lib/xdg"
	"github.com/stubbedev/lys-lib/xkb"
	"github.com/stubbedev/lys-lib/xkb/registry"
)

type choice struct {
	value, label, group string
}

type lister func() []choice

type host struct {
	root string
	env  xdg.Env
}

var systemHost = host{root: "/", env: xdg.OS}

var errNoConfigHome = errors.New("neither XDG_CONFIG_HOME nor HOME is an absolute path")

func (h host) configHome() (string, error) {
	dir, ok := h.env.ConfigHome()
	if !ok {
		return "", errNoConfigHome
	}
	return dir, nil
}

func (h host) path(parts ...string) string {
	return filepath.Join(append([]string{h.root}, parts...)...)
}

func sortedChoices(out []choice) []choice {
	slices.SortFunc(out, func(a, b choice) int {
		return cmp.Or(cmp.Compare(a.group, b.group), cmp.Compare(strings.ToLower(a.label), strings.ToLower(b.label)), cmp.Compare(a.value, b.value))
	})
	return slices.CompactFunc(out, func(a, b choice) bool { return a.value == b.value && a.group == b.group })
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // a system file the listing names
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func entries(dir string) []fs.DirEntry {
	out, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return out
}

func (h host) outputs() []choice {
	var out []choice
	for _, e := range entries(h.path("sys", "class", "drm")) {
		card, connector, ok := strings.Cut(e.Name(), "-")
		if !ok || !strings.HasPrefix(card, "card") {
			continue
		}
		if readTrimmed(h.path("sys", "class", "drm", e.Name(), "status")) == "connected" {
			out = append(out, choice{value: connector, label: connector})
		}
	}
	return sortedChoices(out)
}

func (h host) drmDevices() []choice {
	var out []choice
	for _, e := range entries(h.path("dev", "dri")) {
		name := e.Name()
		if !strings.HasPrefix(name, "card") {
			continue
		}
		label := name
		if driver, err := os.Readlink(h.path("sys", "class", "drm", name, "device", "driver")); err == nil {
			label = name + " (" + filepath.Base(driver) + ")"
		}
		out = append(out, choice{value: name, label: label})
	}
	return sortedChoices(out)
}

func (h host) interfaces() []choice {
	var out []choice
	for _, e := range entries(h.path("sys", "class", "net")) {
		if e.Name() != "lo" {
			out = append(out, choice{value: e.Name(), label: e.Name()})
		}
	}
	return sortedChoices(out)
}

func (h host) sensors() []choice {
	var out []choice
	base := h.path("sys", "class", "hwmon")
	for _, chip := range entries(base) {
		dir := filepath.Join(base, chip.Name())
		name := readTrimmed(filepath.Join(dir, "name"))
		for _, e := range entries(dir) {
			sensor, ok := strings.CutSuffix(e.Name(), "_input")
			if !ok || !strings.HasPrefix(sensor, "temp") {
				continue
			}
			label := readTrimmed(filepath.Join(dir, sensor+"_label"))
			if label == "" {
				label = sensor
			}
			out = append(out, choice{value: label, label: label, group: name})
		}
	}
	return sortedChoices(out)
}

func (h host) pamServices() []choice {
	var out []choice
	for _, e := range entries(h.path("etc", "pam.d")) {
		if !e.IsDir() {
			out = append(out, choice{value: e.Name(), label: e.Name()})
		}
	}
	return sortedChoices(out)
}

func iniValue(path, section, key string) string {
	f, err := os.Open(path) //nolint:gosec // an index.theme or desktop entry the listing found
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "["+section+"]"
			continue
		}
		if k, v, ok := strings.Cut(line, "="); in && ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (h host) themes(cursors bool) []choice {
	var out []choice
	for _, dir := range xcursor.SearchPath(h.env) {
		for _, e := range entries(dir) {
			theme := filepath.Join(dir, e.Name())
			index := filepath.Join(theme, "index.theme")
			if cursors {
				if info, err := os.Stat(filepath.Join(theme, "cursors")); err != nil || !info.IsDir() {
					continue
				}
			} else if iniValue(index, "Icon Theme", "Directories") == "" {
				continue
			}
			label := cmp.Or(iniValue(index, "Icon Theme", "Name"), e.Name())
			out = append(out, choice{value: e.Name(), label: label})
		}
	}
	return sortedChoices(out)
}

func (h host) iconThemes() []choice { return h.themes(false) }

func (h host) cursorThemes() []choice { return h.themes(true) }

func (h host) programs() []choice {
	var out []choice
	for _, dir := range filepath.SplitList(h.env("PATH")) {
		for _, e := range entries(dir) {
			info, err := os.Stat(filepath.Join(dir, e.Name()))
			if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				out = append(out, choice{value: e.Name(), label: e.Name()})
			}
		}
	}
	return sortedChoices(out)
}

func (h host) terminals() []choice {
	var out []choice
	for _, data := range h.env.DataSearchPath() {
		dir := filepath.Join(data, "applications")
		for _, e := range entries(dir) {
			if !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			entry := filepath.Join(dir, e.Name())
			categories := strings.Split(iniValue(entry, "Desktop Entry", "Categories"), ";")
			if !slices.Contains(categories, "TerminalEmulator") {
				continue
			}
			fields := strings.Fields(iniValue(entry, "Desktop Entry", "Exec"))
			if len(fields) == 0 {
				continue
			}
			program := filepath.Base(fields[0])
			out = append(out, choice{value: program, label: cmp.Or(iniValue(entry, "Desktop Entry", "Name"), program)})
		}
	}
	return sortedChoices(out)
}

var fontExtensions = []string{".ttf", ".otf", ".ttc"}

func (h host) fontFiles() []choice {
	data := h.env.DataSearchPath()
	dirs := make([]string, 0, 1+len(data))
	if home, ok := h.env.Home(); ok {
		dirs = append(dirs, filepath.Join(home, ".fonts"))
	}
	for _, dir := range data {
		dirs = append(dirs, filepath.Join(dir, "fonts"))
	}
	var out []choice
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if slices.Contains(fontExtensions, strings.ToLower(filepath.Ext(path))) {
				out = append(out, choice{value: path, label: strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))})
			}
			return nil
		})
	}
	return sortedChoices(out)
}

type xkbLists struct {
	layouts, variants, options, models, rules []choice
}

func (h host) xkb() xkbLists {
	var out xkbLists
	seen := map[string]bool{}
	for _, dir := range xkb.NewContext(h.env).IncludePaths() {
		for _, e := range entries(filepath.Join(dir, "rules")) {
			name, ok := strings.CutSuffix(e.Name(), ".xml")
			if ok && !strings.HasSuffix(name, ".extras") && !seen[name] {
				seen[name] = true
				out.rules = append(out.rules, choice{value: name, label: name})
			}
		}
	}
	reg, err := registry.NewContext(h.env, 0)
	if err != nil || reg.ParseDefaultRuleset() != nil {
		out.rules = sortedChoices(out.rules)
		return out
	}
	for _, m := range reg.Models() {
		out.models = append(out.models, choice{value: m.Name, label: cmp.Or(m.Description, m.Name)})
	}
	for _, l := range reg.Layouts() {
		if l.Variant == "" {
			out.layouts = append(out.layouts, choice{value: l.Name, label: cmp.Or(l.Description, l.Name)})
			continue
		}
		out.variants = append(out.variants, choice{value: l.Variant, label: cmp.Or(l.Description, l.Variant), group: l.Name})
	}
	for _, g := range reg.OptionGroups() {
		for _, o := range g.Options {
			out.options = append(out.options, choice{value: o.Name, label: cmp.Or(o.Description, o.Name), group: cmp.Or(g.Description, g.Name)})
		}
	}
	out.layouts, out.variants, out.options = sortedChoices(out.layouts), sortedChoices(out.variants), sortedChoices(out.options)
	out.models, out.rules = sortedChoices(out.models), sortedChoices(out.rules)
	return out
}

var soundExtensions = []string{".oga", ".ogg", ".wav"}

func (h host) soundNames() []choice {
	var out []choice
	for _, data := range h.env.DataSearchPath() {
		themes := filepath.Join(data, "sounds")
		for _, theme := range entries(themes) {
			for _, e := range entries(filepath.Join(themes, theme.Name(), "stereo")) {
				ext := filepath.Ext(e.Name())
				if slices.Contains(soundExtensions, ext) {
					name := strings.TrimSuffix(e.Name(), ext)
					out = append(out, choice{value: name, label: name})
				}
			}
		}
	}
	return sortedChoices(out)
}
