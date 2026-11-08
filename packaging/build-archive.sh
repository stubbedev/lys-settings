#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
	echo "Usage: $0 <dist-dir> <version> <arch>" >&2
	exit 1
fi

DIST_DIR="$(readlink -f "$1")"
VERSION="$2"
ARCH="$3"

cd "$(dirname "$0")/.."

STAGING="lys-settings-${VERSION}-${ARCH}-linux"
rm -rf "${STAGING}"
mkdir -p "${STAGING}/icons"
cp "${DIST_DIR}/lys-settings" LICENSE \
	resources/dev.stubbe.lys.settings.desktop \
	resources/lys-settings.svg \
	"${STAGING}/"
cp -R "${DIST_DIR}/icons/hicolor" "${STAGING}/icons/"
tar czf "${STAGING}.tar.gz" "${STAGING}"
rm -rf "${STAGING}"
