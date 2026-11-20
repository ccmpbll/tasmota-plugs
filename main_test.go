package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// do calls a route as an operator, the person every test but the access tests
// is. The host decides who that is, so the answer is faked: user:operator may
// switch a plug, anyone else may not.
func do(method, path, body string) *httptest.ResponseRecorder {
	return doAs("user:operator", method, path, body)
}

// doAs calls a route as the principal the host named, through the same
// request the host's routes.serve call builds. An empty principal is a request
// the host attributed to nobody.
func doAs(principal, method, path, body string) *httptest.ResponseRecorder {
	request, err := routesapi.Request{Method: method, Path: path, Body: []byte(body), Principal: principal}.HTTP(context.Background())
	if err != nil {
		panic(err)
	}

	recorder := httptest.NewRecorder()
	routes().ServeHTTP(recorder, request)

	return recorder
}

func TestMain(m *testing.M) {
	mayOperate = func(_ context.Context, principal string) (bool, error) {
		if principal == "user:broken" {
			return false, errors.New("host busy")
		}

		return principal == "user:operator", nil
	}

	os.Exit(m.Run())
}

// code reads the registered code out of a refusal. A client branches on the
// code, so tests read it and not the sentence.
func code(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("the refusal is %s, want application/problem+json", got)
	}

	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}

	return problem.Code
}

// withSettings runs a test with the given settings and puts the old ones back.
func withSettings(t *testing.T, s Settings) {
	t.Helper()

	old := held.Load()

	held.Store(&s)
	t.Cleanup(func() { held.Store(old) })
}

// withFakePlugs points the routes at fake Tasmota plugs, one per host. Each
// fake keeps its own relay state and answers like the firmware.
func withFakePlugs(t *testing.T, plugs ...plugConfig) map[string]*string {
	t.Helper()

	states := map[string]*string{}
	bases := map[string]*httptest.Server{}

	for _, p := range plugs {
		state := "OFF"
		states[p.Host] = &state

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("cmnd") {
			case "Power On":
				state = "ON"
				_, _ = w.Write([]byte(`{"POWER":"ON"}`))

				return
			case "Power Off":
				state = "OFF"
				_, _ = w.Write([]byte(`{"POWER":"OFF"}`))

				return
			}

			_, _ = w.Write([]byte(statusJSON("POWER1", state, energyBlock)))
		}))
		t.Cleanup(srv.Close)

		bases[p.Host] = srv
	}

	old := plugAt
	plugAt = func(host string) (*plug, error) {
		srv, ok := bases[host]
		if !ok {
			return newPlug(host)
		}

		return &plug{base: srv.URL, client: srv.Client()}, nil
	}

	t.Cleanup(func() { plugAt = old })
	withSettings(t, Settings{Plugs: plugs})

	return states
}

func TestAPathThisModuleDoesNotServeIsRefusedByCode(t *testing.T) {
	recorder := do(http.MethodGet, "/nothing-here", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("an unserved path answered %d, want %d", recorder.Code, http.StatusNotFound)
	}

	if got, want := code(t, recorder), RouteNotFound.Code().String(); got != want {
		t.Fatalf("the refusal carries %q, want %q", got, want)
	}
}

var twoPlugs = []plugConfig{
	{Slot: 1, Name: "core-one", Host: "core.lan", Printer: "CZPX-1"},
	{Slot: 2, Name: "mk4s", Host: "mk4.lan", Printer: "CZPX-2"},
}

func TestAnUnknownPlugNameIsRefusedOnEveryRoute(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/plugs/nope", ""},
		{http.MethodPost, "/plugs/nope/power", `{"on":true}`},
	} {
		recorder := do(tc.method, tc.path, tc.body)

		if recorder.Code != http.StatusNotFound || code(t, recorder) != PlugNotFound.Code().String() {
			t.Fatalf("%s %s answered %d, want 404 PLUG_NOT_FOUND", tc.method, tc.path, recorder.Code)
		}
	}
}

