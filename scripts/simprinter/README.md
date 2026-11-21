# simprinter

A stand-in Prusa printer for testing the module's printer pages. It registers
with a Connect server over the documented xBuddy protocol, asks the server to
approve it (so the server must be in open mode, as a throwaway test container
set up with `{"mode":"open"}` is), holds the websocket and reports telemetry.

    go build -o simprinter .
    ./simprinter -server http://<test-server>:18091 -sn CZPX-TEST-1 -type 7.1.0 -state IDLE
    ./simprinter -server http://<test-server>:18091 -sn CZPX-TEST-2 -type 1.4.1 -state PRINTING

`-server` is the address of the test server and is required.
`-state PRINTING` lets you see the confirmation before a plug is turned off. The
`-sn` serial must match the printer of a plug in the `plugs` setting for the
printer to show that plug. Needs the Go toolchain; its own module, so the module's `go vet ./...`
does not build it.
