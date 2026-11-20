#!/bin/sh
# Install (or reinstall) the smart-plugs module into a Prusa Connect Docker
# container. Run it ON the machine that runs Docker (for example an Unraid server), from
# the folder `scripts/make-kit.sh` built (it is installed there as install.sh,
# beside smart-plugs-linux_<cpu>.tar.gz and SHA256SUMS).
#
#   sh install.sh [-n] [-y] [-c <config path>] <container> [<name>=<host>[=<printer serial>] ...]
#
#   sh install.sh prusa-connect          install or upgrade; add and edit plugs in the app
#   sh install.sh prusa-connect \
#       core-one=192.168.1.30=CZPX1234X009XC12345 mk4s=192.168.1.31=CZPX5678X009XC67890
#
# With no plugs the install leaves whatever is set up in the app alone, so it is
# how to upgrade. With plugs it REPLACES the plug settings with exactly these.
#
#   <container>   the Connect container's name, as `docker ps` shows it
#   <plug>        optional, one per plug, up to eight: its name (lower-case letters, digits,
#                 hyphens), its host name or IPv4 address (no port), and optionally
#                 the serial of the printer it powers, as Connect shows it
#   -n            show what would run and stop; nothing is changed
#   -y            do not ask before stopping and starting the container
#   -c <path>     the container's config file, if it is not /config/config.yaml
#
# It checks the copy against SHA256SUMS, copies the module into the container's
# /data, stops the container, runs `module add` in a throwaway container on the
# same volumes (as the image's own user), removes the copies, starts the
# container again (the install is read at start) and shows what the module
# logged. Re-running replaces the install; the plug settings only if plugs are given.
# Needs only sh, docker and coreutils.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
DRY=0
ASSUME_YES=0
CONFIG=

while getopts "nyc:" opt; do
	case $opt in
	n) DRY=1 ;;
	y) ASSUME_YES=1 ;;
	c) CONFIG=$OPTARG ;;
	*) exit 2 ;;
	esac
done
shift $((OPTIND - 1))

if [ $# -lt 1 ]; then
	sed -n '2,31p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
fi

CONTAINER=$1
shift

fail() {
	echo "FAIL  $*" >&2
	exit 1
}

# The container name and config path go into shell lines, so they are plain.
printf '%s\n' "$CONTAINER" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9_.-]*$' ||
	fail "the container name is letters, digits, _, . and - only: $CONTAINER"
if [ -n "$CONFIG" ]; then
	printf '%s\n' "$CONFIG" | grep -Eq '^[A-Za-z0-9_./-]+$' ||
		fail "the config path is letters, digits, _, ., / and - only: $CONFIG"
fi

# The plugs become the settings file. They are held to the rules the module
# enforces at start, so a typo fails here and not after a restart. Every value is
# limited to characters that need no JSON escaping.
[ $# -le 8 ] || fail "give at most eight plugs"

NAME_RE='^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$'
LABEL='[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?'
HOST_RE="^$LABEL(\\.$LABEL)*\$"
SERIAL_RE='^[A-Za-z0-9._-]{0,64}$'

JSON=
SUMMARY=
SEEN=" "
n=0
for spec in "$@"; do
	n=$((n + 1))
	name=${spec%%=*}
	rest=${spec#*=}
	[ "$rest" != "$spec" ] || fail "'$spec': write it as name=host or name=host=printer-serial"
	host=${rest%%=*}
	serial=
	case $rest in *=*) serial=${rest#*=} ;; esac
	case $serial in *=*) fail "'$spec': write it as name=host or name=host=printer-serial" ;; esac

	printf '%s\n' "$name" | grep -Eq "$NAME_RE" ||
		fail "'$spec': the name is lower-case letters, digits and hyphens, at most 32"
	[ ${#host} -le 253 ] && printf '%s\n' "$host" | grep -Eq "$HOST_RE" ||
		fail "'$spec': the host is a name or an IPv4 address, with no port"
	printf '%s\n' "$serial" | grep -Eq "$SERIAL_RE" ||
		fail "'$spec': that is not a printer serial"
	case $SEEN in *" $name "*) fail "the name '$name' is used twice" ;; esac
	SEEN="$SEEN$name "

	[ -z "$JSON" ] || JSON="$JSON,"
	JSON="$JSON\"plug${n}_name\":\"$name\",\"plug${n}_host\":\"$host\",\"plug${n}_printer\":\"$serial\""
	SUMMARY="$SUMMARY  $name -> $host${serial:+ (printer $serial)}
"
done
JSON="{$JSON}"
HAVE_PLUGS=1
[ $n -gt 0 ] || HAVE_PLUGS=0
if [ "$HAVE_PLUGS" = 0 ]; then
	SUMMARY="  (none given: settings made in the app are kept, and a first install starts empty)
"
fi

# Which build matches this machine.
case $(uname -m) in
x86_64 | amd64) PLATFORM=linux_amd64 ;;
aarch64 | arm64) PLATFORM=linux_arm64 ;;
*) fail "no build for this CPU: $(uname -m)" ;;
esac
BUNDLE=smart-plugs-$PLATFORM.tar.gz
[ -f "$DIR/$BUNDLE" ] || fail "$DIR/$BUNDLE is not here. Copy the whole kit folder, not just install.sh"