func TestNoPlugsConfiguredListsNothing(t *testing.T) {
	withSettings(t, Settings{})

	recorder := do(http.MethodGet, "/plugs", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"items":[]`) && !strings.Contains(recorder.Body.String(), `"items":null`) {
		t.Fatalf("GET /plugs with none configured answered %d %s", recorder.Code, recorder.Body)
	}
}

func TestAMalformedPowerBodyIsRefusedBeforeThePlugIsTouched(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)

	for _, body := range []string{strings.Repeat(" ", 2<<10) + `{"on":true}`, ``, `{}`, `{"on":"yes"}`, `{"on":true,"extra":1}`, `not json`, `{"on":null}`} {
		recorder := do(http.MethodPost, "/plugs/core-one/power", body)

		if recorder.Code != http.StatusBadRequest || code(t, recorder) != InvalidRequest.Code().String() {
			t.Fatalf("body %q answered %d, want 400 INVALID_REQUEST", body, recorder.Code)
		}
	}

	if *states["core.lan"] != "OFF" {
		t.Fatal("a refused body switched the plug")
	}
}

// The point of the module: each plug switches on its own, and one printer's
// plug never moves another's.
func TestEachPlugSwitchesIndependently(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)

	if recorder := do(http.MethodPost, "/plugs/mk4s/power", `{"on":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("power on answered %d: %s", recorder.Code, recorder.Body)
	}

	if *states["mk4.lan"] != "ON" || *states["core.lan"] != "OFF" {
		t.Fatalf("mk4s on moved the wrong plug: mk4=%s core=%s", *states["mk4.lan"], *states["core.lan"])
	}

	var one struct {
		Name    string `json:"name"`
		Printer string `json:"printer"`
		On      bool   `json:"on"`
	}

	recorder := do(http.MethodGet, "/plugs/mk4s", "")
	if err := json.Unmarshal(recorder.Body.Bytes(), &one); err != nil || one.Name != "mk4s" || one.Printer != "CZPX-2" || !one.On {
		t.Fatalf("GET /plugs/mk4s answered %s", recorder.Body)
	}

	if recorder = do(http.MethodPost, "/plugs/core-one/power", `{"on":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("power on answered %d", recorder.Code)
	}

	if recorder = do(http.MethodPost, "/plugs/mk4s/power", `{"on":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("power off answered %d", recorder.Code)
	}

	if *states["mk4.lan"] != "OFF" || *states["core.lan"] != "ON" {
		t.Fatalf("final states mk4=%s core=%s, want OFF and ON", *states["mk4.lan"], *states["core.lan"])
	}
}

func TestTheListShowsEveryPlugAndOneDeadPlugDoesNotHideTheRest(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	// A third plug whose host nothing answers: the list keeps it as an error row.
	s := *held.Load()
	s.Plugs = append(s.Plugs, plugConfig{Slot: 3, Name: "dead", Host: "127.0.0.1"})
	held.Store(&s)

	recorder := do(http.MethodGet, "/plugs", "")

	var list struct {
		Items []struct {
			Name  string `json:"name"`
			On    *bool  `json:"on"`
			Error string `json:"error"`
		} `json:"items"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || len(list.Items) != 3 {
		t.Fatalf("the list is %s", recorder.Body)
	}

	for i, want := range []string{"core-one", "mk4s", "dead"} {
		if list.Items[i].Name != want {
			t.Fatalf("row %d is %q, want %q (slot order)", i, list.Items[i].Name, want)
		}
	}

	if list.Items[0].On == nil || list.Items[1].On == nil || list.Items[2].On != nil || list.Items[2].Error == "" {
		t.Fatalf("rows read as %+v", list.Items)
	}
}

// The plug says ON to a request for OFF: that is not a switch.
func TestAPlugThatEchoesTheWrongStateIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"POWER":"ON"}`)) }))
	t.Cleanup(srv.Close)

	withSettings(t, Settings{Plugs: []plugConfig{{Slot: 1, Name: "stuck", Host: "stuck.lan"}}})

	old := plugAt
	plugAt = func(string) (*plug, error) { return &plug{base: srv.URL, client: srv.Client()}, nil }

	t.Cleanup(func() { plugAt = old })

	recorder := do(http.MethodPost, "/plugs/stuck/power", `{"on":false}`)
	if recorder.Code != http.StatusBadGateway || code(t, recorder) != PlugUnreachable.Code().String() {
		t.Fatalf("answered %d, want 502 PLUG_UNREACHABLE", recorder.Code)
	}
}

