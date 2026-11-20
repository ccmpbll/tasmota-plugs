package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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

// mayOperate answers whether the person the host named may switch a plug. It is
// a variable so a test can answer without a host, and run replaces it with the
// host's access API. Until then it refuses, so a process that never got one
// switches nothing.
//
//nolint:gochecknoglobals // a test seam.
var mayOperate = func(context.Context, string) (bool, error) {
	return false, errors.New("no access client")
}

// accessChecker asks the host.
func accessChecker(access interface {
	Check(context.Context, accessapi.CheckParams) (accessapi.Decision, error)
},
) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, principal string) (bool, error) {
		decision, err := access.Check(ctx, accessapi.CheckParams{
			Subject: principal, Action: operateAction, ObjectType: "resource_kind", ObjectID: moduleKind,
		})

		return decision.Allowed, err
	}
}

// canOperate says whether the caller may switch a plug, for the page to show or
// hide its buttons. Nil means unknown (no caller, or the host did not answer):
// the page then shows them, and the switch route decides.
func canOperate(r *http.Request) *bool {
	principal := routesapi.PrincipalOf(r)
	if principal == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	ok, err := mayOperate(ctx, principal)
	if err != nil {
		return nil
	}

	return &ok
}

// allowed reports whether this request may switch a plug, and answers the
// request itself when it may not. A request that names nobody, and a host that
// cannot answer, are refused: a power cut can ruin a print, so doubt is a no.
func allowed(w http.ResponseWriter, r *http.Request) bool {
	principal := routesapi.PrincipalOf(r)
	if principal == "" {
		refuse(w, r, AccessDenied.New("the request names nobody to act as"))

		return false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	ok, err := mayOperate(ctx, principal)

	switch {
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
