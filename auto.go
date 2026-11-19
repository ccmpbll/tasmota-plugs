package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

const (
	// watchEvery is how often the printers are looked at. The rules are in
	// minutes and in degrees that climb over many seconds, so a few seconds'
	// delay in acting on them costs nothing.
	watchEvery = 10 * time.Second
	// tripRetry is how long a cut for overheating is given to show: a printer
	// that is still on and still hot after it was cut is cut again.
	tripRetry = time.Minute
	// cutTimeout bounds one plug's switch off.
	cutTimeout = 8 * time.Second

	stateOffline = "OFFLINE"

	reasonIdle     = "idle"
	reasonOverheat = "overheat"
)

// idleStates are the states in which a printer is doing nothing and can be cut
// once it is cool. A printer in any other state is left alone, whatever else it
// reads: ERROR and ATTENTION want a person, not a power cut.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var idleStates = map[string]bool{"IDLE": true, "READY": true, "FINISHED": true, "STOPPED": true}

// The state tree holds a printer's temperatures at these paths.
var nozzlePath = regexp.MustCompile(`^/tools/[0-9]+/nozzle_temp$`)

const bedPath = "/temperature/bed"

// snapshot is what the watcher needs of a printer: its state word and the
// hottest of its hotends, and its bed. A temperature is nil when the printer
// does not report it.
type snapshot struct {
	State  string
	Nozzle *float64
	Bed    *float64
}

// snapshotOf reads a snapshot out of a printer's state tree.
func snapshotOf(tree map[string]json.RawMessage) snapshot {
	var snap snapshot

	_ = json.Unmarshal(tree[statePath], &snap.State)

	for path, raw := range tree {
		// A pointer, so a null is no reading and not zero degrees: a printer must
		// never look cool because it said nothing.
		var degrees *float64
		if json.Unmarshal(raw, &degrees) != nil || degrees == nil {
			continue
		}

		switch {
		case path == bedPath:
			snap.Bed = degrees
		case nozzlePath.MatchString(path):
			if snap.Nozzle == nil || *degrees > *snap.Nozzle {
				snap.Nozzle = degrees
			}
		}
	}

	return snap
}

// readSnapshots reads every printer the host knows, by serial, through the
// device API, which the module may use because it asked for `observe`.
func readSnapshots(devices *deviceapi.Client) func(context.Context) (map[string]snapshot, error) {
	return func(ctx context.Context) (map[string]snapshot, error) {
		found := map[string]snapshot{}

		for offset := 0; ; {
			page, err := devices.List(ctx, deviceapi.ListParams{Limit: deviceapi.MaxPageLimit, Offset: offset})
			if err != nil {
				return nil, err
			}

			for _, device := range page.Items {
				if device.Serial != "" {
					found[device.Serial] = snapshotOf(device.State)
				}
			}

			offset += len(page.Items)
			if len(page.Items) == 0 || offset >= page.Total {
				return found, nil
			}
		}
	}
}

