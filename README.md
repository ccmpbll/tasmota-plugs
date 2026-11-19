# prusa-connect-tasmota

> An unofficial, community-made extension. It is not made, endorsed or supported
> by Prusa Research.

An extension for [Prusa Connect](https://github.com/prusa3d/prusa-connect) (the
self-hosted server) that is one Go process. It drives up to eight Tasmota
smart plugs over HTTP, one or more per printer, shows what each one is drawing,
and logs the print events of the printers the host lets it observe. It adds a
Plugs page under Extensions in the app, where the plugs are also set up, and a
Power line and section for each printer that has a plug. `make new-module` wrote the skeleton.

Today it switches nothing by itself: each print event is only logged, with the
plugs linked to its printer. Auto-off after a print and a thermal check are the
obvious next steps.

| route | does |
| --- | --- |
| `GET /plugs` | every plug, with its state and meter (`energy`, `signal_dbm`, `uptime_s`), and the price. A plug that does not answer is a row with `on: null` and an `error` |
| `GET /plugs/{name}` | one plug, the same way |
| `POST /plugs/{name}/power` | `{"on": true\|false}` switches it, and succeeds only when the plug answers 200 with the state asked for |

All are under `/api/v1/modules/com.github.aux.ccmpbll.prusa-connect-tasmota/`.

## In the app

**Set up plugs** is a button on the Plugs page (and a link on the page of any
printer that has no plug yet). Each plug is a row: a name, an address, and a
dropdown of the printers Connect knows. **+ Add a plug** adds one at a time, up to
eight, and an optional electricity price per kWh turns the energy figures into
money. Save writes the settings through Connect's own API and the extension
restarts by itself; nothing on the command line. It is checked as you type (the
same rules the module holds the settings to), and Save stays off until it is
valid. Connect also has its generic form for the same settings at Settings →
Extensions → Smart plugs.

**The Plugs page** has a card per plug: name, host, linked printer, state (On,
Off, or Not answering with the reason) and one button: Turn on when it is off,
Turn off when it is on, both when it does not answer. Under it, what the plug's
meter reports (when it has one): power draw now in watts, volts and amps, energy
today, yesterday and in total (with the cost when a price is set), and the Wi-Fi
signal. It refreshes every five seconds.

**On each printer that has a plug** (linked by serial): a line on its card in
the Printers list, `Power: On · 14 W` (or each plug's name and state, and the
total draw, when it has several), and a Power section at the foot of its Now tab
with the same buttons. A printer with no plug shows neither, and offers a link to
set one up once any plug exists.

When the linked printer is printing or paused, Turn off asks first, since cutting
its power ruins the print. Whether an account may switch plugs or change the setup
is the host's call: a refusal shows its reason.

The page is Svelte, built with Connect's component library in a Node container,
so nothing is installed on your machine:

```bash
sh scripts/ui-build.sh      # needs docker and go; writes ui/dist
```

It regenerates `client/` (a typed client for the routes, from `openapi.json`),
builds the two library packages from `.core`, formats and lints `ui/`, type-checks
and builds the bundle. Run it again after any change under `ui/` or to
`openapi.json`, then rebuild the kit. The bundle may not carry its own copy of
Svelte's code, so `svelte/reactivity` is not used: the host shares only `svelte`.

Its namespace is `com.github.aux.ccmpbll.prusa-connect-tasmota`, derived by the host from the repository
address `github.com/ccmpbll/prusa-connect-tasmota`. You never write the namespace: every address the module
answers on is built from it.

| path | purpose |
| --- | --- |
| `module.yaml` | the manifest: the kind and the version |
| `install.sh` | build, install and see it. The one command below |
| `CHANGELOG.md` | what an operator sees change when they upgrade this module |
| `.vscode/extensions.json` | the editor extensions for the languages in here |
| `main.go` | the process: the handshake, the lifecycle set and the routes |
| `settings.go` | the eight plug slots: parsing, validation, and the plug-to-printer link by serial |
| `tasmota.go` | the Tasmota HTTP client: host check, status and echo checks, no redirects, bounded answers |
| `follower.go` | subscribes to `device.job.started` and `device.job.settled` and logs each, resuming from the last cursor |
| `faults.go` | every code this module refuses with, each documented once |
| `*_test.go` | the client's refusals, the settings, the routes and their codes, the follower's resume and printer match, and the manifest parsed by the SDK |
| `ui/`, `client/` | the Plugs page (Svelte) and the generated typed client for the routes |
| `.github/workflows/` | `ci.yml` (Go checks and the browser build) and `release.yml` (publishes to the marketplace when a GitHub release is published) |
| `scripts/` | `ui-steps.sh` (the browser build, native), `simprinter/` (a stand-in printer for testing the printer pages), `ui-build.sh` (the page, in a container), `make-kit.sh` and `install-on-host.sh` (install into a Docker host), `bundle.sh` and `release.sh` (builds), `demo.sh` and `fake-tasmota.py` (try it locally) |
| `openapi.json` | the fragment describing the routes, with every path relative to the mount |

