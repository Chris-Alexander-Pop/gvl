package mode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

// StatusMatches reports whether the bulb is already showing look.
// Kelvin is matched within 100 K, the same slack ExecTemp allows.
func StatusMatches(st *govee.Status, look Look) bool {
	if st == nil || st.Brightness != govee.ClampBrightness(look.Brightness) {
		return false
	}
	if look.Temp > 0 && !govee.BelowWhiteFloor(look.Temp) {
		if st.ColorTemInKelvin <= 0 {
			return false
		}
		d := st.ColorTemInKelvin - look.Temp
		if d < 0 {
			d = -d
		}
		return d <= 100
	}
	rgb := RGBForLook(look)
	return st.ColorTemInKelvin == 0 && st.Color == rgb
}

// Describe formats a look for logs.
func Describe(look Look) string {
	if look.Color != nil {
		return fmt.Sprintf("rgb %d,%d,%d at %d%%", look.Color.R, look.Color.G, look.Color.B, look.Brightness)
	}
	if look.Temp > 0 {
		return fmt.Sprintf("%dK at %d%%", look.Temp, look.Brightness)
	}
	return fmt.Sprintf("%d%%", look.Brightness)
}

// LookFromStatus builds a look from a bulb status payload.
func LookFromStatus(st *govee.Status) Look {
	if st == nil {
		return Look{}
	}
	if st.ColorTemInKelvin > 0 {
		return Look{Temp: st.ColorTemInKelvin, Brightness: st.Brightness}
	}
	c := st.Color
	return Look{Color: &c, Brightness: st.Brightness}
}

// DesiredAt is the look the bulb should show at now.
// False after a sleep ramp that ended by turning off, and before anything has been set.
func (r *Runner) DesiredAt(now time.Time) (Look, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asleep {
		return Look{}, false
	}
	if r.ramp != nil {
		t := 0.0
		if r.ramp.duration > 0 {
			t = now.Sub(r.ramp.start).Seconds() / r.ramp.duration.Seconds()
		}
		return copyLook(lerpLook(r.ramp.from, r.ramp.to, t, r.ramp.splitPct)), true
	}
	if !r.hasLook {
		return Look{}, false
	}
	return copyLook(r.look), true
}

// SetLook replaces the tracked look. Manual colour, temperature, and brightness
// commands use this so the minute reconcile does not put the old ramp back.
func (r *Runner) SetLook(look Look) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ramp = nil
	r.asleep = false
	r.look = copyLook(look)
	r.hasLook = true
	r.persistLocked()
}

// SeenOn is the last observation of bulb power.
func (r *Runner) SeenOn() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seenOn
}

// SetSeenOn records whether the bulb was on at the last status read.
func (r *Runner) SetSeenOn(on bool) {
	r.mu.Lock()
	r.seenOn = on
	r.mu.Unlock()
}

// BlocksReconcile is true while an animated mode or preview owns the bulb.
// Wake and sleep ramps stay reconcilable so a bulb that was off can catch up.
func (r *Runner) BlocksReconcile() bool {
	k := r.Running()
	if k == "" {
		return false
	}
	return !strings.HasPrefix(k, "wake:") && !strings.HasPrefix(k, "sleep:")
}

func (r *Runner) arm(spec rampSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	spec.from = copyLook(spec.from)
	spec.to = copyLook(spec.to)
	r.ramp = &spec
	r.asleep = false
	r.persistLocked()
}

func (r *Runner) settle(endOff bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	end := r.look
	if r.ramp != nil {
		end = r.ramp.to
	}
	r.ramp = nil
	if endOff {
		r.asleep = true
		r.hasLook = false
		r.look = Look{}
	} else {
		r.asleep = false
		r.look = copyLook(end)
		r.hasLook = true
	}
	r.persistLocked()
}

