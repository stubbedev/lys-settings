package settings

import (
	"os"
	"testing"

	"github.com/stubbedev/lys-settings/internal/wmconfig"
)

func TestEveryWMRowIsAKeyOfTheInstalledLysWM(t *testing.T) {
	if os.Getenv("LYS_SETTINGS_HEADLESS") == "" {
		t.Skip("needs the dev shell's lys-wm; set by `just test`")
	}
	f, err := wmconfig.Open(os.Getenv, wmconfig.Exec)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := f.Schema()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range allWMRows() {
		if _, ok := schema.Field(wmPath(r.path)); !ok {
			t.Errorf("lys-wm has no key %s", r.path)
		}
	}
}
