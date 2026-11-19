package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func deg(n float64) *float64 { return &n }

// rig is a watcher with a clock, printers and plugs that a test moves by hand.
type rig struct {
	mu     sync.Mutex // the cuts and the log are written from the goroutines of one cutAll
	t      *testing.T
	w      *watcher
	clock  time.Time
	snaps  map[string]snapshot
	err    error
	cuts   []string
	cutErr map[string]error
	logs   []string
}

func newRig(t *testing.T, auto Auto, plugs ...plugConfig) *rig {
	t.Helper()

	r := &rig{t: t, clock: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC), snaps: map[string]snapshot{}, cutErr: map[string]error{}}
	settings := &Settings{Plugs: plugs, Auto: auto}

	r.w = newWatcher(func(context.Context) (map[string]snapshot, error) { return r.snaps, r.err })
	r.w.settings = func() *Settings { return settings }
	r.w.now = func() time.Time { return r.clock }
	r.w.cut = func(_ context.Context, p plugConfig) error {
		r.mu.Lock()
		defer r.mu.Unlock()

		if err := r.cutErr[p.Name]; err != nil {
			return err
		}

		r.cuts = append(r.cuts, p.Name)

		return nil
	}
	r.w.log = func(format string, args ...any) {
		r.mu.Lock()
		defer r.mu.Unlock()

		r.logs = append(r.logs, fmt.Sprintf(format, args...))
	}

	return r
}

// at moves the clock and looks, so the rig reads as a timeline.
func (r *rig) look(after time.Duration) {
	r.t.Helper()

	r.clock = r.clock.Add(after)
	r.w.tick(context.Background())
}

func (r *rig) cut(want ...string) {
	r.t.Helper()

	// Plugs of one printer are cut at the same time, so their order is not one.
	got := slices.Sorted(slices.Values(r.cuts))
	if !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		r.t.Fatalf("plugs cut so far: %v, want %v (log: %v)", r.cuts, want, r.logs)
	}
}

func cool(state string) snapshot { return snapshot{State: state, Nozzle: deg(25), Bed: deg(24)} }

var (
	coreOne = plugConfig{Slot: 1, Name: "core-one", Host: "core.lan", Printer: "CZPX-1"}
	mk4s    = plugConfig{Slot: 2, Name: "mk4s", Host: "mk4.lan", Printer: "CZPX-2"}
)

func TestNothingIsCutWhenNothingIsSet(t *testing.T) {
	r := newRig(t, Auto{}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(480), Bed: deg(190)}

	r.look(0)
	r.look(48 * time.Hour)
	r.cut()
}

func TestAnIdlePrinterIsCutOnlyAfterTheTimeout(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 30}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")

	r.look(0) // it is first seen idle: the clock starts here
	r.look(29*time.Minute + 59*time.Second)
	r.cut()

	r.look(time.Second)
	r.cut("core-one")
}

func TestAnIdlePrinterThatIsStillHotWaitsToCool(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 10, CooldownC: 40}, coreOne)

	for _, hot := range []snapshot{
		{State: "FINISHED", Nozzle: deg(120), Bed: deg(30)},
		{State: "FINISHED", Nozzle: deg(30), Bed: deg(55)},
		{State: "FINISHED", Nozzle: deg(40.5), Bed: deg(30)},
	} {
		r.snaps["CZPX-1"] = hot
		r.look(0)
		r.look(time.Hour)
		r.cut()
	}

	r.snaps["CZPX-1"] = snapshot{State: "FINISHED", Nozzle: deg(40), Bed: deg(40)}
	r.look(10 * time.Second)
	r.cut("core-one")
}

func TestTheDefaultCooldownIsFortyDegrees(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne) // no CooldownC
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(41), Bed: deg(20)}

	r.look(0)
	r.look(time.Hour)
	r.cut()

	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(40), Bed: deg(20)}
	r.look(time.Second)
	r.cut("core-one")
}

