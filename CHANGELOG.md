## 0.1.0 - unreleased

### Added

- `GET /plugs` lists every configured plug and whether it is on, in slot order. A plug that does not answer is a row with `on` null and an `error`, so one dead plug does not hide the others.
- `GET /plugs/{name}` reads one plug.
- `POST /plugs/{name}/power` switches one plug. It succeeds only when the plug answers 200 with the state asked for, and refuses with `PLUG_UNREACHABLE` otherwise. An unknown name is refused with `PLUG_NOT_FOUND`.
- Up to eight plugs are set with `plug1_name`, `plug1_host` and `plug1_printer` through `plug8_*`. A bad key, a slot past 8, a plug with no host, a duplicate name or a host that is not a name or an IPv4 address refuses the start and names the key.
- `plug<N>_printer` links a plug to a printer by serial. The module logs each `device.job.started` and `device.job.settled` event with the names of that printer's plugs. It switches nothing in response yet.
- Every plug host is declared as a connection on port 80, one `plug<N>_host` entry each.
- The app has a Tasmota Plugs page under Extensions. Each plug shows its name, host, linked printer and state, with Turn on or Turn off, and the list refreshes every five seconds. Turning off the plug of a printer that is printing or paused asks first.
- Each printer with a linked plug shows `Power: On` or `Off` on its card in the Printers list, and a Power section with the same Turn on and Turn off buttons at the foot of its Now tab.
- The Tasmota Plugs page has a Set up plugs button: one row per plug (name, address, and a dropdown of the printers Connect knows), `+ Add a plug` to add them one at a time, and an optional price per kWh. Save applies it without a command line and the extension restarts by itself. A printer with no plug offers a link that opens the editor with that printer chosen.
- Plugs that have an energy meter show their power draw, volts and amps, today's, yesterday's and the total energy, and the Wi-Fi signal, on the Tasmota Plugs page. A printer's card and Power section show its draw. `GET /plugs` and `GET /plugs/{name}` carry `energy`, `signal_dbm` and `uptime_s`, read with one request per plug.
- The settings `price_per_kwh` and `currency` show what the energy cost. An empty price shows no cost, and a price outside 0 to 1000 refuses the start.
- A reinstall with no plug arguments keeps the plugs saved in the app.
- `POST /plugs/{name}/power` refuses to turn a plug off with `409 PRINTER_BUSY` while the printer it powers is printing, paused or waiting for a person, unless the request carries `"force": true`. The app sends that only after you confirm. Turning on, a plug with no printer, and a printer the server does not know are never held up.
- A printer's page has a **Tasmota** tab in its tab bar (instead of a section at the foot of the Now tab), and its card in the Printers list reads `Tasmota: On · 14 W`.
- The Tasmota Plugs page shows the meter as labelled figures (Power, Voltage, Current, Today, Yesterday, Total), without the Wi-Fi signal.
- A dashboard card, Tasmota plugs: a row per plug with its state, its draw and a Turn on or Turn off button (with the same confirmation before cutting the power of a printer that is printing). Its setting narrows it to the plugs of chosen printers.
- The sidebar item is called Tasmota Plugs. Saving the plug setup no longer looks like a failure: the extension restarts for a few seconds, the page keeps the plugs it had and says it is applying the changes, and an error appears only if reads keep failing.
- Connect itself shows "This server has no extension page at this address" for about three seconds while the extension restarts after a save, and then reopens the page. The extension cannot prevent that. The page now remembers the save across it and, when it comes back, shows the saved plugs without an error.
