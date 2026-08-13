package govee

import (
	"net"
	"testing"
)

func TestSubnetFromIP(t *testing.T) {
	got, err := SubnetFromIP("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.0.2.0/24" {
		t.Fatalf("got %q", got)
	}
	if _, err := SubnetFromIP("not-an-ip"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCrawlTarget(t *testing.T) {
	got, err := ParseCrawlTarget("192.0.2.50")
	if err != nil || got != "192.0.2.0/24" {
		t.Fatalf("ip: %q %v", got, err)
	}
	got, err = ParseCrawlTarget("198.51.100.0/24")
	if err != nil || got != "198.51.100.0/24" {
		t.Fatalf("cidr: %q %v", got, err)
	}
	if _, err := ParseCrawlTarget("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultCrawlCIDRsHint(t *testing.T) {
	t.Setenv("GVL_DISCOVER_SUBNET", "")
	got := DefaultCrawlCIDRs("192.0.2.10")
	if len(got) == 0 || got[0] != "192.0.2.0/24" {
		t.Fatalf("want hint /24 first, got %v", got)
	}
}

func TestHostsInNet24(t *testing.T) {
	_, ipNet, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	hosts := hostsInNet(ipNet)
	if len(hosts) != 254 {
		t.Fatalf("want 254 hosts, got %d", len(hosts))
	}
	if hosts[0] != "192.0.2.1" || hosts[len(hosts)-1] != "192.0.2.254" {
		t.Fatalf("range %s .. %s", hosts[0], hosts[len(hosts)-1])
	}
}

func TestPickProbedIP(t *testing.T) {
	results := []ProbeResult{
		{IP: "192.0.2.10"},
		{IP: "192.0.2.20"},
	}
	ip, err := PickProbedIP(results, "192.0.2.10")
	if err != nil || ip != "192.0.2.10" {
		t.Fatalf("preferred: %q %v", ip, err)
	}
	ip, err = PickProbedIP(results[:1], "")
	if err != nil || ip != "192.0.2.10" {
		t.Fatalf("single: %q %v", ip, err)
	}
	if _, err := PickProbedIP(results, "192.0.2.1"); err == nil {
		t.Fatal("expected ambiguity error")
	}
	if _, err := PickProbedIP(nil, ""); err == nil {
		t.Fatal("expected empty error")
	}
}