func TestOnlyAnIdleStateIsEverCutForBeingIdle(t *testing.T) {
	for _, state := range []string{"PRINTING", "PAUSED", "ATTENTION", "ERROR", "BUSY", "MANIPULATING", "OFFLINE", "UNKNOWN", ""} {
		r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
		r.snaps["CZPX-1"] = cool(state)

		r.look(0)
		r.look(24 * time.Hour)
		r.cut()
	}

	for _, state := range []string{"IDLE", "READY", "FINISHED", "STOPPED"} {
		r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
		r.snaps["CZPX-1"] = cool(state)

		r.look(0)
		r.look(time.Minute)
		r.cut("core-one")
	}
}

func TestLeavingIdleStartsTheClockAgain(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 30}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")

	r.look(0)
	r.look(29 * time.Minute)

	r.snaps["CZPX-1"] = cool("PRINTING")
	r.look(time.Minute)

	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(time.Minute) // idle again, from now
	r.look(29 * time.Minute)
	r.cut()

	r.look(time.Minute)
	r.cut("core-one")
}

func TestAnIdleCutHappensOncePerStreak(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 5}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")

	r.look(0)
	r.look(5 * time.Minute)
	r.cut("core-one")

	// The host may keep saying IDLE for a while after the cut. No second cut.
	r.look(time.Hour)
	r.look(time.Hour)
	r.cut("core-one")

	// It goes offline with its power, comes back when someone switches it on, and
	// is a new streak: five minutes more, not at once.
	r.snaps["CZPX-1"] = cool("OFFLINE")
	r.look(time.Minute)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(time.Minute)
	r.look(4 * time.Minute)
	r.cut("core-one")

	r.look(time.Minute)
	r.cut("core-one", "core-one")
}

func TestAPrinterThatReportsNoTemperatureIsNotCutAndItIsSaidOnce(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE"}

	r.look(0)

	for range 5 {
		r.look(time.Hour)
	}

	r.cut()

	said := 0
	for _, line := range r.logs {
		if strings.Contains(line, "reports no temperature") {
			said++
		}
	}

	if said != 1 {
		t.Fatalf("said %d times, want once: %v", said, r.logs)
	}
}

func TestAPrinterWithOnlyABedTemperatureIsJudgedOnThat(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Bed: deg(60)}

	r.look(0)
	r.look(time.Hour)
	r.cut()

	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Bed: deg(30)}
	r.look(time.Second)
	r.cut("core-one")
}

func TestAnIdleCutThatFailsWaitsFiveMinutesAndIsThenTriedAgain(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.cutErr["core-one"] = errors.New("plug unreachable")

	r.look(0)
	r.look(time.Minute) // the first try fails

	// Looking every ten seconds, it does not try or say anything again.
	for range 29 {
		r.look(10 * time.Second)
	}

	if len(r.logs) != 1 {
		t.Fatalf("a failing plug was reported %d times in five minutes: %v", len(r.logs), r.logs)
	}

	delete(r.cutErr, "core-one")
	r.look(10 * time.Second) // five minutes since the failure
	r.cut("core-one")

	r.look(10 * time.Second)
	r.cut("core-one")
}

func TestOnlyPlugsLinkedToThePrinterAreCut(t *testing.T) {
	second := plugConfig{Slot: 3, Name: "core-one-fan", Host: "fan.lan", Printer: "CZPX-1"}
	free := plugConfig{Slot: 4, Name: "lamp", Host: "lamp.lan"}
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne, mk4s, second, free)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.snaps["CZPX-2"] = cool("PRINTING")

	r.look(0)
	r.look(time.Hour)
	r.cut("core-one", "core-one-fan")
}

func TestOnePlugFailingDoesNotStopTheOthers(t *testing.T) {
	second := plugConfig{Slot: 3, Name: "core-one-fan", Host: "fan.lan", Printer: "CZPX-1"}
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne, second)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.cutErr["core-one"] = errors.New("down")

	r.look(0)
	r.look(time.Minute)
	r.cut("core-one-fan")
}

