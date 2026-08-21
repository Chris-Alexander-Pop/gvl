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
	if end.Temp != 1800 || end.Color != nil || end.Brightness != 1 {
		t.Fatalf("t=1: %+v", end)
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

func TestPreviewLooksSleepIsDistinctAndEndsAtCandle(t *testing.T) {
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
	if last.Temp != 1800 || last.Brightness != 1 {
		t.Fatalf("end %+v", last)
	}
	for i := 1; i < len(frames); i++ {
		if looksEqual(frames[i-1], frames[i]) {
			t.Fatalf("duplicate frame %d", i)
		}
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
