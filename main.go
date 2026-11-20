// Command smart-plugs is a Prusa Connect module that is this process and
// nothing else. It drives Tasmota smart plugs, one or more per printer, and
// follows the print events of the printers the host lets it observe.
//
// # The routes
//
//	GET  /plugs                 every configured plug and whether it is on
//	GET  /plugs/{name}          one plug
//	POST /plugs/{name}/power    {"on": true|false} switches it and reports what the plug says
//
// The paths are relative to the mount, so a caller reaches them under
// /api/v1/modules/com.github.aux.ccmpbll.tasmota-plugs/. The host decides
// who may call them: view_resource on the instance for a read, operate_resource
// for a write. This process checks no permission and holds no grant.
//
// # Settings
//
// Up to eight plugs, each three keys: plug<N>_name, plug<N>_host and
// plug<N>_printer (the serial of the printer it powers). settings.go reads them,
// module.yaml declares them, and declares each host as the egress this module
// makes, on port 80. Nothing here reads an environment variable.
//
// # Errors
//
// faults.go lists every error code of this module.
//
// # Installing it
//
//	sh install.sh
//
// README.md says what that runs and why every change is a new install.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// What this child reports when the host asks it what it is. The name is the
// manifest's, and the host holds the two to each other.
const (
	name    = "smart-plugs"
	release = extension.Version("0.1.0")
)

// held is the settings this module is running on. It is an atomic value because
// plugin.configure is answered on the wire goroutine and the routes are served
// on others.
//
//nolint:gochecknoglobals // the one piece of state this skeleton has.
var held atomic.Pointer[Settings]

// settings is what this module is running on: what the host sent, or the
// defaults this code holds when the host sent nothing.
func settings() Settings {
	if current := held.Load(); current != nil {
		return *current
	}

	return Settings{}
}

// openAPI is the fragment describing the route, with every path relative to
// the mount. The host holds it to routesapi.ParseFragment at launch and serves
// it rebased under the namespace, which is what cmd/generate-client-sdk reads
// to type this module's surface.
//
//go:embed openapi.json
var openAPI []byte

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

// run writes the handshake line and then serves frames until the host closes
// the channel.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	line, err := wire.Handshake{
		Protocol: wire.ProtocolMajor,
		MinMinor: wire.MinMinor,
		MaxMinor: wire.MaxMinor,
		Address:  wire.StdioAddress,
	}.Line()
	if err != nil {
		return fmt.Errorf("render the handshake: %w", err)
	}

	if _, err = os.Stdout.WriteString(line); err != nil {
		return fmt.Errorf("write the handshake: %w", err)
	}

	serves := wirerpc.Methods{}

	conn, err := wirerpc.NewConn(stdio{}, wirerpc.Options{
		Role:    wirerpc.RoleChild,
		Methods: serves,
	})
	if err != nil {
		return fmt.Errorf("build the session: %w", err)
	}

	maps.Copy(serves, methods())

	// The pid is here because the host's log is the only place a person sees
	// this process. Nothing on the wire carries it.
	fmt.Fprintf(os.Stderr, "%s %s is serving on stdio as pid %d\n", name, release, os.Getpid())

	devices := deviceapi.NewClient(conn)
	snapshots := readSnapshots(devices)
	printerState = stateReader(snapshots)
	auto = newWatcher(snapshots)

	events := &follower{
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		subscribe: func(ctx context.Context, p busapi.SubscribeParams, on func(busapi.Window) error) error {
			return busapi.Subscribe(ctx, conn, p, on)
		},
		serialOf: func(ctx context.Context, id string) (string, error) {
			device, err := devices.Get(ctx, deviceapi.GetParams{ID: id})

			return device.Serial, err
		},
	}

	go events.Run(ctx)
	go auto.Run(ctx)

	if err = conn.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// methods is the lifecycle set every child answers, plus the one call that
// carries this module's own HTTP surface.
func methods() wirerpc.Methods {
	return wirerpc.Methods{
		wirerpc.MethodInfo: func(context.Context, wirerpc.Call) (any, error) {
			return routesapi.Describe(
				wirerpc.Info{
					Name:     name,
					Version:  release,
					Contract: wirerpc.Contract,
					// Naming the call is what asks the host for the settings. A
					// module that names none is never configured.
					Serves: []wirerpc.Method{wirerpc.MethodConfigure},
				}, openAPI)
		},
		// The settings the host merged from module.yaml's defaults and this
		// instance's config block, sent once before the first request.
		wirerpc.MethodConfigure: wirerpc.Configure(func(_ context.Context, sent Settings) error {
			if err := sent.validate(); err != nil {
				return err
			}

			held.Store(&sent)

			return nil
		}),
		wirerpc.MethodShutdown: func(context.Context, wirerpc.Call) (any, error) {
			fmt.Fprintf(os.Stderr, "%s was asked to stop\n", name)

			return nil, nil
		},
		routesapi.MethodServe: routesapi.Serve(routes()),
	}
}

// plugAt builds the plug at a host. It is a variable so a test can point the
// routes at fake plugs.
//
//nolint:gochecknoglobals // a test seam.
var plugAt = newPlug