# A copy that was cut short or changed would be installed and run as it is.
if [ -f "$DIR/SHA256SUMS" ]; then
	if command -v sha256sum > /dev/null 2>&1; then
		(cd "$DIR" && grep " $BUNDLE\$" SHA256SUMS | sha256sum -c - > /dev/null 2>&1) ||
			fail "$BUNDLE does not match SHA256SUMS. Copy it again"
	elif command -v shasum > /dev/null 2>&1; then
		(cd "$DIR" && grep " $BUNDLE\$" SHA256SUMS | shasum -a 256 -c - > /dev/null 2>&1) ||
			fail "$BUNDLE does not match SHA256SUMS. Copy it again"
	else
		echo "NOTE  no sha256sum here, so the copy was not checked against SHA256SUMS"
	fi
else
	echo "NOTE  no SHA256SUMS beside the bundle, so the copy was not checked"
fi

MODULE=github.com/ccmpbll/tasmota-plugs
VERSION=$(tar -xzOf "$DIR/$BUNDLE" module.yaml | sed -n 's/^version: *//p' | head -n 1)
[ -n "$VERSION" ] || fail "could not read the version from $BUNDLE"

CONFIG_FLAG=
[ -z "$CONFIG" ] || CONFIG_FLAG=" -config $CONFIG"

echo "Container   $CONTAINER"
echo "Module      $MODULE@$VERSION ($PLATFORM)"
echo "Plugs"
printf '%s' "$SUMMARY"
echo

# run shows the command and runs it, or only shows it with -n.
run() {
	if [ "$DRY" = 1 ]; then
		echo "  would run: $*"
	else
		"$@"
	fi
}

IMAGE=$(docker inspect -f '{{.Config.Image}}' "$CONTAINER" 2>/dev/null || true)
[ -n "$IMAGE" ] || IMAGE="<the container's image>"

if [ "$DRY" = 0 ]; then
	state=$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || true)
	[ "$state" = true ] || fail "$CONTAINER is not a running container. Check the name with: docker ps"

	if [ "$ASSUME_YES" = 0 ]; then
		echo "This STOPS $CONTAINER, installs the module, and starts it again. Printers"
		echo "reconnect afterwards, and a file transfer to a printer that is in progress"
		echo "would fail."
		printf "Continue? [y/N] "
		read -r answer
		case $answer in y | Y | yes) ;; *)
			echo "Stopped. Nothing was changed."
			exit 1
			;;
		esac
	fi
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/smart-plugs-install.XXXXXX")
SETTINGS=$WORK/smart-plugs-settings.json
# mktemp makes private files, docker cp keeps the mode, and the server runs as
# another user inside the container, so the copies must be readable by others.
chmod 755 "$WORK"
if [ "$HAVE_PLUGS" = 1 ]; then
	printf '%s\n' "$JSON" > "$SETTINGS"
	chmod 644 "$SETTINGS"
fi
cp "$DIR/$BUNDLE" "$WORK/$BUNDLE"
chmod 644 "$WORK/$BUNDLE"

