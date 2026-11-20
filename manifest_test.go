package main

import (
	"os"
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"gopkg.in/yaml.v3"
)

// TestTheManifestParsesWithTheSDK runs module.yaml through the parser the host
// uses at install, so a refused key or a bad egress entry fails here and not
// at `module add`.
func TestTheManifestParsesWithTheSDK(t *testing.T) {
	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatal(err)
	}

	declared, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("module.yaml is refused: %v", err)
	}

	if want := []string{operateAction, operatePrinter}; !slices.Equal(declared.Checks, want) {
		t.Fatalf("the manifest checks %v, want %v: access.go asks about both", declared.Checks, want)
	}

	if declared.Kind != moduleKind {
		t.Fatalf("the manifest's kind is %q, access.go asks about %q", declared.Kind, moduleKind)
	}

	if declared.Name != name {
		t.Fatalf("the manifest names %q, the code reports %q", declared.Name, name)
	}
}
