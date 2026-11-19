#!/bin/sh
# Fetch the Prusa Connect source this module builds against into .core, at the
# commit named in .core-version. The module's go.mod and ui/pnpm-workspace.yaml
# point at it (the SDK and the component library are not published yet).
#
#   sh scripts/setup.sh
#
# Needs git. Safe to run again: it does nothing when .core is already at the
# commit. CORE_URL overrides the repository address (to use a local mirror).
set -eu

HERE=$(cd "$(dirname "$0")/.." && pwd)
CORE=$HERE/.core
PIN=$(tr -d '[:space:]' < "$HERE/.core-version")
URL=${CORE_URL:-https://github.com/prusa3d/prusa-connect.git}

if [ -d "$CORE/.git" ]; then
	if [ "$(git -C "$CORE" rev-parse HEAD)" = "$PIN" ]; then
		echo "OK    .core is already at $PIN"
		exit 0
	fi
	echo "FAIL  .core exists at a different commit. Remove it and run this again:"
	echo "      rm -rf .core"
	exit 1
fi

if [ -e "$CORE" ]; then
	echo "FAIL  .core exists and is not a git checkout. Remove it and run this again."
	exit 1
fi

echo "fetching $PIN from $URL..."
git init -q "$CORE"
git -C "$CORE" remote add origin "$URL"
git -C "$CORE" fetch -q --depth 1 origin "$PIN"
git -C "$CORE" checkout -q FETCH_HEAD
echo "OK    .core is at $(git -C "$CORE" rev-parse HEAD)"
