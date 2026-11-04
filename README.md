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

One window for the desktop's settings. Changes to
[lys-shell](https://github.com/stubbedev/lys-shell) land as runtime overrides, as
they always have. Changes to [lys-wm](https://github.com/stubbedev/lys-wm) edit
`~/.config/lys-wm/config.yaml` in place, keeping your comments and layout: lys-wm
checks the result before it is written, and reloads it after.

A config Home Manager writes is a read-only link, which the app says instead of
changing: change that setting in your Nix configuration.

## Install

lys-shell's Nix modules install it by default. Without them:

```nix
{
  inputs.lys-settings.url = "github:stubbedev/lys-settings";

  # NixOS or home-manager
  imports = [inputs.lys-settings.homeManagerModules.default];
  programs.lys-settings.enable = true;
}
```

Debian and Fedora packages and an archive are on the
[releases page](https://github.com/stubbedev/lys-settings/releases); see
[Install](https://stubbedev.github.io/lys-settings/guide/install).

## Use

```sh
lys-settings                 # open the window
lys-settings --page wm-input # open on a page
lys-settings --list-pages    # the pages as JSON, which lys-shell's launcher lists
```

## Development

```sh
nix develop
just docker check  # every gate, away from the desktop session
```

See [Development](https://stubbedev.github.io/lys-settings/guide/development).
