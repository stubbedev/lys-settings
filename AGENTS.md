# AGENTS.md

Read `README.md` for what lys-settings is, and `docs/guide/how-it-works.md` for
how it speaks to lys-shell and lys-wm.

## Gates

Run before every commit:

```sh
just docker check
nix build -L
```

`just test` starts a headless lys-wm in a private runtime directory. Run it, and
`just check`, through `just docker <recipe>` or in CI, never against the desktop
session on the host. A plain `go test ./...` stays safe: the end-to-end test and
the contract test skip without `LYS_SETTINGS_HEADLESS`, and no test touches the
real config, whose directories are private to each test.

After a `go.mod` change, refresh `nix/lys-settings.nix` with `just vendor-hash`.

Verify locally before pushing, push, and check CI once when pushes stop. A red
`main` is fixed before anything else lands.

## Tests define behavior

Every feature and every bug fix lands with tests, positive and negative: a
behavior holds when it should, and when it should not, the failure is the defined
one. A fix without a test that fails before it is not a fix. Gates count work,
never wall clock; timeouts only stop hangs.

## Where things belong

- lys-shell's and lys-wm's parsers are the one source of truth for their configs.
  This repository holds no copy of either's rules: it reads lys-shell through its
  public packages and lys-wm through `lys-wm --config-schema`, `--default-config`
  and `--check-config`. A new key in either needs a row here, not a new rule.
- `internal/wmconfig` is pure edits over bytes plus the file's effects at the edge.
  An edit that would change more than its key is refused, never approximated.
- Every message lives in `internal/locale/locales`, in en-US and fr.
- An icon a page names is one `resources/icons` ships, in the app's own share
  directory, so no file is owned by both this package and lys-shell's.

## The tracker

GitHub issues on `stubbedev/lys-settings`. Nothing is planned in local files.