# inside runs one command in a throwaway container that shares the Connect
# container's volumes and image (so it is the same user, config and data), with
# no network. It works whether the Connect container is running or stopped.
inside() {
	entry=$1
	shift
	docker run --rm --volumes-from "$CONTAINER" --network none --entrypoint "$entry" "$IMAGE" "$@"
}

COPIED=0
STOPPED=0
cleanup() {
	status=$?
	if [ "$COPIED" = 1 ]; then
		inside rm -f "/data/$BUNDLE" /data/smart-plugs-settings.json > /dev/null 2>&1 || true
	fi
	if [ "$STOPPED" = 1 ]; then
		echo "Starting $CONTAINER again..."
		docker start "$CONTAINER" > /dev/null 2>&1 || echo "FAIL  could not start $CONTAINER. Start it from the Unraid Docker tab." >&2
	fi
	rm -rf "$WORK"
	exit $status
}
trap cleanup EXIT

echo "1. copying into $CONTAINER:/data..."
# Old copies go first: docker cp overwrites a file without changing its mode, so
# a copy left by an earlier failed run could keep the wrong one.
if [ "$DRY" = 0 ]; then
	COPIED=1
	docker exec "$CONTAINER" rm -f "/data/$BUNDLE" /data/smart-plugs-settings.json
fi
run docker cp "$WORK/$BUNDLE" "$CONTAINER:/data/$BUNDLE"
if [ "$HAVE_PLUGS" = 1 ]; then
	run docker cp "$SETTINGS" "$CONTAINER:/data/smart-plugs-settings.json"
fi

echo "2. stopping $CONTAINER..."
# A running module holds its own files open, and on some file systems (Unraid's
# user shares among them) a file that is open cannot be cleared away, so
# `module add` could not replace an earlier install. A stopped core also has no
# database open while the grants are written.
run docker stop "$CONTAINER"
[ "$DRY" = 1 ] || STOPPED=1

echo "3. installing..."
# This module was installed as github.com/ccmpbll/connect-smart-plugs and then as
# github.com/ccmpbll/prusa-connect-tasmota before it was named tasmota-plugs. If
# either is still here it is taken away, so the two do not both draw a Tasmota
# Plugs page. Nothing happens when neither is.
for OLD_MODULE in github.com/ccmpbll/connect-smart-plugs github.com/ccmpbll/prusa-connect-tasmota; do
	# shellcheck disable=SC2086 # CONFIG_FLAG is empty or " -config <path>"
	run inside /prusa_connect module remove "$OLD_MODULE" $CONFIG_FLAG > /dev/null 2>&1 || true
done
# With no plugs there is no -settings, so the settings already saved are kept.
SETTINGS_FLAG=
[ "$HAVE_PLUGS" = 0 ] || SETTINGS_FLAG=" -settings /data/smart-plugs-settings.json"
# shellcheck disable=SC2086 # the flags are empty or " -flag <value>"
run inside /prusa_connect module add "$MODULE@$VERSION" \
	-from "/data/$BUNDLE" -approve $SETTINGS_FLAG $CONFIG_FLAG

echo "4. removing the copies..."
run inside rm -f "/data/$BUNDLE" /data/smart-plugs-settings.json
[ "$DRY" = 1 ] || COPIED=0

echo "5. starting $CONTAINER..."
run docker start "$CONTAINER"
[ "$DRY" = 1 ] || STOPPED=0

echo "6. waiting for it to be healthy..."
if [ "$DRY" = 1 ]; then
	echo "  would wait for the container's health check, up to 90 seconds"
else
	i=0
	health=
	while [ $i -lt 45 ]; do
		health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER" 2>/dev/null || true)
		if [ "$health" = healthy ] || [ "$health" = none ]; then
			break
		fi
		i=$((i + 1))
		sleep 2
	done
	echo "   health: $health"
fi

echo "7. what the module logged:"
if [ "$DRY" = 1 ]; then
	echo "  would run: docker logs --since 2m $CONTAINER | grep smart-plugs"
else
	sleep 3
	docker logs --since 2m "$CONTAINER" 2>&1 | grep -i 'smart-plugs' | tail -n 8 | cut -c1-220 || true
fi

echo
echo "Done. Watch it with:  docker logs -f $CONTAINER 2>&1 | grep -E 'smart-plugs|print event'"
