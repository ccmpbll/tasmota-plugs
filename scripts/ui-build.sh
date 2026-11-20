#!/bin/sh
# Build the module's browser half in a Node container, so nothing is installed on
# this machine. Needs Docker, and go for the one generator step.
#
#   sh scripts/ui-build.sh
#
# What it does:
#   1. regenerates client/ (the typed client for the module's routes) from openapi.json
#   2. in a node container, runs scripts/ui-steps.sh: builds the two library
#      packages from .core, the client and the bundle (formatting ui/ with
#      prettier, then lint, type check and build), with pnpm's download cache
#      kept in .pnpm-store (git-ignored)
#   3. leaves the release's browser half in ui/dist: module.yaml and ui/
#
# The container runs as your user, so the files it writes are yours. The checkout
# in .core and the module's node_modules are written by it and are git-ignored.
set -eu

HERE=$(cd "$(dirname "$0")/.." && pwd)
IMAGE=${NODE_IMAGE:-node:24-bookworm-slim}
NAMESPACE=com.github.aux.ccmpbll.tasmota-plugs

echo "1. generating the typed client..."
(cd "$HERE/.core" && go run ./cmd/generate-client-sdk -module -namespace "$NAMESPACE" \
	-name smart-plugs-client -document ../openapi.json -out ../client > /dev/null)

echo "2. building in $IMAGE (the first run downloads packages, and takes a few minutes)..."
docker run --rm --user "$(id -u):$(id -g)" \
	-e HOME=/tmp/home -e TMPDIR=/tmp -e COREPACK_HOME=/tmp/home/corepack -e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
	-e CI=true -e FORMAT=1 \
	-v "$HERE:/mod" -w /mod "$IMAGE" sh /mod/scripts/ui-steps.sh

echo "3. the browser half is in ui/dist:"
find "$HERE/ui/dist" -type f | sed "s#$HERE/##" | sort
