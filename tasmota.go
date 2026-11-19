package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// maxPlugAnswer bounds what is read from a plug. A real `Status 0` answer is
// about 4 KiB, and a plug with several sensors is longer, so there is room; an
// answer past this is cut, and a cut answer is not JSON.
const maxPlugAnswer = 32 << 10

// plug is one Tasmota relay, reached over the firmware's HTTP command
// interface: GET /cm?cmnd=<command>.
type plug struct {
	base   string
	client *http.Client
}

// Energy is what a plug with an energy meter reports. A field the plug does not
// send, or sends as something that is not a number, is nil.
type Energy struct {
	Watts        *float64 `json:"watts,omitempty"`
	Volts        *float64 `json:"volts,omitempty"`
	Amps         *float64 `json:"amps,omitempty"`
	PowerFactor  *float64 `json:"power_factor,omitempty"`
	TodayKWh     *float64 `json:"today_kwh,omitempty"`
	YesterdayKWh *float64 `json:"yesterday_kwh,omitempty"`
	TotalKWh     *float64 `json:"total_kwh,omitempty"`
}

// Reading is everything one `Status 0` call tells: whether the relay is on, the
// energy meter if the plug has one, the Wi-Fi signal and how long it has been up.
type Reading struct {
	On            bool
	Energy        *Energy
	SignalDBm     *int
	UptimeSeconds *int64
}

// hostPattern is a DNS name or an IPv4 address, with no port, path, user or
// scheme. The host comes from the operator's setting, so this is a typo guard
// and not a boundary: the module's egress entry in module.yaml names the same
// setting and port 80.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// plugClient is shared by every call, so connections are reused. It follows no
// redirect: a plug that answers 3xx is not one the operator configured.
//
//nolint:gochecknoglobals // one client for the process.
var plugClient = noRedirects(&http.Client{Timeout: 5 * time.Second})

func noRedirects(c *http.Client) *http.Client {
	clone := *c
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &clone
}

// newPlug builds the plug at host, or says why host is not one.
func newPlug(host string) (*plug, error) {
	if len(host) > 253 || !hostPattern.MatchString(host) {
		return nil, fmt.Errorf("%q is not a host name or an IPv4 address", host)
	}

	return &plug{base: "http://" + host, client: plugClient}, nil
}

// Read asks the plug for everything at once with `Status 0`: the relay state,
// the energy meter, the signal and the uptime. It is one request where the
// state and the energy would otherwise be two.
func (p *plug) Read(ctx context.Context) (Reading, error) {
	raw, err := p.fetch(ctx, "Status 0")
	if err != nil {
		return Reading{}, err
	}

	var top map[string]json.RawMessage
	if err = json.Unmarshal(raw, &top); err != nil {
		return Reading{}, fmt.Errorf("the plug's answer is not JSON: %w", err)
	}

	// Each group is read on its own, so a group that is missing or odd costs only
	// what it carries. The relay state is POWER on a one-relay plug and POWER1 on
	// others, and sits under StatusSTS.
	var (
		sts map[string]json.RawMessage
		sns struct {
			Energy map[string]json.RawMessage `json:"ENERGY"`
		}
		wifi struct {
			Signal *int `json:"Signal"`
		}
		uptime *int64
	)

	_ = json.Unmarshal(top["StatusSTS"], &sts)
	_ = json.Unmarshal(top["StatusSNS"], &sns)
	_ = json.Unmarshal(sts["Wifi"], &wifi)
	_ = json.Unmarshal(sts["UptimeSec"], &uptime)

	on, ok := relayState(sts)
	if !ok {
		return Reading{}, errors.New("the plug's answer carries no POWER state")
	}

	return Reading{On: on, Energy: energyOf(sns.Energy), SignalDBm: wifi.Signal, UptimeSeconds: uptime}, nil
}

// relayState reads ON or OFF out of POWER or POWER1.
func relayState(sts map[string]json.RawMessage) (on, ok bool) {
	for _, key := range []string{"POWER", "POWER1"} {
		var word string
		if json.Unmarshal(sts[key], &word) != nil {
			continue
		}

		switch strings.ToUpper(word) {
		case "ON":
			return true, true
		case "OFF":
			return false, true
		}
	}

	return false, false
}

// energyOf reads the meter. A plug with no meter has no ENERGY block, which is
// nil here and not an error.
func energyOf(block map[string]json.RawMessage) *Energy {
	if len(block) == 0 {
		return nil
	}

	number := func(key string) *float64 {
		// null would decode as 0, which is a reading, so it is refused first.
		var v float64
		if string(block[key]) == "null" || json.Unmarshal(block[key], &v) != nil {
			return nil
		}

		return &v
	}

	return &Energy{
		Watts:        number("Power"),
		Volts:        number("Voltage"),
		Amps:         number("Current"),
		PowerFactor:  number("Factor"),
		TodayKWh:     number("Today"),
		YesterdayKWh: number("Yesterday"),
		TotalKWh:     number("Total"),
	}
}

// Set switches the relay and checks the plug says it did. A plug that answers
// the opposite state, or anything but 200, is an error and not a switch.
func (p *plug) Set(ctx context.Context, on bool) error {
	command, want := "Power Off", false
	if on {
		command, want = "Power On", true
	}

	raw, err := p.fetch(ctx, command)
	if err != nil {
		return err
	}

	var echo map[string]json.RawMessage
	if err = json.Unmarshal(raw, &echo); err != nil {
		return fmt.Errorf("the plug's answer is not JSON: %w", err)
	}

	got, ok := relayState(echo)
	if !ok {
		return errors.New("the plug's answer carries no POWER state")
	}

	if got != want {
		return fmt.Errorf("sent %q and the plug answered the other state", command)
	}

	return nil
}

// fetch sends one Tasmota command and returns the body of a 200 answer, cut at
// maxPlugAnswer.
func (p *plug) fetch(ctx context.Context, command string) ([]byte, error) {
	// Tasmota reads a space in the command as %20.
	target := p.base + "/cm?cmnd=" + strings.ReplaceAll(url.QueryEscape(command), "+", "%20")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the plug answered %s", resp.Status)
	}

	// A longer answer is cut here, and a cut answer is not JSON.
	return io.ReadAll(io.LimitReader(resp.Body, maxPlugAnswer))
}
