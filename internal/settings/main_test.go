package settings

import (
	"os"
	"testing"

	"github.com/stubbedev/lys-lib/testhome"
	"golang.org/x/sys/unix"
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

func TestTemporaryDirectoriesAreOnATmpfs(t *testing.T) {
	var fs unix.Statfs_t
	dir := t.TempDir()
	if err := unix.Statfs(dir, &fs); err != nil || fs.Type != unix.TMPFS_MAGIC {
		t.Fatalf("%s: statfs %v, type %#x, want a tmpfs", dir, err, fs.Type)
	}
}
