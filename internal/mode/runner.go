package mode

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

// Look describes a static light appearance used by ramps.
type Look struct {
	Color      *govee.RGB `json:"color,omitempty"`
	Temp       int        `json:"temp,omitempty"` // Kelvin; 0 = unset
	Brightness int        `json:"brightness"`
}

// ApplyLook turns on and applies a look, retrying until the bulb confirms.
func ApplyLook(c *govee.Client, look Look) error {
	if _, err := c.ExecTurn(true); err != nil {
		return err
	}
	return applyLookAppearance(c, look)
}

func applyLookAppearance(c *govee.Client, look Look) error {
	return applyLookFrom(c, nil, look, true)
}

func sameAppearance(a, b Look) bool {
	if a.Temp != b.Temp {
		return false
	}
	if a.Color == nil && b.Color == nil {
		return true
	}
	if a.Color == nil || b.Color == nil {
		return false
	}
	return *a.Color == *b.Color
}

func copyLook(l Look) Look {
	out := l
	if l.Color != nil {
		c := *l.Color
		out.Color = &c
	}
	return out
}

// applyLookFrom sends only what changed. Re-sending colorwc for the same kelvin
// resets H60A1 white mix and looks like a brightness pulse. After any color/temp
// change, brightness is re-applied because colorwc often snaps PWM to 100.
func applyLookFrom(c *govee.Client, prev *Look, look Look, confirm bool) error {
	sendColor := prev == nil || !sameAppearance(*prev, look)
	sendBright := prev == nil || prev.Brightness != look.Brightness || sendColor
	if !sendColor && !sendBright {
		return nil
	}
	if look.Temp > 0 && sendColor {
		if confirm {
			if _, err := c.ExecTemp(look.Temp); err != nil {
				return err
			}
		} else if err := c.PushTemp(look.Temp); err != nil {
			return err
		}
	} else if look.Color != nil && sendColor {
		if confirm {
			if _, err := c.ExecColor(*look.Color); err != nil {
				return err
			}
		} else if err := c.PushColor(*look.Color); err != nil {
			return err
		}
	}
	if !sendBright {
		return nil
	}
	if confirm {
		_, err := c.ExecBrightness(look.Brightness)
		return err
	}
	return c.PushBrightness(look.Brightness)
}

// kelvinRamp is true when both ends are colour-temp looks. White CCT stays
// on the WW/CW LEDs; anything warmer than KelvinMin is RGB (candle).
func kelvinRamp(from, to Look) bool {
	return from.Temp > 0 && to.Temp > 0
}

func roundKelvin(k float64) int {
	if k < 1 {
		return 0
	}
	return int(math.Round(k/100) * 100)
}

// smoothstep is Hermite ease-in-out: 3t² − 2t³. Slow at both ends, faster in
// the middle. Applied to wall-clock t before interpolating look.
func smoothstep(t float64) float64 {
	return t * t * (3 - 2*t)
}

// brightnessGamma maps Govee 0–100 (roughly linear PWM / flux) to a curve
// closer to perceived brightness. Linear % lerp keeps a 100→1 sleep ramp
// looking "still on" until the last minutes; this drops PWM faster up front.
const brightnessGamma = 2.2

func perceptualY(brightness int) float64 {
	x := float64(govee.ClampBrightness(brightness)) / 100
	if x <= 0 {
		return 0
	}
	return math.Pow(x, 1/brightnessGamma)
}

func lerpBrightness(from, to int, t float64) int {
	if t <= 0 {
		return govee.ClampBrightness(from)
	}
	if t >= 1 {
		return govee.ClampBrightness(to)
	}
	y := govee.Lerp(perceptualY(from), perceptualY(to), t)
	b := int(math.Round(100 * math.Pow(y, brightnessGamma)))
	return govee.ClampBrightness(b)
}

func lookForKelvin(k, brightness int) Look {
	if govee.BelowWhiteFloor(k) {
		rgb := govee.KelvinToRGB(k)
		return Look{Color: &rgb, Brightness: brightness}
	}
	return Look{Temp: k, Brightness: brightness}
}

// twoPhase is a ramp that hands off between white LEDs and RGB.
func twoPhase(from, to Look) bool {
	return colourSecond(from, to) || colourFirst(from, to)
}

