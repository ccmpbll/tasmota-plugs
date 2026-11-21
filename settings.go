package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// keyPlugs is the one setting that lists the plugs, as JSON text:
// [{"name":"Core One","host":"192.168.1.30","printer":"CZPX1234"}, ...]. The
// list has no length limit because it is one setting. An egress entry names one
// string option, so the manifest's one entry for this option covers every host
// written in it.
const keyPlugs = "plugs"

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

// Automatic power-off. Each limit is a whole number the module refuses outside
// its range, so a typo cannot cut a print's power in the middle of normal use:
// the overheat limits sit above anything a Prusa printer does on purpose.
const (
	maxIdleMinutes    = 24 * 60
	minCooldownC      = 20
	maxCooldownC      = 100
	defaultCooldownC  = 40
	minOverheatNozzle = 250
	maxOverheatNozzle = 500
	minOverheatBed    = 100
	maxOverheatBed    = 200
)

// Auto is what the module does to a plug on its own, for the plugs that are
// linked to a printer. Each part is off until it is set.
type Auto struct {
	// IdleMinutes cuts a printer's plugs once it has been idle this long and has
	// cooled to CooldownC. Zero is off.
	IdleMinutes int
	// CooldownC is how cool the hotend and the bed must be, in degrees, before an
	// idle printer is cut. It has a default, so a printer is never cut hot.
	CooldownC int
	// OverheatNozzleC and OverheatBedC cut a printer's plugs, in any state, when
	// a heater reads above them. Zero is off. A second layer: the printer's own
	// firmware stops a runaway heater first.
	OverheatNozzleC int
	OverheatBedC    int
}

// parseWhole reads a whole number, or zero for empty. Whether it is in range is
// validate's to say, so the rule is written once.
func parseWhole(key, text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a whole number", key, text)
	}

	return n, nil
}

// cooldown is CooldownC, or the default when none is set.
func (a Auto) cooldown() int {
	if a.CooldownC == 0 {
		return defaultCooldownC
	}

	return a.CooldownC
}

// autoLimit is one of the four settings: its key, the range it must be in when
// set, and the field it fills. The one place that lists them, so the key list, the
// reader and the range check cannot drift; a test holds the page and the manifest
// to these ranges.
type autoLimit struct {
	key       string
	low, high int
	field     func(*Auto) *int
}

//nolint:gochecknoglobals // a fixed table, read-only.
var autoLimits = []autoLimit{
	{"auto_off_minutes", 1, maxIdleMinutes, func(a *Auto) *int { return &a.IdleMinutes }},
	{"auto_off_cooldown_c", minCooldownC, maxCooldownC, func(a *Auto) *int { return &a.CooldownC }},
	{"overheat_nozzle_c", minOverheatNozzle, maxOverheatNozzle, func(a *Auto) *int { return &a.OverheatNozzleC }},
	{"overheat_bed_c", minOverheatBed, maxOverheatBed, func(a *Auto) *int { return &a.OverheatBedC }},
}

// autoLimitFor is the setting a key names.
func autoLimitFor(key string) (autoLimit, bool) {
	for _, limit := range autoLimits {
		if limit.key == key {
			return limit, true
		}
	}

	return autoLimit{}, false
}

// validate holds each limit to its range. Zero is off, and for the cooldown the
// default.
func (a Auto) validate() error {
	for _, limit := range autoLimits {
		value := *limit.field(&a)
		if value == 0 {
			continue
		}

		if value < limit.low || value > limit.high {
			return fmt.Errorf("%s %d is not allowed: a whole number from %d to %d, or empty for off", limit.key, value, limit.low, limit.high)
		}
	}

	return nil
}

// plugConfig is one configured plug: a Tasmota relay, and optionally the
// printer it powers.
type plugConfig struct {
	// Slot is the plug's place in the list, from 1.
	Slot int
	// Name is how the routes address the plug. It defaults to plug<slot>.
	Name string
	// Host is the plug's host name or IPv4 address.
	Host string
	// Printer is the serial of the printer this plug powers. Empty means no
	// printer is linked.
	Printer string
}

