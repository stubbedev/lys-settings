{
  description = "lys-settings - the lys desktop's settings app";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    lys-ci.url = "github:stubbedev/lys-ci";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    # The compositor whose config the tests hold this app to. The Go code never
    # links it: lys-wm's contract is its --check-config, --default-config and
    # --config-schema flags.
    lys-wm = {
      url = "github:stubbedev/lys-wm";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-parts.follows = "flake-parts";
      inputs.lys-shell.follows = "";
    };
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = ["x86_64-linux" "aarch64-linux"];
      imports = [
        ./nix/package.nix
        ./nix/shell.nix
        ./nix/modules.nix
        ./nix/checks.nix
      ];
    };
}
