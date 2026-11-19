#!/bin/sh
# Build the release folder (dist/) for every platform module.yaml lists:
# module.yaml and the browser half (ui/, from scripts/ui-build.sh) at the root,
# and one binary per platform, named
# smart-plugs_<GOOS>_<GOARCH>. Point `module add -from` or core.modules_local
# at the folder. Needs go only.
set -e

HERE=$(cd "$(dirname "$0")/.." && pwd)
DIST=$HERE/dist

[ -f "$HERE/ui/dist/ui/index.js" ] || { echo "FAIL  the browser half is not built. Run: sh scripts/ui-build.sh" >&2; exit 1; }
rm -rf "$DIST" && mkdir -p "$DIST"
cp -R "$HERE/ui/dist/." "$DIST/"
cp "$HERE/module.yaml" "$DIST/module.yaml"

for platform in darwin_arm64 linux_amd64 linux_arm64 windows_amd64; do
	os=${platform%_*}
	arch=${platform#*_}
	ext=
	[ "$os" = windows ] && ext=.exe
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go -C "$HERE" build -trimpath -o "$DIST/smart-plugs_${platform}${ext}" .
	echo "built smart-plugs_${platform}${ext}"
done

echo "release folder: $DIST"
