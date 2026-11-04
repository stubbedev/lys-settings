# lys-wm settings

The **Compositor** pages edit `$XDG_CONFIG_HOME/lys-wm/config.yaml`,
`~/.config/lys-wm/config.yaml` when that is unset. See lys-wm's
[config reference](https://stubbedev.github.io/lys-wm/config) for what each key does.

## What a change does

1. The file is read fresh, so a hand edit made while the window was open stays.
2. Only the bytes of that key's value change. Comments, blank lines and key order
   stay.
3. The result is written beside the file and checked by `lys-wm --check-config`,
   the parser a reload uses.
4. It replaces the file atomically, keeping its mode.
5. `lys-wmctl reload` makes a running lys-wm read it.

A change lys-wm refuses leaves the file and the session as they were, and the page
shows lys-wm's message, with the file and the line.

## With no config file yet

lys-wm's config file replaces its built-in config whole: a file without `binds` has
no binds. So the first change writes the built-in config and that change together,
and your keys keep working.

## What it will not change

- **A read-only config.** A Home Manager file is a link into the Nix store. The page
  says so and names both paths; change it in your Nix configuration.
- **A writable link** is written through and stays a link, as with a dotfiles
  directory.
- **YAML anchors and aliases.** An edit through one could change other keys, so it is
  refused, naming the line. Change it by hand.
- **A file that does not parse.** Fix it first; the page shows the error.

Lists and keys with more than one form, such as `binds`, `rules`, `outputs` and a
gradient border, are edited as YAML. Applying one replaces that whole value, with any
comments inside it.
