# Development

The flake provides every dev tool and the `lys-wm` the tests run against:
`direnv allow`, or `nix develop`.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just docker [recipe]` | Run a recipe (default `check`) in a container away from the desktop session |
| `just check` | `vet`, `lint`, `no-fused`, `test`, `race`, `build`, in order |
| `just vet` | `go vet` |
| `just lint` | `golangci-lint` formatting check and lints |
| `just fmt` | Format every Go source |
| `just no-fused` | Fail on float products fused into an FMA on arm64 |
| `just test` | The tests, including the end-to-end test against a headless lys-wm |
| `just race` | The tests under the race detector |
| `just build` | Compile-check every package |
| `just dist` | The release binary, into `dist/` |
| `just run [args]` | Run the app |
| `just vendor-hash` | Refresh `vendorHash` in `nix/lys-settings.nix` after a `go.mod` change |
| `just release-patch`, `-minor`, `-major` | Tag and push the next release |

`just test`, `just race` and `just check` start a headless lys-wm: run them through
`just docker`, never against the desktop session. A plain `go test ./...` is safe:
the end-to-end and contract tests skip without `LYS_SETTINGS_HEADLESS`.

## Tests

- `internal/wmconfig` tests byte-level edits, with a fuzz target, and the override
  file's failure paths against a fake `lys-wm`: refused, read-only, a Home Manager
  config, a dangling link, changed during the check, reload failed.
- `internal/settings` tests the pages and rows offscreen, including that every
  control writes only what lys-shell or lys-wm accepts, and that no row falls back
  to a plain text editor unless it is on the free-form list.
- `e2e` starts a headless lys-wm in a private runtime directory, changes its
  config, reads the result back over IPC, and opens the real window on it.

## Nix

`nix build` builds the package. `nix flake check` adds the Home Manager module
check, the wrapper and desktop-file check, and a NixOS VM test of accounts-daemon.
`nix fmt` formats Nix files with alejandra.

## Releases

`just release-patch` (or `-minor`, `-major`) tags a clean `main` whose CI passed.
`.github/workflows/release.yml` then publishes the archive, `.deb` and `.rpm` for
x86_64 and aarch64, with `SHA256SUMS`. Release after the lys-shell and lys-wm versions
it needs: `go.mod` pins lys-shell, and `flake.lock` pins the lys-wm the tests use.
