package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// busyStates are the printer states in which cutting the power ruins the print:
// printing, paused, or waiting for a person in the middle of one. A printer in
// any other state, or one the host does not know, may be turned off.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var busyStates = map[string]bool{"PRINTING": true, "PAUSED": true, "ATTENTION": true}

// statePath is where a printer keeps its state word in the state tree the host
// holds for it.
const statePath = "/state"

// printerState answers the state of the printer with this serial, and whether the
// host knows it at all. It is a variable so a test can answer without a host, and
// run replaces it with the host's device API.
//
//nolint:gochecknoglobals // a test seam.
var printerState = func(context.Context, string) (state string, known bool, err error) {
	return "", false, nil
}

// stateReader answers a printer's state from the snapshots of every printer the
// host knows.
func stateReader(snapshots func(context.Context) (map[string]snapshot, error)) func(context.Context, string) (string, bool, error) {
	return func(ctx context.Context, serial string) (string, bool, error) {
		found, err := snapshots(ctx)
		if err != nil {
			return "", false, err
		}

		snap, known := found[serial]

		return snap.State, known, nil
	}
}

// printerBusy says whether the printer with this serial is in a state a power cut
// would ruin, and which state. A lookup that fails does not stop the switch: the
// plug can be the only way to restart a printer that has frozen, so a host that
// cannot answer is not allowed to hold it up. The failure is written to the log.
func printerBusy(ctx context.Context, serial string) (busy bool, state string) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	state, known, err := printerState(ctx, serial)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: could not read the state of printer %s, so its plug is not guarded: %v\n", name, serial, err)

		return false, ""
	}

	return known && busyStates[state], state
}
