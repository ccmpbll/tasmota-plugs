// A stand-in Prusa printer for testing: it registers with a Connect server over
// the documented xBuddy protocol, waits for approval, then holds the websocket
// and reports telemetry in the state it was asked to be in.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:18091", "the Connect server")
	sn := flag.String("sn", "", "the printer's serial")
	ptype := flag.String("type", "7.1.0", "the device type id (7.1.0 is a CORE One, 1.4.1 an MK4S)")
	state := flag.String("state", "IDLE", "the state to report: IDLE, PRINTING, PAUSED")
	nozzle := flag.Float64("nozzle", -1, "the nozzle temperature to report (default: 25 idle, 215 otherwise)")
	bed := flag.Float64("bed", -1, "the bed temperature to report (default: 24 idle, 60 otherwise)")
	flag.Parse()
	if *sn == "" {
		log.Fatal("-sn is required")
	}

	sum := sha256.Sum256([]byte(*sn))
	fingerprint := hex.EncodeToString(sum[:])[:50]

	body, _ := json.Marshal(map[string]string{"sn": *sn, "fingerprint": fingerprint, "printer_type": *ptype, "firmware": "6.5.7"})
	resp, err := http.Post(*server+"/p/register", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	code := resp.Header.Get("Code")
	resp.Body.Close()
	log.Printf("registered %s, code %q, status %d", *sn, code, resp.StatusCode)

	// An operator approves the printer. The test server is in open mode.
	areq, _ := http.NewRequest(http.MethodPost, *server+"/api/v1/registrations/"+url.PathEscape(code)+"/approve", strings.NewReader("{}"))
	areq.Header.Set("content-type", "application/json")
	aresp, err := http.DefaultClient.Do(areq)
	if err != nil {
		log.Fatal(err)
	}
	ab, _ := io.ReadAll(aresp.Body)
	aresp.Body.Close()
	log.Printf("approve: %d %s", aresp.StatusCode, strings.TrimSpace(string(ab)))

	var token string
	for i := 0; i < 30 && token == ""; i++ {
		req, _ := http.NewRequest(http.MethodGet, *server+"/p/register", nil)
		req.Header.Set("Code", code)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		token = r.Header.Get("Token")
		r.Body.Close()
		if token == "" {
			time.Sleep(time.Second)
		}
	}
	if token == "" {
		log.Fatal("never got a token")
	}
	log.Printf("got a token, connecting")

	ws := strings.Replace(*server, "http", "ws", 1) + "/p/ws"
	hdr := http.Header{}
	hdr.Set("Fingerprint", fingerprint)
	hdr.Set("Token", token)
	dialer := websocket.Dialer{Subprotocols: []string{"prusa-connect"}}
	conn, _, err := dialer.Dial(ws, hdr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	go func() {
		for {
			if _, msg, err := conn.ReadMessage(); err != nil {
				log.Fatalf("closed: %v", err)
			} else if len(msg) > 0 {
				log.Printf("server frame: %.60q", msg)
			}
		}
	}()

	for n := 0; ; n++ {
		t := map[string]any{
			"state": *state, "temp_nozzle": 25.0, "target_nozzle": 0, "temp_bed": 24.0, "target_bed": 0,
			"axis_x": 0.0, "axis_y": 0.0, "axis_z": 1.0, "speed": 100, "flow": 100,
		}
		if *state != "IDLE" {
			t["temp_nozzle"], t["target_nozzle"], t["temp_bed"], t["target_bed"] = 215.0, 215, 60.0, 60
			t["job_id"], t["progress"], t["time_printing"], t["time_remaining"] = 7, 42, 600+n*2, 1800
		}
		if *nozzle >= 0 {
			t["temp_nozzle"] = *nozzle
		}
		if *bed >= 0 {
			t["temp_bed"] = *bed
		}
		if err := conn.WriteJSON(t); err != nil {
			log.Fatal(err)
		}
		if n == 0 {
			fmt.Println("telemetry flowing for", *sn, "as", *state)
		}
		time.Sleep(2 * time.Second)
	}
}
