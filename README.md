# Tasmota Plugs

> An unofficial, community-made extension for [Prusa Connect](https://github.com/prusa3d/prusa-connect).
> Not made, endorsed or supported by Prusa Research.

Control the Tasmota smart plugs that power your printers, from Prusa Connect.

## What it does

- **Switch plugs on and off** from a Tasmota Plugs page, from a Tasmota tab on each printer, or from a dashboard card.
- **Show energy use** for plugs with a meter: power, voltage and current now, and energy today, yesterday and in total, with a cost if you set a price per kWh.
- **Protect prints.** Turning off the plug of a printer that is printing or paused asks first, and the extension itself refuses unless you confirm.
- **Switch printers off by itself**, if you ask it to:
  - after the printer has been idle for a set time and has cooled down, and
  - at once when the hotend or bed reads over a limit you set.

Both automatic rules are off until you set them. A plug switched off this way says why on its card.

## Setup

Open the Tasmota Plugs page and choose **Set up plugs**. Give each plug a name and an address, and pick the printer it powers (as many plugs as you like, and several can share a printer). Saving restarts the extension for a few seconds.

| setting | meaning |
| --- | --- |
| Switch off after idle (minutes) | 1 to 1440. Empty is off |
| Cooled down below (°C) | how cool the hotend and bed must be first, 20 to 100. Default 40 |
| Overheat limit, hotend (°C) | 250 to 500. Empty is off |
| Overheat limit, bed (°C) | 100 to 200. Empty is off |

The overheat limits are a second layer: the printer's own firmware stops a runaway heater first. Set them above any filament you print.

## Permissions

It asks for `observe`, to read printer states and serials, and connects only to the plug addresses you enter, on port 80. The plugs are kept in one setting, `plugs`, whose value is text that holds a JSON list, such as `'[{"name": "Core One", "host": "192.168.1.30", "printer": "CZPX1234"}]'`. The quotes matter: it is a string, not a list of the configuration file's own. The Set up plugs page writes it; you only edit it by hand in the server's configuration file.

## License

MIT, see `LICENSE`. It is built against Prusa Connect's SDK, which Prusa publishes under its own licence.