// colourSecond is sleep-style: white CCT first, then RGB (candle kelvin or a colour).
func colourSecond(from, to Look) bool {
	if from.Temp < govee.KelvinMin {
		return false
	}
	if govee.BelowWhiteFloor(to.Temp) {
		return true
	}
	return to.Color != nil && to.Temp == 0
}

// colourFirst is wake-style: RGB first, then white CCT.
func colourFirst(from, to Look) bool {
	return from.Color != nil && from.Temp == 0 && to.Temp >= govee.KelvinMin
}

// phaseSplit is the wall-clock fraction spent on the start look.
// splitPct 1–99 is explicit; 0 means auto (kelvin-span, or 50% for mixed).
func phaseSplit(from, to Look, splitPct int) float64 {
	if !twoPhase(from, to) {
		return 0
	}
	if splitPct > 0 && splitPct < 100 {
		return float64(splitPct) / 100
	}
	if kelvinRamp(from, to) {
		den := float64(from.Temp - to.Temp)
		if den <= 0 {
			return 0.5
		}
		s := float64(from.Temp-govee.KelvinMin) / den
		if s < 0.05 {
			return 0.05
		}
		if s > 0.95 {
			return 0.95
		}
		return s
	}
	return 0.5
}

func clamp01t(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// lerpLook interpolates from→to at wall-clock t in [0,1].
// Sleep (colour second): white holds from-brightness; RGB runs 100% → to-brightness
// (100% colour is still dimmer than low kelvin on the H60A1). Wake keeps a
// full-duration brightness lerp. splitPct is the start-look time share.
func lerpLook(from, to Look, t float64, splitPct int) Look {
	t = clamp01t(t)
	s := phaseSplit(from, to, splitPct)
	if s > 0 && s < 1 {
		if t <= s {
			u := 0.0
			if s > 0 {
				u = smoothstep(t / s)
			}
			b := from.Brightness
			if !colourSecond(from, to) {
				b = lerpBrightness(from.Brightness, to.Brightness, smoothstep(t))
			}
			return lerpStartPhase(from, to, u, b)
		}
		u := 0.0
		if s < 1 {
			u = smoothstep((t - s) / (1 - s))
		}
		b := lerpBrightness(from.Brightness, to.Brightness, smoothstep(t))
		if colourSecond(from, to) {
			b = lerpBrightness(100, to.Brightness, u)
		}
		return lerpEndPhase(from, to, u, b)
	}
	b := lerpBrightness(from.Brightness, to.Brightness, smoothstep(t))
	ts := smoothstep(t)
	if kelvinRamp(from, to) {
		k := roundKelvin(govee.Lerp(float64(from.Temp), float64(to.Temp), ts))
		return lookForKelvin(k, b)
	}
	rgb := govee.LerpRGB(RGBForLook(from), RGBForLook(to), ts)
	return Look{Color: &rgb, Brightness: b}
}

func lerpStartPhase(from, to Look, u float64, b int) Look {
	if from.Temp >= govee.KelvinMin {
		k := roundKelvin(govee.Lerp(float64(from.Temp), float64(govee.KelvinMin), u))
		return Look{Temp: k, Brightness: b}
	}
	if from.Color != nil {
		c := *from.Color
		return Look{Color: &c, Brightness: b}
	}
	return lookForKelvin(from.Temp, b)
}

func lerpEndPhase(from, to Look, u float64, b int) Look {
	if to.Color != nil && to.Temp == 0 {
		c := *to.Color
		return Look{Color: &c, Brightness: b}
	}
	if kelvinRamp(from, to) {
		k := roundKelvin(govee.Lerp(float64(govee.KelvinMin-100), float64(to.Temp), u))
		return lookForKelvin(k, b)
	}
	k0 := govee.KelvinMin
	if to.Temp < k0 {
		k0 = to.Temp
	}
	k := roundKelvin(govee.Lerp(float64(k0), float64(to.Temp), u))
	return lookForKelvin(k, b)
}

func looksEqual(a, b Look) bool {
	if a.Brightness != b.Brightness || a.Temp != b.Temp {
		return false
	}
	if a.Color == nil && b.Color == nil {
		return true
	}
	if a.Color == nil || b.Color == nil {
		return false
	}
	return *a.Color == *b.Color
}

const previewSamples = 400

// PreviewLooks is the timed ramp sampled densely, with duplicate looks removed.
// Used by Test Ramp: one confirmed Exec per distinct look, no wall-clock wait.
func PreviewLooks(from, to Look, splitPct int) []Look {
	out := make([]Look, 0, 64)
	for i := 0; i <= previewSamples; i++ {
		t := float64(i) / float64(previewSamples)
		look := lerpLook(from, to, t, splitPct)
		if len(out) > 0 && looksEqual(out[len(out)-1], look) {
			continue
		}
		out = append(out, look)
	}
	end := lerpLook(from, to, 1, splitPct)
	if len(out) == 0 || !looksEqual(out[len(out)-1], end) {
		out = append(out, end)
	}
	return out
}

// RGBForLook returns the RGB used for blending (temp approximated when set).
func RGBForLook(look Look) govee.RGB {
	if look.Temp > 0 {
		return govee.KelvinToRGB(look.Temp)
	}
	if look.Color != nil {
		return *look.Color
	}
	return govee.NamedColors["warm-white"]
}

// Config for animated modes.
type Config struct {
	Name          string
	Interval      time.Duration
	Speed         float64
	Brightness    int
	MinBrightness int
	ColorA        *govee.RGB
	ColorB        *govee.RGB
	Color         *govee.RGB
	TempA         int
	TempB         int
	Low           int
	High          int
	Temp          int
}

// Runner runs modes/ramps with cancel support.
type Runner struct {
	client *govee.Client
	mu     sync.Mutex
	cancel context.CancelFunc
	kind   string
}

// NewRunner creates a mode runner bound to a device client.
func NewRunner(client *govee.Client) *Runner {
	return &Runner{client: client}
}

// Stop cancels any running mode or ramp.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.kind = ""
}

