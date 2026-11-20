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
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// The question put to the host before a plug is switched: may this person take
// operate_resource on this module's kind. module.yaml lists the action under
// checks:, so the host answers it and grants this module nothing.
const (
	operateAction = "operate_resource"
	moduleKind    = "sink" // the kind in module.yaml
)

// askTimeout bounds one question to the host.
const askTimeout = 4 * time.Second

// hintFor is how long the page's hint (can_operate) is kept per person. The
// switch route never uses it: it asks the host every time.
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

// canOperate says whether the caller may switch a plug, for the page to show or
// hide its buttons. Nil means unknown (no caller, or the host did not answer):
// the page then shows them, and the switch route decides. It may be up to
// hintFor behind a change of role, which only a button's looks depend on.
func canOperate(r *http.Request) *bool {
	if ok, hit := pageHints.get(routesapi.PrincipalOf(r)); hit {
		return &ok
	}

	principal, ok, err := decide(r)
	if err != nil {
		return nil
	}

	pageHints.put(principal, ok)

	return &ok
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
