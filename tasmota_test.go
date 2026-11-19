package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// energyBlock is a meter's reading, shaped as the firmware reports it.
const energyBlock = `{"TotalStartTime":"2026-06-27T00:12:27","Total":17.666,"Yesterday":2.94,"Today":0.131,
"Power":15,"ApparentPower":20,"ReactivePower":12,"Factor":0.78,"Voltage":120,"Current":0.165}`

// statusJSON is a `Status 0` answer: the relay under relayKey, the meter block
// (empty for a plug without one), and the Wi-Fi and uptime a plug always has.
func statusJSON(relayKey, state, energy string) string {
	sns := `{"Time":"2026-11-19T18:00:00"}`
	if energy != "" {
		sns = `{"Time":"2026-11-19T18:00:00","ENERGY":` + energy + `}`
	}

	return `{"Status":{"Module":1,"DeviceName":"Plug","Power":1},"StatusNET":{"Hostname":"plug"},` +
		`"StatusSNS":` + sns + `,` +
		`"StatusSTS":{"Time":"2026-11-19T18:00:00","Uptime":"35T20:08:30","UptimeSec":3096510,` +
		`"Wifi":{"RSSI":100,"Signal":-44},"` + relayKey + `":"` + state + `"}}`
}

// fakePlug answers the Tasmota command interface the way the firmware does:
// GET /cm?cmnd=<command>. reply decides the answer to each command.
func fakePlug(t *testing.T, reply func(cmnd string) (int, string)) (*plug, *[]string) {
	t.Helper()

	seen := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cm" {
			http.NotFound(w, r)

			return
		}

		cmnd := r.URL.Query().Get("cmnd")
		*seen = append(*seen, cmnd)
		status, body := reply(cmnd)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return &plug{base: srv.URL, client: srv.Client()}, seen
}

// answering is a reply that gives every command the same body.
func answering(body string) func(string) (int, string) {
	return func(string) (int, string) { return http.StatusOK, body }
}

func f(v float64) *float64 { return &v }

func TestSetSendsTheCommandAndReadsTheEcho(t *testing.T) {
	p, seen := fakePlug(t, func(cmnd string) (int, string) {
		return http.StatusOK, `{"POWER":"` + strings.ToUpper(strings.TrimPrefix(cmnd, "Power ")) + `"}`
	})

	if err := p.Set(context.Background(), true); err != nil {
		t.Fatalf("Set(on): %v", err)
	}

	if err := p.Set(context.Background(), false); err != nil {
		t.Fatalf("Set(off): %v", err)
	}

	if got := strings.Join(*seen, "|"); got != "Power On|Power Off" {
		t.Fatalf("the plug was sent %q, want Power On|Power Off", got)
	}
}

func TestReadAsksOnceAndReturnsStateEnergySignalAndUptime(t *testing.T) {
	p, seen := fakePlug(t, answering(statusJSON("POWER1", "ON", energyBlock)))

	got, err := p.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(*seen) != 1 || (*seen)[0] != "Status 0" {
		t.Fatalf("Read sent %v, want exactly one Status 0", *seen)
	}

	e := got.Energy
	if !got.On || e == nil {
		t.Fatalf("read %+v", got)
	}

	for name, pair := range map[string][2]*float64{
		"watts": {e.Watts, f(15)}, "volts": {e.Volts, f(120)}, "amps": {e.Amps, f(0.165)},
		"power factor": {e.PowerFactor, f(0.78)}, "today": {e.TodayKWh, f(0.131)},
		"yesterday": {e.YesterdayKWh, f(2.94)}, "total": {e.TotalKWh, f(17.666)},
	} {
		if pair[0] == nil || *pair[0] != *pair[1] {
			t.Fatalf("%s read as %v, want %v", name, pair[0], *pair[1])
		}
	}

	if got.SignalDBm == nil || *got.SignalDBm != -44 || got.UptimeSeconds == nil || *got.UptimeSeconds != 3096510 {
		t.Fatalf("signal %v and uptime %v read wrongly", got.SignalDBm, got.UptimeSeconds)
	}
}

func TestReadSeesBothRelayKeysAndBothStates(t *testing.T) {
	for _, tc := range []struct {
		key, state string
		want       bool
	}{{"POWER", "ON", true}, {"POWER", "OFF", false}, {"POWER1", "ON", true}, {"POWER1", "off", false}} {
		p, _ := fakePlug(t, answering(statusJSON(tc.key, tc.state, "")))

		got, err := p.Read(context.Background())
		if err != nil || got.On != tc.want {
			t.Fatalf("%s %s: got %v, %v; want %v", tc.key, tc.state, got.On, err, tc.want)
		}
	}
}

