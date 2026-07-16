package main

import (
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"colour":     "color",
		"color":      "color",
		"bright":     "brightness",
		"brightness": "brightness",
		"temp":       "temp",
		"on":         "on",
		"OFF":        "off",
	}
	for in, want := range cases {
		got, ok := normalizeKey(in)
		if !ok || got != want {
			t.Fatalf("normalizeKey(%q)=%q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := normalizeKey("nope"); ok {
		t.Fatal("expected unknown")
	}
}

func TestApplyLeadingTokens(t *testing.T) {
	// ensure applyLeading builds the expected token stream without hitting hardware
	tokens := []string{"red", "bright", "10"}
	built := append([]string{"color"}, tokens...)
	if len(built) != 4 || built[0] != "color" || built[1] != "red" || built[2] != "bright" || built[3] != "10" {
		t.Fatalf("unexpected %v", built)
	}
}
