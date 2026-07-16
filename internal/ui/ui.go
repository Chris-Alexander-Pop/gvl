package ui

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"golang.org/x/term"
)

// Enabled is true when ANSI styling should be used.
func Enabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if v := os.Getenv("FORCE_COLOR"); v != "" && v != "0" {
		return true
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func reset() string {
	if !Enabled() {
		return ""
	}
	return "\x1b[0m"
}

func bold(s string) string {
	if !Enabled() {
		return s
	}
	return "\x1b[1m" + s + reset()
}

func dim(s string) string {
	if !Enabled() {
		return s
	}
	return "\x1b[2m" + s + reset()
}

func fgRGB(r, g, b int, s string) string {
	if !Enabled() {
		return s
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s%s", r, g, b, s, reset())
}

func bgRGB(r, g, b int) string {
	if !Enabled() {
		return "  "
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm  %s", r, g, b, reset())
}

func accent(s string) string {
	return fgRGB(120, 190, 255, s)
}

func cmd(s string) string {
	if !Enabled() {
		return s
	}
	return fmt.Sprintf("\x1b[1;38;2;120;190;255m%s%s", s, reset())
}

func title(s string) string {
	if !Enabled() {
		return s
	}
	return fmt.Sprintf("\x1b[1;38;2;255;200;80m%s%s", s, reset())
}

// Swatch returns a two-cell truecolor block for an RGB colour.
func Swatch(c govee.RGB) string {
	return bgRGB(c.R, c.G, c.B)
}

// TempSwatch approximates a Kelvin look as an RGB swatch.
func TempSwatch(kelvin int) string {
	return Swatch(kelvinRGB(kelvin))
}

func kelvinRGB(k int) govee.RGB {
	t := float64(k) / 100
	var r, g, b float64
	if t <= 66 {
		r = 255
		g = 99.4708025861*math.Log(t) - 161.1195681661
		if t <= 19 {
			b = 0
		} else {
			b = 138.5177312231*math.Log(t-10) - 305.0447927307
		}
	} else {
		r = 329.698727446 * math.Pow(t-60, -0.1332047592)
		g = 288.1221695283 * math.Pow(t-60, -0.0755148492)
		b = 255
	}
	return govee.RGB{clampByte(r), clampByte(g), clampByte(b)}
}

func clampByte(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v + 0.5)
}

func paletteStrip() string {
	names := []string{"red", "orange", "amber", "yellow", "lime", "green", "teal", "cyan", "blue", "indigo", "purple", "pink", "magenta"}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, Swatch(govee.NamedColors[n]))
	}
	return strings.Join(parts, "")
}

func line(label, example, note string) string {
	left := fmt.Sprintf("  %s", cmd(example))
	if note == "" {
		return left
	}
	pad := 36 - visibleLen(example)
	if pad < 2 {
		pad = 2
	}
	return left + strings.Repeat(" ", pad) + dim(note)
}

func visibleLen(s string) int {
	return len(s) // examples are ASCII-only
}

func section(title string) string {
	return "\n" + bold(title) + "\n"
}

