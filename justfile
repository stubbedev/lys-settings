# lys-settings dev tasks.

set shell := ["bash", "-euo", "pipefail", "-c"]

# The program interpreter the dist/ binary requests: the distro's loader. purego
# links it dynamically, and a Go toolchain from Nix would otherwise name its own
# /nix/store loader, which exists only on NixOS.
fhs_interpreter := if arch() == "aarch64" { "/lib/ld-linux-aarch64.so.1" } else { "/lib64/ld-linux-x86-64.so.2" }

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, the FMA check, the tests, the race
# detector, build. The tests start a headless lys-wm: run this as `just docker
# check`, away from the desktop session.
check: vet lint no-fused test race build

# Static analysis of every package with go vet.
vet:
    go vet ./...

# Lint every package; settings live in .golangci.yml.
lint:
    golangci-lint fmt --diff
    golangci-lint run

# Fail on any float product fused into an FMA, which rounds differently from amd64.
no-fused:
    no-fused

# Run the test suite for every package, the end-to-end test against a headless
# lys-wm in a private runtime directory included.
test:
    LYS_SETTINGS_HEADLESS=1 go test -count=1 -shuffle=on ./...

# Every package under the race detector, which needs cgo.
race:
    CGO_ENABLED=1 LYS_SETTINGS_HEADLESS=1 go test -race -count=1 -shuffle=on ./...

# Compile-check every package; the output is discarded.
build:
    go build -o /dev/null ./...

# Build the release binary into dist/, as the archive and the packages ship it.
dist version="dev":
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -I {{fhs_interpreter}} -X main.version={{version}}" -o dist/ ./cmd/lys-settings

# Run the settings app.
run *args:
    go run ./cmd/lys-settings {{args}}

# Format every Go source in place.
fmt:
    golangci-lint fmt

# Set nix/lys-settings.nix's vendorHash to the hash of the modules go.sum pins.
vendor-hash:
    nix run github:stubbedev/lys-ci#vendor-hash -- nix/lys-settings.nix .#default.goModules

# Run a recipe in the flake's dev shell inside a throwaway container, so nothing
# the tests start reaches the desktop session: no host runtime dir, display, bus
# or DRM node.
docker recipe="check" memory="6g":
    #!/usr/bin/env bash
    set -euo pipefail
    dir=$(mktemp -d)
    trap 'rm -rf "$dir"' EXIT
    nix print-dev-env > "$dir/env.sh"
    printf 'root:x:0:0::/root:/bin/sh\nlys-settings:x:%s:%s::/go/home:/bin/sh\n' "$(id -u)" "$(id -g)" > "$dir/passwd"
    printf 'root:x:0:\nlys-settings:x:%s:\n' "$(id -g)" > "$dir/group"
    bash=$(nix build --inputs-from . --no-link --print-out-paths nixpkgs#bash^out)/bin/bash
    certs=$(nix build --inputs-from . --no-link --print-out-paths nixpkgs#cacert^out)/etc/ssl/certs/ca-bundle.crt
    docker volume create lys-settings-go > /dev/null
    docker run --rm -v lys-settings-go:/go debian:trixie-slim chown "$(id -u):$(id -g)" /go
    docker run --rm --init --user "$(id -u):$(id -g)" \
        --memory {{quote(memory)}} --memory-swap {{quote(memory)}} --pids-limit 4096 --shm-size 1g \
        -v /nix/store:/nix/store:ro -v "$dir/env.sh:/env.sh:ro" -v "$dir/passwd:/etc/passwd:ro" -v "$dir/group:/etc/group:ro" -v "$PWD:/src" -v lys-settings-go:/go -w /src -e SSL_CERT_FILE="$certs" \
        debian:trixie-slim "$bash" -c 'source /env.sh && export HOME=/go/home GOMODCACHE=/go/mod GOCACHE=/go/cache GOLANGCI_LINT_CACHE=/go/lint TMPDIR=/tmp && unset XDG_RUNTIME_DIR WAYLAND_DISPLAY DISPLAY DBUS_SESSION_BUS_ADDRESS && mkdir -p "$HOME" && exec just "$1"' docker {{quote(recipe)}}

# Tag the current commit as the next release and push it, refusing anything but
# a clean default branch whose CI succeeded.
release-patch:
    nix run github:stubbedev/lys-ci#release -- patch

release-minor:
    nix run github:stubbedev/lys-ci#release -- minor

release-major:
    nix run github:stubbedev/lys-ci#release -- major
