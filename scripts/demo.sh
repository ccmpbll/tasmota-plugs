#!/bin/sh
# Run a demo Connect core (simulated printers, no web app) with this module
# installed and plugs configured, in the foreground. Ctrl+C stops it.
#
#   sh scripts/demo.sh <name>=<host>[=<printer serial>] ...
#   sh scripts/demo.sh core-one=127.0.0.1=DEMO-01 mk4s=localhost=DEMO-02
#
# One argument per plug, up to eight. 127.0.0.1 is where scripts/fake-tasmota.py
# listens, and the demo's simulated printers have the serials DEMO-01 and up.
#
# It installs the module first (sh install.sh), copies the demo's data to
# .core/bin/smart-plugs-try so the install stays as built, and sets the plug
# settings on the copy. Needs go. Does not need pnpm.
set -e

HERE=$(cd "$(dirname "$0")/.." && pwd)
CORE=$HERE/.core
[ $# -ge 1 ] || { echo "usage: sh scripts/demo.sh <name>=<host>[=<printer serial>] ..."; exit 1; }
PORT=${PORT:-18080}
PASS=${PASS:-demo-pass-1234}

sh "$HERE/install.sh" > /dev/null
[ -x "$CORE/bin/demo" ] || (cd "$CORE" && go build -tags "full diagnostics" -o bin/demo ./cmd/demo)

SRC=$CORE/bin/smart-plugs-demo
TRY=$CORE/bin/smart-plugs-try
rm -rf "$TRY" && mkdir -p "$TRY" && cp -R "$SRC/demo" "$TRY/demo"

python3 - "$TRY" "$SRC" "$@" <<'PY'
import json, sys
try_dir, src, plugs = sys.argv[1], sys.argv[2], sys.argv[3:]
config = {}
for n, spec in enumerate(plugs, 1):
    name, host, *serial = spec.split("=")
    config.update({f"plug{n}_name": name, f"plug{n}_host": host, f"plug{n}_printer": (serial or [""])[0]})
path = try_dir + "/demo/resources.json"
doc = json.load(open(path))
for inst in doc["instances"]:
    if inst["name"] == "smart-plugs":
        inst["config"] = config
json.dump(doc, open(path, "w"), indent=1)
cfg = open(src + "/config.yaml").read().replace(src + "/demo/resources.json", path)
open(try_dir + "/config.yaml", "w").write(cfg)
PY

NS=com.github.aux.ccmpbll.prusa-connect-tasmota
cat <<EOT
Demo core on http://127.0.0.1:$PORT, plugs: $*. In another terminal:

  curl -s -c /tmp/ck -X POST http://127.0.0.1:$PORT/api/v1/auth/login -H 'content-type: application/json' -d '{"username":"demo","password":"$PASS"}'
  curl -s -b /tmp/ck http://127.0.0.1:$PORT/api/v1/modules/$NS/plugs
  curl -s -b /tmp/ck -X POST http://127.0.0.1:$PORT/api/v1/modules/$NS/plugs/<name>/power -H 'content-type: application/json' -d '{"on":true}'

Print events from the simulated printers appear below as "print event" lines.
EOT

cd "$CORE"
exec ./bin/demo -config "$TRY/config.yaml" -dir "$TRY/demo" -addr "127.0.0.1:$PORT" \
  -password "$PASS" -log-level info -print-gap 6s -printers 2 -flap 0
