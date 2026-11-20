## 0.1.1 - 2026-11-20

### Fixed

- Switching a plug now asks the host whether the caller may operate this extension (`operate_resource`), and refuses with `403 ACCESS_DENIED` when not. A viewer can no longer switch a plug, with or without `force`. A host that cannot answer refuses with `503 ACCESS_UNKNOWN`. The question is declared under `checks:` in `module.yaml`.
- `GET /plugs` carries `can_operate`, and the Tasmota Plugs page, the printer tab and the dashboard card show no Turn on or Turn off buttons to a person who may not operate. The `403` and `503` answers are in `openapi.json`.

## 0.1.0 - 2026-11-19

First release. Switches and monitors Tasmota smart plugs from Prusa Connect.

- Up to eight plugs, each linked to a printer by serial, set up in the app on the Tasmota Plugs page.
- Turn a plug on or off from the Tasmota Plugs page, a printer's Tasmota tab or a dashboard card, with a confirmation before cutting the power of a printer that is printing.
- Shows what each plug's energy meter reports (power, volts, amps, kWh today, yesterday and total) and, with a price per kWh, the cost.
- Optionally switches a printer's plugs off when it has been idle and cooled down, or when a heater reads over a limit.
- Declares every plug host as a connection on port 80 and sends only Tasmota power commands.