func TestOnlyTheDeclaredMethodsAreServed(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/plugs"}, {http.MethodGet, "/plugs/mk4s/power"}, {http.MethodDelete, "/plugs/mk4s"},
		{http.MethodGet, "/plug"},
	} {
		if recorder := do(tc.method, tc.path, ""); recorder.Code < 400 {
			t.Fatalf("%s %s answered %d", tc.method, tc.path, recorder.Code)
		}
	}
}

// TestTheManifestDeclaresEverySlotAndItsEgress holds module.yaml to the code:
// every key the settings accept is a declared option, and every plug host is a
// declared connection on port 80, so the operator's list is the whole list.
func TestTheManifestDeclaresEverySlotAndItsEgress(t *testing.T) {
	manifest, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	text := string(manifest)

	for slot := 1; slot <= maxPlugs; slot++ {
		for _, field := range []string{"name", "host", "printer"} {
			if key := fmt.Sprintf("plug%d_%s", slot, field); !strings.Contains(text, "key: "+key+"\n") {
				t.Fatalf("module.yaml declares no option %s", key)
			}
		}

		if want := fmt.Sprintf("option: plug%d_host\n    ports: [80]", slot); !strings.Contains(text, want) {
			t.Fatalf("module.yaml declares no egress for plug%d_host on port 80", slot)
		}
	}

	if strings.Contains(text, fmt.Sprintf("plug%d_host", maxPlugs+1)) {
		t.Fatalf("module.yaml names a slot past maxPlugs (%d)", maxPlugs)
	}

	for _, key := range []string{"auto_off_minutes", "auto_off_cooldown_c", "overheat_nozzle_c", "overheat_bed_c"} {
		if !strings.Contains(text, "key: "+key+"\n") {
			t.Fatalf("module.yaml declares no option %s", key)
		}
	}

	if !strings.Contains(text, "- observe") {
		t.Fatal("module.yaml does not ask for observe")
	}
}

// TestNoSettingIsReadFromTheEnvironment keeps every setting in the manifest.
func TestNoSettingIsReadFromTheEnvironment(t *testing.T) {
	for _, file := range []string{"main.go", "tasmota.go", "follower.go", "auto.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		if strings.Contains(string(source), "os.Getenv") {
			t.Fatalf("%s reads an environment variable. Declare the setting in module.yaml instead", file)
		}
	}
}

func TestTheListCarriesEnergyAndThePrice(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	price := 0.14
	s := *held.Load()
	s.PricePerKWH, s.Currency = &price, "€"
	held.Store(&s)

	var list struct {
		Items []struct {
			Name   string `json:"name"`
			Energy *struct {
				Watts    *float64 `json:"watts"`
				TodayKWh *float64 `json:"today_kwh"`
			} `json:"energy"`
			SignalDBm *int `json:"signal_dbm"`
		} `json:"items"`
		PricePerKWH *float64 `json:"price_per_kwh"`
		Currency    string   `json:"currency"`
	}

	recorder := do(http.MethodGet, "/plugs", "")
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || len(list.Items) != 2 {
		t.Fatalf("the list is %s", recorder.Body)
	}

	first := list.Items[0]
	if first.Energy == nil || first.Energy.Watts == nil || *first.Energy.Watts != 15 ||
		first.Energy.TodayKWh == nil || *first.Energy.TodayKWh != 0.131 || first.SignalDBm == nil || *first.SignalDBm != -44 {
		t.Fatalf("the first row carries %s", recorder.Body)
	}

	if list.PricePerKWH == nil || *list.PricePerKWH != 0.14 || list.Currency != "€" {
		t.Fatalf("the price reads %v %q", list.PricePerKWH, list.Currency)
	}
}