## Building

The module builds against a checkout of Prusa Connect's source, which is not
copied into this repository. Fetch it once, at the commit this was built against
(`.core-version`):

```bash
sh scripts/setup.sh
```

That needs `git`. `go test ./...` then works. The browser half is built in a
container, so it needs Docker and nothing else installed (`sh scripts/ui-build.sh`).
The same steps run natively, where Node 22 and Go are already present, as
`LOCKED=1 sh scripts/ui-steps.sh`: that is what CI and the marketplace builder run.

The SDK checkout is the Prusa Connect release `v0.1.0-beta.1` (`.core-version` is
its commit). The component library changes between releases (a button size that
exists on `main` is missing from the beta, for one), so build against that
release, the one a server running it will load the bundle into.

## Installation

```bash
sh install.sh
```

That builds this platform's binary, and installs the release with
`prusa-connect module add` into the demo's data, under `bin/smart-plugs-demo`
in the checkout. `TARGET=app sh install.sh` installs it into the real app's
data, `bin/smart-plugs-app`, instead. What the tools print goes to `install.log`
there, and a good run prints one line saying where the module is installed.

To see it, run this in the checkout. `-dir` is this folder, `../smart-plugs` when
it sits beside the checkout:

```bash
go run ./cmd/new-module -start demo -dir ../smart-plugs
```

It starts the demo on a free port with this module installed. It prints the address to open and the
account to sign in with. `-start app` starts the real app instead, which asks
for an account on its first run. Ctrl+C stops everything it started.

Paste what the command printed on your machine under this paragraph. A README
that quotes real output is one a reader can check; one that describes the output
is one they have to trust.

Run `sh install.sh` again after a change to `module.yaml` or to the process.
`module add` records the bytes it installed and the host serves nothing it
cannot verify against that record, so a rebuilt release is a new install. A
core that is already running read its declarations when it started, so it is
restarted too.

## Configuration

`options:` in `module.yaml` names every setting this module reads, each with its
type, its default and one sentence saying what it controls. Nothing here reads
an environment variable, so that list is the whole of what an operator can set.

The host applies the defaults, writes the instance's `config:` block over them
key by key, and sends the result on `plugin.configure` before the first request
arrives. A key the manifest does not name is refused before the process starts.

These are what the in-app setup edits. Up to eight plugs, each three keys,
`plug1_*` to `plug8_*`:

| key | meaning |
| --- | --- |
| `plug<N>_host` | the Tasmota plug's host name or IPv4 address, no port. Empty leaves the slot unused |
| `plug<N>_name` | the name its routes use: lower-case letters, digits and hyphens, at most 32. Empty names it `plug<N>` |
| `plug<N>_printer` | the serial of the printer it powers, as Connect shows it. Empty links no printer. Several plugs may share a printer |

```yaml
resources:
  sink:
    smart-plugs:
      config:
        plug1_name: core-one
        plug1_host: 192.168.1.30
        plug1_printer: CZPX1234X009XC12345
        plug2_name: mk4s
        plug2_host: 192.168.1.31
        plug2_printer: CZPX5678X009XC67890
```

| key | meaning |
| --- | --- |
| `price_per_kwh` | what a kilowatt-hour costs, such as `0.14`. Empty shows no cost |
| `currency` | what the price is written in, such as `$` or `EUR`. Default `$` |

The slots are fixed, not a list, because the manifest declares every host this
module connects to under `egress`, and one egress entry names one option and so
one host. A bad key, a slot outside 1 to 8, a plug with no host, a duplicate
name or a host that is not a name or an IPv4 address refuses the start with the
key named.

## Permissions

The module asks for `observe`, to read the print events and the serial of every printer. The
operator reads that before `module add -approve` writes it into the declaration.
The routes are gated by the host: `view_resource` on the instance for a read,
`operate_resource` for a write.

The module also declares one egress entry per plug host (`plug1_host` to
`plug8_host`, port 80). The host does not stop a module reaching other
addresses, so the entries are what the operator reads, not a sandbox.

## Errors

When the module cannot do what a request asks, it answers with an error. The
error has a `code` and a `title` in English. Every code of this module starts
with `com.github.aux.ccmpbll.prusa-connect-tasmota`.

A program that calls the module should check the `code`. The title is for
people. You can change the wording of a title without breaking a caller.

`faults.go` lists every code, with what went wrong and what the caller can do
about it. Add a new code in that file.

## What the pieces are

