package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func parse(t *testing.T, body string) (Settings, error) {
	t.Helper()

	var s Settings

	return s, json.Unmarshal([]byte(body), &s)
}

func TestSlotsAreReadInOrderAndEmptyOnesAreDropped(t *testing.T) {
	s, err := parse(t, `{"plug2_host":"b.lan","plug2_name":"mk4s","plug2_printer":"CZPX-2",
		"plug1_host":"a.lan","plug3_host":"","plug3_name":"","plug3_printer":"","plug8_host":"h.lan"}`)
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Plugs) != 3 || s.Plugs[0].Slot != 1 || s.Plugs[1].Slot != 2 || s.Plugs[2].Slot != 8 {
		t.Fatalf("slots read as %+v", s.Plugs)
	}

	// A plug with no name is named after its slot.
	if s.Plugs[0].Name != "plug1" || s.Plugs[1].Name != "mk4s" {
		t.Fatalf("names read as %q and %q", s.Plugs[0].Name, s.Plugs[1].Name)
	}
}

func TestUnknownKeysAndOutOfRangeSlotsAreRefusedByName(t *testing.T) {
	for _, body := range []string{
		`{"plug_host":"a.lan"}`, `{"plug0_host":"a.lan"}`, `{"plug9_host":"a.lan"}`,
		`{"plug1_hots":"a.lan"}`, `{"plug1_host":5}`, `[]`, `{"plug99999999999999999999_host":"x"}`,
	} {
		if _, err := parse(t, body); err == nil {
			t.Fatalf("%s was accepted", body)
		}
	}
}

func TestValidateRefusesWhatCannotRun(t *testing.T) {
	ok := plugConfig{Slot: 1, Name: "a", Host: "a.lan"}

	for name, plugs := range map[string][]plugConfig{
		"a name with no host":    {{Slot: 1, Name: "a"}},
		"a printer with no host": {{Slot: 1, Name: "a", Printer: "CZPX-1"}},
		"a host with a port":     {{Slot: 1, Name: "a", Host: "a.lan:80"}},
		"a bad name":             {{Slot: 1, Name: "Not A Name", Host: "a.lan"}},
		"a name with a slash":    {{Slot: 1, Name: "a/b", Host: "a.lan"}},
		"a long name":            {{Slot: 1, Name: strings.Repeat("a", 33), Host: "a.lan"}},
		"a duplicate name":       {ok, {Slot: 2, Name: "a", Host: "b.lan"}},
		"a serial with a space":  {{Slot: 1, Name: "a", Host: "a.lan", Printer: "CZ PX"}},
		"a long serial":          {{Slot: 1, Name: "a", Host: "a.lan", Printer: strings.Repeat("9", 65)}},
	} {
		if err := (Settings{Plugs: plugs}).validate(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	if err := (Settings{}).validate(); err != nil {
		t.Fatalf("no plugs at all was refused: %v", err)
	}

	two := Settings{Plugs: []plugConfig{ok, {Slot: 2, Name: "b-1", Host: "10.0.0.7", Printer: "CZPX-1"}}}
	if err := two.validate(); err != nil {
		t.Fatalf("two good plugs were refused: %v", err)
	}
}

func TestNamesForOnlyMatchesAnExactNonEmptySerial(t *testing.T) {
	s := Settings{Plugs: []plugConfig{
		{Name: "a", Printer: "CZPX-1"}, {Name: "b", Printer: "CZPX-1"}, {Name: "c", Printer: ""}, {Name: "d", Printer: "CZPX-10"},
	}}

	if got := strings.Join(s.namesFor("CZPX-1"), ","); got != "a,b" {
		t.Fatalf("CZPX-1 matched %q, want a,b", got)
	}

	if got := s.namesFor(""); len(got) != 0 {
		t.Fatalf("an empty serial matched %v: every unlinked plug would match", got)
	}
}

// A slot with a name or a printer but no host is the likeliest mistake, so the
// error says which key is empty rather than that "" is not a host.
func TestAMissingHostNamesTheKeyToFill(t *testing.T) {
	err := (Settings{Plugs: []plugConfig{{Slot: 3, Name: "mk4s", Printer: "CZPX-2"}}}).validate()
	if err == nil || !strings.Contains(err.Error(), "plug3_host is empty") {
		t.Fatalf("the error is %v, want one naming plug3_host as empty", err)
	}
}

func TestPriceAndCurrencyAreReadAndHeldToRules(t *testing.T) {
	s, err := parse(t, `{"plug1_host":"a.lan","price_per_kwh":"0.14","currency":"€"}`)
	if err != nil || s.PricePerKWH == nil || *s.PricePerKWH != 0.14 || s.Currency != "€" {
		t.Fatalf("read %+v, %v", s, err)
	}

	// No price and no currency: nothing is priced, and the currency is the default.
	s, err = parse(t, `{"plug1_host":"a.lan","price_per_kwh":"","currency":""}`)
	if err != nil || s.PricePerKWH != nil || s.Currency != defaultCurrency {
		t.Fatalf("read %+v, %v", s, err)
	}

	for _, bad := range []string{"abc", "-0.01", "1001", "1e9", "NaN", "Inf", "0,14", "$0.14"} {
		if _, err = parse(t, `{"price_per_kwh":"`+bad+`"}`); err == nil {
			t.Fatalf("price %q was accepted", bad)
		}
	}

	for _, currency := range []string{"toolongcur", "U SD"} {
		if err = (Settings{Currency: currency}).validate(); err == nil {
			t.Fatalf("currency %q was accepted", currency)
		}
	}

	for _, currency := range []string{"", "$", "€", "kr", "CAD", "12345678"} {
		if err = (Settings{Currency: currency}).validate(); err != nil {
			t.Fatalf("currency %q was refused: %v", currency, err)
		}
	}

	neg := -1.0
	if err = (Settings{PricePerKWH: &neg}).validate(); err == nil {
		t.Fatal("a negative price passed validate")
	}
}
