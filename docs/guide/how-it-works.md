# How it works

lys-settings links neither project's internals. Each project's parser stays the one
source of truth for its config.

Both backends work the same way: a change is a runtime override, written to the
project's `runtime.yaml` above the declared `config.yaml`, which the app only
reads. A `config.yaml` that Home Manager links into the Nix store therefore stays
usable. Each row shows where its value comes from (an override, `config.yaml`, or the
default), **Reset** drops that row's override, and **Reset all** drops every override
of both.

## lys-shell

Through its public, versioned Go packages: `config` for the fields, the layers and
runtime overrides; `styling` and `shell/apptheme` for the theme, so the window looks
like the shell; `i18n` for the field labels. The app's own messages sit on top of
those labels.

## lys-wm

lys-wm's config package imports the whole compositor, so the contract is the flags
of the `lys-wm` on the path:

| Flag | Gives |
| --- | --- |
| `--config-schema` | Every key, with the choices, bounds, formats and defaults its tests hold to the parser |
| `--default-config` | The built-in config, which applies under the overrides when there is no `config.yaml` |
| `--check-config --runtime <file>` | Whether a reload would accept that override file over the real config, and why not |
| `--effective-config` | Which layer each key comes from; the end-to-end test holds the app's view to it |

The overrides live in `$XDG_CONFIG_HOME/lys-wm/runtime.yaml`. lys-wm merges them
over `config.yaml`, or over its built-in config when there is none: mappings key by
key, a scalar or a list whole. The file holds only the keys changed here.

Asking the installed binary means a key an older lys-wm lacks is refused by that
lys-wm, rather than being written for a reload to reject. `lys-wmctl reload` exits 3
when no lys-wm answers, which the app reports as "saved, for the next one".

## The launcher

lys-shell's launcher runs `lys-settings --list-pages`, which prints the pages as JSON
in the desktop's language without a session or reading any config, and opens one with
`lys-settings --page <id>`. lys-shell's `settingspage` package holds that contract
and both ends use it. Without lys-settings installed the launcher logs why and offers
no rows.

## Editors

Each row's editor comes from its field's type: lys-shell's `FieldMeta.Type` (`Monitor`,
`IconTheme`, `Directory`, `Command`, `Secret`, `StrftimeFormat`, ...) and placeholders,
or the `format` lys-wm's schema gives a key (`lys-wm-color`, `lys-wm-font-file`,
`lys-wm-xkb-layout`, ...). A value the system can list offers the list, with the
type's special values (`auto`, `primary`, the default) first: connected outputs from
`/sys/class/drm`, icon and cursor themes from the icon directories, xkeyboard-config's
registry, DRM cards under `/dev/dri`, network interfaces, hwmon sensors, the audio
daemon's sources, `/etc/pam.d`, font files, programs on `PATH` and terminal
emulators' desktop entries. The entry still takes any value, and a value the list
lacks is kept and marked as not found. A format field offers its placeholders and
previews the result with each one's sample.

A text row whose value is neither listable nor structured is a deliberate choice: a
test walks every page and fails on a plain text row that is not on its free-form
list.

## Icons

The icons the pages name are installed under `share/lys-settings/icons`, which the
app adds to the search path, so no file belongs to both this package and lys-shell's.
