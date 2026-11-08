package locale

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/lys-desktop/i18n"
)

var messageLine = regexp.MustCompile(`(?m)^(?:([a-z][a-zA-Z0-9-]*) *=|\s+\.([a-z][a-zA-Z0-9-]*) *=)`)

func ownIDs(t *testing.T, lang string) []string {
	t.Helper()
	var ids []string
	err := fs.WalkDir(locales, "locales/"+lang, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(locales, path)
		if err != nil {
			return err
		}
		message := ""
		for _, m := range messageLine.FindAllSubmatch(data, -1) {
			if len(m[1]) > 0 {
				message = string(m[1])
				ids = append(ids, message)
				continue
			}
			ids = append(ids, message+"."+string(m[2]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	return ids
}

func TestFrenchHasEveryMessageEnglishHas(t *testing.T) {
	en, fr := ownIDs(t, "en-US"), ownIDs(t, "fr")
	if len(en) == 0 {
		t.Fatal("no messages found, so the pattern is wrong")
	}
	for _, id := range en {
		if !slices.Contains(fr, id) {
			t.Errorf("fr has no %s", id)
		}
	}
	for _, id := range fr {
		if !slices.Contains(en, id) {
			t.Errorf("en-US has no %s", id)
		}
	}
}

func TestOwnMessagesAndTheConfigsLabelsResolveTogether(t *testing.T) {
	for _, lang := range []string{"en-US", "fr"} {
		l, err := i18n.Select(Assets(), []i18n.LangID{i18n.MustLangID(lang)})
		if err != nil {
			t.Fatal(err)
		}
		if !l.Has("settings-title") {
			t.Errorf("%s: no message of this app", lang)
		}
		if !l.Has("wm-gaps-inner") {
			t.Errorf("%s: no lys-wm label", lang)
		}
		if !l.Has("app-name") {
			t.Errorf("%s: no message of lys-shell's config domain", lang)
		}
		if got := l.Get("settings-title"); strings.Contains(got, "No localization") {
			t.Errorf("%s: %q", lang, got)
		}
	}
}

func TestAMessageNoneHasIsNotFound(t *testing.T) {
	if T().Has("no-such-message") {
		t.Fatal("found")
	}
}
