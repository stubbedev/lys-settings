{
  lib,
  buildGo127Module,
  makeWrapper,
  libX11,
  xkeyboard-config,
  libva,
  vulkan-loader,
  version,
  src,
}: let
  # What the binary dlopens through purego: lys-ui's GPU renderer and the
  # recorder pages' device probes.
  libs = [libva vulkan-loader];
in
  buildGo127Module {
    pname = "lys-settings";
    inherit version src;

    vendorHash = "sha256-IOzSOW8b4ycBb9cIqlr9vBjzRUWcf7vINbHNtLLptuQ=";

    subPackages = ["cmd/lys-settings"];

    env.CGO_ENABLED = "0";

    ldflags = [
      "-s"
      "-w"
      "-X main.version=${version}"
      "-X github.com/stubbedev/lys-lib/xkb.defaultRoot=${xkeyboard-config}/share/X11/xkb"
      "-X github.com/stubbedev/lys-lib/xkb.defaultXLocaleDir=${libX11}/share/X11/locale"
    ];

    # The suite runs in `just check`: it needs lys-wm and fonts.
    doCheck = false;

    nativeBuildInputs = [makeWrapper];

    postInstall = ''
      install -Dm0644 resources/dev.stubbe.lys.settings.desktop -t $out/share/applications
      install -Dm0644 resources/lys-settings.svg -t $out/share/icons/hicolor/scalable/apps
      go tool install-icons $out/share/lys-settings/icons/hicolor/scalable/actions $(cat resources/icons.txt)
    '';

    postFixup = ''
      wrapProgram $out/bin/lys-settings --prefix LD_LIBRARY_PATH : "${lib.makeLibraryPath libs}"
    '';

    doInstallCheck = true;
    installCheckPhase = ''
      runHook preInstallCheck
      export HOME=$TMPDIR XDG_CONFIG_HOME=$TMPDIR/config
      $out/bin/lys-settings --version | grep -qxF "lys-settings ${version}"
      $out/bin/lys-settings --list-pages | grep -qF '"id":"bar-general"'
      if $out/bin/lys-settings --bogus 2>/dev/null; then
        echo "an unknown flag was accepted" >&2
        exit 1
      fi
      runHook postInstallCheck
    '';

    passthru = {inherit libs;};

    meta = {
      description = "The lys desktop's settings app, for lys-shell and lys-wm";
      homepage = "https://github.com/stubbedev/lys-settings";
      license = lib.licenses.mit;
      mainProgram = "lys-settings";
      platforms = lib.platforms.linux;
    };
  }
