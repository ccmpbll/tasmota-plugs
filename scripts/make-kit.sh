#!/bin/sh
# Build the folder to copy to the Docker host: the installer, the module for
# each CPU asked for, and a SHA256SUMS to check the copy against.
#
#   sh scripts/make-kit.sh [platform ...]     default: linux_amd64 (Unraid, most servers)
#
# Writes kit/ beside the module:
#
#   kit/install.sh
#   kit/smart-plugs-linux_amd64.tar.gz
#   kit/SHA256SUMS
#
# Then: scp -r kit root@<host>:/tmp/   and on the host: sh /tmp/kit/install.sh ...
set -eu

HERE=$(cd "$(dirname "$0")/.." && pwd)
KIT=$HERE/kit

[ $# -gt 0 ] || set -- linux_amd64

rm -rf "$KIT"
mkdir -p "$KIT"
cp "$HERE/scripts/install-on-host.sh" "$KIT/install.sh"
chmod 755 "$KIT/install.sh"

for platform in "$@"; do
	sh "$HERE/scripts/bundle.sh" "$platform" > /dev/null
	mv "$HERE/smart-plugs-$platform.tar.gz" "$KIT/"
done

(
	cd "$KIT"
	if command -v sha256sum > /dev/null 2>&1; then
		sha256sum smart-plugs-*.tar.gz > SHA256SUMS
	else
		shasum -a 256 smart-plugs-*.tar.gz > SHA256SUMS
	fi
)

echo "kit: $KIT"
ls -la "$KIT"