// Settings is what an operator writes under this instance's config block: the
// plugs setting and the few others module.yaml declares. The host merges it over
// the defaults module.yaml declares and sends the result on plugin.configure.
type Settings struct {
	// Plugs are the plugs in the order the list gives them.
	Plugs []plugConfig
	// PricePerKWH is what a kilowatt-hour costs, so the energy a plug has used
	// can be shown as money. Nil means no price: nothing is priced.
	PricePerKWH *float64
	// Currency is what a price is written in, such as $ or EUR.
	Currency string
	// Auto is the module's own power-off rules.
	Auto Auto
}

// plugEntry is one element of the plugs setting.
type plugEntry struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Printer string `json:"printer"`
}

// parsePlugs reads the plugs setting: empty is no plugs, and anything else is a
// JSON list of {name, host, printer}. A key it does not know is refused, so a
// typo such as "hots" is found at start and not at the first request.
func parsePlugs(text string) ([]plugConfig, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}

	var entries []plugEntry

	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf(`%s is not a list of {"name", "host", "printer"}: %w`, keyPlugs, err)
	}

	// More() would say false for a stray ] or }, so the rest is read as a value:
	// anything but the end of the text is text after the list.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s has text after the list", keyPlugs)
	}

	plugs := make([]plugConfig, 0, len(entries))

	for i, entry := range entries {
		held := plugConfig{
			Slot:    i + 1,
			Name:    strings.TrimSpace(entry.Name),
			Host:    strings.TrimSpace(entry.Host),
			Printer: strings.TrimSpace(entry.Printer),
		}

		if held.Name == "" {
			held.Name = "plug" + strconv.Itoa(held.Slot)
		}

		plugs = append(plugs, held)
	}

	return plugs, nil
}

// UnmarshalJSON reads the settings. A key that is not one of this module's is
// refused by name, so a typo is found at start and not at the first request.
func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("the settings are not a flat map of text values: %w", err)
	}

	s.PricePerKWH, s.Currency, s.Plugs = nil, defaultCurrency, nil

	for key, value := range raw {
		switch key {
		case keyPlugs:
			plugs, err := parsePlugs(value)
			if err != nil {
				return err
			}

			s.Plugs = plugs
		case "price_per_kwh":
			price, err := parsePrice(value)
			if err != nil {
				return err
			}

			s.PricePerKWH = price
		case "currency":
			if value = strings.TrimSpace(value); value != "" {
				s.Currency = value
			}
		default:
			limit, ok := autoLimitFor(key)
			if !ok {
				return fmt.Errorf("%q is not a setting of this module", key)
			}

			n, err := parseWhole(key, value)
			if err != nil {
				return err
			}

			*limit.field(&s.Auto) = n
		}
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

	if err := s.Auto.validate(); err != nil {
		return err
	}

	names := map[string]int{}

	for _, p := range s.Plugs {
		switch {
		case p.Host == "":
			return fmt.Errorf("plug %d (%s): the host is empty, and a plug needs one", p.Slot, p.Name)
		case !validName(p.Name):
			return fmt.Errorf("plug %d: the name %q is not a name: one to 40 characters, with no slash, question mark, hash or percent sign, and no space at either end", p.Slot, p.Name)
		case len(p.Printer) > 64 || strings.ContainsAny(p.Printer, " \t\r\n/"):
			return fmt.Errorf("plug %d (%s): the printer %q is not a printer serial", p.Slot, p.Name, p.Printer)
		}

		if _, err := newPlug(p.Host); err != nil {
			return fmt.Errorf("plug %d (%s): the host: %w", p.Slot, p.Name, err)
		}

		// Capitals do not make two names different: "Core One" and "core one"
		// would read as the same plug.
		key := strings.ToLower(p.Name)
		if other, dup := names[key]; dup {
			return fmt.Errorf("plugs %d and %d are both named %q", other, p.Slot, p.Name)
		}

		names[key] = p.Slot
	}

	return nil
}

// validName holds a plug's name to what a person can read and a route can carry:
// text of one to 40 characters, trimmed, with nothing that ends or splits a path
// and not only dots, which a path reads as "here" and "up".
func validName(name string) bool {
	n := len([]rune(name))

	return n >= 1 && n <= 40 &&
		name == strings.TrimSpace(name) &&
		strings.Trim(name, ".") != "" &&
		!strings.ContainsAny(name, "/\\?#%") &&
		!strings.ContainsFunc(name, unicode.IsControl)
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
