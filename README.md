<p align="center">
  <img src="docs/public/lys-settings.svg" width="160" alt="lys-settings">
</p>

<h1 align="center">lys-settings</h1>

<p align="center">
  The lys desktop's settings app, for lys-shell and lys-wm.
</p>

<p align="center">
  <a href="https://github.com/stubbedev/lys-settings/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/stubbedev/lys-settings/ci.yml?branch=main&style=for-the-badge&label=CI" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=for-the-badge" alt="MIT license"></a>
  <a href="https://stubbedev.github.io/lys-settings/"><img src="https://img.shields.io/badge/docs-stubbedev.github.io-informational?style=for-the-badge" alt="Documentation"></a>
</p>

One window for the settings of
[lys-shell](https://github.com/stubbedev/lys-shell),
[lys-wm](https://github.com/stubbedev/lys-wm) and the system services they run on.
Config changes are written as runtime overrides to each project's `runtime.yaml`,
over the `config.yaml` you declare, which the app never writes. lys-wm checks every
change before it is written and reloads it after. A config that Home Manager links
into the Nix store stays editable.

## Install

lys-shell's Nix modules install it by default. To install it alone:

```nix
{
  inputs.lys-settings.url = "github:stubbedev/lys-settings";

  imports = [inputs.lys-settings.homeManagerModules.default]; # or nixosModules.default
  programs.lys-settings.enable = true;
}
```

Debian and Fedora packages and an archive are on the
[releases page](https://github.com/stubbedev/lys-settings/releases). See
[Install](https://stubbedev.github.io/lys-settings/guide/install).

## Use

```sh
lys-settings                                          # open the window
lys-settings --page wm-input                          # open on a page
lys-settings --setting input.touchpad.natural_scroll  # open on a setting, focused
lys-settings --list-pages                             # print the pages and settings as JSON
```

## Development

```sh
nix develop
just docker check  # every gate, in a container away from the desktop session
```

See [Development](https://stubbedev.github.io/lys-settings/guide/development).
