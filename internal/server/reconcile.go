package server

import (
	"encoding/json"
	"log"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
)

// reconcileLoop puts the tracked look on the bulb about once a minute.
// It never turns the bulb on. A ramp that ran while the light was off
// shows up the next time the bulb is already on.
func (s *Server) reconcileLoop(stop <-chan struct{}) {
	s.reconcile()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.reconcile()
		}
	}
}

func (s *Server) reconcile() {
	if s.runner.BlocksReconcile() {
		return
	}
	look, ok := s.runner.DesiredAt(time.Now())
	if !ok {
		return
	}
	s.deviceMu.Lock()
	defer s.deviceMu.Unlock()
	st, err := s.client.Status(2 * time.Second)
	if err != nil {
		if rerr := s.recoverDevice(err); rerr == nil {
			st, err = s.client.Status(2 * time.Second)
		}
	}
	if err != nil {
		log.Printf("gvld: reconcile: %v", err)
		return
	}
	s.notePower(st)
	if st.OnOff == 0 {
		return
	}
	if mode.StatusMatches(st, look) {
		return
	}
	if err := mode.ApplyAppearance(s.client, look); err != nil {
		log.Printf("gvld: reconcile apply: %v", err)
		return
	}
	log.Printf("gvld: reconcile applied %s", mode.Describe(look))
}

func (s *Server) notePower(st *govee.Status) {
	if st == nil {
		return
	}
	on := st.OnOff != 0
	if !on && s.runner.SeenOn() {
		log.Printf("gvld: reconcile bulb is off, not turning it on")
	}
	s.runner.SetSeenOn(on)
}

// catchUp applies the tracked look after a successful turn-on from the API.
// Caller holds the device lock. A nil status means nothing was sent.
func (s *Server) catchUp(st *govee.Status) (*govee.Status, error) {
	if st == nil || st.OnOff == 0 {
		return nil, nil
	}
	look, ok := s.runner.DesiredAt(time.Now())
	if !ok || mode.StatusMatches(st, look) {
		return nil, nil
	}
	if err := mode.ApplyAppearance(s.client, look); err != nil {
		return nil, err
	}
	log.Printf("gvld: catch-up applied %s", mode.Describe(look))
	updated, err := s.client.Status(2 * time.Second)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Server) remember(cmd string, body map[string]any, st *govee.Status) {
	switch cmd {
	case "off":
		s.runner.SetSeenOn(false)
		return
	case "on":
		s.runner.SetSeenOn(true)
		return
	}
	look, have := s.runner.DesiredAt(time.Now())
	if !have {
		look = mode.LookFromStatus(st)
	}
	switch cmd {
	case "brightness":
		v, ok := asInt(body["value"])
		if !ok {
			return
		}
		look.Brightness = v
	case "color":
		var rgb govee.RGB
		b, err := json.Marshal(body["color"])
		if err != nil {
			return
		}
		if err := json.Unmarshal(b, &rgb); err != nil {
			return
		}
		look.Color = &rgb
		look.Temp = 0
	case "temp":
		v, ok := asInt(body["value"])
		if !ok {
			return
		}
		look.Temp = v
		look.Color = nil
	default:
		return
	}
	s.runner.SetLook(look)
}