// Resume continues a ramp that was still running when the process last exited.
func (r *Runner) Resume() {
	r.mu.Lock()
	if r.ramp == nil {
		r.mu.Unlock()
		return
	}
	spec := *r.ramp
	overdue := time.Since(spec.start.Add(spec.duration))
	if overdue >= 0 {
		r.mu.Unlock()
		if spec.endOff && overdue > time.Minute {
			r.settle(true)
			log.Printf("gvl: ramp %s finished while down; leaving the bulb's power alone", spec.name)
			return
		}
		if err := r.finishRamp(context.Background(), spec); err != nil {
			log.Printf("gvl: ramp %s resume finish: %v", spec.name, err)
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.kind = spec.name
	r.mu.Unlock()
	go r.launch(ctx, spec)
}

func (r *Runner) observe(name string) bool {
	if r.client == nil {
		return false
	}
	st, err := r.client.Status(2 * time.Second)
	if err != nil {
		log.Printf("gvl: ramp %s status: %v", name, err)
		return false
	}
	on := st.OnOff != 0
	r.mu.Lock()
	was := r.seenOn
	known := r.seenKnown
	r.seenOn = on
	r.seenKnown = true
	r.mu.Unlock()
	if !known || was == on {
		return true
	}
	if on {
		log.Printf("gvl: ramp %s bulb came on, catching up", name)
		return true
	}
	log.Printf("gvl: ramp %s bulb is off, not turning it on", name)
	return true
}

type holdFile struct {
	Asleep bool      `json:"asleep,omitempty"`
	Look   *Look     `json:"look,omitempty"`
	Ramp   *holdRamp `json:"ramp,omitempty"`
}

type holdRamp struct {
	Name     string        `json:"name"`
	From     Look          `json:"from"`
	To       Look          `json:"to"`
	Start    time.Time     `json:"start"`
	Duration time.Duration `json:"duration"`
	EndOff   bool          `json:"end_off,omitempty"`
	SplitPct int           `json:"split_pct,omitempty"`
}

// UseHoldFile loads the last look and any in-progress ramp from path.
func (r *Runner) UseHoldFile(path string) {
	r.mu.Lock()
	r.holdPath = path
	r.mu.Unlock()
	if err := r.load(); err != nil {
		log.Printf("gvl: load hold: %v", err)
	}
}

func (r *Runner) load() error {
	r.mu.Lock()
	path := r.holdPath
	r.mu.Unlock()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file holdFile
	if err := json.Unmarshal(b, &file); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asleep = file.Asleep
	r.hasLook = false
	if file.Look != nil {
		r.look = copyLook(*file.Look)
		r.hasLook = true
	}
	r.ramp = nil
	if file.Ramp != nil {
		r.ramp = &rampSpec{
			name:     file.Ramp.Name,
			from:     copyLook(file.Ramp.From),
			to:       copyLook(file.Ramp.To),
			start:    file.Ramp.Start,
			duration: file.Ramp.Duration,
			endOff:   file.Ramp.EndOff,
			splitPct: file.Ramp.SplitPct,
		}
		r.asleep = false
	}
	return nil
}

func (r *Runner) persistLocked() {
	if r.holdPath == "" {
		return
	}
	file := holdFile{Asleep: r.asleep}
	if r.hasLook {
		l := copyLook(r.look)
		file.Look = &l
	}
	if r.ramp != nil {
		file.Ramp = &holdRamp{
			Name:     r.ramp.name,
			From:     copyLook(r.ramp.from),
			To:       copyLook(r.ramp.to),
			Start:    r.ramp.start,
			Duration: r.ramp.duration,
			EndOff:   r.ramp.endOff,
			SplitPct: r.ramp.splitPct,
		}
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		log.Printf("gvl: save hold: %v", err)
		return
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(r.holdPath), 0o755); err != nil {
		log.Printf("gvl: save hold: %v", err)
		return
	}
	tmp := r.holdPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("gvl: save hold: %v", err)
		return
	}
	if err := os.Rename(tmp, r.holdPath); err != nil {
		log.Printf("gvl: save hold: %v", err)
	}
}
