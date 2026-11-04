{self, ...}: let
  options = {
    lib,
    pkgs,
    ...
  }: {
    options.programs.lys-settings = {
      enable = lib.mkEnableOption "lys-settings, the lys desktop's settings app";
      package = lib.mkOption {
        type = lib.types.package;
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        defaultText = lib.literalExpression "lys-settings.packages.\${system}.default";
        description = "The lys-settings package to install.";
      };
    };
  };
in {
  flake = {
    nixosModules.default = {
      config,
      lib,
      ...
    }: {
      imports = [options];
      config = lib.mkIf config.programs.lys-settings.enable {
        environment.systemPackages = [config.programs.lys-settings.package];
      };
    };

    homeManagerModules.default = {
      config,
      lib,
      ...
    }: {
      imports = [options];
      config = lib.mkIf config.programs.lys-settings.enable {
        home.packages = [config.programs.lys-settings.package];
      };
    };

    overlays.default = _final: prev: {
      lys-settings = self.packages.${prev.stdenv.hostPlatform.system}.default;
    };
  };
}
