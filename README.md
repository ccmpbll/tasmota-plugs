# tasmota-plugs

> An unofficial, community-made extension. It is not made, endorsed or supported
> by Prusa Research.

An extension for [Prusa Connect](https://github.com/prusa3d/prusa-connect) (the
self-hosted server) that is one Go process. It drives up to eight Tasmota
smart plugs over HTTP, one or more per printer, shows what each one is drawing,
and logs the print events of the printers the host lets it observe. It adds a
Tasmota Plugs page under Extensions in the app, where the plugs are also set up, and a
Tasmota line and tab for each printer that has a plug, and a dashboard card.

It can also switch a printer's plugs off by itself: after the printer has been
idle and cooled for a set time, or when a heater reads over a limit. Both are off
until you set them (see "Switching printers off on its own").

| route | does |
| --- | --- |
| `GET /plugs` | every plug, with its state and meter (`energy`, `signal_dbm`, `uptime_s`), and the price. A plug that does not answer is a row with `on: null` and an `error` |
| `GET /plugs/{name}` | one plug, the same way |
| `POST /plugs/{name}/power` | `{"on": true\|false, "force"?: true}` switches it, and succeeds only when the plug answers 200 with the state asked for. Turning off is refused with `409 PRINTER_BUSY` while the printer is busy, unless `force` |

All are under `/api/v1/modules/com.github.aux.ccmpbll.tasmota-plugs/`.

## In the app

**Set up plugs** is a button on the Tasmota Plugs page (and a link on the page of any
printer that has no plug yet). Each plug is a row: a name, an address, and a
dropdown of the printers Connect knows. **+ Add a plug** adds one at a time, up to
eight, and an optional electricity price per kWh turns the energy figures into
money. Save writes the settings through Connect's own API and the extension
restarts by itself; nothing on the command line. It is checked as you type (the
same rules the module holds the settings to), and Save stays off until it is
valid. Connect also has its generic form for the same settings at Settings →
Extensions → Tasmota Plugs.

**The Tasmota Plugs page** has a card per plug: name, host, linked printer, state (On,
Off, or Not answering with the reason) and one button: Turn on when it is off,
Turn off when it is on, both when it does not answer. Under it, what the plug's
meter reports (when it has one), as labelled figures: Power, Voltage and Current
now, and Today, Yesterday and Total energy in kWh (with what it cost under each,
when a price is set). It refreshes every five seconds.

**On each printer that has a plug** (linked by serial): a line on its card in
the Printers list, `Tasmota: On · 14 W` (or each plug's name and state, and the
total draw, when it has several), and a **Tasmota** tab on its page, in the tab
bar beside Now, Controls and Files, with the same cards and buttons. A printer
with no plug gets the tab too, with a link to set one up.

**A dashboard card**, *Tasmota Plugs*, can be added to any dashboard (Edit, Add a card): a row per plug with its state, what it is drawing and the same Turn on / Turn off button. Its one setting narrows it to the plugs of chosen printers; with none chosen it shows them all. The buttons are inert while the dashboard is being edited.

