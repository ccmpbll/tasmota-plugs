# Roadmap

Ideas for later versions, and things not yet checked on real hardware. Nothing here is promised. Shipped changes are in `CHANGELOG.md`.

## Ideas

1. **Tell other extensions when it cuts power.** The module could publish an event whenever the idle or overheat rule switches a plug off, carrying the printer, the plug, the reason and the detail. A notification extension (such as a Pushover one) could then alert on an overheat cut.
   - Needs an `events` entry in `module.yaml` and a call to `EmitRaw`. The payload is limited to 8 KiB, which is plenty.
   - Not yet checked: whether another module may subscribe to a type published under this module's own prefix. Check against a running host before building the sender.
2. **Leave a plug out of auto-off.** A per-plug switch, so a light or a fan sharing a printer's plug is never cut by the idle rule.
3. **Pause auto-off for maintenance.** A printer being serviced sits idle and cool, so the idle rule would switch it off. A per-plug or global "pause until I turn it back on" would stop that.
4. **Power on before a print arrives.** PrintSpy switched the printer on when a print was sent from the slicer. Not checked: whether Connect has a hook for a print queued to a printer that is off.
5. **Energy per print.** Read the plug's total kWh at `device.job.started` and `device.job.settled` and show the difference, with a cost when a price is set. The follower already receives both events.
6. **MQTT mode for plugs.** PrintSpy offered it as an opt-in. Low value while HTTP works.

## Not yet verified

- Both temperature paths (hotend and bed) on real printers. The idle rule has run on both printers; the overheat rule has never fired for real.
- Whether a printer that is receiving an upload reports an idle state. If it does, the idle rule could cut power in the middle of an upload.
- The kit on the Gandalf host. It still runs the old `prusa-connect-tasmota` install. Moving to this one means running the kit and entering the plugs and limits again.

## Housekeeping

- Some internal names still say `smart-plugs`: the `name:` in `module.yaml`, the binary names, the locale keys and the generated client. Renaming them changes the declaration key on an installed host, so do it with a version bump.
- `scripts/install-on-host.sh` passes plugs as `name=host=serial` and does not set the automatic limits.
- Several scripts have no documentation: `install.sh`, `make-kit.sh`, `bundle.sh`, `release.sh`, `demo.sh`, `fake-tasmota.py` and `simprinter/`. Document or delete them.
