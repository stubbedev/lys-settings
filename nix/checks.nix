{
  self,
  inputs,
  ...
}: {
  perSystem = {
    lib,
    pkgs,
    ...
  }: let
    pkg = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
    home = enable:
      inputs.home-manager.lib.homeManagerConfiguration {
        inherit pkgs;
        modules = [
          self.homeManagerModules.default
          {
            home = {
              username = "alice";
              homeDirectory = "/home/alice";
              stateVersion = "25.11";
            };
            programs.lys-settings.enable = enable;
          }
        ];
      };
    has = enable: lib.elem pkg (home enable).config.home.packages;
  in {
    checks = {
      lys-settings-hm-module = pkgs.runCommand "lys-settings-hm-module" {} ''
        ${lib.optionalString (!has true) "echo 'enabled, and not in home.packages' >&2; exit 1"}
        ${lib.optionalString (has false) "echo 'disabled, and in home.packages' >&2; exit 1"}
        touch $out
      '';
      # lys-ui renders on the GPU through the loader and silently stays on the
      # CPU without it, and NixOS has no global library path.
      lys-settings-wrapper-vulkan = pkgs.runCommand "lys-settings-wrapper-vulkan" {} ''
        grep -q "${lib.getLib pkgs.vulkan-loader}/lib" ${pkg}/bin/lys-settings
        test -f ${pkg}/share/applications/dev.stubbe.lys.settings.desktop
        test -f ${pkg}/share/icons/hicolor/scalable/apps/lys-settings.svg
        test -f ${pkg}/share/lys-settings/icons/hicolor/scalable/actions/ld-x-symbolic.svg
        ${pkgs.desktop-file-utils}/bin/desktop-file-validate ${pkg}/share/applications/dev.stubbe.lys.settings.desktop
        touch $out
      '';
    };
  };
}
