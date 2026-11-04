# How it works

lys-settings links neither project's internals. Each project's parser stays the one
source of truth for its config.

## lys-shell

Through its public, versioned Go packages: `config` for the fields, the layers and
runtime overrides; `styling` and `shell/apptheme` for the theme, so the window looks
like the shell; `i18n` for the field labels. The app's own messages sit on top of
those labels.

## lys-wm

lys-wm's config package imports the whole compositor, so the contract is three flags
of the `lys-wm` on the path:

| Flag | Gives |
| --- | --- |
| `--config-schema` | Every key, with the choices, bounds and defaults its tests hold to the parser |
| `--default-config` | The built-in config, which a first file starts from |
| `--check-config --config <file>` | Whether a reload would accept the file, and why not |

Asking the installed binary means a key an older lys-wm lacks is refused by that
lys-wm, rather than being written for a reload to reject. `lys-wmctl reload` exits 3
when no lys-wm answers, which the app reports as "saved, for the next one".

## The launcher

lys-shell's launcher runs `lys-settings --list-pages`, which prints the pages as JSON
in the desktop's language without a session or reading any config, and opens one with
`lys-settings --page <id>`. lys-shell's `settingspage` package holds that contract
and both ends use it. Without lys-settings installed the launcher logs why and offers
no rows.

## Icons

The icons the pages name are installed under `share/lys-settings/icons`, which the
app adds to the search path, so no file belongs to both this package and lys-shell's.