// Running returns the active mode/ramp name, or empty.
func (r *Runner) Running() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kind
}

func (r *Runner) start(kind string) context.Context {
	r.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel = cancel
	r.kind = kind
	r.mu.Unlock()
	return ctx
}

func (r *Runner) clearIf(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.kind == kind {
		r.kind = ""
		r.cancel = nil
	}
}

// StartMode runs an animated mode in the background.
func (r *Runner) StartMode(cfg Config) {
	ctx := r.start(cfg.Name)
	go func() {
		defer r.clearIf(cfg.Name)
		r.runMode(ctx, cfg)
	}()
}

// StartRamp runs a one-shot from→to ramp over duration.
func (r *Runner) StartRamp(name string, from, to Look, duration time.Duration, endOff bool, splitPct int) {
	ctx := r.start(name)
	go func() {
		defer r.clearIf(name)
		if err := r.runRamp(ctx, name, from, to, duration, endOff, splitPct); err != nil && ctx.Err() == nil {
			log.Printf("gvl: ramp %s: %v", name, err)
		}
	}()
}

// StartPreview plays from→to as fast as the bulb confirms each look.
func (r *Runner) StartPreview(name string, from, to Look, endOff bool, splitPct int) int {
	frames := PreviewLooks(from, to, splitPct)
	if s := phaseSplit(from, to, splitPct); s > 0 {
		log.Printf("gvl: preview %s split=%.0f/%.0f start/end phase", name, s*100, (1-s)*100)
	}
	ctx := r.start(name)
	go func() {
		defer r.clearIf(name)
		if err := r.runPreview(ctx, name, frames, endOff); err != nil && ctx.Err() == nil {
			log.Printf("gvl: preview %s: %v", name, err)
		}
	}()
	return len(frames)
}

