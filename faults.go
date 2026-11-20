// The codes this module refuses a caller with.
//
// A refusal a person or a client can see is a registered code, never a bare
// English sentence. The code is what a browser half switches on to choose a
// translation, and the English sentence travels beside it so a log line and an
// untranslated screen still read.
//
// Registering a code takes its documentation with it: what went wrong, what the
// caller should do, whether sending the same request again can work, and the
// parameters the sentence names. The sdk refuses a registration without them.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// faultDomain is the first half of every code below. It is this module's
// namespace, which the host derived from the Go module path github.com/ccmpbll/tasmota-plugs, so no
// other author can register a code under it. The sdk refuses the core
// namespace, which is what keeps this module's codes apart from the server's.
//
//nolint:gochecknoglobals // a domain built once at start-up; a const cannot hold one.
var faultDomain = mustDomain("com.github.aux.ccmpbll.tasmota-plugs")

// The codes this module raises. Add a code here, and nowhere else, so the list
// a client branches on is one file.
//
//nolint:gochecknoglobals // definitions built once at start-up.
var (
	// RouteNotFound is a path this module does not serve.
	RouteNotFound = fault.Register(faultDomain, "ROUTE_NOT_FOUND", http.StatusNotFound, fault.Document(
		"This module does not serve that path",
		"Read openapi.json. Every path in it is relative to the mount the operator gave this module.",
		fault.RetryNever,
		fault.ShowDetail,
		"path",
	))

	// InvalidRequest is a body the module cannot read.
	InvalidRequest = fault.Register(faultDomain, "INVALID_REQUEST", http.StatusBadRequest, fault.Document(
		"The request body is not one this module accepts",
		`Send {"on": true} or {"on": false}.`,
		fault.RetryNever,
		fault.ShowDetail,
	))

	// PrinterBusy is a switch off of a plug whose printer is printing or paused.
	PrinterBusy = fault.Register(faultDomain, "PRINTER_BUSY", http.StatusConflict, fault.Document(
		"The printer is printing, so its plug was not switched off",
		`Wait for the print to end, or send {"on": false, "force": true} if cutting the power is meant.`,
		fault.RetryNever,
		fault.ShowDetail,
		"name",
		"printer",
		"state",
	))

	// AccessDenied is a person who may not switch a plug.
	AccessDenied = fault.Register(faultDomain, "ACCESS_DENIED", http.StatusForbidden, fault.Document(
		"This person may not switch a plug",
		"Ask an operator or an administrator of this server to switch it.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// AccessUnknown is a switch the host could not decide.
	AccessUnknown = fault.Register(faultDomain, "ACCESS_UNKNOWN", http.StatusServiceUnavailable, fault.Document(
		"The host could not say whether this person may switch a plug",
		"Send the request again in a moment.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))

	// PlugNotFound is a call to a plug no setting names.
	PlugNotFound = fault.Register(faultDomain, "PLUG_NOT_FOUND", http.StatusNotFound, fault.Document(
		"No plug has that name",
		"List the plugs with GET /plugs, or set plug<N>_host and plug<N>_name in this instance's config block.",
		fault.RetryNever,
		fault.ShowDetail,
		"name",
	))

	// PlugUnreachable is a plug that did not answer, or answered wrongly.
	PlugUnreachable = fault.Register(faultDomain, "PLUG_UNREACHABLE", http.StatusBadGateway, fault.Document(
		"The plug did not answer correctly",
		"Check the plug is powered and on the network, then send the request again.",
		fault.RetryAllowed,
		fault.ShowDetail,
		"name",
		"host",
	))
)

// mustDomain builds this module's fault domain. A name the sdk refuses is a
// mistake in this file, so it stops the process at start-up rather than
// answering a caller with an empty code.
func mustDomain(name string) fault.Domain {
	domain, err := fault.NewDomain(name)
	if err != nil {
		panic(fmt.Sprintf("smart-plugs: %s is not a fault domain: %v", name, err))
	}

	return domain
}

// refuse writes one refusal as RFC 9457 frames it, which is the shape the
// generated client reads and the shape describeFault reads in the browser half:
// the code, the English sentence, the values that sentence names, and where the
// code is documented.
func refuse(w http.ResponseWriter, r *http.Request, raised *fault.Fault) {
	problem := raised.ProblemJSON(r.URL.Path)

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)

	_ = json.NewEncoder(w).Encode(problem)
}
