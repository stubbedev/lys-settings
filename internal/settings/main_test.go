package settings

import (
	"os"
	"testing"

	"github.com/stubbedev/lys-lib/testhome"
)

const noHostDir = "/nonexistent/lys-settings-test"

// TestMain runs the package against a scratch home and no session:
// building a theme or a config reads, and the shell scaffolds, the
// user's config dir, and a test that wants a compositor starts its own.
func TestMain(m *testing.M) {
	for _, key := range []string{"XDG_CONFIG_DIRS", "XDG_DATA_DIRS"} {
		if err := os.Setenv(key, noHostDir); err != nil {
			panic(err)
		}
	}
	testhome.Run(m)
}

func TestTheTestsReachNoHostSession(t *testing.T) { testhome.RequireNoHostSession(t) }
