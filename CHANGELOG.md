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
- A dashboard card, Tasmota Plugs: a row per plug with its state, its draw and a Turn on or Turn off button (with the same confirmation before cutting the power of a printer that is printing). Its setting narrows it to the plugs of chosen printers.
- The sidebar item is called Tasmota Plugs. Saving the plug setup no longer looks like a failure: the extension restarts for a few seconds, the page keeps the plugs it had and says it is applying the changes, and an error appears only if reads keep failing.
- Connect itself shows "This server has no extension page at this address" for about three seconds while the extension restarts after a save, and then reopens the page. The extension cannot prevent that. The page now remembers the save across it and, when it comes back, shows the saved plugs without an error.
- Saving the plug setup covers the app with an "Applying your changes…" notice until the extension is back, so Connect's own "no extension page" message during the restart is not seen.
- A plug's name is a display name: up to 40 characters of any text, such as `Core One` or `Büro (MK4S)`, without `/ ? # %`, not only dots, and with no space at either end. Two names may not differ only by capitals. It was lower-case letters, digits and hyphens.
- If the extension does not come back with the saved plugs, the page says so after about twenty seconds. A slow plug no longer has a new read started behind each unfinished one.
- Switching printers off on their own, for the plugs linked to a printer, set in **Set up plugs** and off until set. A printer idle for the set number of minutes and cooled below a set temperature (40 °C unless set) has its plugs switched off, once for each time it goes idle. A hotend or bed over its overheat limit has its plugs switched off at once, in any state, and again after a minute if the printer is still on and hot. A plug switched off this way says why on its card. Nothing is switched off while the printers cannot be read. The idle time is kept in memory, so saving the setup starts it again.
- The reason a plug was switched off automatically ends when the plug is switched on again, by anyone. An idle printer whose plug cannot be reached is tried again after five minutes, not at every look.
- Several plugs on one printer: an idle cut that reached only some of them is retried, after five minutes, for the rest, and an overheat cut at every look. The plugs are switched off at the same time, and every overheat is handled before any idle cut.
- An offline printer's last readings no longer count as an overheat, and a hot reading right after a printer comes back must be seen on two looks in a row before it is cut again.
- An overheat limit with no heater reading to watch is said once in the log.
- A plug read that began before an automatic cut no longer erases the reason for it.