func TestAPrinterTheHostDoesNotKnowIsLeftAlone(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1, OverheatNozzleC: 300}, coreOne)

	r.look(0)
	r.look(time.Hour)
	r.cut()
}

func TestNothingIsCutWhileThePrintersCannotBeReadAndItIsSaidOnce(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1, OverheatBedC: 150}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(25), Bed: deg(190)}
	r.err = errors.New("host busy")

	for range 4 {
		r.look(time.Hour)
	}

	r.cut()

	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "host busy") {
		t.Fatalf("log: %v, want one line naming the cause", r.logs)
	}

	// When it can be read again, it acts, and a later outage is said again.
	r.err = nil
	r.look(time.Second)
	r.cut("core-one")
}

func TestARestartOfTheRulesCutsNothingThatWasAlreadyIdle(t *testing.T) {
	// The clock is in memory: a fresh watcher meets an idle printer for the first
	// time and waits the full time.
	r := newRig(t, Auto{IdleMinutes: 60}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")

	r.look(0)
	r.cut()
}

func TestAHeaterOverItsLimitIsCutInAnyState(t *testing.T) {
	for _, state := range []string{"PRINTING", "PAUSED", "ATTENTION", "IDLE", "ERROR", ""} { // not OFFLINE: see the offline test
		for name, snap := range map[string]snapshot{
			"nozzle": {State: state, Nozzle: deg(301), Bed: deg(60)},
			"bed":    {State: state, Nozzle: deg(200), Bed: deg(151)},
		} {
			r := newRig(t, Auto{OverheatNozzleC: 300, OverheatBedC: 150}, coreOne)
			r.snaps["CZPX-1"] = snap

			r.look(0) // at once, with no waiting
			if len(r.cuts) != 1 {
				t.Fatalf("%s over in state %q: cuts %v", name, state, r.cuts)
			}
		}
	}
}

func TestAHeaterAtItsLimitOrBelowIsNotCut(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300, OverheatBedC: 150}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(300), Bed: deg(150)}

	r.look(0)
	r.look(time.Hour)
	r.cut()
}

func TestEachOverheatLimitIsOptionalOnItsOwn(t *testing.T) {
	r := newRig(t, Auto{OverheatBedC: 150}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(499), Bed: deg(60)}

	r.look(0)
	r.cut() // no nozzle limit set

	r = newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(60), Bed: deg(199)}

	r.look(0)
	r.cut() // no bed limit set
}

func TestAMissingTemperatureIsNotOverheating(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300, OverheatBedC: 150}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING"}

	r.look(0)
	r.cut()
}

func TestAnOverheatCutIsNotRepeatedEveryLookButIsRetriedIfItDidNotTake(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320), Bed: deg(60)}

	r.look(0)
	r.cut("core-one")

	r.look(10 * time.Second)
	r.look(10 * time.Second)
	r.look(30 * time.Second)
	r.cut("core-one") // 50 seconds in: still within the minute it is given

	r.look(11 * time.Second) // over a minute, printer still printing and hot
	r.cut("core-one", "core-one")
}

func TestAnOverheatCutIsMadeAgainOnlyAfterThePrinterWentOffAndCameBackHot(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	hot := func(state string) snapshot { return snapshot{State: state, Nozzle: deg(320), Bed: deg(60)} }

	r.snaps["CZPX-1"] = hot("PRINTING")
	r.look(0)
	r.cut("core-one")

	// The cut took: the printer goes offline, still showing its last hot reading.
	r.snaps["CZPX-1"] = hot("OFFLINE")
	r.look(5 * time.Second)

	for range 20 {
		r.look(time.Minute)
	}

	r.cut("core-one") // offline, stale: no repeats

	// Someone switches it on and it is really still hot: the first look back is
	// not trusted (see the stale-reading test), the next one is.
	r.snaps["CZPX-1"] = hot("IDLE")
	r.look(time.Second)
	r.cut("core-one")
	r.look(10 * time.Second)
	r.cut("core-one", "core-one")
}

