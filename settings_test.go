package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
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
		"a name with no host":              {{Slot: 1, Name: "a"}},
		"a printer with no host":           {{Slot: 1, Name: "a", Printer: "CZPX-1"}},
		"a host with a port":               {{Slot: 1, Name: "a", Host: "a.lan:80"}},
		"a name with a slash":              {{Slot: 1, Name: "a/b", Host: "a.lan"}},
		"a name with a question":           {{Slot: 1, Name: "a?b", Host: "a.lan"}},
		"a name with a hash":               {{Slot: 1, Name: "a#b", Host: "a.lan"}},
		"a name with a percent":            {{Slot: 1, Name: "100%", Host: "a.lan"}},
		"a name with a backslash":          {{Slot: 1, Name: `a\b`, Host: "a.lan"}},
		"a name with a newline":            {{Slot: 1, Name: "a\nb", Host: "a.lan"}},
		"a name of one dot":                {{Slot: 1, Name: ".", Host: "a.lan"}},
		"a name of two dots":               {{Slot: 1, Name: "..", Host: "a.lan"}},
		"a name with a tab":                {{Slot: 1, Name: "a\tb", Host: "a.lan"}},
		"a name with a space at the start": {{Slot: 1, Name: " a", Host: "a.lan"}},
		"a name with a space at the end":   {{Slot: 1, Name: "a ", Host: "a.lan"}},
		"a long name":                      {{Slot: 1, Name: strings.Repeat("a", 41), Host: "a.lan"}},
		"names differing in case":          {ok, {Slot: 2, Name: "A", Host: "b.lan"}},
		"a duplicate name":                 {ok, {Slot: 2, Name: "a", Host: "b.lan"}},
		"a serial with a space":            {{Slot: 1, Name: "a", Host: "a.lan", Printer: "CZ PX"}},
		"a long serial":                    {{Slot: 1, Name: "a", Host: "a.lan", Printer: strings.Repeat("9", 65)}},
	} {
		if err := (Settings{Plugs: plugs}).validate(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	if err := (Settings{}).validate(); err != nil {
		t.Fatalf("no plugs at all was refused: %v", err)
	}

	two := Settings{Plugs: []plugConfig{ok, {Slot: 2, Name: "b-1", Host: "10.0.0.7", Printer: "CZPX-1"}}}
	friendly := Settings{Plugs: []plugConfig{
		{Slot: 1, Name: "Core One", Host: "a.lan"},
		{Slot: 2, Name: "MK4S (garage)", Host: "b.lan"},
		{Slot: 3, Name: "Drucker Büro", Host: "c.lan"},
		{Slot: 4, Name: strings.Repeat("é", 40), Host: "d.lan"},
		{Slot: 5, Name: "v1.2", Host: "e.lan"},
	}}
	if err := friendly.validate(); err != nil {
		t.Fatalf("display names were refused: %v", err)
	}
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

func TestAutomaticPowerOffIsOffUntilSet(t *testing.T) {
	for _, body := range []string{`{}`, `{"auto_off_minutes":"","auto_off_cooldown_c":"","overheat_nozzle_c":"","overheat_bed_c":""}`, `{"auto_off_minutes":" 0 "}`} {
		s, err := parse(t, body)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}

		if s.Auto.IdleMinutes != 0 || s.Auto.OverheatNozzleC != 0 || s.Auto.OverheatBedC != 0 || s.Auto.cooldown() != 40 {
			t.Fatalf("%s read as %+v, want everything off and a cooldown of 40", body, s.Auto)
		}

		if err := s.validate(); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
	}
}

func TestAutomaticPowerOffSettingsAreRead(t *testing.T) {
	s, err := parse(t, `{"auto_off_minutes":"30","auto_off_cooldown_c":" 35 ","overheat_nozzle_c":"300","overheat_bed_c":"150"}`)
	if err != nil {
		t.Fatal(err)
	}

	if want := (Auto{IdleMinutes: 30, CooldownC: 35, OverheatNozzleC: 300, OverheatBedC: 150}); s.Auto != want {
		t.Fatalf("read %+v, want %+v", s.Auto, want)
	}

	if err := s.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticPowerOffLimitsAreHeldToTheirRanges(t *testing.T) {
	for key, bad := range map[string][]string{
		"auto_off_minutes":    {"-1", "1441", "abc", "1.5", "30 min"},
		"auto_off_cooldown_c": {"19", "101", "-5", "cold"},
		"overheat_nozzle_c":   {"249", "501", "-1", "hot"},
		"overheat_bed_c":      {"99", "201", "-1", "hot"},
	} {
		for _, value := range bad {
			s, err := parse(t, fmt.Sprintf(`{%q:%q}`, key, value))
			if err == nil {
				err = s.validate()
			}

			if err == nil {
				t.Fatalf("%s=%s was accepted", key, value)
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%s was refused without naming the key: %v", key, value, err)
			}
		}
	}

	for key, ok := range map[string][]string{
		"auto_off_minutes":    {"1", "1440"},
		"auto_off_cooldown_c": {"20", "100"},
		"overheat_nozzle_c":   {"250", "500"},
		"overheat_bed_c":      {"100", "200"},
	} {
		for _, value := range ok {
			s, err := parse(t, fmt.Sprintf(`{%q:%q}`, key, value))
			if err == nil {
				err = s.validate()
			}

			if err != nil {
				t.Fatalf("%s=%s was refused: %v", key, value, err)
			}
		}
	}
}

// The ranges are written in Go, in the manifest's summaries and in the page's
// own checks. This holds the last two to the first, so a changed range cannot
// leave the page accepting what the module refuses at start.
func TestTheAutomaticLimitRangesAreTheSameInGoTheManifestAndThePage(t *testing.T) {
	page, err := os.ReadFile("ui/src/plug-setup.svelte")
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatal(err)
	}

	onPage := regexp.MustCompile(`key: '(\w+)',\s+label:[^\n]*\n\s+hint:[^\n]*\n\s+low: (\d+),\s+high: (\d+),`)
	inManifest := regexp.MustCompile(`- key: (\w+)\n\s+type: string\n\s+default: "[^"]*"\n\s+summary: "([^"]*)"`)

	found := map[string][2]int{}

	for _, m := range onPage.FindAllStringSubmatch(string(page), -1) {
		low, _ := strconv.Atoi(m[2])
		high, _ := strconv.Atoi(m[3])
		found[m[1]] = [2]int{low, high}
	}

	summaries := map[string]string{}
	for _, m := range inManifest.FindAllStringSubmatch(string(manifest), -1) {
		summaries[m[1]] = m[2]
	}

	for _, limit := range autoLimits {
		if got := found[limit.key]; got != [2]int{limit.low, limit.high} {
			t.Errorf("the page checks %s against %v, the module against %d to %d", limit.key, got, limit.low, limit.high)
		}

		if want := fmt.Sprintf("%d to %d", limit.low, limit.high); !strings.Contains(summaries[limit.key], want) {
			t.Errorf("the manifest says %q for %s, want it to say %q", summaries[limit.key], limit.key, want)
		}
	}
}
