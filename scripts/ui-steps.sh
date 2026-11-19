#!/bin/sh
# The build of the browser half: the two Prusa Connect library packages from
# .core, the typed client for this module's routes, then the bundle itself
# (format, lint, type check, tests, build). It leaves ui/dist: module.yaml and ui/.
#
# Needs Node 22 or newer with corepack (pnpm comes through it). It is run by
# scripts/ui-build.sh inside a container, and directly by CI and by Prusa's
# marketplace builder, which have Node and Go on the runner:
#
#   LOCKED=1 sh scripts/ui-steps.sh
#
# LOCKED=1 refuses to change a lockfile (CI). FORMAT=1 rewrites ui/ with prettier
# first (the container build). Run `sh scripts/setup.sh` before this, so .core exists.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOG=${TMPDIR:-/tmp}/ui-steps.log

if ! command -v pnpm > /dev/null 2>&1; then
	BIN=${TMPDIR:-/tmp}/pnpm-bin
	mkdir -p "$BIN"
	corepack enable --install-directory "$BIN"
	PATH=$BIN:$PATH
	export PATH
fi

# quietly runs a command, keeps what it prints in $LOG, and shows the end of that
# only when the command fails.
quietly() {
	if ! "$@" > "$LOG" 2>&1; then
		echo "FAIL  $*"
		tail -n 60 "$LOG"
		exit 1
	fi
}

# install is `pnpm install` for this project's own lockfile: frozen when LOCKED=1.
install() {
	if [ "${LOCKED:-0}" = 1 ]; then
		quietly pnpm install --frozen-lockfile
	else
		quietly pnpm install --no-frozen-lockfile
	fi
}

echo "   @prusa/connect..."
cd "$ROOT/.core/packages/typescript"
quietly pnpm install --frozen-lockfile
quietly pnpm run build

echo "   @prusa/connect-ui..."
cd "$ROOT/.core/packages/svelte"
quietly pnpm install --frozen-lockfile
quietly pnpm run build

echo "   the typed client..."
cd "$ROOT/client"
install
quietly pnpm run generate:types
quietly pnpm run build

echo "   the bundle..."
cd "$ROOT/ui"
install
if [ "${FORMAT:-0}" = 1 ]; then
	pnpm exec prettier --write . > /dev/null 2>&1 || true
fi
quietly pnpm run check
quietly pnpm run build
