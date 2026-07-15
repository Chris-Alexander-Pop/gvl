package govee

import (
	"fmt"
	"strconv"
	"strings"
)

// NamedColors maps friendly names to RGB.
var NamedColors = map[string]RGB{
	"red":         {255, 0, 0},
	"orange":      {255, 100, 0},
	"amber":       {255, 191, 0},
	"yellow":      {255, 255, 0},
	"lime":        {128, 255, 0},
	"green":       {0, 255, 0},
	"teal":        {0, 200, 170},
	"cyan":        {0, 255, 255},
	"blue":        {0, 80, 255},
	"indigo":      {75, 0, 130},
	"purple":      {160, 32, 240},
	"pink":        {255, 105, 180},
	"magenta":     {255, 0, 255},
	"white":       {255, 255, 255},
	"warm-white":  {255, 214, 170},
	"cool-white":  {214, 233, 255},
}

// TempPresets maps names to Kelvin.
var TempPresets = map[string]int{
	"candle":   1800,
	"warm":     2700,
	"soft":     3000,
	"neutral":  4000,
	"daylight": 5000,
	"cool":     6500,
	"overcast": 7000,
}

// ColorNames returns sorted named color keys.
func ColorNames() []string {
	names := make([]string, 0, len(NamedColors))
	for k := range NamedColors {
		names = append(names, k)
	}
	return names
}

// TempNames returns sorted temperature preset keys.
func TempNames() []string {
	names := make([]string, 0, len(TempPresets))
	for k := range TempPresets {
		names = append(names, k)
	}
	return names
}

// ParseColor accepts a name, #RRGGBB, or r,g,b.
func ParseColor(s string) (RGB, error) {
	key := strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if c, ok := NamedColors[key]; ok {
		return c, nil
	}
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts) != 3 {
			return RGB{}, fmt.Errorf("RGB must be r,g,b")
		}
		vals := [3]int{}
		for i, p := range parts {
			v, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || v < 0 || v > 255 {
				return RGB{}, fmt.Errorf("RGB values must be 0-255")
			}
			vals[i] = v
		}
		return RGB{vals[0], vals[1], vals[2]}, nil
	}
	hex := strings.TrimPrefix(s, "#")
	if len(hex) == 6 {
		r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
		g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
		b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return RGB{}, fmt.Errorf("invalid hex color %q", s)
		}
		return RGB{int(r), int(g), int(b)}, nil
	}
	return RGB{}, fmt.Errorf("unknown color %q; try a name, #RRGGBB, or r,g,b", s)
}

// ParseTemp accepts a Kelvin value or preset name.
func ParseTemp(s string) (int, error) {
	key := strings.ToLower(s)
	if k, ok := TempPresets[key]; ok {
		return k, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("temperature must be kelvin or preset")
	}
	if v < 1800 || v > 9000 {
		return 0, fmt.Errorf("temperature must be between 1800 and 9000K")
	}
	return v, nil
}

// FormatColor describes device color state.
func FormatColor(color RGB, kelvin int) string {
	if kelvin > 0 {
		for name, k := range TempPresets {
			if k == kelvin {
				return fmt.Sprintf("%s (%dK)", name, kelvin)
			}
		}
		return fmt.Sprintf("%dK", kelvin)
	}
	for name, rgb := range NamedColors {
		if rgb == color {
			return name
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B)
}
