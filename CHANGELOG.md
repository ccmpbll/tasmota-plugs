## 0.2.0 - 2026-11-21

### Changed

- There is no limit on how many plugs you can set up. The plugs are now one setting, `plugs`, a JSON list of `{"name", "host", "printer"}`, in place of the `plug1_name` to `plug8_printer` settings. The Set up plugs page writes it, and a name that is left empty is `plug1`, `plug2` and so on by its place in the list. `install.sh` writes it from its `name=host=serial` arguments, and the egress is one entry for the `plugs` setting.
- **Upgrading from 0.1:** the old per-plug settings are no longer accepted. Set the plugs up again on the Tasmota Plugs page, or give them to `install.sh` again. Until you do, no plug is linked to a printer, so the automatic power-off and overheat limits you set have nothing to act on. The module says so in its log.
- Reading the plugs talks to at most 32 at a time and gives up after 20 seconds, so a long list does not open hundreds of connections at once. A plug that did not get its turn shows an error, and is read at the next refresh.

## 0.1.2 - 2026-11-20

Replaces 0.1.1, which the marketplace refused before publishing because `module.yaml` held an invalid YAML escape.

### Fixed

- Switching a plug now asks the host whether the caller may operate this extension (`operate_resource`), and refuses with `403 ACCESS_DENIED` when not. A viewer can no longer switch a plug, with or without `force`, and a plug linked to a printer also needs permission to operate that printer (`operate`). A host that cannot answer refuses with `503 ACCESS_UNKNOWN`. The question is declared under `checks:` in `module.yaml`. **Upgrading:** approve the new `operate_resource` and `operate` checks when you update (`module add ... -approve`, or an administrator approving it when the install lists it as a request). Without it every switch is refused with `503 ACCESS_UNKNOWN`, and the log says why.
- `GET /plugs` carries `can_operate`, and the Tasmota Plugs page, the printer tab and the dashboard card show no Turn on or Turn off buttons to a person who may not operate. The `403` and `503` answers are in `openapi.json`.

## 0.1.0 - 2026-11-19

First release. Switches and monitors Tasmota smart plugs from Prusa Connect.

- Up to eight plugs, each linked to a printer by serial, set up in the app on the Tasmota Plugs page.
- Turn a plug on or off from the Tasmota Plugs page, a printer's Tasmota tab or a dashboard card, with a confirmation before cutting the power of a printer that is printing.
- Shows what each plug's energy meter reports (power, volts, amps, kWh today, yesterday and total) and, with a price per kWh, the cost.
- Optionally switches a printer's plugs off when it has been idle and cooled down, or when a heater reads over a limit.
- Declares every plug host as a connection on port 80. It sends `Status 0` (a read-only report) and `Power On` / `Power Off`, nothing else.
