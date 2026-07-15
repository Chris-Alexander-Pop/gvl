package govee

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	ScanPort    = 4001
	ListenPort  = 4002
	CmdPort     = 4003
	MulticastIP = "239.255.255.250"
)

// RGB is an 8-bit color.
type RGB struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// Device is a discovered Govee LAN device.
type Device struct {
	IP     string `json:"ip"`
	SKU    string `json:"sku,omitempty"`
	Device string `json:"device,omitempty"`
}

// Status is the device status payload.
type Status struct {
	OnOff            int `json:"onOff"`
	Brightness       int `json:"brightness"`
	Color            RGB `json:"color"`
	ColorTemInKelvin int `json:"colorTemInKelvin"`
}

// Client talks to a Govee light over LAN UDP.
type Client struct {
	IP string
	mu sync.Mutex
}

// NewClient returns a client for the given device IP.
func NewClient(ip string) *Client {
	return &Client{IP: ip}
}

type envelope struct {
	Msg struct {
		Cmd  string          `json:"cmd"`
		Data json.RawMessage `json:"data"`
	} `json:"msg"`
}

func (c *Client) send(cmd string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload := map[string]any{
		"msg": map[string]any{
			"cmd":  cmd,
			"data": data,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(c.IP), Port: CmdPort})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(b)
	return err
}

// Turn sets power on (true) or off (false).
func (c *Client) Turn(on bool) error {
	v := 0
	if on {
		v = 1
	}
	return c.send("turn", map[string]int{"value": v})
}

// Brightness sets brightness 0-100.
func (c *Client) Brightness(v int) error {
	return c.send("brightness", map[string]int{"value": ClampBrightness(v)})
}

// Color sets an RGB color (clears color temperature).
func (c *Client) Color(rgb RGB) error {
	return c.send("colorwc", map[string]any{
		"color":            map[string]int{"r": rgb.R, "g": rgb.G, "b": rgb.B},
		"colorTemInKelvin": 0,
	})
}

// Temp sets color temperature in Kelvin.
func (c *Client) Temp(kelvin int) error {
	return c.send("colorwc", map[string]any{
		"color":            map[string]int{"r": 0, "g": 0, "b": 0},
		"colorTemInKelvin": kelvin,
	})
}

// Status queries device status.
func (c *Client) Status(timeout time.Duration) (*Status, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	pc, err := listenUDP(timeout)
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	if err := c.send("devStatus", map[string]any{}); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, _, err := pc.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("no response from device")
	}
	var env envelope
	if err := json.Unmarshal(buf[:n], &env); err != nil {
		return nil, err
	}
	var st Status
	if err := json.Unmarshal(env.Msg.Data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// FormatStatus returns a human-readable status block.
func FormatStatus(st *Status, ip string) string {
	state := "off"
	if st.OnOff != 0 {
		state = "on"
	}
	out := ""
	if ip != "" {
		out += fmt.Sprintf("device  %s\n", ip)
	}
	out += fmt.Sprintf("power   %s\n", state)
	out += fmt.Sprintf("bright  %d%%\n", st.Brightness)
	out += fmt.Sprintf("color   %s", FormatColor(st.Color, st.ColorTemInKelvin))
	return out
}

func listenUDP(timeout time.Duration) (*net.UDPConn, error) {
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: ListenPort}
	pc, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	_ = pc.SetReadDeadline(time.Now().Add(timeout))
	return pc, nil
}

var scanMsg = []byte(`{"msg":{"cmd":"scan","data":{"account_topic":"reserve"}}}`)

// Discover finds LAN-enabled Govee devices.
func Discover(timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	pc, err := listenUDP(timeout)
	if err != nil {
		return nil, err
	}
	defer pc.Close()

	blast := func() {
		targets := []struct {
			addr      string
			multicast bool
		}{
			{MulticastIP, true},
			{"255.255.255.255", false},
		}
		for _, t := range targets {
			c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(t.addr), Port: ScanPort})
			if err != nil {
				continue
			}
			if !t.multicast {
				_ = c.SetWriteBuffer(1 << 16)
			}
			_, _ = c.Write(scanMsg)
			_ = c.Close()
		}
	}

	found := map[string]Device{}
	deadline := time.Now().Add(timeout)
	blast()
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = pc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			blast()
			continue
		}
		var env envelope
		if err := json.Unmarshal(buf[:n], &env); err != nil || env.Msg.Cmd != "scan" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(env.Msg.Data, &data); err != nil {
			continue
		}
		ip, _ := data["ip"].(string)
		if ip == "" && addr != nil {
			ip = addr.IP.String()
		}
		sku, _ := data["sku"].(string)
		dev, _ := data["device"].(string)
		key := dev
		if key == "" {
			key = ip
		}
		found[key] = Device{IP: ip, SKU: sku, Device: dev}
	}
	out := make([]Device, 0, len(found))
	for _, d := range found {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out, nil
}

// CacheDir returns ~/.cache/govee-lan (or XDG_CACHE_HOME).
func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "govee-lan")
}

// CacheIPs writes discovered IPs to the cache file.
func CacheIPs(devices []Device) error {
	dir := CacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	var ips []string
	for _, d := range devices {
		if d.IP == "" {
			continue
		}
		if _, ok := seen[d.IP]; ok {
			continue
		}
		seen[d.IP] = struct{}{}
		ips = append(ips, d.IP)
	}
	sort.Strings(ips)
	data := ""
	for _, ip := range ips {
		data += ip + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "ips"), []byte(data), 0o644)
}

// LoadCachedIPs returns previously discovered IPs.
func LoadCachedIPs() []string {
	b, err := os.ReadFile(filepath.Join(CacheDir(), "ips"))
	if err != nil {
		return nil
	}
	var ips []string
	for _, line := range splitLines(string(b)) {
		if line != "" {
			ips = append(ips, line)
		}
	}
	return ips
}

// DefaultDeviceIP returns the first cached IP, or empty.
func DefaultDeviceIP() string {
	ips := LoadCachedIPs()
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
