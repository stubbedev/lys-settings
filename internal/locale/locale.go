// Package locale is the app's messages: lys-shell's labels for its config
// fields with this app's own on top, in the desktop's language.
package locale

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/stubbedev/lys-desktop/i18n"
)

//go:embed all:locales
var locales embed.FS

// Assets is every locale's messages.
func Assets() i18n.Assets {
	own, err := fs.Sub(locales, "locales")
	if err != nil {
		panic(err)
	}
	return i18n.ConfigAssets().With(own)
}

// T is the loader for the desktop's language. The embedded fallback is
// validated by tests, so failing here is a build defect.
var T = sync.OnceValue(func() *i18n.Loader {
	l, err := i18n.Select(Assets(), i18n.RequestedLanguages())
	if err != nil {
		panic(err)
	}
	return l
})