func (r *Runner) runPreview(ctx context.Context, name string, frames []Look, endOff bool) error {
	log.Printf("gvl: preview %s frames=%d end_off=%v", name, len(frames), endOff)
	t0 := time.Now()
	var prev *Look
	var runErr error
	for i, look := range frames {
		if err := ctx.Err(); err != nil {
			log.Printf("gvl: preview %s cancelled at %d/%d", name, i, len(frames))
			runErr = err
			break
		}
		var err error
		if i == 0 {
			err = ApplyLook(r.client, look)
		} else {
			err = applyLookFrom(r.client, prev, look, true)
		}
		if err != nil {
			runErr = fmt.Errorf("frame %d/%d: %w", i+1, len(frames), err)
			log.Printf("gvl: preview %s: %v", name, runErr)
			break
		}
		cp := copyLook(look)
		prev = &cp
	}
	if endOff && ctx.Err() == nil {
		if _, err := r.client.ExecTurn(false); err != nil {
			log.Printf("gvl: preview %s end-off failed: %v", name, err)
			if runErr == nil {
				runErr = err
			}
		}
	}
	log.Printf("gvl: preview %s done frames=%d in %s err=%v", name, len(frames), time.Since(t0).Round(time.Millisecond), runErr)
	return runErr
}

func (r *Runner) runRamp(ctx context.Context, name string, from, to Look, duration time.Duration, endOff bool, splitPct int) error {
	if duration <= 0 {
		duration = time.Minute
	}
	s := phaseSplit(from, to, splitPct)
	log.Printf("gvl: ramp %s start duration=%s kelvin=%v two_phase=%v split=%.0f/%.0f end_off=%v",
		name, duration.Round(time.Second), kelvinRamp(from, to), s > 0, s*100, (1-s)*100, endOff)
	if err := ApplyLook(r.client, from); err != nil {
		return fmt.Errorf("start look: %w", err)
	}
	prev := copyLook(from)

	const frame = 400 * time.Millisecond
	const confirmEvery = 10 * time.Second
	start := time.Now()
	lastConfirm := start
	ticker := time.NewTicker(frame)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("gvl: ramp %s cancelled", name)
			return ctx.Err()
		case now := <-ticker.C:
			t := now.Sub(start).Seconds() / duration.Seconds()
			if t >= 1 {
				return r.finishRamp(name, to, endOff)
			}
			look := lerpLook(from, to, t, splitPct)
			var err error
			if now.Sub(lastConfirm) >= confirmEvery {
				err = applyLookFrom(r.client, &prev, look, true)
				lastConfirm = now
			} else {
				err = applyLookFrom(r.client, &prev, look, false)
			}
			if err != nil {
				log.Printf("gvl: ramp %s frame: %v", name, err)
			} else {
				prev = copyLook(look)
			}
		}
	}
}

func (r *Runner) finishRamp(name string, to Look, endOff bool) error {
	if endOff {
		if _, err := r.client.ExecTurn(false); err != nil {
			log.Printf("gvl: ramp %s end-off failed: %v", name, err)
			return err
		}
		log.Printf("gvl: ramp %s ended (off)", name)
		return nil
	}
	if err := applyLookAppearance(r.client, to); err != nil {
		log.Printf("gvl: ramp %s end look failed: %v", name, err)
		return err
	}
	log.Printf("gvl: ramp %s ended (look)", name)
	return nil
}

func (r *Runner) prepare(cfg Config) {
	if _, err := r.client.ExecTurn(true); err != nil {
		log.Printf("gvl: mode %s prepare on: %v", cfg.Name, err)
	}
	if _, err := r.client.ExecBrightness(cfg.Brightness); err != nil {
		log.Printf("gvl: mode %s prepare brightness: %v", cfg.Name, err)
	}
}

