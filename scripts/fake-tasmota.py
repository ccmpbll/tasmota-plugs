#!/usr/bin/env python3
"""A stand-in Tasmota plug for testing, on port 80 of this machine.

    python3 scripts/fake-tasmota.py [--port 80]

Answers GET /cm?cmnd=... the way the firmware does: Power, Power On and Power Off
(the relay), and Status 0 (state, a changing energy meter, Wi-Fi signal and
uptime). The relay lives in memory and the meter draws 15 W plus a little noise
while it is on and 0 W while it is off. Each command is printed.
Binding port 80 needs no sudo on current macOS; on Linux use sudo or setcap.
"""
import argparse
import json
import random
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

state = {"POWER": "OFF", "today": 0.131, "total": 17.666, "since": time.time()}


def status():
    on = state["POWER"] == "ON"
    watts = round(15 + random.uniform(-2, 2)) if on else 0
    if on:
        state["today"] += watts / 1000 / 3600 * 5
        state["total"] += watts / 1000 / 3600 * 5
    return {
        "Status": {"Module": 1, "DeviceName": "Fake plug", "Power": 1 if on else 0},
        "StatusNET": {"Hostname": "fake-plug"},
        "StatusSNS": {
            "Time": time.strftime("%Y-%m-%dT%H:%M:%S"),
            "ENERGY": {
                "TotalStartTime": "2026-06-27T00:12:27",
                "Total": round(state["total"], 3),
                "Yesterday": 2.94,
                "Today": round(state["today"], 3),
                "Power": watts,
                "ApparentPower": watts + 5 if on else 0,
                "ReactivePower": 12 if on else 0,
                "Factor": 0.78 if on else 0,
                "Voltage": 120,
                "Current": round(watts / 120, 3),
            },
        },
        "StatusSTS": {
            "Time": time.strftime("%Y-%m-%dT%H:%M:%S"),
            "UptimeSec": int(time.time() - state["since"]) + 3000000,
            "Wifi": {"RSSI": 100, "Signal": -44},
            "POWER1": state["POWER"],
        },
    }


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        url = urlparse(self.path)
        if url.path != "/cm":
            self.send_error(404)
            return
        command = parse_qs(url.query).get("cmnd", [""])[0]
        print(f"command: {command!r}", flush=True)
        word = command.strip().lower()
        if word == "status 0":
            self._send(status())
            return
        if word == "power on":
            state["POWER"] = "ON"
        elif word == "power off":
            state["POWER"] = "OFF"
        elif word not in ("power", "power1"):
            self._send({"Command": "Unknown"})
            return
        self._send({"POWER": state["POWER"]})

    def _send(self, body):
        raw = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=80)
    args = ap.parse_args()
    print(f"fake Tasmota on :{args.port}, relay {state['POWER']}", flush=True)
    HTTPServer(("0.0.0.0", args.port), Handler).serve_forever()
