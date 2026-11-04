# Getting started

Open the window from the launcher, the desktop menu, or a terminal:

```sh
lys-settings
lys-settings --page wm-input
```

A second launch raises the window that is already open, on the page it asked for.

The sidebar holds lys-shell's pages, and with `lys-wm` on the path a **Compositor**
group for lys-wm's. The shell's pages work without the shell running: they edit its
config, which the shell reads when it starts.

lys-shell's launcher finds these pages too: type a page's name to open it.

## Where changes go

| Pages | Written to | Takes effect |
| --- | --- | --- |
| lys-shell's | `~/.config/lys-shell/runtime.yaml`, as an override | At once, over the shell's config watcher |
| Compositor | `~/.config/lys-wm/config.yaml` | At once when a lys-wm is running, by `lys-wmctl reload` |

**Reset all** drops lys-shell's runtime overrides. It does not touch lys-wm's file.
A row's reset button there restores what lys-wm would use: the built-in config's
value for what that sets, such as `binds`, and otherwise removes the key.
