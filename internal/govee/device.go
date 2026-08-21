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

	"github.com/Chris-Alexander-Pop/gvl/internal/trace"
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
	IP   string
	mu   sync.Mutex // single datagram
	opMu sync.Mutex // status listen + exec (UDP 4002 is exclusive)
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

// sendBurst sends the same UDP command twice. Govee bulbs often drop the first
// packet after idle; a quick repeat is cheap and makes interactive control reliable.
func (c *Client) sendBurst(cmd string, data any) error {
	if err := c.send(cmd, data); err != nil {
		return err
	}
	time.Sleep(40 * time.Millisecond)
	return c.send(cmd, data)
}

// Turn sets power on (true) or off (false). Always a two-packet burst.
// Prefer ExecTurn when the result must be confirmed.
func (c *Client) Turn(on bool) error {
	return c.PushTurn(on)
}

// Brightness sets brightness 0-100. Always a two-packet burst.
func (c *Client) Brightness(v int) error {
	return c.PushBrightness(v)
}

// Color sets an RGB color (clears color temperature). Always a two-packet burst.
func (c *Client) Color(rgb RGB) error {
	return c.PushColor(rgb)
}

// Temp sets color temperature in Kelvin. Always a two-packet burst.
func (c *Client) Temp(kelvin int) error {
	return c.PushTemp(kelvin)
}

func turnPayload(on bool) map[string]int {
	v := 0
	if on {
		v = 1
	}
	return map[string]int{"value": v}
}

// ExecTurn turns the light on/off, retries until status matches, and returns
// the confirmed device status.
func (c *Client) ExecTurn(on bool) (*Status, error) {
	want := 0
	if on {
		want = 1
	}
	return c.execUntil("turn",
		func() error { return c.PushTurn(on) },
		func(st *Status) bool { return st.OnOff == want },
		func(st *Status) string {
			return fmt.Sprintf("device still %s after turn", powerWord(st.OnOff))
		},
	)
}

// ExecBrightness sets brightness, retries until status matches, and returns status.
func (c *Client) ExecBrightness(v int) (*Status, error) {
	want := ClampBrightness(v)
	return c.execUntil("brightness",
		func() error { return c.PushBrightness(want) },
		func(st *Status) bool { return st.Brightness == want },
		func(st *Status) string {
			return fmt.Sprintf("brightness still %d%% (want %d%%)", st.Brightness, want)
		},
	)
}

// ExecColor sets RGB color, retries until status matches, and returns status.
func (c *Client) ExecColor(rgb RGB) (*Status, error) {
	return c.execUntil("color",
		func() error { return c.PushColor(rgb) },
		func(st *Status) bool { return colorMatches(st, rgb) },
		func(st *Status) string {
			return fmt.Sprintf("color still %s (want %s)", FormatColor(st.Color, st.ColorTemInKelvin), FormatColor(rgb, 0))
		},
	)
}

// ExecTemp sets color temperature, retries until status matches, and returns status.
func (c *Client) ExecTemp(kelvin int) (*Status, error) {
	return c.execUntil("temp",
		func() error { return c.PushTemp(kelvin) },
		func(st *Status) bool { return tempMatches(st, kelvin) },
		func(st *Status) string {
			return fmt.Sprintf("temp still %s (want %s)", FormatColor(st.Color, st.ColorTemInKelvin), FormatColor(RGB{}, kelvin))
		},
	)
}

// PushTurn sends a turn command twice without waiting for status (chain steps).
func (c *Client) PushTurn(on bool) error {
	return c.sendBurst("turn", turnPayload(on))
}

// PushBrightness sends brightness twice without waiting for status.
func (c *Client) PushBrightness(v int) error {
	return c.sendBurst("brightness", map[string]int{"value": ClampBrightness(v)})
}

// PushColor sends color twice without waiting for status.
func (c *Client) PushColor(rgb RGB) error {
	return c.sendBurst("colorwc", map[string]any{
		"color":            map[string]int{"r": rgb.R, "g": rgb.G, "b": rgb.B},
		"colorTemInKelvin": 0,
	})
}

// PushTemp sends color temperature twice without waiting for status.
func (c *Client) PushTemp(kelvin int) error {
	return c.sendBurst("colorwc", map[string]any{
		"color":            map[string]int{"r": 0, "g": 0, "b": 0},
		"colorTemInKelvin": kelvin,
	})
}

