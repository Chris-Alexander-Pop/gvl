package govee

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeviceStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := DeviceState{IP: "192.168.68.64", Device: "24:8D:5C:E7:53:E5:BB:BE", SKU: "H60A1"}
	if err := SaveDeviceState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDeviceState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestLoadDeviceStateMissing(t *testing.T) {
	got, err := LoadDeviceState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.IP != "" {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestLoadDeviceStateCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "device.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeviceState(dir); err == nil {
		t.Fatal("expected error")
	}
}