**Turning a printer off while it prints is guarded twice.** The app asks first when
the linked printer is printing, paused or waiting for a person ("Cutting its power
will ruin the print"). And the module itself refuses: `POST /plugs/{name}/power`
with `{"on": false}` answers `409 PRINTER_BUSY` in those states, whoever calls it,
unless the request carries `"force": true`, which the app sends only after you
confirm. Turning a plug on is never held up, nor is a plug with no printer linked,
nor a printer the server does not know (offline or frozen: the plug may be the only
way to restart it). If the module cannot read the printer's state, it does not
block the switch, and says so in its log. Whether an account may switch plugs or
change the setup is the host's call: a refusal shows its reason.

## Configuration

Everything is set from **Set up plugs** in the app. `options:` in `module.yaml`
names every setting the module reads, with its type, default and what it controls.
Nothing reads an environment variable, so that list is the whole of it. A key the
manifest does not name is refused before the process starts.

Up to eight plugs, each three keys, `plug1_*` to `plug8_*`:

| key | meaning |
| --- | --- |
| `plug<N>_host` | the Tasmota plug's host name or IPv4 address, no port. Empty leaves the slot unused |
| `plug<N>_name` | the name shown for it, such as `Core One`: up to 40 characters, without `/ ? # %`, and two names may not differ only by capitals. Empty names it `plug<N>` |
| `plug<N>_printer` | the serial of the printer it powers, as Connect shows it. Empty links no printer. Several plugs may share a printer |

| key | meaning |
| --- | --- |
| `price_per_kwh` | what a kilowatt-hour costs, such as `0.14`. Empty shows no cost |
| `currency` | what the price is written in, such as `$` or `EUR`. Default `$` |

### Switching printers off on its own

Two rules, both off until set, both for plugs linked to a printer. **Set up
plugs** has a section for them.

| key | meaning |
| --- | --- |
| `auto_off_minutes` | switch the plugs off once the printer has been idle this long (`IDLE`, `READY`, `FINISHED` or `STOPPED`) and has cooled, 1 to 1440. Empty is off |
| `auto_off_cooldown_c` | how cool the hotend and the bed must both be first, 20 to 100. Empty is 40 |
| `overheat_nozzle_c` | switch the plugs off, in any state, when a hotend reads above this, 250 to 500. Empty is off |
| `overheat_bed_c` | the same for the bed, 100 to 200. Empty is off |

The module looks at the printers every ten seconds, through the same device API
the printing guard uses, and reads the state and the temperatures
(`/tools/<n>/nozzle_temp`, `/temperature/bed`) from each printer's state. Notes
on what it does:

- A printer is switched off once for each time it goes idle. It goes offline with
  its power, and when it is switched on again it is a new idle streak, so it
  waits the full time again.
- The idle time is held in memory. Saving the setup restarts the module, so every
  printer's idle time starts again then, which delays a switch off and never
  brings one on early. Lowering the time does not switch off a printer that has
  been idle for longer already.
- A printer that reports no temperature is not switched off for being idle, since
  whether it has cooled cannot be told. The log says so once.
- A heater over its limit is switched off at once, even in the middle of a print.
  If the printer is still on and still hot a minute later it is tried again. The
  printer's own firmware stops a runaway heater first; this is a second layer,
  so set the limits above any filament you print. The ranges keep a typo from
  cutting a normal print.
- Nothing is switched off while the printers cannot be read, and the log says so
  once.
- A plug switched off this way says why on its card, until it is switched on
  again by anyone. Each switch off is also a line in the log.
- If an idle printer's plug cannot be reached, the module waits five minutes
  before it tries again, so a wrong address does not fill the log. Only the plugs
  that failed are tried again; the ones already off are left alone. An overheat
  cut is tried again, for the plugs that failed, at every look.
- The plugs of a printer are switched off at the same time, and every printer's
  overheat is dealt with before any idle switch off, so a plug that does not
  answer (eight seconds) does not hold back a safety cut.
- An offline printer is not judged on its last readings, which Connect keeps
  after the printer goes dark. After a printer comes back from a cut it is
  switched off again only if it reads over the limit on two looks in a row.
- An overheat limit that cannot see its heater (the printer reports no such
  temperature, or the server does not know the printer) is said once in the log,
  so a limit that never fires does not look like one that works.

The slots are fixed, not a list, because the manifest declares every host this
module connects to under `egress`, and one egress entry names one option and so
one host. A bad key, a slot outside 1 to 8, a plug with no host, a duplicate
name or a host that is not a name or an IPv4 address refuses the start with the
key named.

## Permissions

The module asks for `observe`, to read the print events and the serial of every printer. The
operator reads that before approving the install.
The routes are gated by the host: `view_resource` on the instance for a read,
`operate_resource` for a write.

The module also declares one egress entry per plug host (`plug1_host` to
`plug8_host`, port 80). The host does not stop a module reaching other
addresses, so the entries are what the operator reads, not a sandbox.

## Errors

When the module cannot do what a request asks, it answers with an error. The
error has a `code` and a `title` in English. Every code of this module starts
with `com.github.aux.ccmpbll.tasmota-plugs`.

A program that calls the module should check the `code`. The title is for
people. You can change the wording of a title without breaking a caller.

`faults.go` lists every code, with what went wrong and what the caller can do
about it. Add a new code in that file.

## Development

The module builds against a checkout of Prusa Connect's source, which is not
copied into this repository. Fetch it once, at the commit this was built against
(`.core-version`, the release `v0.1.0-beta.1`):

```bash
sh scripts/setup.sh
```

That needs `git`. `go test ./...` then works. The page is Svelte, built with
Connect's component library, and the build is in a container so nothing but
Docker and Go is needed:

```bash
sh scripts/ui-build.sh       # regenerates client/ from openapi.json, then builds ui/dist
```

The same steps run natively, where Node 22 and Go are present, as
`LOCKED=1 sh scripts/ui-steps.sh`: that is what CI runs. Run the build again after
any change under `ui/` or to `openapi.json`. Build against the pinned release: the
component library changes between releases (a button size that exists on `main`
is missing from the beta, for one), and the bundle may not carry its own copy of
Svelte's code, so `svelte/reactivity` is not used.

`scripts/simprinter` is a stand-in printer and `scripts/fake-tasmota.py` a stand-in
plug, for trying the pages without hardware.

| path | purpose |
| --- | --- |
| `module.yaml` | the manifest: version, settings, permissions, egress and the page |
| `main.go` | the process: the handshake, the lifecycle and the routes |
| `settings.go` | the eight plug slots and the automatic power-off limits: parsing, validation, and the plug-to-printer link by serial |
| `auto.go` | the watcher that switches printers off when idle and cooled, or overheating, and remembers why |
| `guard.go` | the printing guard on switching off |
| `tasmota.go` | the Tasmota HTTP client: host check, status and echo checks, no redirects, bounded answers |
| `follower.go` | subscribes to `device.job.started` and `device.job.settled` and logs each, resuming from the last cursor |
| `faults.go` | every code the module refuses with, each documented once |
| `*_test.go` | the client's refusals, the settings, the routes and their codes, the watcher, the follower, and the manifest parsed by the SDK |
| `ui/`, `client/` | the Tasmota Plugs page (Svelte) and the generated typed client for the routes |
| `openapi.json` | the fragment describing the routes, with every path relative to the mount |
| `CHANGELOG.md` | what changes between versions |

The extension's id is `com.github.aux.ccmpbll.tasmota-plugs`, derived by the host
from the Go module path `github.com/ccmpbll/tasmota-plugs`. Every address the module
answers on is built from it.

## License

MIT (see `LICENSE`). It is built against Prusa Connect's SDK, which Prusa publishes
under its own licence; Prusa's own example extension is MIT as well.
