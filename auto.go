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
	// idleRetry is how long an idle printer is left alone after its plug could not
	// be switched off. An overheat cut is not held back like this.
	idleRetry = 5 * time.Minute

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
		if path != bedPath && !nozzlePath.MatchString(path) {
			continue
		}

		// A pointer, so a null is no reading and not zero degrees: a printer must
		// never look cool because it said nothing.
		var degrees *float64
		if json.Unmarshal(raw, &degrees) != nil || degrees == nil {
			continue
		}

		if path == bedPath {
			snap.Bed = degrees
		} else if snap.Nozzle == nil || *degrees > *snap.Nozzle {
			snap.Nozzle = degrees
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

	// seq orders the events, so a read that began before a cut can be told from
	// one that began after it.
	seq uint64
}

// streak is how long a printer has been idle, and whether it was cut for it.
type streak struct {
	since time.Time
	// retryAt is when a cut that failed may be tried again.
	retryAt time.Time
	// left is the plugs a failed cut did not reach, so the next try is for them
	// alone. Nil until a cut has been tried.
	left   []plugConfig
	done   bool
	warned bool
}

// trip is a cut made for overheating, kept until the printer shows that it took
// effect (it went offline and came back) or cooled.
type trip struct {
	at         time.Time
	sawOffline bool
	// back is set on the first look at the printer after it came back from offline
	// still over its limit: a reading that old may be the last one before the cut,
	// so it is cut again only if the next look agrees.
	back bool
	// left is the plugs the cut did not reach, tried again at every look.
	left []plugConfig
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
	// blind is the printers whose missing readings were said already.
	blind map[string]bool

	mu     sync.Mutex
	events map[string]autoEvent
	seq    uint64
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
		blind:   map[string]bool{},
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

// forget ends the reason a plug was switched off. Safe on a watcher that is not
// running, which is how the routes are tested.
func (w *watcher) forget(plug string) {
	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.events, plug)
}

// mark is taken before a plug is read. A reading that finds the plug on ends the
// reason with forgetSeen and the mark, and only for a cut made before the read
// began: a read that was already in flight when the cut came answers from before
// it, and must not erase it.
func (w *watcher) mark() uint64 {
	if w == nil {
		return 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return w.seq
}

// forgetSeen ends the reason for a cut that a read, begun at mark, could have seen.
func (w *watcher) forgetSeen(plug string, mark uint64) {
	if w == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if event, ok := w.events[plug]; ok && event.seq <= mark {
		delete(w.events, plug)
	}
}

// last is why the plug was last switched off by the module, if it was.
func (w *watcher) last(plug string) *autoEvent {
	if w == nil {
		return nil
	}

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
		clear(w.blind)

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

	// A heater over its limit comes first, for every printer, so no slow plug of
	// an idle printer holds a safety cut back.
	var calm []string

	for _, serial := range serials {
		// A printer the host does not know reads as no state and no temperature,
		// which neither rule acts on.
		snap, known := snaps[serial]

		w.sight(cfg.Auto, serial, snap, known)

		if !w.overheat(ctx, cfg.Auto, serial, snap, linked[serial]) {
			calm = append(calm, serial)
		}
	}

	for _, serial := range calm {
		w.idle(ctx, cfg.Auto, serial, snaps[serial], linked[serial])
	}
}

// sight says, once, when a printer cannot be watched for a limit that is set: a
// limit that never fires must not look like one that is working. An offline
// printer is skipped, since it has nothing to read.
func (w *watcher) sight(a Auto, serial string, snap snapshot, known bool) {
	if (a.OverheatNozzleC == 0 && a.OverheatBedC == 0) || snap.State == stateOffline {
		return
	}

	var problem string

	switch {
	case !known:
		problem = "is not known to Prusa Connect"
	case a.OverheatNozzleC > 0 && snap.Nozzle == nil:
		problem = "reports no hotend temperature"
	case a.OverheatBedC > 0 && snap.Bed == nil:
		problem = "reports no bed temperature"
	default:
		delete(w.blind, serial)

		return
	}

	if !w.blind[serial] {
		w.log("printer %s %s, so the overheat limit cannot protect it", serial, problem)
	}

	w.blind[serial] = true
}

// overheat cuts a printer whose heater reads over its limit, whatever it is
// doing. It reports whether the printer is being dealt with, so the idle rule
// stays out of it.
func (w *watcher) overheat(ctx context.Context, a Auto, serial string, snap snapshot, plugs []plugConfig) bool {
	// An offline printer's readings are the last ones it sent, not what its heaters
	// do now. The cut took, if one was made; there is nothing to cut otherwise.
	if snap.State == stateOffline {
		if t := w.trips[serial]; t != nil {
			t.sawOffline = true
		}

		return false
	}

	detail, over := overheated(a, snap)
	if !over {
		delete(w.trips, serial)

		return false
	}

	now := w.now()

	if t := w.trips[serial]; t != nil {
		switch {
		case t.sawOffline && !t.back:
			// It is back and reads over its limit. Its first reading may be the old
			// one, so this look only notes it.
			t.back = true

			return true
		case t.sawOffline:
			// Over on two looks in a row since it came back: the cut took, and this is
			// a new overheat.
			delete(w.trips, serial)
		case len(t.left) > 0:
			t.left = w.cutAll(ctx, t.left, reasonOverheat, detail)
			t.at = now

			return true
		case now.Sub(t.at) < tripRetry:
			return true
		}
	}

	if failed := w.cutAll(ctx, plugs, reasonOverheat, detail); len(failed) < len(plugs) {
		w.trips[serial] = &trip{at: now, left: failed}
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

	if s.done || now.Before(s.retryAt) || now.Sub(s.since) < time.Duration(a.IdleMinutes)*time.Minute {
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

	todo := plugs
	if s.left != nil {
		todo = s.left
	}

	s.left = w.cutAll(ctx, todo, reasonIdle, fmt.Sprintf("the printer was idle for %d min and had cooled", a.IdleMinutes))
	if len(s.left) == 0 {
		s.done = true
	} else {
		s.retryAt = now.Add(idleRetry)
	}
}

// cutAll switches the plugs off at the same time, so one that does not answer
// costs its own timeout and no other plug's, and remembers why. It returns the
// plugs that could not be switched off, for the caller to try again.
func (w *watcher) cutAll(ctx context.Context, plugs []plugConfig, reason, detail string) []plugConfig {
	failed := make([]bool, len(plugs))

	var wg sync.WaitGroup

	for i, p := range plugs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			within, cancel := context.WithTimeout(ctx, cutTimeout)
			defer cancel()

			if err := w.cut(within, p); err != nil {
				w.log("could not switch off %q (%s): %v", p.Name, reason, err)

				failed[i] = true

				return
			}

			w.log("switched off %q (%s): %s", p.Name, reason, detail)

			w.mu.Lock()
			w.seq++
			w.events[p.Name] = autoEvent{Reason: reason, Detail: detail, At: w.now(), seq: w.seq}
			w.mu.Unlock()
		}()
	}

	wg.Wait()

	var left []plugConfig

	for i, p := range plugs {
		if failed[i] {
			left = append(left, p)
		}
	}

	return left
}