func (r *Runner) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (r *Runner) runMode(ctx context.Context, cfg Config) {
	if cfg.Interval <= 0 {
		cfg.Interval = 40 * time.Millisecond
	}
	if cfg.Speed <= 0 {
		cfg.Speed = 1
	}
	if cfg.Brightness <= 0 {
		cfg.Brightness = 100
	}
	if cfg.MinBrightness < 0 {
		cfg.MinBrightness = 15
	}
	r.prepare(cfg)

	switch cfg.Name {
	case "rainbow":
		r.rainbow(ctx, cfg)
	case "cycle", "aurora":
		r.cycle(ctx, cfg)
	case "fade":
		a, b := govee.NamedColors["orange"], govee.NamedColors["blue"]
		if cfg.ColorA != nil {
			a = *cfg.ColorA
		}
		if cfg.ColorB != nil {
			b = *cfg.ColorB
		}
		r.fade(ctx, cfg, a, b)
	case "breathe":
		c := govee.NamedColors["warm-white"]
		if cfg.Color != nil {
			c = *cfg.Color
		}
		r.breathe(ctx, cfg, c)
	case "pulse":
		r.pulse(ctx, cfg)
	case "temp-fade":
		a, b := govee.TempPresets["warm"], govee.TempPresets["cool"]
		if cfg.TempA > 0 {
			a = cfg.TempA
		}
		if cfg.TempB > 0 {
			b = cfg.TempB
		}
		r.tempFade(ctx, cfg, a, b)
	case "temp-cycle":
		low, high := govee.TempPresets["warm"], govee.TempPresets["cool"]
		if cfg.Low > 0 {
			low = cfg.Low
		}
		if cfg.High > 0 {
			high = cfg.High
		}
		r.tempFade(ctx, cfg, low, high)
	case "fire":
		r.fire(ctx, cfg)
	case "candle":
		r.candle(ctx, cfg)
	case "blend":
		c := govee.NamedColors["blue"]
		if cfg.Color != nil {
			c = *cfg.Color
		}
		temp := govee.TempPresets["warm"]
		if cfg.Temp > 0 {
			temp = cfg.Temp
		}
		r.blend(ctx, cfg, c, temp)
	}
}

