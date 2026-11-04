# Development

Every dev tool, and the `lys-wm` the tests hold the app to, comes from the flake:
`direnv allow` on entry, or `nix develop`.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just docker check` | Every gate below, in a container away from the desktop session |
| `just vet` | `go vet` |
| `just lint` | `golangci-lint` formatting and lints |
| `just no-fused` | No float product fused into an FMA |
| `just test` | The tests, with a headless lys-wm: run through `just docker` |
| `just race` | The tests under the race detector |
| `just build` | Compile-check every package |
| `just dist` | The release binary, into `dist/` |
| `just vendor-hash` | Refresh `vendorHash` after a `go.mod` change |

## Tests

- `internal/wmconfig` tests the edits on bytes, with a fuzz target holding each edit to
  what it says, and the file's negative paths against a fake `lys-wm`: refused, read-only,
  a Home Manager link, a dangling link, changed during the check, reload failed.
- `internal/settings` tests the pages and rows, offscreen.
- `e2e` starts a headless lys-wm in a private runtime directory, changes its config,
  reads the result back over IPC, and opens the real window on it. It skips without
  `LYS_SETTINGS_HEADLESS`, which `just test` sets.

## Nix

`nix build` builds the package; `nix flake check` adds the Home Manager module and the
wrapper and desktop-file checks. `nix fmt` formats with alejandra.

## Releases

`just release-patch`, `-minor` or `-major` tags a clean `main` whose CI passed;
`.github/workflows/release.yml` publishes the archive, `.deb` and `.rpm`. Release after
the lys-shell and lys-wm versions it needs: `go.mod` pins the first.
