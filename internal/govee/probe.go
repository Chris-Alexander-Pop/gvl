package govee

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// ProbeResult is a device that answered a unicast status probe.
type ProbeResult struct {
	IP     string
	Status *Status
}

// SubnetFromIP returns the IPv4 /24 CIDR containing ip (e.g. 192.0.2.10 → 192.0.2.0/24).
func SubnetFromIP(ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("invalid IP %q", ip)
	}
	v4 := parsed.To4()
	if v4 == nil {
		return "", fmt.Errorf("not an IPv4 address %q", ip)
	}
	return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2]), nil
}

// ParseCrawlTarget turns a CIDR or bare IPv4 into a probeable CIDR.
// Bare IPs become the containing /24.
func ParseCrawlTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty target")
	}
	if strings.Contains(s, "/") {
		if _, _, err := net.ParseCIDR(s); err != nil {
			return "", fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		return s, nil
	}
	return SubnetFromIP(s)
}

// LocalIPv4CIDRs returns unicast IPv4 networks from up interfaces.
// Skips loopback and Tailscale CGNAT (100.64/10). Prefixes wider than /16 are omitted
// (ProbeSubnet refuses wild scans).
func LocalIPv4CIDRs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() {
				continue
			}
			if isTailscaleCGNAT(v4) {
				continue
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 || ones < 16 {
				continue
			}
			network := &net.IPNet{IP: v4.Mask(ipNet.Mask), Mask: ipNet.Mask}
			cidr := network.String()
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			out = append(out, cidr)
		}
	}
	sort.Strings(out)
	return out
}

func isTailscaleCGNAT(ip net.IP) bool {
	// 100.64.0.0/10
	return ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127
}

// DefaultCrawlCIDRs picks subnets to probe when the user did not pass any.
// Order: GVL_DISCOVER_SUBNET, /24 of hintIP, then local interface networks.
func DefaultCrawlCIDRs(hintIP string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(cidr string) {
		if cidr == "" {
			return
		}
		if _, ok := seen[cidr]; ok {
			return
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}
	if v := strings.TrimSpace(os.Getenv("GVL_DISCOVER_SUBNET")); v != "" {
		if c, err := ParseCrawlTarget(v); err == nil {
			add(c)
		}
	}
	if hintIP != "" {
		if c, err := SubnetFromIP(hintIP); err == nil {
			add(c)
		}
	}
	for _, c := range LocalIPv4CIDRs() {
		add(c)
	}
	return out
}

// ProbeSubnet sends devStatus to every host in cidr and collects replies on UDP 4002.
// Used when multicast discovery cannot cross subnets (e.g. daemon on another LAN).
func ProbeSubnet(cidr string, timeout time.Duration) ([]ProbeResult, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	hosts := hostsInNet(ipNet)
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no hosts in %s", cidr)
	}

	pc, err := listenUDP(timeout)
	if err != nil {
		return nil, err
	}
	defer pc.Close()

	payload, err := json.Marshal(map[string]any{
		"msg": map[string]any{
			"cmd":  "devStatus",
			"data": map[string]any{},
		},
	})
	if err != nil {
		return nil, err
	}

	// Blast quickly; replies are correlated by source IP.
	for _, ip := range hosts {
		addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: CmdPort}
		_, _ = pc.WriteToUDP(payload, addr)
	}

	found := map[string]*Status{}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = pc.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var env envelope
		if err := json.Unmarshal(buf[:n], &env); err != nil {
			continue
		}
		if env.Msg.Cmd != "" && env.Msg.Cmd != "devStatus" {
			continue
		}
		var st Status
		if err := json.Unmarshal(env.Msg.Data, &st); err != nil {
			continue
		}
		ip := addr.IP.String()
		found[ip] = &st
	}

	out := make([]ProbeResult, 0, len(found))
	for ip, st := range found {
		out = append(out, ProbeResult{IP: ip, Status: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

// PickProbedIP chooses a device IP from probe results.
// Prefer preferred if it still answered; else a single responder; else error.
func PickProbedIP(results []ProbeResult, preferred string) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("no devices answered status probe")
	}
	ips := make([]string, len(results))
	seen := map[string]struct{}{}
	for i, r := range results {
		ips[i] = r.IP
		seen[r.IP] = struct{}{}
	}
	if preferred != "" {
		if _, ok := seen[preferred]; ok {
			return preferred, nil
		}
	}
	if len(results) == 1 {
		return results[0].IP, nil
	}
	return "", fmt.Errorf("multiple devices answered (%s); set GVL_DEVICE_IP or reserve DHCP", strings.Join(ips, ", "))
}

func hostsInNet(ipNet *net.IPNet) []string {
	ip := ipNet.IP.To4()
	if ip == nil {
		return nil
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil
	}
	hostBits := bits - ones
	if hostBits > 16 {
		// Cap wild scans; caller should use a /24 or tighter.
		return nil
	}
	n := 1 << hostBits
	if n <= 2 {
		return nil
	}
	base := binaryIP(ip)
	out := make([]string, 0, n-2)
	for i := 1; i < n-1; i++ {
		out = append(out, formatIPv4(base+uint32(i)))
	}
	return out
}

func binaryIP(ip net.IP) uint32 {
	v4 := ip.To4()
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3])
}

func formatIPv4(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
