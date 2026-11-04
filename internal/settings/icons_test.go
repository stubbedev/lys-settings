package settings

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryIconTheAppNamesIsOneItShips(t *testing.T) {
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
			icon := filepath.Join("..", "..", "resources", "icons", "hicolor", "scalable", "actions", string(m[1])+".svg")
			if _, err := os.Stat(icon); err != nil {
				t.Errorf("%s names %s, which resources/icons does not hold", f, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no icon names, so the pattern is wrong")
	}
}
