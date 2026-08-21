package govee

import "math"

// Lerp linearly interpolates a→b by t in [0,1].
func Lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// LerpRGB interpolates two colors.
func LerpRGB(a, b RGB, t float64) RGB {
	return RGB{
		R: int(math.Round(Lerp(float64(a.R), float64(b.R), t))),
		G: int(math.Round(Lerp(float64(a.G), float64(b.G), t))),
		B: int(math.Round(Lerp(float64(a.B), float64(b.B), t))),
	}
}

// SmoothPulse returns 0→1→0 easing for t in [0,1].
func SmoothPulse(t float64) float64 {
	return (1.0 - math.Cos(t*2.0*math.Pi)) / 2.0
}

// RGBToHSV converts 0-255 RGB to HSV (h in [0,1]).
func RGBToHSV(c RGB) (h, s, v float64) {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	v = max
	d := max - min
	if max == 0 {
		s = 0
	} else {
		s = d / max
	}
	if d == 0 {
		h = 0
		return
	}
	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	return
}

// HSVToRGB converts HSV to RGB.
func HSVToRGB(h, s, v float64) RGB {
	for h < 0 {
		h++
	}
	h = math.Mod(h, 1)
	s = clamp01(s)
	v = clamp01(v)
	i := math.Floor(h * 6)
	f := h*6 - i
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return RGB{
		R: int(math.Round(r * 255)),
		G: int(math.Round(g * 255)),
		B: int(math.Round(b * 255)),
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

// KelvinToRGB approximates white-point RGB for temperature blending.
func KelvinToRGB(kelvin int) RGB {
	k := float64(clamp(kelvin, 1800, 9000)) / 100
	var r, g, b float64
	if k <= 66 {
		r = 255
		g = 99.4708025861*math.Log(k) - 161.1195681661
		if k > 19 {
			b = 138.5177312231*math.Log(math.Max(2, k-10)) - 305.0447927307
		} else {
			b = 0
		}
	} else {
		r = 329.698727446 * math.Pow(k-60, -0.1332047592)
		g = 288.1221695283 * math.Pow(k-60, -0.0755148492)
		b = 255
	}
	return RGB{
		R: clamp(int(math.Round(r)), 0, 255),
		G: clamp(int(math.Round(g)), 0, 255),
		B: clamp(int(math.Round(b)), 0, 255),
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampBrightness clamps 0-100.
func ClampBrightness(v int) int {
	return clamp(v, 0, 100)
}

// KelvinMin is the warmest colour-temp the H60A1 accepts over LAN.
// Requests below this (including the "candle" 1800K preset) are ignored;
// the bulb stays at 2700K. Re-sending colorwc in that state is a visible pulse.
const KelvinMin = 2700

// ClampKelvin snaps a requested colour-temp up to the LAN floor.
// 0 is left unset (not a temperature look).
func ClampKelvin(k int) int {
	if k > 0 && k < KelvinMin {
		return KelvinMin
	}
	return k
}
