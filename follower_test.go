package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

func jobEvent(t *testing.T, kind, subject string) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(event.Envelope{Type: kind, Subject: subject, Data: json.RawMessage(`{"file":"x.bgcode"}`)})
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func TestFollowerSubscribesToJobEventsAndResumesFromTheCursor(t *testing.T) {
	var (
		logs    bytes.Buffer
		calls   []busapi.SubscribeParams
		ctx, cn = context.WithCancel(context.Background())
	)
	defer cn()

	f := &follower{
		log: slog.New(slog.NewTextHandler(&logs, nil)),
		subscribe: func(_ context.Context, p busapi.SubscribeParams, on func(busapi.Window) error) error {
			calls = append(calls, p)

			if len(calls) == 1 {
				_ = on(busapi.Window{
					Events: []json.RawMessage{jobEvent(t, event.TypeDeviceJobStarted, "device:abc"), json.RawMessage(`not json`)},
					Cursor: "c1",
				})

				return errors.New("stream dropped")
			}

			cn()

			return nil
		},
	}

	done := make(chan struct{})

	go func() { f.Run(ctx); close(done) }()

	select {
	case <-done:
	case <-time.After(firstRetry + 3*time.Second):
		t.Fatal("the follower did not stop when its context ended")
	}

	if len(calls) != 2 {
		t.Fatalf("subscribed %d times, want 2", len(calls))
	}

	if got := strings.Join(calls[0].Types, ","); got != event.TypeDeviceJobStarted+","+event.TypeDeviceJobSettled {
		t.Fatalf("subscribed to %q", got)
	}

	if calls[1].Resume != "c1" {
		t.Fatalf("the second subscription resumed from %q, want c1", calls[1].Resume)
	}

	if !strings.Contains(logs.String(), "device:abc") {
		t.Fatalf("the job event was not logged: %s", logs.String())
	}
}

// An event names its printer's plugs, so a log line says which plug to switch.
func TestAnEventIsLoggedWithTheLinkedPlugsOfItsPrinter(t *testing.T) {
	withSettings(t, Settings{Plugs: []plugConfig{
		{Slot: 1, Name: "core-one", Host: "a.lan", Printer: "CZPX-1"},
		{Slot: 2, Name: "core-one-light", Host: "b.lan", Printer: "CZPX-1"},
		{Slot: 3, Name: "mk4s", Host: "c.lan", Printer: "CZPX-2"},
	}})

	var (
		logs  bytes.Buffer
		asked int
	)

	f := &follower{
		log: slog.New(slog.NewTextHandler(&logs, nil)),
		serialOf: func(_ context.Context, id string) (string, error) {
			asked++

			if id == "dev1" {
				return "CZPX-1", nil
			}

			return "", errors.New("unknown")
		},
	}

	window := busapi.Window{Events: []json.RawMessage{
		jobEvent(t, event.TypeDeviceJobStarted, "device:dev1"),
		jobEvent(t, event.TypeDeviceJobSettled, "device:dev1"),
		jobEvent(t, event.TypeDeviceJobStarted, "device:dev2"),
		jobEvent(t, event.TypeDeviceJobStarted, "camera:cam1"),
	}}

	if err := f.window(context.Background(), window); err != nil {
		t.Fatal(err)
	}

	out := logs.String()

	if !strings.Contains(out, `plugs=core-one,core-one-light`) {
		t.Fatalf("the plugs of CZPX-1 are not in the log:\n%s", out)
	}

	if strings.Contains(out, "mk4s") {
		t.Fatalf("another printer's plug is in the log:\n%s", out)
	}

	// dev1 is asked once for two events, dev2 once, and a camera never.
	if asked != 2 {
		t.Fatalf("the serial was asked for %d times, want 2", asked)
	}
}