// routes is the surface this module serves, written as any Go program writes
// one. The paths carry no prefix: /plugs here is
// /api/v1/modules/com.github.aux.ccmpbll.tasmota-plugs/plugs to a caller.
func routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /plugs", func(w http.ResponseWriter, r *http.Request) {
		current := settings()

		answer(w, http.StatusOK, map[string]any{
			"items":         readAll(r.Context(), current.Plugs),
			"price_per_kwh": current.PricePerKWH,
			"currency":      current.Currency,
		})
	})

	mux.HandleFunc("GET /plugs/{name}", func(w http.ResponseWriter, r *http.Request) {
		config, p, ok := resolve(w, r)
		if !ok {
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		mark := auto.mark()

		reading, err := p.Read(ctx)
		if err != nil {
			refuse(w, r, unreachable(config, "the plug did not answer", err))

			return
		}

		answer(w, http.StatusOK, viewOf(config, reading, mark))
	})

	mux.HandleFunc("POST /plugs/{name}/power", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On *bool `json:"on"`
			// Force switches a plug off although its printer is busy. The app sends
			// it after a person has confirmed, and nothing else should.
			Force bool `json:"force"`
		}

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&body); err != nil || body.On == nil {
			refuse(w, r, InvalidRequest.New(`the body must be {"on": true} or {"on": false}`))

			return
		}

		config, p, ok := resolve(w, r)
		if !ok {
			return
		}

		// Cutting the power of a printer that is printing ruins the print, so a
		// switch off is refused unless the request says it is meant. Switching on,
		// and a plug that powers no printer, are never held up.
		if !*body.On && !body.Force && config.Printer != "" {
			if busy, state := printerBusy(r.Context(), config.Printer); busy {
				refuse(w, r, PrinterBusy.New("the printer is "+state+", so its plug is not switched off").With(
					fault.StringParam("name", config.Name),
					fault.StringParam("printer", config.Printer),
					fault.StringParam("state", state),
				))

				return
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		if err := p.Set(ctx, *body.On); err != nil {
			refuse(w, r, unreachable(config, "the plug did not switch", err))

			return
		}

		// A person has switched it now, so what the module did to it before is no
		// longer why it is as it is.
		auto.forget(config.Name)

		answer(w, http.StatusOK, plugView{Name: config.Name, Host: config.Host, Printer: config.Printer, On: body.On})
	})

	// Everything else is this module's own refusal. It carries the code from
	// faults.go and names the path, which is what proves the group prefix was
	// taken off before the request arrived.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, r, RouteNotFound.
			New("this module serves no route at "+r.URL.Path).
			With(fault.StringParam("path", r.URL.Path)))
	})

	return mux
}

// plugView is one plug as a caller sees it. On is null when the plug could not
// be read, and Error says why.
type plugView struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Printer string `json:"printer,omitempty"`
	On      *bool  `json:"on"`
	Error   string `json:"error,omitempty"`
	// Energy is the plug's meter, when it has one.
	Energy        *Energy `json:"energy,omitempty"`
	SignalDBm     *int    `json:"signal_dbm,omitempty"`
	UptimeSeconds *int64  `json:"uptime_s,omitempty"`
	// AutoOff is why the module itself last switched this plug off, shown while
	// it is still off so an outage is explained.
	AutoOff *autoEvent `json:"auto_off,omitempty"`
}

// viewOf is a plug and what it just said. mark is the watcher's mark from before
// the plug was asked.
func viewOf(config plugConfig, r Reading, mark uint64) plugView {
	view := plugView{
		Name: config.Name, Host: config.Host, Printer: config.Printer, On: &r.On,
		Energy: r.Energy, SignalDBm: r.SignalDBm, UptimeSeconds: r.UptimeSeconds,
	}

	// Why it was switched off is shown only while it stays off. A plug seen on
	// has been switched back on, by a person or at the plug, and its reason ends.
	if r.On {
		auto.forgetSeen(config.Name, mark)
	} else {
		view.AutoOff = auto.last(config.Name)
	}

	return view
}

// readAll asks every plug for its state at once. A plug that does not answer
// is a row with an error, so one dead plug never hides the others.
func readAll(ctx context.Context, plugs []plugConfig) []plugView {
	views := make([]plugView, len(plugs))

	var wg sync.WaitGroup

	for i, config := range plugs {
		views[i] = plugView{Name: config.Name, Host: config.Host, Printer: config.Printer}

		wg.Add(1)

		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()

			mark := auto.mark()

			p, err := plugAt(config.Host)
			if err == nil {
				var reading Reading

				if reading, err = p.Read(ctx); err == nil {
					views[i] = viewOf(config, reading, mark)
				}
			}

			if err != nil {
				views[i].Error = err.Error()
			}
		}()
	}

	wg.Wait()

	return views
}

// resolve is the plug the path names, or the refusal that says there is none.
func resolve(w http.ResponseWriter, r *http.Request) (plugConfig, *plug, bool) {
	wanted := r.PathValue("name")

	config, found := settings().find(wanted)
	if !found {
		refuse(w, r, PlugNotFound.New("no plug is named "+wanted).With(fault.StringParam("name", wanted)))

		return plugConfig{}, nil, false
	}

	p, err := plugAt(config.Host)
	if err != nil {
		refuse(w, r, PlugNotFound.New("the plug has no usable host").Wrap(err).With(fault.StringParam("name", wanted)))

		return plugConfig{}, nil, false
	}

	return config, p, true
}

// unreachable is the refusal for a plug that did not answer or answered wrongly.
func unreachable(config plugConfig, what string, err error) *fault.Fault {
	return PlugUnreachable.New(what).Wrap(err).With(
		fault.StringParam("name", config.Name), fault.StringParam("host", config.Host))
}

// answer writes one JSON body, which is every route above.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// stdio is the pair the host handed over. Close shuts the reading half, which
// is what ends a session whose host has gone quiet.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdin.Close() }
