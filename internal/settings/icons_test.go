package settings

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-desktop/resources"
)

func TestEveryIconTheAppNamesIsOneItShips(t *testing.T) {
	list, err := os.ReadFile(filepath.Join("..", "..", "resources", "icons.txt"))
	if err != nil {
		t.Fatal(err)
	}
	listed := strings.Fields(string(list))
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	name := regexp.MustCompile(`"((?:ld|si|tb|md|cm)-[a-z0-9-]+-symbolic)"`)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range name.FindAllSubmatch(data, -1) {
			seen++
			if !slices.Contains(listed, string(m[1])) {
				t.Errorf("%s names %s, which resources/icons.txt does not list", f, m[1])
			}
			if _, err := fs.Stat(resources.Icons, path.Join(resources.IconsDir, string(m[1])+".svg")); err != nil {
				t.Errorf("%s names %s, which the bundle does not hold", f, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no icon names, so the pattern is wrong")
	}
}