func TestACooledPrinterClearsTheTripSoTheNextOverheatIsCut(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)

	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320)}
	r.look(0)

	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(220)}
	r.look(10 * time.Second)

	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320)}
	r.look(10 * time.Second)
	r.cut("core-one", "core-one")
}

func TestAnOverheatCutThatFailsIsTriedAtTheNextLook(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320)}
	r.cutErr["core-one"] = errors.New("down")

	r.look(0)
	r.look(10 * time.Second)
	r.cut()

	delete(r.cutErr, "core-one")
	r.look(10 * time.Second)
	r.cut("core-one")
}

func TestOverheatAndIdleDoNotBothCutTheSamePrinter(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1, OverheatNozzleC: 300}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(25), Bed: deg(24)}

	r.look(0)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(320), Bed: deg(24)}
	r.look(time.Hour)
	r.cut("core-one")

	if event := r.w.last("core-one"); event == nil || event.Reason != reasonOverheat {
		t.Fatalf("remembered %+v, want the overheat", event)
	}
}

func TestTheReasonIsRememberedPerPlug(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 5}, coreOne, mk4s)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.snaps["CZPX-2"] = cool("PRINTING")

	if r.w.last("core-one") != nil {
		t.Fatal("a reason before any cut")
	}

	r.look(0)
	r.look(5 * time.Minute)

	event := r.w.last("core-one")
	if event == nil || event.Reason != reasonIdle || !strings.Contains(event.Detail, "5 min") || !event.At.Equal(r.clock) {
		t.Fatalf("remembered %+v", event)
	}

	if r.w.last("mk4s") != nil {
		t.Fatal("a reason for a plug that was not cut")
	}
}

func TestTurningTheRulesOffForgetsTheClocks(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 30}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")

	r.look(0)
	r.look(25 * time.Minute)

	off := r.w.settings
	r.w.settings = func() *Settings { return &Settings{Plugs: []plugConfig{coreOne}} }
	r.look(time.Minute)

	// Switched on again: the printer is met afresh, whatever it was before.
	r.w.settings = off
	r.look(time.Minute)
	r.look(29 * time.Minute)
	r.cut()

	r.look(time.Minute)
	r.cut("core-one")
}

func TestSnapshotsReadTheStateAndTheHottestNozzle(t *testing.T) {
	tree := map[string]json.RawMessage{
		"/state":                  json.RawMessage(`"PRINTING"`),
		"/temperature/bed":        json.RawMessage(`60.5`),
		"/tools/0/nozzle_temp":    json.RawMessage(`215`),
		"/tools/1/nozzle_temp":    json.RawMessage(`231.5`),
		"/tools/10/nozzle_temp":   json.RawMessage(`100`),
		"/tools/0/nozzle_target":  json.RawMessage(`999`),
		"/temperature/bed_target": json.RawMessage(`999`),
		"/tools/x/nozzle_temp":    json.RawMessage(`999`),
	}

	// Map order is random, so the hottest must win whatever came first.
	for range 60 {
		got := snapshotOf(tree)
		if got.State != "PRINTING" || got.Bed == nil || *got.Bed != 60.5 || got.Nozzle == nil || *got.Nozzle != 231.5 {
			t.Fatalf("read %+v", got)
		}
	}

	// A null beside a real reading is no reading, and does not stop the rest.
	mixed := map[string]json.RawMessage{
		"/tools/0/nozzle_temp": json.RawMessage(`215`),
		"/tools/1/nozzle_temp": json.RawMessage(`null`),
		"/temperature/bed":     json.RawMessage(`null`),
	}
	for range 60 {
		if got := snapshotOf(mixed); got.Nozzle == nil || *got.Nozzle != 215 || got.Bed != nil {
			t.Fatalf("read %+v", got)
		}
	}

	for name, tree := range map[string]map[string]json.RawMessage{
		"nothing":     {},
		"not numbers": {"/temperature/bed": json.RawMessage(`"hot"`), "/tools/0/nozzle_temp": json.RawMessage(`null`), "/state": json.RawMessage(`5`)},
	} {
		got := snapshotOf(tree)
		if got.State != "" || got.Bed != nil || got.Nozzle != nil {
			t.Fatalf("%s read as %+v", name, got)
		}
	}
}

