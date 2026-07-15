package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/Chris-Alexander-Pop/gvl/internal/schedule"
)

// Client talks to gvld over HTTP.
type Client struct {
	Base   string
	Token  string
	HTTP   *http.Client
}

// New creates an API client.
func New(base, token string) *Client {
	base = strings.TrimRight(base, "/")
	return &Client{
		Base:  base,
		Token: token,
		HTTP:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Health checks /health.
func (c *Client) Health() error {
	return c.do(http.MethodGet, "/health", nil, nil)
}

// Status returns device status from the daemon.
func (c *Client) Status() (*govee.Status, string, error) {
	var resp struct {
		IP     string        `json:"ip"`
		Status *govee.Status `json:"status"`
	}
	if err := c.do(http.MethodGet, "/v1/status", nil, &resp); err != nil {
		return nil, "", err
	}
	return resp.Status, resp.IP, nil
}

// DeviceCmd sends a simple device command.
func (c *Client) DeviceCmd(cmd string, payload map[string]any) (*govee.Status, error) {
	var st govee.Status
	body := map[string]any{"cmd": cmd}
	for k, v := range payload {
		body[k] = v
	}
	if err := c.do(http.MethodPost, "/v1/device", body, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Stop stops modes/ramps.
func (c *Client) Stop() error {
	return c.do(http.MethodPost, "/v1/stop", nil, nil)
}

// StartMode starts an animated mode on the daemon.
func (c *Client) StartMode(cfg mode.Config) error {
	return c.do(http.MethodPost, "/v1/mode", cfg, nil)
}

// ListSchedules lists schedules.
func (c *Client) ListSchedules() ([]schedule.Entry, error) {
	var list []schedule.Entry
	if err := c.do(http.MethodGet, "/v1/schedules", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// GetSchedule fetches one schedule.
func (c *Client) GetSchedule(id string) (schedule.Entry, error) {
	var e schedule.Entry
	err := c.do(http.MethodGet, "/v1/schedules/"+id, nil, &e)
	return e, err
}

// PutSchedule creates or updates a schedule.
func (c *Client) PutSchedule(e schedule.Entry) error {
	return c.do(http.MethodPut, "/v1/schedules/"+e.ID, e, nil)
}

// DeleteSchedule deletes a schedule.
func (c *Client) DeleteSchedule(id string) error {
	return c.do(http.MethodDelete, "/v1/schedules/"+id, nil, nil)
}

// SetScheduleEnabled enables/disables a schedule.
func (c *Client) SetScheduleEnabled(id string, enabled bool) error {
	return c.do(http.MethodPost, "/v1/schedules/"+id+"/enabled", map[string]bool{"enabled": enabled}, nil)
}

// RunSchedule fires a schedule now.
func (c *Client) RunSchedule(id string) error {
	return c.do(http.MethodPost, "/v1/schedules/"+id+"/run", nil, nil)
}

// Discover asks the daemon to discover devices.
func (c *Client) Discover() ([]govee.Device, error) {
	var list []govee.Device
	if err := c.do(http.MethodPost, "/v1/discover", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}
