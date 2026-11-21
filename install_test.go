package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// fakeDocker answers the few questions install-on-host.sh asks docker and keeps
// the settings file it is told to copy into the container, which is the one
// thing these tests read.
const fakeDocker = `#!/bin/sh
case "$1" in
inspect)
	case "$*" in
	*State.Running*) echo true ;;
	*Config.Image*) echo fake/image ;;
	*) echo none ;;
	esac
	;;
cp)
	case "$3" in *smart-plugs-settings.json) cp "$2" "$CAPTURE" ;; esac
	;;
esac
exit 0
`

// installSettings runs scripts/install-on-host.sh against a fake docker, with
// these plug arguments, and returns the settings file it would have copied into
// the container, or nil when it copied none.
func installSettings(t *testing.T, plugs ...string) []byte {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the install script is a shell script")
	}

	arch := map[string]string{"amd64": "linux_amd64", "arm64": "linux_arm64"}[runtime.GOARCH]
	if arch == "" {
		t.Skipf("the install script has no build for %s", runtime.GOARCH)
	}

	script, err := os.ReadFile("scripts/install-on-host.sh")
	if err != nil {
		t.Fatal(err)
	}

	dir, bin, capture := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "settings.json")

	if err = os.WriteFile(filepath.Join(dir, "install.sh"), script, 0o600); err != nil {
		t.Fatal(err)
	}

	// The script takes the version from the bundle's module.yaml.
	bundle, err := os.Create(filepath.Join(dir, "smart-plugs-"+arch+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}

	zipped := gzip.NewWriter(bundle)
	archive := tar.NewWriter(zipped)
	manifest := []byte("name: smart-plugs\nversion: 9.9.9\n")

	if err = archive.WriteHeader(&tar.Header{Name: "module.yaml", Mode: 0o644, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}

	if _, err = archive.Write(manifest); err != nil {
		t.Fatal(err)
	}

	for _, closer := range []interface{ Close() error }{archive, zipped, bundle} {
		if err = closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// A sleep that returns at once keeps the script's waits out of the test.
	for name, body := range map[string]string{"docker": fakeDocker, "sleep": "#!/bin/sh\nexit 0\n"} {
		if err = os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("sh", append([]string{filepath.Join(dir, "install.sh"), "-y", "shots"}, plugs...)...)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "CAPTURE=" + capture, "TMPDIR=" + t.TempDir()}

	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("the install script failed: %v\n%s", runErr, out)
	}

	written, err := os.ReadFile(capture)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		t.Fatal(err)
	}

	return written
}

// The script writes the plugs setting as JSON inside a JSON value, so a quoting
// mistake would give a settings file the module refuses at start. This reads
// what it writes with the module's own parser.
func TestTheInstallScriptWritesSettingsTheModuleReads(t *testing.T) {
	written := installSettings(t,
		"core-one=core-one.lan=15715-1042770182630451",
		"mk4s=192.168.1.31",
		"bench-2=plug.example.com=CZPX1234X009XC12345")

	var s Settings
	if err := json.Unmarshal(written, &s); err != nil {
		t.Fatalf("the module refused %s: %v", written, err)
	}

	if err := s.validate(); err != nil {
		t.Fatalf("the module refused %s: %v", written, err)
	}

	want := []plugConfig{
		{Slot: 1, Name: "core-one", Host: "core-one.lan", Printer: "15715-1042770182630451"},
		{Slot: 2, Name: "mk4s", Host: "192.168.1.31"},
		{Slot: 3, Name: "bench-2", Host: "plug.example.com", Printer: "CZPX1234X009XC12345"},
	}

	if !slices.Equal(s.Plugs, want) {
		t.Fatalf("the plugs read back as %+v, want %+v (file: %s)", s.Plugs, want, written)
	}
}

// With no plug arguments it writes no settings, so what was set up in the app is
// kept.
func TestTheInstallScriptWritesNoSettingsWhenGivenNoPlugs(t *testing.T) {
	if written := installSettings(t); written != nil {
		t.Fatalf("it wrote %s with no plugs given", written)
	}
}
