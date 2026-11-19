package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	firstRetry = 2 * time.Second
	lastRetry  = 30 * time.Second
)

// subscribeFunc opens the host's event stream, as busapi.Subscribe does on the
// module's session. It is a field so a test can feed windows without one.
type subscribeFunc func(ctx context.Context, p busapi.SubscribeParams, onWindow func(busapi.Window) error) error

// follower reads the print events of every printer the module may observe and
// writes one log line for each. It is the place later steps hang behaviour on:
// switch a plug off after a job settles, check a heater reading. It switches
// nothing today.
type follower struct {
	subscribe subscribeFunc
	// serialOf answers a device's serial, which is what links a printer to its
	// plugs in the settings.
	serialOf func(ctx context.Context, deviceID string) (string, error)
	log      *slog.Logger
	cursor   string
	serials  map[string]string
}

// Run follows the stream until ctx ends. A failed subscription is retried with
// a growing wait, and a restart resumes from the last cursor, so a gap is
// reported by the host as Resync and not lost silently.
func (f *follower) Run(ctx context.Context) {
	params := busapi.SubscribeParams{Types: []string{
		event.TypeDeviceJobStarted, event.TypeDeviceJobSettled,
	}}

	for wait := firstRetry; ; {
		params.Resume = f.cursor
		read := false

		err := f.subscribe(ctx, params, func(w busapi.Window) error {
			read = true

			return f.window(ctx, w)
		})

		select {
		case <-ctx.Done():
			return
		default:
			f.log.WarnContext(ctx, "the event stream ended, trying again", "error", err, "wait", wait)
		}

		if read {
			wait = firstRetry
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		wait = min(wait+wait, lastRetry)
	}
}

// window logs each event of one window, with the plugs linked to its printer,
// and keeps the cursor.
func (f *follower) window(ctx context.Context, w busapi.Window) error {
	if w.Resync {
		f.log.Warn("the stream lost continuity: events may have been missed")
	}

	for _, raw := range w.Events {
		var envelope event.Envelope
		if json.Unmarshal(raw, &envelope) != nil {
			continue
		}

		attrs := []any{"type", envelope.Type, "subject", envelope.Subject}

		if serial := f.serial(ctx, envelope.Subject); serial != "" {
			attrs = append(attrs, "serial", serial, "plugs", strings.Join(settings().namesFor(serial), ","))
		}

		f.log.Info("print event", append(attrs, "data", string(envelope.Data))...)
	}

	f.cursor = w.Cursor

	return nil
}

// serial is the serial of the device an event's subject names, or "" when the
// subject is not a device or the host cannot say. A serial is asked for once
// per device and kept.
func (f *follower) serial(ctx context.Context, subject string) string {
	origin, err := event.ParseOrigin(subject)
	if err != nil || origin.Kind() != event.KindDevice || f.serialOf == nil {
		return ""
	}

	if serial, ok := f.serials[origin.ID()]; ok {
		return serial
	}

	serial, err := f.serialOf(ctx, origin.ID())
	if err != nil {
		f.log.Warn("the printer's serial could not be read", "device", origin.ID(), "error", err)

		return ""
	}

	if f.serials == nil {
		f.serials = map[string]string{}
	}

	f.serials[origin.ID()] = serial

	return serial
}