func TestAPlugShowsWhyItWasSwitchedOffWhileItIsOff(t *testing.T) {
	plugs := []plugConfig{coreOne}
	withSettings(t, Settings{Plugs: plugs})
	states := withFakePlugs(t, plugs...)

	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(0)
	r.look(time.Minute)

	old := auto
	auto = r.w

	t.Cleanup(func() { auto = old })

	read := func() plugView {
		var v plugView
		if err := json.Unmarshal(do(http.MethodGet, "/plugs/core-one", "").Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}

		return v
	}

	*states["core.lan"] = "OFF"
	if v := read(); v.AutoOff == nil || v.AutoOff.Reason != reasonIdle {
		t.Fatalf("off plug shows %+v", v.AutoOff)
	}

	*states["core.lan"] = "ON"
	if v := read(); v.AutoOff != nil {
		t.Fatalf("a plug that was switched on again still shows %+v", v.AutoOff)
	}
}

func TestAPrinterBackOnlineSoonAfterTheCutAndStillHotIsCutAgain(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	hot := func(state string) snapshot { return snapshot{State: state, Nozzle: deg(320)} }

	r.snaps["CZPX-1"] = hot("PRINTING")
	r.look(0)

	r.snaps["CZPX-1"] = hot("OFFLINE")
	r.look(5 * time.Second)

	// Switched on again inside the minute the cut was given, and still hot on two
	// looks in a row.
	r.snaps["CZPX-1"] = hot("IDLE")
	r.look(5 * time.Second)
	r.look(10 * time.Second)
	r.cut("core-one", "core-one")
}

func TestThePrintersAreNotReadWhenNoPlugIsLinkedToOne(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1, OverheatNozzleC: 300}, plugConfig{Slot: 1, Name: "lamp", Host: "lamp.lan"})
	asked := 0
	r.w.snapshots = func(context.Context) (map[string]snapshot, error) { asked++; return r.snaps, nil }

	r.look(0)
	r.look(time.Hour)

	if asked != 0 {
		t.Fatalf("the printers were read %d times for plugs that power none", asked)
	}
}

func TestAReasonEndsWhenAPlugIsSeenOnOrSwitchedByAPerson(t *testing.T) {
	plugs := []plugConfig{coreOne}
	withSettings(t, Settings{Plugs: plugs})
	states := withFakePlugs(t, plugs...)

	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(0)
	r.look(time.Minute)

	old := auto
	auto = r.w

	t.Cleanup(func() { auto = old })

	shown := func() bool {
		var v plugView
		if err := json.Unmarshal(do(http.MethodGet, "/plugs/core-one", "").Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}

		return v.AutoOff != nil
	}

	// Seen on, then off again by someone: the old reason is not shown.
	*states["core.lan"] = "OFF"
	if !shown() {
		t.Fatal("the reason is not shown for a plug the module switched off")
	}

	*states["core.lan"] = "ON"
	shown()

	*states["core.lan"] = "OFF"
	if shown() {
		t.Fatal("a plug switched on and off again by a person still shows the module's reason")
	}

	// Switched through the route, with nobody reading it in between.
	r.snaps["CZPX-1"] = cool("PRINTING")
	r.look(time.Second)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(time.Second)
	r.look(time.Minute)

	*states["core.lan"] = "OFF"
	if !shown() {
		t.Fatal("the second cut was not remembered")
	}

	do(http.MethodPost, "/plugs/core-one/power", `{"on":true}`)
	do(http.MethodPost, "/plugs/core-one/power", `{"on":false,"force":true}`)

	if shown() {
		t.Fatal("a plug switched by a person through the route still shows the module's reason")
	}
}

