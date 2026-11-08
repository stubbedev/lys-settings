# lys-wm settings

The **Compositor** pages appear when `lys-wm` is on `PATH`. They write runtime
overrides to `$XDG_CONFIG_HOME/lys-wm/runtime.yaml` (`~/.config/lys-wm/runtime.yaml`
when unset). lys-wm merges them over `config.yaml`, which this app only reads. See
lys-wm's [config reference](https://stubbedev.github.io/lys-wm/config) for what each
key does.

| Page | ID | Keys |
| --- | --- | --- |
| Windows | `wm-windows` | `gaps`, `borders`, corner radius, opacity and dimming under `decoration`, `focus.on_activate` |
| Shadow & Blur | `wm-effects` | `decoration.shadow`, `decoration.blur` |
| Motion | `wm-motion` | `animations`, `overview`, `drag_preview` |
| Tiling & Tabs | `wm-tiling` | `autotile`, `tabs` |
| Input | `wm-input` | `input.keyboard`, `input.pointer`, `input.touchpad` |
| Keys & Gestures | `wm-binds` | `binds`, `submaps`, app shortcuts, `gestures` |
| Displays | `wm-outputs` | Output layout, `color_management`, `tearing`, `gpu`, `lid_suspend` |
| Rules & Session | `wm-session` | `rules`, `env`, `xwayland`, `palette` |

## What a change does

1. The override file is read fresh, so hand edits made while the window is open
   are kept.
2. Only the bytes of the key's value change. Comments, blank lines and key order
   stay.
3. The result is written to a temporary file beside it and checked with
   `lys-wm --check-config --runtime <file>`, over the real `config.yaml`.
4. If the file changed during the check, the change starts over, up to three tries.
5. The override file is replaced atomically, keeping its mode.
6. `lys-wmctl reload` makes a running lys-wm read it. With no lys-wm running, the
   page says the change is saved for the next one.

Changes made in quick succession to the same key are written once.

A change lys-wm refuses leaves the file and the session unchanged. The row goes
back to the last accepted value and shows lys-wm's message, with the file and line.
A new or edited card (a bind, rule, output or gesture) is checked before it is
written; while lys-wm refuses it, it stays on the page as an unsaved draft with the
reason. The row's reset button discards a draft.

## Displayed values

A key without an override shows the value lys-wm uses: its built-in config, else
its schema's default. Keys lys-wm leaves to the system, such as the keyboard layout
or a touchpad setting libinput decides, show **Inherited**. The focus border
defaults to `accent`, which follows the desktop accent set on the **Theme** page.

Without a `config.yaml`, lys-wm's built-in config applies under the overrides, so
its default binds keep working.

## Displays

The Displays page arranges outputs by dragging them on a canvas, and turns each on
or off and sets its resolution, refresh rate and scale, over wlr-output-management.
**Identify** shows each output's name on it.
**Apply** tests the layout, applies it, and asks on every output whether to keep it;
without an answer it reverts after 15 seconds. **Keep** writes the layout to
`outputs`, matching each monitor by its description when known, else by connector.

## Home Manager and Nix

A `config.yaml` linked into the Nix store is never written, so every setting stays
editable. To make changes permanent, copy each key of `runtime.yaml` into the
attribute set Home Manager writes `config.yaml` from, then delete `runtime.yaml`.
`lys-wm --effective-config` lists the keys still coming from it.

## Limits

- **A read-only override file** is not written. The page names it, and the link's
  target when it is a link.
- **A writable link** is written through and stays a link.
- **A dangling link** is an error.
- **YAML anchors and aliases**: an edit through one is refused, naming the line.
  Change it by hand.
- **A file that does not parse** is not written; the page shows the error.
- **Lists and multi-form keys** (`binds`, `rules`, `outputs`, a gradient border) are
  written whole: changing one item overrides the whole value.
