package govee

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DeviceState is persisted last-known bulb identity/IP (survives DHCP moves).
type DeviceState struct {
	IP     string `json:"ip"`
	Device string `json:"device,omitempty"`
	SKU    string `json:"sku,omitempty"`
}

// LoadDeviceState reads device.json from dir. Missing file returns empty state, nil error.
func LoadDeviceState(dir string) (DeviceState, error) {
	var st DeviceState
	b, err := os.ReadFile(filepath.Join(dir, "device.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

// SaveDeviceState writes device.json atomically.
func SaveDeviceState(dir string, st DeviceState) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	path := filepath.Join(dir, "device.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
