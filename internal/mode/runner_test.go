package mode

import (
	"testing"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
)

func TestKelvinRampSleepLooks(t *testing.T) {
	from := Look{Temp: 4000, Brightness: 100}
	to := Look{Temp: 1800, Brightness: 1}
	if !kelvinRamp(from, to) {
		t.Fatal("sleep 4000K→1800K must stay in kelvin mode")
	}
	start := lerpLook(from, to, 0)
	if start.Temp != 4000 || start.Color != nil || start.Brightness != 100 {
		t.Fatalf("t=0: %+v", start)
	}
	end := lerpLook(from, to, 1)
	if end.Temp != govee.KelvinMin || end.Color != nil || end.Brightness != 1 {
		t.Fatalf("t=1: %+v (LAN floor is %dK, not candle 1800K)", end, govee.KelvinMin)
	}
	mid := lerpLook(from, to, 0.5)
	if mid.Color != nil {
		t.Fatal("mid frame must not switch to RGB")
	}
	if mid.Temp < 2800 || mid.Temp > 3000 {
		t.Fatalf("mid kelvin got %d", mid.Temp)
	}
	if mid.Brightness >= 50 {
		t.Fatalf("perceptual mid brightness should be well under linear 50%%, got %d", mid.Brightness)
	}
}

func TestPreviewLooksSleepIsDistinctAndEndsAtLanFloor(t *testing.T) {
	from := Look{Temp: 4000, Brightness: 100}
	to := Look{Temp: 1800, Brightness: 1}
	frames := PreviewLooks(from, to)
	if len(frames) < 20 {
		t.Fatalf("too few frames: %d", len(frames))
	}
	if frames[0].Temp != 4000 || frames[0].Brightness != 100 {
		t.Fatalf("start %+v", frames[0])
	}
	last := frames[len(frames)-1]
	if last.Temp != govee.KelvinMin || last.Brightness != 1 {
		t.Fatalf("end %+v want %dK @ 1%%", last, govee.KelvinMin)
	}
	for i := 1; i < len(frames); i++ {
		if looksEqual(frames[i-1], frames[i]) {
			t.Fatalf("duplicate frame %d", i)
		}
		if frames[i].Temp > 0 && frames[i].Temp < govee.KelvinMin {
			t.Fatalf("frame %d below LAN floor: %+v", i, frames[i])
		}
	}
}

func TestPreviewLooksConstantBrightnessIsKelvinOnly(t *testing.T) {
	from := Look{Temp: 4000, Brightness: 1}
	to := Look{Temp: 1800, Brightness: 1}
	frames := PreviewLooks(from, to)
	if len(frames) < 10 || len(frames) > 20 {
		t.Fatalf("expected ~14 kelvin steps at 1%%, got %d", len(frames))
	}
	for _, f := range frames {
		if f.Brightness != 1 || f.Temp < govee.KelvinMin {
			t.Fatalf("%+v", f)
		}
	}
	if frames[len(frames)-1].Temp != govee.KelvinMin {
		t.Fatalf("end %+v", frames[len(frames)-1])
	}
}

func TestApplyLookFromSkipsRedundantKelvin(t *testing.T) {
	a := Look{Temp: 4000, Brightness: 100}
	b := Look{Temp: 4000, Brightness: 99}
	if !sameAppearance(a, b) {
		t.Fatal("same kelvin should be same appearance")
	}
	c := Look{Temp: 3900, Brightness: 99}
	if sameAppearance(b, c) {
		t.Fatal("kelvin change must be a new appearance")
	}
}

func TestLerpBrightnessPerceptual(t *testing.T) {
	if lerpBrightness(100, 1, 0) != 100 || lerpBrightness(100, 1, 1) != 1 {
		t.Fatal("endpoints")
	}
	mid := lerpBrightness(100, 1, 0.5)
	linear := 50
	if mid >= linear {
		t.Fatalf("t=0.5: perceptual %d should be < linear %d", mid, linear)
	}
	if mid < 15 || mid > 40 {
		t.Fatalf("t=0.5: unexpected %d%% (want roughly 20–35)", mid)
	}
}

func TestLerpLookWakeStaysRGBUntilEnd(t *testing.T) {
	cyan := govee.NamedColors["cyan"]
	from := Look{Color: &cyan, Brightness: 1}
	to := Look{Temp: 5000, Brightness: 100}
	if kelvinRamp(from, to) {
		t.Fatal("cyan→daylight is a mixed ramp")
	}
	start := lerpLook(from, to, 0)
	if start.Color == nil || *start.Color != cyan || start.Temp != 0 {
		t.Fatalf("t=0 should be cyan RGB, got %+v", start)
	}
}
