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

func TestTempMatches(t *testing.T) {
	if tempMatches(&Status{ColorTemInKelvin: 0}, 4000) {
		t.Fatal("RGB mode must not match a kelvin target")
	}
	if !tempMatches(&Status{ColorTemInKelvin: 4000}, 4000) {
		t.Fatal("exact kelvin should match")
	}
	if !tempMatches(&Status{ColorTemInKelvin: 3900}, 4000) {
		t.Fatal("100K snap should still match")
	}
	if tempMatches(&Status{ColorTemInKelvin: 2700}, 4000) {
		t.Fatal("far kelvin should not match")
	}
}

func TestBelowWhiteFloor(t *testing.T) {
	if BelowWhiteFloor(0) || BelowWhiteFloor(2700) || BelowWhiteFloor(4000) {
		t.Fatal("0 and ≥2700K are white-LED temps")
	}
	if !BelowWhiteFloor(1800) || !BelowWhiteFloor(2200) || !BelowWhiteFloor(2699) {
		t.Fatal("candle/2200 must be RGB on this lamp")
	}
}

func TestKelvinToRGBCandleIsOrange(t *testing.T) {
	c := KelvinToRGB(1800)
	if c.R != 255 || c.G < 100 || c.G > 140 || c.B != 0 {
		t.Fatalf("candle RGB should be orange, got %+v", c)
	}
}

func TestKelvinToRGBNeutralIsWarmDominant(t *testing.T) {
	c := KelvinToRGB(4000)
	if c.B > c.R {
		t.Fatalf("4000K RGB approx should not be blue-dominant (that looks cyan on RGB mode): %+v", c)
	}
}
