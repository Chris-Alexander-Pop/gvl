package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

const rediscoverCooldown = 30 * time.Second

func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no response") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout")
}

// ensureReachable checks the current bulb IP and rediscovers if needed.
func (s *Server) ensureReachable() error {
	s.mu.Lock()
	ip := s.client.IP
	auto := s.opts.AutoDiscover
	s.mu.Unlock()

	if ip != "" {
		if _, err := s.client.Status(1200 * time.Millisecond); err == nil {
			return nil
		}
	}
	if !auto {
		if ip == "" {
			return fmt.Errorf("no device IP configured")
		}
		return fmt.Errorf("no response from device")
	}
	return s.rediscover()
}

// recoverDevice runs rediscover after a failed device op (with cooldown).
func (s *Server) recoverDevice(opErr error) error {
	if !isUnreachable(opErr) || !s.opts.AutoDiscover {
		return opErr
	}
	if err := s.rediscover(); err != nil {
		return opErr
	}
	return nil
}

func (s *Server) rediscover() error {
	s.mu.Lock()
	if time.Since(s.lastRediscover) < rediscoverCooldown && s.lastRediscoverErr != nil {
		err := s.lastRediscoverErr
		s.mu.Unlock()
		return err
	}
	preferredIP := s.client.IP
	preferredDev := s.device.Device
	preferredSKU := s.device.SKU
	subnet := s.opts.DiscoverSubnet
	dataDir := s.opts.DataDir
	s.mu.Unlock()

	log.Printf("gvld: auto-discover: looking for device (last ip=%s)", preferredIP)

	if ip, dev, ok := s.fromMulticast(preferredIP, preferredDev, preferredSKU); ok {
		return s.commitDevice(ip, dev.Device, dev.SKU, dataDir)
	}

	if subnet == "" && preferredIP != "" {
		if derived, err := govee.SubnetFromIP(preferredIP); err == nil {
			subnet = derived
		}
	}
	if subnet == "" {
		err := fmt.Errorf("auto-discover: no subnet (set GVL_DISCOVER_SUBNET or GVL_DEVICE_IP)")
		s.noteRediscover(err)
		return err
	}

	results, err := govee.ProbeSubnet(subnet, 2*time.Second)
	if err != nil {
		s.noteRediscover(err)
		return err
	}
	ip, err := govee.PickProbedIP(results, preferredIP)
	if err != nil {
		s.noteRediscover(err)
		return err
	}
	return s.commitDevice(ip, preferredDev, preferredSKU, dataDir)
}

func (s *Server) fromMulticast(preferredIP, preferredDev, preferredSKU string) (string, govee.Device, bool) {
	devs, err := govee.Discover(3 * time.Second)
	if err != nil || len(devs) == 0 {
		return "", govee.Device{}, false
	}
	_ = govee.CacheIPs(devs)
	if d, ok := pickDevice(devs, preferredIP, preferredDev, preferredSKU); ok {
		return d.IP, d, true
	}
	return "", govee.Device{}, false
}

func pickDevice(devs []govee.Device, preferredIP, preferredDev, preferredSKU string) (govee.Device, bool) {
	if preferredDev != "" {
		for _, d := range devs {
			if d.Device == preferredDev {
				return d, true
			}
		}
	}
	if preferredIP != "" {
		for _, d := range devs {
			if d.IP == preferredIP {
				return d, true
			}
		}
	}
	if preferredSKU != "" {
		var matches []govee.Device
		for _, d := range devs {
			if d.SKU == preferredSKU {
				matches = append(matches, d)
			}
		}
		if len(matches) == 1 {
			return matches[0], true
		}
	}
	if len(devs) == 1 {
		return devs[0], true
	}
	return govee.Device{}, false
}

func (s *Server) commitDevice(ip, device, sku, dataDir string) error {
	s.mu.Lock()
	old := s.client.IP
	s.client.IP = ip
	if device != "" {
		s.device.Device = device
	}
	if sku != "" {
		s.device.SKU = sku
	}
	s.device.IP = ip
	st := s.device
	s.lastRediscover = time.Now()
	s.lastRediscoverErr = nil
	s.mu.Unlock()

	if err := govee.SaveDeviceState(dataDir, st); err != nil {
		log.Printf("gvld: auto-discover: save state: %v", err)
	}
	if old != ip {
		log.Printf("gvld: auto-discover: device IP %s → %s", old, ip)
	} else {
		log.Printf("gvld: auto-discover: confirmed device IP %s", ip)
	}
	return nil
}

func (s *Server) noteRediscover(err error) {
	s.mu.Lock()
	s.lastRediscover = time.Now()
	s.lastRediscoverErr = err
	s.mu.Unlock()
	log.Printf("gvld: auto-discover failed: %v", err)
}

func (s *Server) adoptDiscovered(devs []govee.Device) {
	if len(devs) == 0 {
		return
	}
	s.mu.Lock()
	preferredIP := s.client.IP
	preferredDev := s.device.Device
	preferredSKU := s.device.SKU
	dataDir := s.opts.DataDir
	s.mu.Unlock()

	d, ok := pickDevice(devs, preferredIP, preferredDev, preferredSKU)
	if !ok {
		// Multiple unknown devices — adopt first only if we had no IP yet.
		if preferredIP != "" || preferredDev != "" {
			return
		}
		d = devs[0]
	}
	_ = s.commitDevice(d.IP, d.Device, d.SKU, dataDir)
}