var enclosure = plugConfig{Slot: 3, Name: "enclosure", Host: "enc.lan", Printer: "CZPX-1"}

func TestAnIdleCutRetriesOnlyThePlugsThatFailed(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne, enclosure)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.cutErr["enclosure"] = errors.New("down")

	r.look(0)
	r.look(time.Minute)
	r.cut("core-one")

	// Inside the five minutes nothing is tried; after them only the plug that failed.
	r.look(4 * time.Minute)
	r.cut("core-one")

	delete(r.cutErr, "enclosure")
	r.look(time.Minute + time.Second)
	r.cut("core-one", "enclosure")

	// Both are off now: that is the one cut of this idle streak.
	r.look(time.Hour)
	r.cut("core-one", "enclosure")
}

func TestAnOverheatCutRetriesOnlyThePlugsThatFailedAtTheNextLook(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne, enclosure)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320)}
	r.cutErr["enclosure"] = errors.New("down")

	r.look(0)
	r.cut("core-one")

	r.look(10 * time.Second)
	r.cut("core-one")

	r.look(40 * time.Second) // a minute in: a retry that fails does not start the printer over

	delete(r.cutErr, "enclosure")
	r.look(10 * time.Second)
	r.cut("core-one", "enclosure")

	// The minute the cut is given to show starts from the retry that took.
	r.look(40 * time.Second)
	r.cut("core-one", "enclosure")

	r.look(21 * time.Second)
	r.cut("core-one", "enclosure", "core-one", "enclosure")
}

func TestAPrinterThatIsOfflineIsNotJudgedOnItsLastReadings(t *testing.T) {
	// A module restart forgets the trips; the printer was cut by hand while hot
	// and Connect still holds its last reading.
	r := newRig(t, Auto{OverheatNozzleC: 300, OverheatBedC: 150}, coreOne)
	r.snaps["CZPX-1"] = snapshot{State: "OFFLINE", Nozzle: deg(320), Bed: deg(190)}

	r.look(0)
	r.look(time.Hour)
	r.cut()

	if r.w.last("core-one") != nil {
		t.Fatal("an offline printer's last readings were taken for an overheat")
	}
}

func TestAHotReadingRightAfterAPrinterComesBackIsConfirmedBeforeItCuts(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne)
	hot := func(state string) snapshot { return snapshot{State: state, Nozzle: deg(320)} }

	r.snaps["CZPX-1"] = hot("PRINTING")
	r.look(0)
	r.snaps["CZPX-1"] = hot("OFFLINE")
	r.look(5 * time.Second)

	// Back, still showing the old reading; the fresh one is cool.
	r.snaps["CZPX-1"] = hot("IDLE")
	r.look(5 * time.Second)
	r.snaps["CZPX-1"] = snapshot{State: "IDLE", Nozzle: deg(30)}
	r.look(10 * time.Second)
	r.look(time.Hour)
	r.cut("core-one") // the first cut only

	// Hot twice in a row is real, and is cut.
	r.snaps["CZPX-1"] = hot("IDLE")
	r.look(10 * time.Second)
	r.look(10 * time.Second)
	r.cut("core-one", "core-one")
}

func TestAnOverheatIsCutBeforeAnyIdleCutCanHoldItBack(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1, OverheatNozzleC: 300}, coreOne, mk4s)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.snaps["CZPX-2"] = snapshot{State: "PRINTING", Nozzle: deg(25)}
	r.look(0)

	// The idle cut of the first printer (looked at first) waits until the second
	// printer's overheat cut has been made.
	released := make(chan struct{})
	inner := r.w.cut
	r.w.cut = func(ctx context.Context, p plugConfig) error {
		if p.Name == "core-one" {
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				return errors.New("the overheat cut was queued behind an idle cut")
			}
		}

		err := inner(ctx, p)
		if p.Name == "mk4s" {
			close(released)
		}

		return err
	}

	r.snaps["CZPX-2"] = snapshot{State: "PRINTING", Nozzle: deg(320)}
	r.look(time.Minute)
	r.cut("core-one", "mk4s")
}