func TestNoPriceIsNullAndNotZero(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	if body := do(http.MethodGet, "/plugs", "").Body.String(); !strings.Contains(body, `"price_per_kwh":null`) {
		t.Fatalf("an unset price must be null, not 0 (which would price everything free): %s", body)
	}
}

func TestOnePlugCarriesItsEnergyToo(t *testing.T) {
	withFakePlugs(t, twoPlugs...)

	if body := do(http.MethodGet, "/plugs/mk4s", "").Body.String(); !strings.Contains(body, `"total_kwh":17.666`) {
		t.Fatalf("GET /plugs/mk4s lost the energy: %s", body)
	}
}

// A display name reaches the route as the browser wrote it, with a space as
// %20 and a non-ASCII letter as percent-encoded bytes.
func TestADisplayNameIsFoundByItsEncodedRoute(t *testing.T) {
	plugs := []plugConfig{
		{Slot: 1, Name: "Core One", Host: "core.lan", Printer: "CZPX-1"},
		{Slot: 2, Name: "Büro (MK4S)", Host: "mk4.lan", Printer: "CZPX-2"},
	}
	withSettings(t, Settings{Plugs: plugs})
	states := withFakePlugs(t, plugs...)

	for path, host := range map[string]string{
		"/plugs/Core%20One":             "core.lan",
		"/plugs/B%C3%BCro%20%28MK4S%29": "mk4.lan",
	} {
		if recorder := do(http.MethodGet, path, ""); recorder.Code != http.StatusOK {
			t.Fatalf("GET %s answered %d: %s", path, recorder.Code, recorder.Body)
		}

		if recorder := do(http.MethodPost, path+"/power", `{"on":true}`); recorder.Code != http.StatusOK {
			t.Fatalf("POST %s/power answered %d: %s", path, recorder.Code, recorder.Body)
		}

		if *states[host] != "ON" {
			t.Fatalf("%s did not switch %s", path, host)
		}
	}
}

// A person who may not operate cannot switch a plug, with or without force, and
// the plug is never touched. Reads stay open to anyone the host let in.
func TestOnlyAnOperatorSwitchesAPlug(t *testing.T) {
	states := withFakePlugs(t, twoPlugs...)

	for _, tc := range []struct {
		who  string
		body string
		want int
		code string
	}{
		{"user:viewer", `{"on":false}`, http.StatusForbidden, AccessDenied.Code().String()},
		{"user:viewer", `{"on":false,"force":true}`, http.StatusForbidden, AccessDenied.Code().String()},
		{"user:viewer", `{"on":true}`, http.StatusForbidden, AccessDenied.Code().String()},
		{"", `{"on":false,"force":true}`, http.StatusForbidden, AccessDenied.Code().String()},
		{"user:broken", `{"on":false,"force":true}`, http.StatusServiceUnavailable, AccessUnknown.Code().String()},
	} {
		recorder := doAs(tc.who, http.MethodPost, "/plugs/core-one/power", tc.body)
		if recorder.Code != tc.want || code(t, recorder) != tc.code {
			t.Fatalf("%q sending %s: answered %d %s, want %d %s", tc.who, tc.body, recorder.Code, recorder.Body, tc.want, tc.code)
		}
	}

	if *states["core.lan"] != "OFF" || *states["mk4.lan"] != "OFF" {
		t.Fatal("a refused caller switched a plug")
	}

	// Refused before the plug is looked up, so an unknown name tells a viewer nothing.
	if recorder := doAs("user:viewer", http.MethodPost, "/plugs/nope/power", `{"on":true}`); recorder.Code != http.StatusForbidden {
		t.Fatalf("a viewer calling an unknown plug got %d", recorder.Code)
	}

	if recorder := doAs("user:viewer", http.MethodGet, "/plugs", ""); recorder.Code != http.StatusOK {
		t.Fatalf("a read answered %d", recorder.Code)
	}

	if recorder := doAs("user:operator", http.MethodPost, "/plugs/core-one/power", `{"on":true}`); recorder.Code != http.StatusOK || *states["core.lan"] != "ON" {
		t.Fatalf("an operator answered %d", recorder.Code)
	}
}
