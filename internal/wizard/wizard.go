package wizard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/Chris-Alexander-Pop/gvl/internal/schedule"
)

// Run walks the user through creating a wake/sleep schedule on stdin/stdout.
func Run(defaultTZ string) (schedule.Entry, error) {
	return RunInteractive(os.Stdin, os.Stdout, defaultTZ)
}

// RunInteractive is the testable wizard.
func RunInteractive(in io.Reader, out io.Writer, defaultTZ string) (schedule.Entry, error) {
	r := bufio.NewReader(in)
	ask := func(prompt, def string) (string, error) {
		if def != "" {
			fmt.Fprintf(out, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return def, nil
		}
		return line, nil
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "GVL schedule wizard")
	fmt.Fprintln(out, "───────────────────")
	fmt.Fprintln(out, "Sets up a wake or sleep ramp on the daemon.")
	fmt.Fprintln(out, "Wake example: dark blue @ 5% → daylight @ 55% over 30 minutes.")
	fmt.Fprintln(out, "Sleep example: neutral @ 40% → candle @ 5% (then optional off).")
	fmt.Fprintln(out, "")

	kindStr, err := ask("Kind? (wake / sleep)", "wake")
	if err != nil {
		return schedule.Entry{}, err
	}
	kind := schedule.Kind(strings.ToLower(kindStr))
	if kind != schedule.KindWake && kind != schedule.KindSleep {
		return schedule.Entry{}, fmt.Errorf("kind must be wake or sleep")
	}

	idDef := string(kind) + "-" + time.Now().Format("1504")
	id, err := ask("Schedule id (slug)", idDef)
	if err != nil {
		return schedule.Entry{}, err
	}

	at, err := ask("Time (HH:MM, 24h)", "07:00")
	if err != nil {
		return schedule.Entry{}, err
	}
	if _, err := time.Parse("15:04", at); err != nil {
		return schedule.Entry{}, fmt.Errorf("invalid time %q (want HH:MM)", at)
	}

	daysStr, err := ask("Days (weekdays / weekend / everyday / mon,tue,...)", "weekdays")
	if err != nil {
		return schedule.Entry{}, err
	}
	days, err := schedule.ParseDays(daysStr)
	if err != nil {
		return schedule.Entry{}, err
	}

	tz := defaultTZ
	if tz == "" {
		tz = "UTC"
	}
	tz, err = ask("Timezone (IANA)", tz)
	if err != nil {
		return schedule.Entry{}, err
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return schedule.Entry{}, fmt.Errorf("invalid timezone: %w", err)
	}

	durStr, err := ask("Ramp duration in minutes", "30")
	if err != nil {
		return schedule.Entry{}, err
	}
	dur, err := strconv.Atoi(durStr)
	if err != nil || dur < 1 {
		return schedule.Entry{}, fmt.Errorf("duration must be a positive integer")
	}

	fromLook, toLook := schedule.DefaultWake()
	if kind == schedule.KindSleep {
		fromLook, toLook = schedule.DefaultSleep()
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Start look — color name/#hex OR temperature preset/kelvin.")
	fmt.Fprintf(out, "Colors: %s\n", strings.Join(sortedKeys(govee.NamedColors), ", "))
	fmt.Fprintf(out, "Temps:  %s\n", strings.Join(sortedKeys(govee.TempPresets), ", "))

	fromColorDef, fromTempDef := lookDefaults(fromLook)
	fromColor, err := ask("Start color (blank to use temp)", fromColorDef)
	if err != nil {
		return schedule.Entry{}, err
	}
	fromTemp, err := ask("Start temp (blank if using color)", fromTempDef)
	if err != nil {
		return schedule.Entry{}, err
	}
	fromBrightStr, err := ask("Start brightness 0-100", strconv.Itoa(fromLook.Brightness))
	if err != nil {
		return schedule.Entry{}, err
	}
	fromBright, _ := strconv.Atoi(fromBrightStr)
	fromParsed, err := schedule.ParseLook(fromColor, fromTemp, fromBright)
	if err != nil {
		return schedule.Entry{}, fmt.Errorf("start look: %w", err)
	}

	fmt.Fprintln(out, "")
	toColorDef, toTempDef := lookDefaults(toLook)
	toColor, err := ask("End color (blank to use temp)", toColorDef)
	if err != nil {
		return schedule.Entry{}, err
	}
	toTemp, err := ask("End temp (blank if using color)", toTempDef)
	if err != nil {
		return schedule.Entry{}, err
	}
	toBrightStr, err := ask("End brightness 0-100", strconv.Itoa(toLook.Brightness))
	if err != nil {
		return schedule.Entry{}, err
	}
	toBright, _ := strconv.Atoi(toBrightStr)
	toParsed, err := schedule.ParseLook(toColor, toTemp, toBright)
	if err != nil {
		return schedule.Entry{}, fmt.Errorf("end look: %w", err)
	}

	endOff := false
	if kind == schedule.KindSleep {
		offStr, err := ask("Turn light off when ramp finishes? (y/n)", "y")
		if err != nil {
			return schedule.Entry{}, err
		}
		endOff = strings.HasPrefix(strings.ToLower(offStr), "y")
	}

	entry := schedule.Entry{
		ID:          id,
		Enabled:     true,
		Kind:        kind,
		Days:        days,
		At:          at,
		Timezone:    tz,
		DurationMin: dur,
		From:        fromParsed,
		To:          toParsed,
		EndOff:      endOff,
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Preview")
	fmt.Fprintln(out, "───────")
	fmt.Fprintf(out, "  id:       %s\n", entry.ID)
	fmt.Fprintf(out, "  kind:     %s\n", entry.Kind)
	fmt.Fprintf(out, "  when:     %s %s (%s)\n", entry.At, daysLabel(entry.Days), entry.Timezone)
	fmt.Fprintf(out, "  duration: %d min\n", entry.DurationMin)
	fmt.Fprintf(out, "  from:     %s\n", formatLook(entry.From))
	fmt.Fprintf(out, "  to:       %s\n", formatLook(entry.To))
	if entry.EndOff {
		fmt.Fprintln(out, "  end:      turn off")
	}
	fmt.Fprintln(out, "")

	confirm, err := ask("Save this schedule? (y/n)", "y")
	if err != nil {
		return schedule.Entry{}, err
	}
	if !strings.HasPrefix(strings.ToLower(confirm), "y") {
		return schedule.Entry{}, fmt.Errorf("cancelled")
	}
	return entry, nil
}

func lookDefaults(l mode.Look) (color, temp string) {
	if l.Temp > 0 {
		for name, k := range govee.TempPresets {
			if k == l.Temp {
				return "", name
			}
		}
		return "", strconv.Itoa(l.Temp)
	}
	if l.Color != nil {
		for name, rgb := range govee.NamedColors {
			if rgb == *l.Color {
				return name, ""
			}
		}
		return fmt.Sprintf("#%02x%02x%02x", l.Color.R, l.Color.G, l.Color.B), ""
	}
	return "", ""
}

func formatLook(l mode.Look) string {
	part := ""
	if l.Temp > 0 {
		part = govee.FormatColor(govee.RGB{}, l.Temp)
	} else if l.Color != nil {
		part = govee.FormatColor(*l.Color, 0)
	} else {
		part = "?"
	}
	return fmt.Sprintf("%s @ %d%%", part, l.Brightness)
}

func daysLabel(days []string) string {
	if len(days) == 0 {
		return "everyday"
	}
	return strings.Join(days, ",")
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
