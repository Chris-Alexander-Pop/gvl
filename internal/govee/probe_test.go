package govee

import (
	"net"
	"testing"
)

func TestSubnetFromIP(t *testing.T) {
	got, err := SubnetFromIP("192.168.68.64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.68.0/24" {
		t.Fatalf("got %q", got)
	}
	if _, err := SubnetFromIP("not-an-ip"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHostsInNet24(t *testing.T) {
	_, ipNet, err := net.ParseCIDR("192.168.68.0/24")
	if err != nil {
		t.Fatal(err)
	}
	hosts := hostsInNet(ipNet)
	if len(hosts) != 254 {
		t.Fatalf("want 254 hosts, got %d", len(hosts))
	}
	if hosts[0] != "192.168.68.1" || hosts[len(hosts)-1] != "192.168.68.254" {
		t.Fatalf("range %s .. %s", hosts[0], hosts[len(hosts)-1])
	}
}

func TestPickProbedIP(t *testing.T) {
	results := []ProbeResult{
		{IP: "192.168.68.64"},
		{IP: "192.168.68.70"},
	}
	ip, err := PickProbedIP(results, "192.168.68.64")
	if err != nil || ip != "192.168.68.64" {
		t.Fatalf("preferred: %q %v", ip, err)
	}
	ip, err = PickProbedIP(results[:1], "")
	if err != nil || ip != "192.168.68.64" {
		t.Fatalf("single: %q %v", ip, err)
	}
	if _, err := PickProbedIP(results, "192.168.68.1"); err == nil {
		t.Fatal("expected ambiguity error")
	}
	if _, err := PickProbedIP(nil, ""); err == nil {
		t.Fatal("expected empty error")
	}
}
