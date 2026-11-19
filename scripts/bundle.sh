#!/bin/sh
# Pack one platform's release as a .tar.gz for `module add -from`: module.yaml,
# the browser half (ui/) and the one binary, at the root of the archive. The
# browser half comes from `sh scripts/ui-build.sh`.
#
#   sh scripts/bundle.sh [platform]     default linux_amd64 (Unraid, most NAS and servers)
#
# Writes smart-plugs-<platform>.tar.gz beside the module. Needs go and tar.
set -e

HERE=$(cd "$(dirname "$0")/.." && pwd)
PLATFORM=${1:-linux_amd64}
OS=${PLATFORM%_*}
ARCH=${PLATFORM#*_}
EXT=
[ "$OS" = windows ] && EXT=.exe

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

if [ ! -f "$HERE/ui/dist/ui/index.js" ]; then
	echo "FAIL  the browser half is not built. Run: sh scripts/ui-build.sh" >&2
	exit 1
fi
cp -R "$HERE/ui/dist/." "$STAGE/"
cp "$HERE/module.yaml" "$STAGE/module.yaml"
CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go -C "$HERE" build -trimpath -o "$STAGE/smart-plugs_${PLATFORM}${EXT}" .

# docker cp extracts as root, and the server runs as an unprivileged user, so
# the files must be readable by others whatever mode the source tree has.
find "$STAGE" -type d -exec chmod 755 {} +
find "$STAGE" -type f -exec chmod 644 {} +
chmod 755 "$STAGE/smart-plugs_${PLATFORM}${EXT}"

OUT=$HERE/smart-plugs-$PLATFORM.tar.gz
# Named files, not ".": module add refuses an archive entry like "./".
COPYFILE_DISABLE=1 tar --no-xattrs -C "$STAGE" -czf "$OUT" module.yaml ui "smart-plugs_${PLATFORM}${EXT}"
echo "wrote $OUT"
tar -tzf "$OUT"
