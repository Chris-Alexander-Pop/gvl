package schedule

import (
	"testing"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
)

func TestParseDays(t *testing.T) {
	d, err := ParseDays("weekdays")
	if err != nil || len(d) != 5 {
		t.Fatalf("weekdays: %v %v", d, err)
	}
	d, err = ParseDays("everyday")
	if err != nil || d != nil {
		t.Fatalf("everyday: %v %v", d, err)
	}
}

func TestDue(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// Monday 2026-07-13 07:00:30 in NY
	now := time.Date(2026, 7, 13, 7, 0, 30, 0, loc)
	e := Entry{
		Enabled:  true,
		At:       "07:00",
		Timezone: "America/New_York",
		Days:     []string{"mon"},
	}
	if !Due(e, now) {
		t.Fatal("expected due")
	}
	e.LastFired = "2026-07-13"
	if Due(e, now) {
		t.Fatal("should not fire twice same day")
	}
}

func TestParseLook(t *testing.T) {
	l, err := ParseLook("blue", "", 5)
	if err != nil || l.Color == nil || l.Brightness != 5 {
		t.Fatalf("%+v %v", l, err)
	}
	l, err = ParseLook("", "warm", 40)
	if err != nil || l.Temp != 2700 {
		t.Fatalf("%+v %v", l, err)
	}
	_ = mode.Look{}
}
