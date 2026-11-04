{
  perSystem = {
    lib,
    pkgs,
    ...
  }: let
    src = lib.fileset.toSource {
      root = ../.;
      fileset = lib.fileset.unions [
        ../go.mod
        ../go.sum
        ../cmd
        ../internal
        ../resources
      ];
    };
  in {
    packages = rec {
      lys-settings = pkgs.callPackage ./lys-settings.nix {
        version = "0-unstable-${lib.substring 0 8 (builtins.unsafeDiscardStringContext (baseNameOf src))}";
        inherit src;
      };
      default = lys-settings;
    };
  };
}