func (r *Runner) rainbow(ctx context.Context, cfg Config) {
	start := time.Now()
	period := (14 * time.Second).Seconds() / cfg.Speed
	for {
		hue := math.Mod(time.Since(start).Seconds()/period, 1)
		_ = r.client.Color(govee.HSVToRGB(hue, 1, 1))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

var auroraColors = []govee.RGB{
	govee.NamedColors["teal"],
	govee.NamedColors["cyan"],
	govee.NamedColors["blue"],
	govee.NamedColors["indigo"],
	govee.NamedColors["purple"],
	govee.NamedColors["magenta"],
	govee.NamedColors["pink"],
}

func (r *Runner) cycle(ctx context.Context, cfg Config) {
	stops := auroraColors
	if cfg.Name == "cycle" {
		stops = paletteByHue()
	}
	n := len(stops)
	start := time.Now()
	period := (5.5 * float64(n)) / cfg.Speed
	for {
		pos := math.Mod(time.Since(start).Seconds()/period, 1) * float64(n)
		idx := int(pos) % n
		nxt := (idx + 1) % n
		frac := pos - math.Floor(pos)
		_ = r.client.Color(govee.LerpRGB(stops[idx], stops[nxt], frac))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func paletteByHue() []govee.RGB {
	type item struct {
		h   float64
		rgb govee.RGB
	}
	items := make([]item, 0, len(govee.NamedColors))
	for _, rgb := range govee.NamedColors {
		h, _, _ := govee.RGBToHSV(rgb)
		items = append(items, item{h, rgb})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].h < items[i].h {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	out := make([]govee.RGB, len(items))
	for i, it := range items {
		out[i] = it.rgb
	}
	return out
}

func (r *Runner) fade(ctx context.Context, cfg Config, a, b govee.RGB) {
	start := time.Now()
	period := 7.0 / cfg.Speed
	for {
		t := govee.SmoothPulse(math.Mod(time.Since(start).Seconds()/period, 1))
		_ = r.client.Color(govee.LerpRGB(a, b, t))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) breathe(ctx context.Context, cfg Config, color govee.RGB) {
	start := time.Now()
	period := 5.5 / cfg.Speed
	span := cfg.Brightness - cfg.MinBrightness
	for {
		t := govee.SmoothPulse(math.Mod(time.Since(start).Seconds()/period, 1))
		level := cfg.MinBrightness + int(math.Round(float64(span)*t))
		_ = r.client.Color(color)
		_ = r.client.Brightness(level)
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) pulse(ctx context.Context, cfg Config) {
	baseHue, baseSat, baseVal := 0.08, 0.35, 1.0
	if cfg.Color != nil {
		baseHue, baseSat, baseVal = govee.RGBToHSV(*cfg.Color)
	}
	start := time.Now()
	brightPeriod := 4.5 / cfg.Speed
	huePeriod := 28.0 / cfg.Speed
	span := cfg.Brightness - cfg.MinBrightness
	for {
		brightT := govee.SmoothPulse(math.Mod(time.Since(start).Seconds()/brightPeriod, 1))
		hue := math.Mod(baseHue+time.Since(start).Seconds()/huePeriod, 1)
		level := cfg.MinBrightness + int(math.Round(float64(span)*brightT))
		drift := 0.82 + 0.18*brightT
		_ = r.client.Color(govee.HSVToRGB(hue, baseSat*drift, baseVal))
		_ = r.client.Brightness(level)
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) tempFade(ctx context.Context, cfg Config, a, b int) {
	start := time.Now()
	period := 7.0 / cfg.Speed
	for {
		t := govee.SmoothPulse(math.Mod(time.Since(start).Seconds()/period, 1))
		_ = r.client.Temp(int(math.Round(govee.Lerp(float64(a), float64(b), t))))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) fire(ctx context.Context, cfg Config) {
	base := govee.NamedColors["orange"]
	ember := govee.NamedColors["red"]
	warm := govee.NamedColors["amber"]
	flicker := 0.55
	targetFlicker := flicker
	current := base
	target := base
	nextAt := time.Now()
	for {
		now := time.Now()
		if !now.Before(nextAt) {
			targetFlicker = clamp01(targetFlicker + (rand.Float64()-0.5)*0.35)
			roll := rand.Float64()
			if roll < 0.1 {
				target = warm
			} else if targetFlicker > 0.7 {
				target = ember
			} else {
				target = base
			}
			nextAt = now.Add(time.Duration((0.12+rand.Float64()*0.33)/cfg.Speed) * time.Second)
		}
		flicker = govee.Lerp(flicker, targetFlicker, 0.18)
		current = govee.LerpRGB(current, target, 0.16)
		_ = r.client.Color(current)
		level := cfg.MinBrightness + int(math.Round(float64(cfg.Brightness-cfg.MinBrightness)*(0.5+0.5*flicker)))
		_ = r.client.Brightness(level)
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) candle(ctx context.Context, cfg Config) {
	center := float64(govee.TempPresets["candle"])
	currentTemp := center
	targetTemp := center
	currentLevel := float64(cfg.Brightness)
	targetLevel := float64(cfg.Brightness)
	nextAt := time.Now()
	for {
		now := time.Now()
		if !now.Before(nextAt) {
			targetTemp = center + (rand.Float64()*280 - 140)
			targetLevel = float64(cfg.MinBrightness) + rand.Float64()*float64(cfg.Brightness-cfg.MinBrightness)
			nextAt = now.Add(time.Duration((0.15+rand.Float64()*0.4)/cfg.Speed) * time.Second)
		}
		currentTemp = govee.Lerp(currentTemp, targetTemp, 0.14)
		currentLevel = govee.Lerp(currentLevel, targetLevel, 0.12)
		_ = r.client.Temp(int(math.Round(currentTemp)))
		_ = r.client.Brightness(int(math.Round(currentLevel)))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func (r *Runner) blend(ctx context.Context, cfg Config, color govee.RGB, temp int) {
	start := time.Now()
	period := 8.0 / cfg.Speed
	tempRGB := govee.KelvinToRGB(temp)
	for {
		t := govee.SmoothPulse(math.Mod(time.Since(start).Seconds()/period, 1))
		_ = r.client.Color(govee.LerpRGB(color, tempRGB, t))
		if !r.sleep(ctx, cfg.Interval) {
			return
		}
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// Help descriptions for modes.
var Help = map[string]string{
	"rainbow":    "smooth continuous hue cycle",
	"cycle":      "smoothly cycle through the named color palette",
	"fade":       "breathe between two colors",
	"breathe":    "pulse brightness on a fixed color",
	"pulse":      "brightness pulse with a slow hue drift",
	"temp-fade":  "breathe between two color temperatures",
	"temp-cycle": "smooth sine sweep across a temperature range",
	"aurora":     "slow drift through teal / purple / pink tones",
	"fire":       "warm orange-red flicker",
	"candle":     "soft warm-white flicker like a candle",
	"blend":      "crossfade between a color and a temperature",
}

// Names lists known mode names.
func Names() []string {
	return []string{
		"rainbow", "cycle", "fade", "breathe", "pulse",
		"temp-fade", "temp-cycle", "aurora", "fire", "candle", "blend",
	}
}
