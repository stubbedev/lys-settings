{inputs, ...}: {
  perSystem = {
    lib,
    pkgs,
    self',
    ...
  }: {
    devShells.default = pkgs.mkShell {
      inputsFrom = [self'.packages.default];

      packages = with pkgs; [
        gopls
        golangci-lint
        gofumpt
        delve
        just
        git
        gh
        "${inputs.lys-ci}"
        fontconfig
        inter
        jetbrains-mono
        # What the tests hold the app to, and the compositor the end-to-end test
        # starts in a private runtime directory.
        inputs.lys-wm.packages.${pkgs.stdenv.hostPlatform.system}.default
        wtype
        # Private buses for the fake system services the tests run.
        dbus
      ];

      LD_LIBRARY_PATH = lib.makeLibraryPath self'.packages.default.libs;
      XKB_CONFIG_ROOT = "${pkgs.xkeyboard-config}/share/X11/xkb";
      XLOCALEDIR = "${pkgs.libX11}/share/X11/locale";
      LOCALE_ARCHIVE = "${pkgs.glibcLocales.override {
        allLocales = false;
        locales = ["en_US.UTF-8/UTF-8" "en_GB.UTF-8/UTF-8" "fr_FR.UTF-8/UTF-8"];
      }}/lib/locale/locale-archive";
      FONTCONFIG_FILE = pkgs.makeFontsConf {
        fontDirectories = with pkgs; [inter jetbrains-mono dejavu_fonts];
      };

      CGO_ENABLED = "0";
      GOTOOLCHAIN = "local";
    };

    formatter = pkgs.alejandra;
  };
}