// FormatHome is the coloured root help / landing screen.
func FormatHome() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s  %s\n", title("gvl"), dim("govee lan lights + sleep/wake"))
	fmt.Fprintf(&b, "%s\n", paletteStrip())

	b.WriteString(section("light"))
	b.WriteString(line("", "gvl on / off / status / stop", "") + "\n")
	b.WriteString(line("", "gvl colour red bright 10", "chain colour + brightness") + "\n")
	b.WriteString(line("", "gvl temp warm bright 25", "kelvin presets or raw K") + "\n")
	b.WriteString(line("", "gvl set on colour blue bright 50", "any order of settings") + "\n")
	b.WriteString(line("", "gvl mode aurora", "animated looks") + "\n")

	b.WriteString(section("aliases"))
	b.WriteString(line("", "gvl presets", "swatches for colour / temp / mode names") + "\n")
	b.WriteString("  ")
	mini := []string{"red", "blue", "purple", "warm-white", "teal"}
	for _, n := range mini {
		c := govee.NamedColors[n]
		fmt.Fprintf(&b, "%s%s ", Swatch(c), fgRGB(c.R, c.G, c.B, n))
	}
	b.WriteString(dim("…") + "\n")

	b.WriteString(section("schedule") + dim("  (needs gvld — gvl config set-url …)") + "\n")
	b.WriteString(line("", "gvl schedule wizard", "interactive wake/sleep setup") + "\n")
	b.WriteString(line("", "gvl schedule set-wake 07:00", "--duration 30 …") + "\n")
	b.WriteString(line("", "gvl schedule set-sleep 23:00", "--end-off") + "\n")
	b.WriteString(line("", "gvl schedule list", "") + "\n")
	b.WriteString(line("", "gvl schedule run-now ID", "fire immediately") + "\n")

	b.WriteString(section("flags"))
	fmt.Fprintf(&b, "  %s device IP   %s quiet   %s json   %s daemon url\n",
		cmd("-a"), cmd("-q"), cmd("--json"), cmd("--url"))

	b.WriteString("\n" + dim("more:  gvl help <command>") + "\n")
	return strings.TrimRight(b.String(), "\n")
}

// FormatStatus renders a coloured status block.
func FormatStatus(st *govee.Status, ip string) string {
	state := "off"
	if st.OnOff != 0 {
		state = fgRGB(80, 200, 120, "on")
	} else {
		state = dim("off")
	}

	var b strings.Builder
	if ip != "" {
		fmt.Fprintf(&b, "%s  %s\n", dim("device"), ip)
	}
	fmt.Fprintf(&b, "%s  %s\n", dim("power"), state)
	fmt.Fprintf(&b, "%s %s%%\n", dim("bright"), bold(fmt.Sprintf("%d", st.Brightness)))

	label := govee.FormatColor(st.Color, st.ColorTemInKelvin)
	var sw string
	if st.ColorTemInKelvin > 0 {
		rgb := kelvinRGB(st.ColorTemInKelvin)
		sw = TempSwatch(st.ColorTemInKelvin)
		label = fgRGB(rgb.R, rgb.G, rgb.B, label)
	} else {
		sw = Swatch(st.Color)
		label = fgRGB(st.Color.R, st.Color.G, st.Color.B, label)
	}
	fmt.Fprintf(&b, "%s  %s %s", dim("color"), sw, label)
	return b.String()
}

// FormatPresets renders the named colour / temp / mode catalogue.
func FormatPresets() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s  %s\n", bold("colours"), dim("gvl colour <name>"))
	fmt.Fprintf(&b, "%s\n", paletteStrip())
	for _, name := range sortedKeys(govee.NamedColors) {
		c := govee.NamedColors[name]
		hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
		nameCol := fgRGB(c.R, c.G, c.B, fmt.Sprintf("%-12s", name))
		fmt.Fprintf(&b, "  %s %s %s\n", Swatch(c), nameCol, dim(hex))
	}

	fmt.Fprintf(&b, "\n%s  %s\n", bold("temperatures"), dim("gvl temp <name|kelvin>"))
	for _, name := range sortedKeys(govee.TempPresets) {
		k := govee.TempPresets[name]
		rgb := kelvinRGB(k)
		nameCol := fgRGB(rgb.R, rgb.G, rgb.B, fmt.Sprintf("%-12s", name))
		fmt.Fprintf(&b, "  %s %s %s\n", TempSwatch(k), nameCol, dim(fmt.Sprintf("%dK", k)))
	}

	fmt.Fprintf(&b, "\n%s  %s\n", bold("modes"), dim("gvl mode <name>"))
	for _, name := range mode.Names() {
		fmt.Fprintf(&b, "  %s  %s\n", bold(fmt.Sprintf("%-12s", name)), dim(mode.Help[name]))
	}

	fmt.Fprintf(&b, "\n%s\n", dim("chain:  gvl colour red bright 40   ·   gvl temp warm bright 20   ·   gvl set on colour blue bright 50"))
	return strings.TrimRight(b.String(), "\n")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
