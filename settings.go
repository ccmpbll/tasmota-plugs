package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// maxPlugs is how many plugs the module can drive. The count is fixed because
// the manifest declares every host a module connects to as egress, and an
// egress entry names one string option: one host each. plug1_host .. plug8_host
// are those options.
const maxPlugs = 8

// defaultCurrency is what a price is written in when none is set.
const defaultCurrency = "$"

// maxPrice bounds a price per kWh, so a typo of a few orders of magnitude is
// refused at start. No electricity costs a thousand of anything per kilowatt-hour.
const maxPrice = 1000.0

// parsePrice reads price_per_kwh: empty is no price.
func parsePrice(text string) (*float64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil //nolint:nilnil // no price is a valid answer.
	}

	price, err := strconv.ParseFloat(text, 64)
	if err != nil || !validPrice(price) {
		return nil, fmt.Errorf("price_per_kwh %q is not a price: a number from 0 to %v", text, maxPrice)
	}

	return &price, nil
}

func validPrice(price float64) bool {
	return !math.IsNaN(price) && price >= 0 && price <= maxPrice
}

// plugConfig is one configured plug: a Tasmota relay, and optionally the
// printer it powers.
type plugConfig struct {
	// Slot is the number in the setting keys, 1 to maxPlugs.
	Slot int
	// Name is how the routes address the plug. It defaults to plug<slot>.
	Name string
	// Host is the plug's host name or IPv4 address.
	Host string
	// Printer is the serial of the printer this plug powers. Empty means no
	// printer is linked.
	Printer string
}

// Settings is what an operator writes under this instance's config block: up
// to maxPlugs slots, each three keys, plug<N>_name, plug<N>_host and
// plug<N>_printer. The host merges it over the defaults module.yaml declares
// and sends the result on plugin.configure.
type Settings struct {
	// Plugs are the slots with anything set, in slot order.
	Plugs []plugConfig
	// PricePerKWH is what a kilowatt-hour costs, so the energy a plug has used
	// can be shown as money. Nil means no price: nothing is priced.
	PricePerKWH *float64
	// Currency is what a price is written in, such as $ or EUR.
	Currency string
}

var (
	slotKey = regexp.MustCompile(`^plug([0-9]+)_(name|host|printer)$`)
	// plugName is a short route-safe name.
	plugName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
)

// UnmarshalJSON reads the flat plug<N>_<field> keys into slots. A key that is
// not one of those, or a slot past maxPlugs, is refused by name, so a typo is
// found at start and not at the first request.
func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("the settings are not a flat map of text values: %w", err)
	}

	slots := map[int]*plugConfig{}

	s.PricePerKWH, s.Currency = nil, defaultCurrency

	for key, value := range raw {
		switch key {
		case "price_per_kwh":
			price, err := parsePrice(value)
			if err != nil {
				return err
			}

			s.PricePerKWH = price

			continue
		case "currency":
			if value = strings.TrimSpace(value); value != "" {
				s.Currency = value
			}

			continue
		}

		match := slotKey.FindStringSubmatch(key)
		if match == nil {
			return fmt.Errorf("%q is not a setting of this module", key)
		}

		slot, err := strconv.Atoi(match[1])
		if err != nil || slot < 1 || slot > maxPlugs {
			return fmt.Errorf("%q names a plug slot outside 1 to %d", key, maxPlugs)
		}

		if slots[slot] == nil {
			slots[slot] = &plugConfig{Slot: slot}
		}

		value = strings.TrimSpace(value)

		switch match[2] {
		case "name":
			slots[slot].Name = value
		case "host":
			slots[slot].Host = value
		case "printer":
			slots[slot].Printer = value
		}
	}

	s.Plugs = nil

	for slot := 1; slot <= maxPlugs; slot++ {
		held := slots[slot]
		if held == nil || (held.Name == "" && held.Host == "" && held.Printer == "") {
			continue
		}

		if held.Name == "" {
			held.Name = "plug" + strconv.Itoa(slot)
		}

		s.Plugs = append(s.Plugs, *held)
	}

	return nil
}

// validate refuses a setting the module cannot run on, so the start fails with
// the reason and not the first request.
func (s Settings) validate() error {
	if s.PricePerKWH != nil && !validPrice(*s.PricePerKWH) {
		return fmt.Errorf("price_per_kwh %v is not a price: a number from 0 to %v", *s.PricePerKWH, maxPrice)
	}

	if n := len([]rune(s.Currency)); s.Currency != "" && (n > 8 || strings.ContainsAny(s.Currency, " \t\r\n")) {
		return fmt.Errorf("currency %q is not a currency symbol: one to eight characters, no spaces", s.Currency)
	}

	names := map[string]int{}

	for _, p := range s.Plugs {
		switch {
		case p.Host == "":
			return fmt.Errorf("plug%d: plug%d_host is empty, and a plug needs one", p.Slot, p.Slot)
		case !plugName.MatchString(p.Name):
			return fmt.Errorf("plug%d_name %q is not a name: lower-case letters, digits and hyphens, at most 32", p.Slot, p.Name)
		case len(p.Printer) > 64 || strings.ContainsAny(p.Printer, " \t\r\n/"):
			return fmt.Errorf("plug%d_printer %q is not a printer serial", p.Slot, p.Printer)
		}

		if _, err := newPlug(p.Host); err != nil {
			return fmt.Errorf("plug%d_host: %w", p.Slot, err)
		}

		if other, dup := names[p.Name]; dup {
			return fmt.Errorf("plug%d_name and plug%d_name are both %q", other, p.Slot, p.Name)
		}

		names[p.Name] = p.Slot
	}

	return nil
}

// find is the plug with this name.
func (s Settings) find(name string) (plugConfig, bool) {
	i := slices.IndexFunc(s.Plugs, func(p plugConfig) bool { return p.Name == name })
	if i < 0 {
		return plugConfig{}, false
	}

	return s.Plugs[i], true
}

// namesFor are the names of the plugs linked to the printer with this serial.
func (s Settings) namesFor(serial string) []string {
	var names []string

	for _, p := range s.Plugs {
		if serial != "" && p.Printer == serial {
			names = append(names, p.Name)
		}
	}

	return names
}
