# lys-wm settings

The **Compositor** pages write runtime overrides to
`$XDG_CONFIG_HOME/lys-wm/runtime.yaml`, `~/.config/lys-wm/runtime.yaml` when that is
unset. lys-wm merges them over `config.yaml`, which this app only reads. See lys-wm's
[config reference](https://stubbedev.github.io/lys-wm/config) for what each key does.

## What a change does

1. The override file is read fresh, so a hand edit made while the window was open
   stays.
2. Only the bytes of that key's value change. Comments, blank lines and key order
   stay.
3. The result is written beside the file and checked by
   `lys-wm --check-config --runtime`, the parser a reload uses, over the real
   `config.yaml`.
4. It replaces the override file atomically, keeping its mode.
5. `lys-wmctl reload` makes a running lys-wm read it.

A change lys-wm refuses leaves the file and the session as they were, and the page
shows lys-wm's message, with the file and the line.

## Where a value comes from

Each row shows a badge: **custom** for an override of a key `config.yaml` does not
set, **override** for one that replaces a `config.yaml` value, and **config** for a
`config.yaml` value that differs from the default. **Reset** removes the override, so
the value falls back to `config.yaml`, then to lys-wm's built-in config. **Reset all**
removes every override, lys-shell's and lys-wm's.

The override file holds only the keys changed here. Without a `config.yaml`, lys-wm's
built-in config stays under it, so your binds keep working.

## Home Manager and Nix

A `config.yaml` linked into the Nix store is never written, so every setting can still
be changed. To make a change permanent in your Nix configuration, copy each key of
`runtime.yaml` to the same path in the attribute set Home Manager writes
`config.yaml` from, then delete `runtime.yaml`. `lys-wm --effective-config` lists the
keys that still come from it.

## What it will not change

- **A read-only override file.** The page names it, and the link's target when it is
  one.
- **A writable link** is written through and stays a link, as with a dotfiles
  directory.
- **YAML anchors and aliases.** An edit through one could change other keys, so it is
  refused, naming the line. Change it by hand.
- **A file that does not parse.** Fix it first; the page shows the error.

Lists and keys with more than one form, such as `binds`, `rules`, `outputs` and a
gradient border, are edited as YAML. Applying one overrides that whole value.