func (c *Client) lockOp(reason string) func() {
	if !c.opMu.TryLock() {
		trace.Printf("udp wait %s (busy)", reason)
		c.opMu.Lock()
		trace.Printf("udp acquired %s", reason)
	}
	start := time.Now()
	return func() {
		trace.Printf("udp %s held %s", reason, time.Since(start).Round(time.Millisecond))
		c.opMu.Unlock()
	}
}

const execAttempts = 5

// execUntil sends a command, reads status, and retries until ok(st) or attempts are exhausted.
func (c *Client) execUntil(name string, send func() error, ok func(*Status) bool, mismatch func(*Status) string) (*Status, error) {
	unlock := c.lockOp("exec " + name)
	defer unlock()

	t0 := time.Now()
	var lastErr error
	for attempt := 0; attempt < execAttempts; attempt++ {
		delay := settleDelay(attempt)
		trace.Printf("exec %s attempt %d/%d settle=%s", name, attempt+1, execAttempts, delay)
		if err := send(); err != nil {
			trace.Printf("exec %s send: %v", name, err)
			return nil, err
		}
		time.Sleep(delay)
		st, err := c.statusLoop(1500 * time.Millisecond)
		if err != nil {
			trace.Printf("exec %s status: %v", name, err)
			lastErr = err
			continue
		}
		if ok(st) {
			trace.Printf("exec %s ok attempt=%d total=%s look=%s %d%%",
				name, attempt+1, time.Since(t0).Round(time.Millisecond),
				FormatColor(st.Color, st.ColorTemInKelvin), st.Brightness)
			return st, nil
		}
		lastErr = fmt.Errorf("%s", mismatch(st))
		trace.Printf("exec %s mismatch: %v", name, lastErr)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no response from device")
	}
	trace.Printf("exec %s failed after %s: %v", name, time.Since(t0).Round(time.Millisecond), lastErr)
	return nil, lastErr
}

// colorMatches reports whether status reflects the requested RGB (not a colour-temp mode).
func colorMatches(st *Status, want RGB) bool {
	return st.ColorTemInKelvin == 0 && st.Color == want
}

// tempMatches reports whether the bulb is in kelvin mode near the requested value.
// Govee often snaps to 100 K steps, so exact equality is too brittle for ramps.
func tempMatches(st *Status, kelvin int) bool {
	if st == nil || st.ColorTemInKelvin <= 0 {
		return false
	}
	d := st.ColorTemInKelvin - kelvin
	if d < 0 {
		d = -d
	}
	return d <= 100
}

func settleDelay(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 200 * time.Millisecond
	case 1:
		return 350 * time.Millisecond
	case 2:
		return 500 * time.Millisecond
	default:
		return 650 * time.Millisecond
	}
}

func powerWord(onOff int) string {
	if onOff != 0 {
		return "on"
	}
	return "off"
}

// Status queries device status, retrying within timeout (UDP replies are flaky after idle).
func (c *Client) Status(timeout time.Duration) (*Status, error) {
	unlock := c.lockOp("status")
	defer unlock()
	return c.statusLoop(timeout)
}

func (c *Client) statusLoop(timeout time.Duration) (*Status, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	tries := 0
	for {
		remaining := time.Until(deadline)
		if remaining < 150*time.Millisecond {
			break
		}
		perTry := 800 * time.Millisecond
		if remaining < perTry {
			perTry = remaining
		}
		tries++
		st, err := c.statusOnce(perTry)
		if err == nil {
			trace.Printf("status ok try=%d remain=%s look=%s %d%% onOff=%d",
				tries, remaining.Round(time.Millisecond),
				FormatColor(st.Color, st.ColorTemInKelvin), st.Brightness, st.OnOff)
			return st, nil
		}
		trace.Printf("status try=%d: %v", tries, err)
		lastErr = err
		time.Sleep(60 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no response from device")
	}
	return nil, lastErr
}

func (c *Client) statusOnce(timeout time.Duration) (*Status, error) {
	pc, err := listenUDP(timeout)
	if err != nil {
		return nil, fmt.Errorf("listen :%d: %w", ListenPort, err)
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