// A plug with no meter is a plug, not an error: the energy is simply absent.
func TestAPlugWithoutAMeterHasNoEnergy(t *testing.T) {
	p, _ := fakePlug(t, answering(statusJSON("POWER", "ON", "")))

	got, err := p.Read(context.Background())
	if err != nil || got.Energy != nil || !got.On {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// One odd field costs only itself: a three-phase meter sends Power as a list.
func TestAMeterFieldThatIsNotANumberIsDroppedAndTheRestKept(t *testing.T) {
	odd := `{"Power":[1,2,3],"Voltage":"n/a","Today":0.5,"Total":null}`
	p, _ := fakePlug(t, answering(statusJSON("POWER", "ON", odd)))

	got, err := p.Read(context.Background())
	if err != nil || got.Energy == nil {
		t.Fatalf("got %+v, %v", got, err)
	}

	if got.Energy.Watts != nil || got.Energy.Volts != nil || got.Energy.TotalKWh != nil {
		t.Fatalf("a field that is not a number was kept: %+v", got.Energy)
	}

	if got.Energy.TodayKWh == nil || *got.Energy.TodayKWh != 0.5 {
		t.Fatalf("the good field was lost: %+v", got.Energy)
	}
}

func TestReadRefusesWhatIsNotAStatus(t *testing.T) {
	for _, body := range []string{
		`{}`, `not json`, `[]`, `{"StatusSTS":{}}`, `{"StatusSTS":{"POWER":"MAYBE"}}`,
		`{"StatusSTS":{"POWER":true}}`, `{"Command":"Unknown"}`,
	} {
		p, _ := fakePlug(t, answering(body))

		if _, err := p.Read(context.Background()); err == nil {
			t.Fatalf("%q was read as a status", body)
		}
	}
}

// PrintSpy's Tasmota client once treated any HTTP response as success, so a
// 500 page or an echo of the opposite state read as a plug that had switched.
func TestAnErrorStatusIsAnErrorNotASwitchOrAReading(t *testing.T) {
	p, _ := fakePlug(t, func(string) (int, string) {
		return http.StatusInternalServerError, statusJSON("POWER", "ON", energyBlock)
	})

	if err := p.Set(context.Background(), true); err == nil {
		t.Fatal("a 500 answered Set(on) as success")
	}

	if _, err := p.Read(context.Background()); err == nil {
		t.Fatal("a 500 was read as a status")
	}
}

func TestAnEchoOfTheOppositeStateIsAnError(t *testing.T) {
	p, _ := fakePlug(t, answering(`{"POWER":"OFF"}`))

	if err := p.Set(context.Background(), true); err == nil {
		t.Fatal("an OFF echo to Power On was accepted")
	}
}

func TestAnEchoWithoutAPowerKeyIsNotASwitch(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, `{"POWER":"MAYBE"}`, `{"Command":"Unknown"}`} {
		p, _ := fakePlug(t, answering(body))

		if err := p.Set(context.Background(), true); err == nil {
			t.Fatalf("%q was accepted as a switch", body)
		}
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(statusJSON("POWER", "ON", "")))
	}))
	t.Cleanup(elsewhere.Close)

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)

	p := &plug{base: redirecting.URL, client: noRedirects(redirecting.Client())}
	if _, err := p.Read(context.Background()); err == nil {
		t.Fatal("a redirect was followed to another host")
	}
}

func TestNewPlugRefusesWhatIsNotAHost(t *testing.T) {
	for _, host := range []string{
		"", " ", "10.0.0.5/admin", "user@10.0.0.5", "10.0.0.5:80", "evil.example.com/x?y=1",
		"-bad.example", "bad-.example", "a..b", ".lan", "a b", "http://10.0.0.5",
		strings.Repeat("a", 64), strings.Repeat("a.", 128) + "a", "[::1]",
	} {
		if _, err := newPlug(host); err == nil {
			t.Fatalf("newPlug accepted %q", host)
		}
	}

	for _, host := range []string{"192.168.1.30", "plug-printer.lan", "tasmota-1", strings.Repeat("a", 63)} {
		if _, err := newPlug(host); err != nil {
			t.Fatalf("newPlug refused %q: %v", host, err)
		}
	}
}

func TestAnOversizedAnswerIsRefused(t *testing.T) {
	big := `{"StatusSTS":{"POWER":"ON"},"pad":"` + strings.Repeat("x", 64<<10) + `"}`
	p, _ := fakePlug(t, answering(big))

	if _, err := p.Read(context.Background()); err == nil {
		t.Fatal("a 64 KiB answer was accepted")
	}
}

// A real plug's Status 0 is about 4 KiB, well inside the limit.
func TestARealSizedStatusFitsTheLimit(t *testing.T) {
	padded := statusJSON("POWER1", "ON", energyBlock)
	padded = strings.Replace(padded, `"StatusNET":{"Hostname":"plug"}`, `"StatusNET":{"Hostname":"plug","pad":"`+strings.Repeat("x", 3000)+`"}`, 1)

	p, _ := fakePlug(t, answering(padded))
	if _, err := p.Read(context.Background()); err != nil {
		t.Fatalf("a %d byte status was refused: %v", len(padded), err)
	}
}
