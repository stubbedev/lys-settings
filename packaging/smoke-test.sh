#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "Usage: $0 <package.deb|package.rpm>" >&2
	exit 1
fi

PACKAGE="$(readlink -f "$1")"

case "${PACKAGE}" in
*.deb) apt-get install -y --no-install-recommends "${PACKAGE}" ;;
*.rpm) dnf -y --setopt=install_weak_deps=False install "${PACKAGE}" ;;
*)
	echo "Not a .deb or .rpm: ${PACKAGE}" >&2
	exit 1
	;;
esac

fail() {
	echo "smoke test: $*" >&2
	exit 1
}

for file in /usr/share/applications/dev.stubbe.lys.settings.desktop \
	/usr/share/icons/hicolor/scalable/apps/lys-settings.svg \
	/usr/share/lys-settings/icons/hicolor/scalable/actions/ld-x-symbolic.svg; do
	[[ -f ${file} ]] || fail "${file} is missing"
done

home="$(mktemp -d)"
trap 'rm -rf "${home}"' EXIT
pages="$(HOME="${home}" XDG_CONFIG_HOME="${home}/config" lys-settings --list-pages)"
[[ ${pages} == *'"id":"bar-general"'* ]] || fail "--list-pages printed ${pages@Q}"

status=0
lys-settings --bogus >/dev/null 2>&1 || status=$?
[[ ${status} -eq 2 ]] || fail "an unknown flag exited ${status}, want 2"

echo "smoke test: ${PACKAGE} installs and runs"
