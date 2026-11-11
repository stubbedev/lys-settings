package settings

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeHost(t *testing.T, env map[string]string) host {
	t.Helper()
	return host{root: t.TempDir(), env: func(name string) string { return env[name] }}
}

func values(cs []choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.value
	}
	return out
}

func TestTheOutputsAreTheConnectedConnectors(t *testing.T) {
	h := fakeHost(t, nil)
	put(t, h.path("sys/class/drm/card0-DP-1/status"), "connected\n")
	put(t, h.path("sys/class/drm/card0-HDMI-A-1/status"), "disconnected\n")
	put(t, h.path("sys/class/drm/card1-eDP-1/status"), "connected\n")
	put(t, h.path("sys/class/drm/renderD128/status"), "connected\n")
	if got := values(h.outputs()); !slices.Equal(got, []string{"DP-1", "eDP-1"}) {
		t.Fatalf("%q", got)
	}
	if got := fakeHost(t, nil).outputs(); len(got) != 0 {
		t.Fatalf("no sysfs lists %v", got)
	}
}

func TestTheDRMDevicesAreTheCardsWithTheirDrivers(t *testing.T) {
	h := fakeHost(t, nil)
	put(t, h.path("dev/dri/card0"), "")
	put(t, h.path("dev/dri/renderD128"), "")
	put(t, h.path("sys/bus/pci/drivers/amdgpu/x"), "")
	if err := os.MkdirAll(h.path("sys/class/drm/card0/device"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.path("sys/bus/pci/drivers/amdgpu"), h.path("sys/class/drm/card0/device/driver")); err != nil {
		t.Fatal(err)
	}
	put(t, h.path("dev/dri/card1"), "")
	got := h.drmDevices()
	if !slices.Equal(values(got), []string{"card0", "card1"}) || got[0].label != "card0 (amdgpu)" || got[1].label != "card1" {
		t.Fatalf("%+v", got)
	}
}

func TestTheInterfacesSensorsAndPAMServicesAreListed(t *testing.T) {
	h := fakeHost(t, nil)
	for _, n := range []string{"lo", "wlan0", "eth0"} {
		put(t, h.path("sys/class/net", n, "operstate"), "up")
	}
	if got := values(h.interfaces()); !slices.Equal(got, []string{"eth0", "wlan0"}) {
		t.Fatalf("interfaces %q", got)
	}
	put(t, h.path("sys/class/hwmon/hwmon0/name"), "k10temp\n")
	put(t, h.path("sys/class/hwmon/hwmon0/temp1_input"), "45000")
	put(t, h.path("sys/class/hwmon/hwmon0/temp1_label"), "Tctl\n")
	put(t, h.path("sys/class/hwmon/hwmon1/name"), "nvme\n")
	put(t, h.path("sys/class/hwmon/hwmon1/temp1_input"), "40000")
	put(t, h.path("sys/class/hwmon/hwmon1/fan1_input"), "900")
	got := h.sensors()
	if !slices.Equal(values(got), []string{"Tctl", "temp1"}) || got[0].group != "k10temp" || got[1].group != "nvme" {
		t.Fatalf("sensors %+v", got)
	}
	put(t, h.path("etc/pam.d/login"), "")
	put(t, h.path("etc/pam.d/lys-lock"), "")
	if err := os.MkdirAll(h.path("etc/pam.d/sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := values(h.pamServices()); !slices.Equal(got, []string{"login", "lys-lock"}) {
		t.Fatalf("pam %q", got)
	}
}

func TestTheThemesComeFromTheIconDirectories(t *testing.T) {
	home := t.TempDir()
	system := t.TempDir()
	h := fakeHost(t, map[string]string{"HOME": home, "XDG_DATA_DIRS": system})
	put(t, filepath.Join(system, "icons/Papirus/index.theme"), "[Icon Theme]\nName=Papirus Dark\nDirectories=16x16/apps\n")
	put(t, filepath.Join(system, "icons/Bibata/index.theme"), "[Icon Theme]\nName=Bibata\n")
	put(t, filepath.Join(system, "icons/Bibata/cursors/left_ptr"), "")
	put(t, filepath.Join(home, ".icons/Mine/index.theme"), "[Icon Theme]\nDirectories=a\n")
	put(t, filepath.Join(home, ".local/share/icons/Papirus/index.theme"), "[Icon Theme]\nName=Papirus Dark\nDirectories=16x16/apps\n")
	icons := h.iconThemes()
	if !slices.Equal(values(icons), []string{"Mine", "Papirus"}) || icons[1].label != "Papirus Dark" {
		t.Fatalf("icon themes %+v", icons)
	}
	if got := values(h.cursorThemes()); !slices.Equal(got, []string{"Bibata"}) {
		t.Fatalf("cursor themes %q", got)
	}
}

func TestTheProgramsAndTerminalsAreListed(t *testing.T) {
	bin := t.TempDir()
	data := t.TempDir()
	h := fakeHost(t, map[string]string{"PATH": bin, "XDG_DATA_HOME": data, "XDG_DATA_DIRS": t.TempDir()})
	put(t, filepath.Join(bin, "foot"), "")
	put(t, filepath.Join(bin, "notes.txt"), "")
	if err := os.Chmod(filepath.Join(bin, "foot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := values(h.programs()); !slices.Equal(got, []string{"foot"}) {
		t.Fatalf("programs %q", got)
	}
	put(t, filepath.Join(data, "applications/foot.desktop"), "[Desktop Entry]\nName=Foot\nExec=/usr/bin/foot --server\nCategories=System;TerminalEmulator;\n")
	put(t, filepath.Join(data, "applications/firefox.desktop"), "[Desktop Entry]\nName=Firefox\nExec=firefox %u\nCategories=Network;\n")
	got := h.terminals()
	if len(got) != 1 || got[0].value != "foot" || got[0].label != "Foot" {
		t.Fatalf("terminals %+v", got)
	}
}

func TestTheFontFilesAreFoundUnderTheFontDirectories(t *testing.T) {
	home := t.TempDir()
	h := fakeHost(t, map[string]string{"HOME": home, "XDG_DATA_DIRS": t.TempDir()})
	put(t, filepath.Join(home, ".local/share/fonts/inter/Inter.ttf"), "")
	put(t, filepath.Join(home, ".fonts/Mono.OTF"), "")
	put(t, filepath.Join(home, ".fonts/readme.txt"), "")
	got := h.fontFiles()
	if len(got) != 2 || got[0].label != "Inter" || got[1].value != filepath.Join(home, ".fonts/Mono.OTF") {
		t.Fatalf("%+v", got)
	}
}

func TestTheKeyboardListsComeFromTheXKBRegistry(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	h := fakeHost(t, map[string]string{"HOME": home, "XKB_CONFIG_ROOT": root, "XKB_CONFIG_EXTRA_PATH": filepath.Join(home, "none")})
	put(t, filepath.Join(root, "rules/evdev.xml"), `<?xml version="1.0"?>
<xkbConfigRegistry version="1.1">
<modelList><model><configItem><name>pc105</name><description>Generic 105-key PC</description></configItem></model></modelList>
<layoutList><layout><configItem><name>dk</name><description>Danish</description></configItem>
<variantList><variant><configItem><name>nodeadkeys</name><description>Danish (no dead keys)</description></configItem></variant></variantList></layout></layoutList>
<optionList><group allowMultipleSelection="true"><configItem><name>caps</name><description>Caps Lock behavior</description></configItem><option><configItem><name>caps:escape</name><description>Caps Lock is Escape</description></configItem></option></group></optionList>
</xkbConfigRegistry>`)
	put(t, filepath.Join(root, "rules/evdev.extras.xml"), `<?xml version="1.0"?><xkbConfigRegistry version="1.1"/>`)
	put(t, filepath.Join(root, "rules/evdev"), "")
	x := h.xkb()
	if !slices.Contains(values(x.layouts), "dk") || !slices.Contains(values(x.models), "pc105") {
		t.Fatalf("%+v", x)
	}
	if i := slices.IndexFunc(x.variants, func(c choice) bool { return c.value == "nodeadkeys" }); i < 0 || x.variants[i].group != "dk" {
		t.Fatalf("variants %+v", x.variants)
	}
	if i := slices.IndexFunc(x.options, func(c choice) bool { return c.value == "caps:escape" }); i < 0 || x.options[i].group != "Caps Lock behavior" {
		t.Fatalf("options %+v", x.options)
	}
	if !slices.Contains(values(x.rules), "evdev") || slices.Contains(values(x.rules), "evdev.extras") {
		t.Fatalf("rules %+v", x.rules)
	}
	empty := fakeHost(t, map[string]string{"HOME": t.TempDir(), "XKB_CONFIG_ROOT": t.TempDir(), "XKB_CONFIG_EXTRA_PATH": t.TempDir()})
	if x := empty.xkb(); len(x.layouts) != 0 {
		t.Fatalf("no registry lists %+v", x.layouts)
	}
}

func TestTheSoundNamesComeFromEverySoundThemesStereoDirectory(t *testing.T) {
	home, system := t.TempDir(), t.TempDir()
	h := fakeHost(t, map[string]string{"HOME": home, "XDG_DATA_DIRS": system})
	put(t, filepath.Join(system, "sounds/freedesktop/stereo/message-new-instant.oga"), "")
	put(t, filepath.Join(home, ".local/share/sounds/mine/stereo/bell.wav"), "")
	put(t, filepath.Join(system, "sounds/freedesktop/stereo/index.theme"), "")
	got := h.soundNames()
	if len(got) != 2 || got[0].value != "bell" || got[1].value != "message-new-instant" {
		t.Fatalf("sound names %+v", got)
	}
}

func TestTheListingsReadNoDirectoryRelativeToTheWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	system := t.TempDir()
	h := fakeHost(t, map[string]string{"HOME": "home", "XDG_DATA_HOME": "data", "XDG_DATA_DIRS": "share::" + system})
	terminal := func(name string) string {
		return "[Desktop Entry]\nName=" + name + "\nExec=" + name + "\nCategories=TerminalEmulator;\n"
	}
	for _, data := range []string{"", "data", "share", "home/.local/share"} {
		put(t, filepath.Join(cwd, data, "applications/stray.desktop"), terminal("stray"))
		put(t, filepath.Join(cwd, data, "fonts/Stray.ttf"), "")
		put(t, filepath.Join(cwd, data, "sounds/stray/stereo/stray.oga"), "")
	}
	put(t, filepath.Join(cwd, "home/.fonts/Stray.ttf"), "")
	put(t, filepath.Join(system, "applications/foot.desktop"), terminal("foot"))
	put(t, filepath.Join(system, "fonts/Inter.ttf"), "")
	put(t, filepath.Join(system, "sounds/freedesktop/stereo/bell.oga"), "")
	if got := values(h.terminals()); !slices.Equal(got, []string{"foot"}) {
		t.Errorf("terminals %q, want the system data directory's alone", got)
	}
	if got := values(h.fontFiles()); !slices.Equal(got, []string{filepath.Join(system, "fonts/Inter.ttf")}) {
		t.Errorf("fonts %q, want the system data directory's alone", got)
	}
	if got := values(h.soundNames()); !slices.Equal(got, []string{"bell"}) {
		t.Errorf("sounds %q, want the system data directory's alone", got)
	}
}
