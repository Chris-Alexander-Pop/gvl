package govee

import "testing"

func TestColorMatches(t *testing.T) {
	want := RGB{255, 0, 0}
	if !colorMatches(&Status{Color: want, ColorTemInKelvin: 0}, want) {
		t.Fatal("exact RGB with kelvin 0 should match")
	}
	if colorMatches(&Status{Color: want, ColorTemInKelvin: 2700}, want) {
		t.Fatal("RGB with leftover colour-temp should not match")
	}
	if colorMatches(&Status{Color: RGB{0, 0, 255}, ColorTemInKelvin: 0}, want) {
		t.Fatal("different RGB should not match")
	}
}
