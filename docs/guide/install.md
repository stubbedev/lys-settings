# Install

## Nix

lys-shell's NixOS and Home Manager modules install lys-settings by default:

```nix
programs.lys-shell.settingsApp.enable = false;              # leave it out
programs.lys-shell.settingsApp.package = pkgs.lys-settings; # or use another package
```

To install it alone, as on lys-wm without the shell:

```nix
{
  inputs.lys-settings.url = "github:stubbedev/lys-settings";

  imports = [inputs.lys-settings.homeManagerModules.default]; # or nixosModules.default
  programs.lys-settings.enable = true;
}
```

| Option | Default | Effect |
| --- | --- | --- |
| `programs.lys-settings.enable` | `false` | Install the package |
| `programs.lys-settings.package` | this flake's package | Package to install |

`overlays.default` adds `pkgs.lys-settings`.

## Debian and Fedora

The [releases page](https://github.com/stubbedev/lys-settings/releases) has a
`.deb` and an `.rpm` for amd64 and arm64. They recommend lys-shell 0.3.5 or later,
or lys-wm, and conflict with lys-shell packages older than 0.3.5.

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

The app loads its icons from `../share/lys-settings/icons` relative to its binary.

## Versions

Each release is built against the lys-shell version in its `go.mod`. lys-wm's
rules are read at run time from the `lys-wm` on `PATH`: a key that lys-wm does not
know is refused with its message, and a lys-wm that cannot report its schema or
built-in config leaves the **Compositor** pages showing the error.