The release is `dist`: `module.yaml` at its root and the binary
`smart-plugs_<GOOS>_<GOARCH>` beside it. `-from dist` is what the install
reads. A `checksums.txt` is optional and verified when it is there; a release
you just built has nothing to prove against.

```bash
prusa-connect module add github.com/ccmpbll/prusa-connect-tasmota@0.1.0 -from dist
prusa-connect module list
prusa-connect module remove github.com/ccmpbll/prusa-connect-tasmota
```

Without `-approve` the declaration is written switched off, and the command
prints what the module asked for so an operator can read it first.

## Troubleshooting

| what you see | what it means |
| --- | --- |
| `module add` refuses the release | `dist` is not the release layout. `module.yaml` sits at its root |
| the module is listed and switched off | `module add` ran without `-approve`. Read what it printed, then switch the instance on |
| a response carries a code and an English sentence | the process refused the request. Look the code up in `faults.go` |
| a change does not show | the install was not run again. Every change is a new install |

## Installing it on a Docker deployment (for example Unraid)

Build a kit on your own machine, copy it to the Docker host, and run the script
there. The script needs only `sh`, `docker` and coreutils.

```bash
sh scripts/ui-build.sh                       # the Plugs page, built in a Node container (once, and after UI changes)
sh scripts/make-kit.sh                       # kit/: install.sh, the linux_amd64 build and SHA256SUMS
scp -r kit root@<host>:/tmp/
```

On the host:

```bash
sh /tmp/kit/install.sh prusa-connect
```

Then open the Plugs page and use **Set up plugs**. To set the plugs from the
command line instead, name them (and the printer serial each one powers):

```bash
sh /tmp/kit/install.sh prusa-connect \
    core-one=192.168.1.30=CZPX1234X009XC12345 \
    mk4s=192.168.1.31=CZPX5678X009XC67890
```

With no plugs the install **keeps whatever you set up in the app**, so that is how
to upgrade. With plugs it replaces the plug settings with exactly those.

`-n` shows what it would do without changing anything, `-y` skips the restart
question, and `-c <path>` names the container's config file if it is not
`/config/config.yaml`.

It checks the copy against `SHA256SUMS`, copies the build into the container's
`/data`, runs `module add` there as the image's own user, removes the copies,
restarts the container and prints what the module logged. Tested against the
published `0.1.0-beta.1` image with a read-only root, no capabilities and the
documented volumes. The install lands on the `/config/config.d` volume, so it
survives restarts and image upgrades. A bind-mounted `/config/config.d` must be
owned by uid 10001.

## Publishing to the marketplace

Prusa's [Plugin Marketplace](https://plugin-marketplace.prusa3d.com) (in beta) lists
Connect extensions after a review by Prusa. Publishing is by a verified build: a
GitHub workflow of Prusa's builds this commit, signs the release with your key and
submits it. One-time setup:

1. **Make this repository public.** The builder records a GitHub build provenance
   attestation, which GitHub only does for public repositories.
2. **Become an author.** Sign in to the marketplace with a Prusa Account and set up
   a short author profile.
3. **Signing key.** Create an RSA key pair and register the *public* key in the
   author console under *Signing keys*:

   ```bash
   openssl genpkey -algorithm RSA -out private_key.pem -pkeyopt rsa_keygen_bits:2048
   openssl rsa -pubout -in private_key.pem -out public_key.pem
   ```

   Keep the private key out of the repository and back it up (the marketplace holds
   only the public half). Store it as a secret of a GitHub environment named
   `release`, ideally with a required reviewer:

   ```bash
   gh secret set PRUSA_CONNECT_EXTENSION_SIGNING_KEY --env release < private_key.pem
   ```
4. **Trusted publishing.** In the author console, add the extension id
   `com.github.aux.ccmpbll.prusa-connect-tasmota`, the repository
   `ccmpbll/prusa-connect-tasmota`, the workflow file `release.yml` and the
   environment `release`. There is no marketplace password or API key to store.
5. **Release.** Set `version` in `module.yaml` (and the changelog), then publish a
   GitHub release whose tag ends with it, for example `v0.1.0`. A pre-release goes to
   the beta channel, and the release notes become the changelog:

   ```bash
   gh release create v0.1.0 --prerelease --title v0.1.0 --notes-file CHANGELOG.md
   ```

`release.yml` builds every platform in `module.yaml`, builds the browser half with
`scripts/ui-steps.sh`, signs and uploads. The marketplace checks the manifest (a
`license` is required, and the `egress` entries are compared with the code), then a
person at Prusa reviews the release before it is public. Server operators see
community extensions only after turning on *Show community extensions* in their
marketplace settings.

## License

MIT (see `LICENSE`). It is built against Prusa Connect's SDK, which Prusa publishes
under its own licence; Prusa's own example extension is MIT as well.
