package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// The question put to the host before a plug is switched: may this person take
// operate_resource on this module's kind. module.yaml lists the action under
// checks:, so the host answers it and grants this module nothing.
const (
	operateAction = "operate_resource"
	moduleKind    = "sink" // the kind in module.yaml

	// operatePrinter is the device action asked about for the printer a plug powers.
	operatePrinter = "operate"
)

// askTimeout bounds one question to the host.
const askTimeout = 4 * time.Second

// hintFor is how long a "yes" for the page's hint (can_operate) is kept per
// person. The switch route never uses it: it asks the host every time. A "no" is
// never kept: the host answers within the scopes of the credential that asked,
// and a read-only token's "no" must not hide the buttons from the same person's
// full session.
const hintFor = 30 * time.Second

var errNoCaller = errors.New("the request names nobody to act as")

// mayOperate answers whether the person the host named may switch a plug. It is
// a variable so a test can answer without a host, and run replaces it with the
// host's access API. Until then it is noAccess, so a process that never got one
// switches nothing.
//
//nolint:gochecknoglobals // a test seam.
var mayOperate = noAccess

func noAccess(context.Context, string) (bool, error) {
	return false, errors.New("no access client")
}

// accessChecker asks the host.
func accessChecker(access accessapi.Access) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, principal string) (bool, error) {
		decision, err := access.Check(ctx, accessapi.CheckParams{
			Subject: principal, Action: operateAction, ObjectType: "resource_kind", ObjectID: moduleKind,
		})

		return decision.Allowed, err
	}
}

// mayOperatePrinter answers whether the person may operate the printer with this
// serial. known is false when the host has no printer with that serial, and then
// there is nobody's print to protect: the module-wide answer stands. It is a
// variable for the same reason as mayOperate.
//
//nolint:gochecknoglobals // a test seam.
var mayOperatePrinter = func(context.Context, string, string) (ok, known bool, err error) {
	return false, false, errors.New("no access client")
}

// fleetLister is the half of the host's device API that finds a printer's id.
type fleetLister interface {
	List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error)
}

// printerChecker finds the host's id for a printer by its serial, then asks
// whether the person may operate that device.
func printerChecker(access accessapi.Access, fleet fleetLister) func(context.Context, string, string) (ok, known bool, err error) {
	return func(ctx context.Context, principal, serial string) (ok, known bool, err error) {
		id, err := deviceIDOf(ctx, fleet, serial)
		if err != nil || id == "" {
			return false, false, err
		}

		decision, err := access.Check(ctx, accessapi.CheckParams{
			Subject: principal, Action: operatePrinter, ObjectType: "device", ObjectID: id,
		})

		return decision.Allowed, true, err
	}
}

// deviceIDOf is the id of the printer with this serial, or empty when the host
// has none.
func deviceIDOf(ctx context.Context, fleet fleetLister, serial string) (string, error) {
	for offset := 0; ; {
		page, err := fleet.List(ctx, deviceapi.ListParams{Limit: deviceapi.MaxPageLimit, Offset: offset})
		if err != nil {
			return "", err
		}

		for _, device := range page.Items {
			if device.Serial == serial {
				return device.ID, nil
			}
		}

		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return "", nil
		}
	}
}

// decide asks the host whether the caller of r may switch a plug. A request that
// names nobody is errNoCaller and the host is not asked, because an empty
// subject would mean this module itself.
func decide(r *http.Request) (principal string, ok bool, err error) {
	principal = routesapi.PrincipalOf(r)
	if principal == "" {
		return "", false, errNoCaller
	}

	ctx, cancel := context.WithTimeout(r.Context(), askTimeout)
	defer cancel()

	ok, err = mayOperate(ctx, principal)

	return principal, ok, err
}

// hints keeps the host's last answer per person for hintFor, so a page polling
// every few seconds does not put a question to the host each time.
type hints struct {
	mu   sync.Mutex
	kept map[string]hint
}

type hint struct {
	ok    bool
	until time.Time
}

//nolint:gochecknoglobals // one cache for the process.
var pageHints = &hints{kept: map[string]hint{}}

func (h *hints) get(principal string) (ok, hit bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	kept, found := h.kept[principal]

	return kept.ok, found && time.Now().Before(kept.until)
}

func (h *hints) put(principal string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()

	// A sweep on each write drops what has expired, so the map holds at most the
	// people seen within hintFor.
	for who, kept := range h.kept {
		if !now.Before(kept.until) {
			delete(h.kept, who)
		}
	}

	h.kept[principal] = hint{ok: ok, until: now.Add(hintFor)}
}

// cachedHint is the kept "yes" for the caller, or nil.
func cachedHint(r *http.Request) *bool {
	if ok, hit := pageHints.get(routesapi.PrincipalOf(r)); hit && ok {
		return &ok
	}

	return nil
}

// canOperate says whether the caller may switch a plug, for the page to show or
// hide its buttons. Nil means unknown (no caller, or the host did not answer):
// the page then shows them, and the switch route decides. A "yes" may be up to
// hintFor behind a change of role, which only a button's looks depend on.
func canOperate(r *http.Request) *bool {
	if hit := cachedHint(r); hit != nil {
		return hit
	}

	principal, ok, err := decide(r)
	if err != nil {
		return nil
	}

	if ok {
		pageHints.put(principal, ok)
	}

	return &ok
}

// canOperateSoon starts the host's question and returns what waits for the
// answer, so it runs while the plugs are read. A kept "yes" needs no question.
func canOperateSoon(r *http.Request) func() *bool {
	if hit := cachedHint(r); hit != nil {
		return func() *bool { return hit }
	}

	done := make(chan *bool, 1)

	go func() { done <- canOperate(r) }()

	return func() *bool { return <-done }
}

// allowed reports whether this request may switch a plug, and answers the
// request itself when it may not. A request that names nobody, and a host that
// cannot answer, are refused: a power cut can ruin a print, so doubt is a no.
// It asks the host every time and keeps nothing.
func allowed(w http.ResponseWriter, r *http.Request) bool {
	principal, ok, err := decide(r)

	switch {
	case errors.Is(err, errNoCaller):
		refuse(w, r, AccessDenied.New(err.Error()))

		return false
	case err != nil:
		fmt.Fprintf(os.Stderr, "%s: could not ask the host whether %s may switch a plug, so it was refused: %v\n", name, principal, err)
		refuse(w, r, AccessUnknown.New("the host could not say whether this person may switch a plug"))

		return false
	case !ok:
		refuse(w, r, AccessDenied.New("this person may not switch a plug"))

		return false
	}

	return true
}

// allowedPrinter reports whether this request's caller may operate the printer
// with this serial, and answers the request itself when not. A printer the host
// does not know is nobody's print to protect, so the module-wide answer (asked
// by allowed) stands. Like allowed it asks every time and keeps nothing.
func allowedPrinter(w http.ResponseWriter, r *http.Request, serial string) bool {
	principal := routesapi.PrincipalOf(r)

	ctx, cancel := context.WithTimeout(r.Context(), askTimeout)
	defer cancel()

	ok, known, err := mayOperatePrinter(ctx, principal, serial)

	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "%s: could not ask the host whether %s may operate printer %s, so the switch was refused: %v\n", name, principal, serial, err)
		refuse(w, r, AccessUnknown.New("the host could not say whether this person may operate the printer"))

		return false
	case known && !ok:
		refuse(w, r, AccessDenied.New("this person may not operate the printer this plug powers"))

		return false
	}

	return true
}
