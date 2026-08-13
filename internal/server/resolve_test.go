package server

import (
	"errors"
	"testing"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

func TestPickDevice(t *testing.T) {
	devs := []govee.Device{
		{IP: "192.0.2.10", Device: "dev-a", SKU: "H60A1"},
		{IP: "192.0.2.20", Device: "dev-b", SKU: "H6001"},
	}
	d, ok := pickDevice(devs, "", "dev-b", "")
	if !ok || d.IP != "192.0.2.20" {
		t.Fatalf("by device id: %+v %v", d, ok)
	}
	d, ok = pickDevice(devs, "192.0.2.10", "", "")
	if !ok || d.Device != "dev-a" {
		t.Fatalf("by ip: %+v %v", d, ok)
	}
	d, ok = pickDevice(devs, "", "", "H60A1")
	if !ok || d.IP != "192.0.2.10" {
		t.Fatalf("by sku: %+v %v", d, ok)
	}
	if _, ok := pickDevice(devs, "", "", ""); ok {
		t.Fatal("ambiguous without preference should fail")
	}
	d, ok = pickDevice(devs[:1], "", "", "")
	if !ok || d.IP != "192.0.2.10" {
		t.Fatalf("single: %+v %v", d, ok)
	}
}

func TestIsUnreachable(t *testing.T) {
	if !isUnreachable(errors.New("no response from device")) {
		t.Fatal("expected unreachable")
	}
	if isUnreachable(errors.New("unauthorized")) {
		t.Fatal("expected reachable classification")
	}
	if isUnreachable(nil) {
		t.Fatal("nil")
	}
}
