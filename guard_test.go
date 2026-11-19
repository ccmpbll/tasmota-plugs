package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// withPrinterState answers the printer-state lookup from a table of serials, so
// a test needs no host. A serial not in the table is a printer the host does not
// know; the lookup is told what it was asked.
func withPrinterState(t *testing.T, states map[string]string) *[]string {
	t.Helper()

	asked := &[]string{}
	old := printerState
	printerState = func(_ context.Context, serial string) (string, bool, error) {
		*asked = append(*asked, serial)
		state, known := states[serial]

		return state, known, nil
	}

	t.Cleanup(func() { printerState = old })

	return asked
}

// The point: a printer that is printing keeps its power.
func TestTurningOffTheOnlyPlugOfABusyPrinterIsRefused(t *testing.T) {
	for _, state := range []string{"PRINTING", "PAUSED", "ATTENTION"} {
		states := withFakePlugs(t, twoPlugs...)
		*states["core.lan"] = "ON"

		withPrinterState(t, map[string]string{"CZPX-1": state})

		recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false}`)
		if recorder.Code != http.StatusConflict || code(t, recorder) != PrinterBusy.Code().String() {
			t.Fatalf("%s: answered %d %s, want 409 PRINTER_BUSY", state, recorder.Code, recorder.Body)
		}

		if *states["core.lan"] != "ON" {
			t.Fatalf("%s: the plug was switched although the request was refused", state)
		}
	}
}

func TestAnIdleOrFinishedPrinterMayBeTurnedOff(t *testing.T) {
	for _, state := range []string{"IDLE", "READY", "FINISHED", "STOPPED", "ERROR", ""} {
		states := withFakePlugs(t, twoPlugs...)
		*states["core.lan"] = "ON"

		withPrinterState(t, map[string]string{"CZPX-1": state})

		if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false}`); recorder.Code != http.StatusOK {
			t.Fatalf("%q: answered %d %s", state, recorder.Code, recorder.Body)
		}

		if *states["core.lan"] != "OFF" {
			t.Fatalf("%q: the plug was not switched", state)
		}
	}
}

// Turning ON is never gated, and does not even ask: a busy printer is already on.
func TestTurningOnIsNeverGatedAndNeverAsks(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)
	asked := withPrinterState(t, map[string]string{"CZPX-1": "PRINTING"})

	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}

	if *states["core.lan"] != "ON" || len(*asked) != 0 {
		t.Fatalf("plug %s, lookups %v: turning on must switch and must not ask the host", *states["core.lan"], *asked)
	}
}

// The override is explicit and per request: the dialog sends it after a person
// confirmed, and nothing else does.
func TestForceTurnsOffABusyPrinter(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)
	*states["core.lan"] = "ON"

	withPrinterState(t, map[string]string{"CZPX-1": "PRINTING"})

	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false,"force":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}

	if *states["core.lan"] != "OFF" {
		t.Fatal("force did not switch the plug")
	}

	// Force with a wrong type is a refused body, not a quiet yes.
	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false,"force":"yes"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf(`"force":"yes" answered %d, want 400`, recorder.Code)
	}
}

// A printer the host does not know (off, or not connected) may be power
// cycled: the plug can be the only way to restart one that has frozen.
func TestAnUnknownPrinterMayBeTurnedOff(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)
	*states["core.lan"] = "ON"

	withPrinterState(t, map[string]string{})

	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}
}

func TestAPlugWithNoPrinterIsNotGatedAndDoesNotAsk(t *testing.T) {
	plugs := []plugConfig{{Slot: 1, Name: "lamp", Host: "lamp.lan"}}
	states := withFakePlugs(t, plugs...)
	*states["lamp.lan"] = "ON"

	asked := withPrinterState(t, map[string]string{"": "PRINTING"})

	if recorder := do(http.MethodPost, "/plugs/lamp/power", `{"on":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}

	if len(*asked) != 0 {
		t.Fatalf("a plug with no printer asked the host about %v", *asked)
	}
}

// If the host cannot say, the request is not held up by it; the failure is logged.
func TestALookupThatFailsDoesNotBlockTheSwitch(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)
	*states["core.lan"] = "ON"

	old := printerState
	printerState = func(context.Context, string) (string, bool, error) { return "", false, errors.New("host busy") }

	t.Cleanup(func() { printerState = old })

	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}
}

func TestEveryBusyStateIsOneThatLosesAPrint(t *testing.T) {
	for _, state := range []string{"PRINTING", "PAUSED", "ATTENTION"} {
		if !busyStates[state] {
			t.Fatalf("%s is not treated as busy", state)
		}
	}

	for _, state := range []string{"IDLE", "READY", "FINISHED", "STOPPED", "ERROR", "OFFLINE", "UNKNOWN", "MANIPULATING"} {
		if busyStates[state] {
			t.Fatalf("%s is treated as busy: a person could not turn the plug off", state)
		}
	}
}

// "Known" decides, not the word: a lookup that is not sure the printer exists must
// not hold up the switch even if it hands back a busy word.
func TestKnownDecidesNotTheStateWord(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)
	*states["core.lan"] = "ON"

	old := printerState
	printerState = func(context.Context, string) (string, bool, error) { return "PRINTING", false, nil }

	t.Cleanup(func() { printerState = old })

	if recorder := do(http.MethodPost, "/plugs/core-one/power", `{"on":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("answered %d %s", recorder.Code, recorder.Body)
	}
}
