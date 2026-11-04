# Install

## Nix

lys-shell's modules install lys-settings unless you say otherwise:

```nix
programs.lys-shell.settingsApp.enable = false;              # leave it out
programs.lys-shell.settingsApp.package = pkgs.lys-settings; # or use your own
```

To install it alone, as on lys-wm without the shell:

```nix
{
  inputs.lys-settings.url = "github:stubbedev/lys-settings";

  imports = [inputs.lys-settings.homeManagerModules.default]; # or nixosModules
  programs.lys-settings.enable = true;
}
```

`overlays.default` adds `pkgs.lys-settings`.

## Debian and Fedora

The [releases page](https://github.com/stubbedev/lys-settings/releases) has a
`.deb` and an `.rpm`. lys-shell's packages from 0.3.5 recommend it, and it
replaces the copy lys-shell shipped before that.

## Archive

```sh
tar xzf lys-settings-*-linux.tar.gz
cd lys-settings-*-linux
sudo install -Dm755 lys-settings -t /usr/local/bin
sudo install -Dm644 dev.stubbe.lys.settings.desktop -t /usr/local/share/applications
sudo install -Dm644 lys-settings.svg -t /usr/local/share/icons/hicolor/scalable/apps
sudo mkdir -p /usr/local/share/lys-settings
sudo cp -r icons /usr/local/share/lys-settings/
```

## Versions

Each lys-settings release is built against the lys-shell release in its `go.mod`,
and reads lys-wm's rules from the `lys-wm` on the path when it runs. A key an older
lys-wm does not know is refused by that lys-wm with a message on the page, and the
**Compositor** pages say so when a lys-wm is too old to answer at all.
