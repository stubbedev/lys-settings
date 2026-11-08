# How it works

lys-settings holds no copy of either project's config rules. Each project's parser
is the single source of truth.

## lys-shell

lys-settings uses lys-shell's public Go packages, pinned in `go.mod`:

| Package | Used for |
| --- | --- |
| `config` | Fields, defaults, config layers and runtime overrides |
| `styling`, `shell/apptheme` | The window's theme, matching the shell |
| `i18n` | Field labels; the app's own messages sit on top |
| `settingspage` | The launcher contract below |

## lys-wm

lys-settings does not link lys-wm. It runs the `lys-wm` on `PATH`:

| Command | Gives |
| --- | --- |
| `lys-wm --config-schema` | Every key, with its choices, bounds, format and default |
| `lys-wm --default-config` | The built-in config, which applies under the overrides when there is no `config.yaml` |
| `lys-wm --check-config --runtime <file>` | Whether a reload would accept the override file over the real config, and why not |
| `lys-wmctl reload` | Makes a running lys-wm reload; exit status 3 means none is running |

lys-wm merges overrides over `config.yaml`, or over its built-in config when there
is none: mappings key by key, scalars and lists whole.

## The launcher

lys-shell's launcher runs `lys-settings --list-pages`. It prints every page and
each setting's path, title and keywords as JSON, in the desktop's language,
without a session and without reading any config. The launcher opens a page with
`--page <id>` and a setting with `--setting <path>`. The window focuses the
setting's control and highlights its label, in the running window when there is
one. Search synonyms for a row are a `.keywords` attribute on its label in the
locale files.

## Editors

A row's editor comes from its field's type: lys-shell's field type (`Monitor`,
`IconTheme`, `Directory`, `Command`, `Secret`, `StrftimeFormat`, ...) or the
`format` lys-wm's schema gives a key (`lys-wm-color`, `lys-wm-font-file`,
`lys-wm-xkb-layout`, ...).

Where the system can list the values, the editor offers the list, with special
values (`auto`, `primary`, the default) first:

- connected outputs from `/sys/class/drm` and DRM cards under `/dev/dri`;
- icon and cursor themes;
- xkeyboard-config layouts, variants, options, models and rules;
- network interfaces and hwmon sensors;
- audio sources;
- PAM services in `/etc/pam.d`;
- font files, programs on `PATH` and terminal emulators.

The entry still accepts any value; one the list lacks is kept and marked as not
found. A format field offers its placeholders and previews the result.

## Icons

The icons the pages use are installed under `share/lys-settings/icons`, which the
app adds to its icon search path. No file is shared with lys-shell's package.
