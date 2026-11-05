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
| Compositor | `~/.config/lys-wm/runtime.yaml`, as an override | At once when a lys-wm is running, by `lys-wmctl reload` |

**Reset all** drops every runtime override, lys-shell's and lys-wm's. A row's reset
button drops that row's override, so the value falls back to your `config.yaml`, then
to the default.
