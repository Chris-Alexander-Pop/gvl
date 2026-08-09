package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

// settingKeys are recognised tokens in a chained apply sequence.
var settingKeys = map[string]string{
	"color":      "color",
	"colour":     "color",
	"bright":     "brightness",
	"brightness": "brightness",
	"temp":       "temp",
	"on":         "on",
	"off":        "off",
}

func normalizeKey(s string) (string, bool) {
	k, ok := settingKeys[strings.ToLower(s)]
	return k, ok
}

// applySettings applies a sequence of setting/value pairs (and bare on/off).
// Examples:
//
//	color red bright 40
//	temp warm bright 20
//	on color blue bright 50
func applySettings(tokens []string) error {
	if len(tokens) == 0 {
		return fmt.Errorf("nothing to apply — try: gvl colour red bright 40")
	}

	type step struct {
		key     string
		payload map[string]any
	}
	var steps []step
	i := 0
	for i < len(tokens) {
		key, ok := normalizeKey(tokens[i])
		if !ok {
			return fmt.Errorf("unknown setting %q (want color/colour, bright, temp, on, off)", tokens[i])
		}
		i++
		switch key {
		case "on", "off":
			steps = append(steps, step{key: key})
		case "color":
			if i >= len(tokens) {
				return fmt.Errorf("color needs a value (name, #hex, or r,g,b)")
			}
			rgb, err := govee.ParseColor(tokens[i])
			if err != nil {
				return err
			}
			i++
			steps = append(steps, step{key: "color", payload: map[string]any{"color": rgb}})
		case "brightness":
			if i >= len(tokens) {
				return fmt.Errorf("bright needs a value 0-100")
			}
			v, err := strconv.Atoi(tokens[i])
			if err != nil || v < 0 || v > 100 {
				return fmt.Errorf("brightness must be 0-100")
			}
			i++
			steps = append(steps, step{key: "brightness", payload: map[string]any{"value": v}})
		case "temp":
			if i >= len(tokens) {
				return fmt.Errorf("temp needs a preset or kelvin value")
			}
			k, err := govee.ParseTemp(tokens[i])
			if err != nil {
				return err
			}
			i++
			steps = append(steps, step{key: "temp", payload: map[string]any{"value": k}})
		}
	}
	if len(steps) == 0 {
		return fmt.Errorf("nothing applied")
	}

	var last *govee.Status
	var lastIP string
	// Confirm every step against device status so dropped UDP packets get retried
	// (colour/bright/temp used to return the first status without checking it matched).
	for _, s := range steps {
		st, ip, err := deviceCmd(s.key, s.payload, true)
		if err != nil {
			return err
		}
		last, lastIP = st, ip
	}
	emitStatus(last, lastIP)
	return nil
}

// applyLeading applies an implicit first setting, then any trailing chain.
// e.g. colourCmd with args ["red","bright","10"] → color red + bright 10
func applyLeading(kind string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s needs a value", kind)
	}
	tokens := make([]string, 0, len(args)+1)
	tokens = append(tokens, kind, args[0])
	tokens = append(tokens, args[1:]...)
	return applySettings(tokens)
}