// autoEvent is why a plug was last switched off by the module itself.
type autoEvent struct {
	Reason string    `json:"reason"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// streak is how long a printer has been idle, and whether it was cut for it.
type streak struct {
	since  time.Time
	done   bool
	warned bool
}

// trip is a cut made for overheating, kept until the printer shows that it took
// effect (it went offline and came back) or cooled.
type trip struct {
	at         time.Time
	sawOffline bool
}

// watcher switches plugs off on its own: after a printer has been idle and has
// cooled, and at once when a heater reads over its limit. Only plugs linked to a
// printer are touched, and nothing is touched until a limit is set. Its clocks
// are in memory, so a restart of the module starts every printer's idle time
// again: the safe way to be wrong, since it delays a cut and never brings one on.
type watcher struct {
	settings  func() *Settings
	snapshots func(context.Context) (map[string]snapshot, error)
	cut       func(context.Context, plugConfig) error
	now       func() time.Time
	log       func(format string, args ...any)

	streaks map[string]*streak
	trips   map[string]*trip
	failing bool

	mu     sync.Mutex
	events map[string]autoEvent
}

// auto is the running watcher. It is nil until run starts it, and in tests.
//
//nolint:gochecknoglobals // set once by run, read by the routes.
var auto *watcher

func newWatcher(snapshots func(context.Context) (map[string]snapshot, error)) *watcher {
	return &watcher{
		settings:  held.Load,
		snapshots: snapshots,
		cut: func(ctx context.Context, p plugConfig) error {
			plug, err := plugAt(p.Host)
			if err != nil {
				return err
			}

			return plug.Set(ctx, false)
		},
		now: time.Now,
		log: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, name+": "+format+"\n", args...)
		},
		streaks: map[string]*streak{},
		trips:   map[string]*trip{},
		events:  map[string]autoEvent{},
	}
}

// Run looks at the printers until ctx ends.
func (w *watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(watchEvery)
	defer ticker.Stop()

	for {
		w.tick(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// last is why the plug was last switched off by the module, if it was.
func (w *watcher) last(plug string) *autoEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	event, ok := w.events[plug]
	if !ok {
		return nil
	}

	return &event
}

// tick looks at every printer that has a plug linked and acts on what it finds.
func (w *watcher) tick(ctx context.Context) {
	cfg := w.settings()
	if cfg == nil || (cfg.Auto.IdleMinutes == 0 && cfg.Auto.OverheatNozzleC == 0 && cfg.Auto.OverheatBedC == 0) {
		clear(w.streaks)
		clear(w.trips)

		return
	}

	linked := map[string][]plugConfig{}

	for _, p := range cfg.Plugs {
		if p.Printer != "" {
			linked[p.Printer] = append(linked[p.Printer], p)
		}
	}

	if len(linked) == 0 {
		return
	}

	snaps, err := w.snapshots(ctx)
	if err != nil {
		// Said once per outage. Nothing is switched off while the printers cannot
		// be read: a plug is never cut on a guess.
		if !w.failing {
			w.log("could not read the printers, so nothing is switched off automatically until it can: %v", err)
		}

		w.failing = true

		return
	}

	w.failing = false

	serials := make([]string, 0, len(linked))
	for serial := range linked {
		serials = append(serials, serial)
	}

	slices.Sort(serials)

	for _, serial := range serials {
		// A printer the host does not know reads as no state and no temperature,
		// which neither rule acts on.
		snap := snaps[serial]

		if !w.overheat(ctx, cfg.Auto, serial, snap, linked[serial]) {
			w.idle(ctx, cfg.Auto, serial, snap, linked[serial])
		}
	}
}

// overheat cuts a printer whose heater reads over its limit, whatever it is
// doing. It reports whether the printer is being dealt with, so the idle rule
// stays out of it.
func (w *watcher) overheat(ctx context.Context, a Auto, serial string, snap snapshot, plugs []plugConfig) bool {
	detail, over := overheated(a, snap)
	if !over {
		delete(w.trips, serial)

		return false
	}

	now := w.now()

	if t := w.trips[serial]; t != nil {
		switch {
		case snap.State == stateOffline:
			t.sawOffline = true

			return true
		case t.sawOffline:
			// It went offline, so the cut took, and it is back and still over.
			delete(w.trips, serial)
		case now.Sub(t.at) < tripRetry:
			return true
		}
	}

	if w.cutAll(ctx, plugs, reasonOverheat, detail) {
		w.trips[serial] = &trip{at: now, sawOffline: snap.State == stateOffline}
	}

	return true
}

// overheated says whether a heater reads over its limit, and which.
func overheated(a Auto, snap snapshot) (string, bool) {
	switch {
	case a.OverheatNozzleC > 0 && snap.Nozzle != nil && *snap.Nozzle > float64(a.OverheatNozzleC):
		return fmt.Sprintf("the nozzle read %.0f °C, over the limit of %d °C", *snap.Nozzle, a.OverheatNozzleC), true
	case a.OverheatBedC > 0 && snap.Bed != nil && *snap.Bed > float64(a.OverheatBedC):
		return fmt.Sprintf("the bed read %.0f °C, over the limit of %d °C", *snap.Bed, a.OverheatBedC), true
	}

	return "", false
}

// idle cuts a printer that has been idle for the set time and has cooled. It does
// so once for each time the printer goes idle.
func (w *watcher) idle(ctx context.Context, a Auto, serial string, snap snapshot, plugs []plugConfig) {
	if a.IdleMinutes == 0 || !idleStates[snap.State] {
		delete(w.streaks, serial)

		return
	}

	now := w.now()

	s := w.streaks[serial]
	if s == nil {
		w.streaks[serial] = &streak{since: now}

		return
	}

	if s.done || now.Sub(s.since) < time.Duration(a.IdleMinutes)*time.Minute {
		return
	}

	// A printer that reports no temperature at all is not cut: whether it has
	// cooled cannot be told. Said once, so the setting does not seem to do nothing.
	if snap.Nozzle == nil && snap.Bed == nil {
		if !s.warned {
			w.log("printer %s has been idle for %d minutes, but reports no temperature, so it is not switched off", serial, a.IdleMinutes)
		}

		s.warned = true

		return
	}

	limit := float64(a.cooldown())
	if (snap.Nozzle != nil && *snap.Nozzle > limit) || (snap.Bed != nil && *snap.Bed > limit) {
		return
	}

	if w.cutAll(ctx, plugs, reasonIdle, fmt.Sprintf("the printer was idle for %d min and had cooled", a.IdleMinutes)) {
		s.done = true
	}
}

// cutAll switches every plug off and remembers why. It reports whether any
// plug went off, so a failure is tried again on the next look.
func (w *watcher) cutAll(ctx context.Context, plugs []plugConfig, reason, detail string) bool {
	var acted bool

	for _, p := range plugs {
		within, cancel := context.WithTimeout(ctx, cutTimeout)
		err := w.cut(within, p)

		cancel()

		if err != nil {
			w.log("could not switch off %q (%s): %v", p.Name, reason, err)

			continue
		}

		acted = true

		w.log("switched off %q (%s): %s", p.Name, reason, detail)
		w.mu.Lock()
		w.events[p.Name] = autoEvent{Reason: reason, Detail: detail, At: w.now()}
		w.mu.Unlock()
	}

	return acted
}
