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

func TestClampKelvinLanFloor(t *testing.T) {
	if ClampKelvin(0) != 0 {
		t.Fatal("unset temp must stay 0")
	}
	if ClampKelvin(1800) != KelvinMin || ClampKelvin(2699) != KelvinMin {
		t.Fatal("below floor should snap to 2700")
	}
	if ClampKelvin(2700) != 2700 || ClampKelvin(4000) != 4000 {
		t.Fatal("at/above floor should pass through")
	}
}

func TestKelvinToRGBNeutralIsWarmDominant(t *testing.T) {
	c := KelvinToRGB(4000)
	if c.B > c.R {
		t.Fatalf("4000K RGB approx should not be blue-dominant (that looks cyan on RGB mode): %+v", c)
	}
}