func TestThePlugsOfOnePrinterAreCutTogether(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300}, coreOne, enclosure)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(320)}

	entered := make(chan struct{}, 2)
	together := make(chan struct{})
	inner := r.w.cut
	r.w.cut = func(ctx context.Context, p plugConfig) error {
		entered <- struct{}{}

		select {
		case <-together:
		case <-time.After(5 * time.Second):
			return errors.New("the plugs were cut one after another")
		}

		return inner(ctx, p)
	}

	go func() {
		<-entered
		<-entered
		close(together)
	}()

	r.look(0)
	r.cut("core-one", "enclosure")
}

func TestAnOverheatLimitThatCannotSeeItsHeaterIsSaidOnce(t *testing.T) {
	r := newRig(t, Auto{OverheatNozzleC: 300, OverheatBedC: 150}, coreOne)
	said := func() int {
		n := 0

		for _, line := range r.logs {
			if strings.Contains(line, "cannot protect") {
				n++
			}
		}

		return n
	}

	// No hotend reading, a bed reading: only the hotend limit is blind.
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Bed: deg(60)}
	for range 5 {
		r.look(10 * time.Second)
	}

	if said() != 1 || !strings.Contains(strings.Join(r.logs, "\n"), "hotend") {
		t.Fatalf("log: %v, want one line about the hotend", r.logs)
	}

	// An offline printer says nothing new, a reading back re-arms the warning.
	r.snaps["CZPX-1"] = snapshot{State: "OFFLINE"}
	r.look(10 * time.Second)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Nozzle: deg(200), Bed: deg(60)}
	r.look(10 * time.Second)
	r.snaps["CZPX-1"] = snapshot{State: "PRINTING", Bed: deg(60)}
	r.look(10 * time.Second)

	if said() != 2 {
		t.Fatalf("said %d times: %v", said(), r.logs)
	}

	// Rules that watch no heater do not warn.
	quiet := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	quiet.snaps["CZPX-1"] = snapshot{State: "PRINTING"}
	quiet.look(0)
	quiet.look(time.Hour)

	if len(quiet.logs) != 0 {
		t.Fatalf("an idle-only setup warned: %v", quiet.logs)
	}
}

func TestAnOverheatLimitOnAPrinterTheHostDoesNotKnowIsSaidOnce(t *testing.T) {
	r := newRig(t, Auto{OverheatBedC: 150}, coreOne)

	for range 3 {
		r.look(10 * time.Second)
	}

	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "CZPX-1") || !strings.Contains(r.logs[0], "not known") {
		t.Fatalf("log: %v", r.logs)
	}
}

func TestAReadThatBeganBeforeACutDoesNotEndItsReason(t *testing.T) {
	r := newRig(t, Auto{IdleMinutes: 1}, coreOne)
	r.snaps["CZPX-1"] = cool("IDLE")
	r.look(0)

	mark := r.w.mark() // a read of the plug starts here and answers "on"

	r.look(time.Minute) // the cut happens while it is in flight

	r.w.forgetSeen("core-one", mark)

	if r.w.last("core-one") == nil {
		t.Fatal("a read from before the cut ended the reason for it")
	}

	r.w.forgetSeen("core-one", r.w.mark())

	if r.w.last("core-one") != nil {
		t.Fatal("a read from after the cut that found the plug on did not end the reason")
	}
}

func TestAWatcherThatIsNotRunningRemembersNothing(t *testing.T) {
	var w *watcher

	w.forget("a")
	w.forgetSeen("a", w.mark())

	if w.last("a") != nil {
		t.Fatal("a reason from nowhere")
	}
}
