package settings

import (
	"os"
	"testing"

	"github.com/stubbedev/lys-shell/testhome"
)

const noHostDir = "/nonexistent/lys-settings-test"

// TestMain runs the package against a scratch home: building a theme
// or a config reads, and the shell scaffolds, the user's config dir. The
// Displays page never reaches the session's compositor: a test that wants
// one starts its own.
func TestMain(m *testing.M) {
	if err := os.Unsetenv("WAYLAND_DISPLAY"); err != nil {
		panic(err)
	}
	for _, key := range []string{"XDG_CONFIG_DIRS", "XDG_DATA_DIRS"} {
		if err := os.Setenv(key, noHostDir); err != nil {
			panic(err)
		}
	}
	testhome.Run(m)
}
