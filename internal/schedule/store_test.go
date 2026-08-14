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
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatal(err)
	}
	// Monday 2026-07-13 07:00:30 UTC
	now := time.Date(2026, 7, 13, 7, 0, 30, 0, loc)
	e := Entry{
		Enabled:  true,
		At:       "07:00",
		Timezone: "UTC",
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

func TestNextFireAndSkip(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// Sunday 2026-08-16 22:00 — next weekday wake is Monday 07:00
	now := time.Date(2026, 8, 16, 22, 0, 0, 0, loc)
	e := Entry{
		Enabled:  true,
		Kind:     KindWake,
		At:       "07:00",
		Timezone: "America/New_York",
		Days:     []string{"mon", "tue", "wed", "thu", "fri"},
	}
	when, note, ok := NextFire(e, now)
	if !ok || note != "" {
		t.Fatalf("natural next: %v %q %v", when, note, ok)
	}
	if when.Format("2006-01-02 15:04") != "2026-08-17 07:00" {
		t.Fatalf("want Mon 07:00 got %s", when)
	}

	patches, err := BuildPatches(e, now, Patch{Skip: true}, 1)
	if err != nil || len(patches) != 1 || patches[0].Date != "2026-08-17" {
		t.Fatalf("skip patches: %v %v", patches, err)
	}
	e.Next = patches
	when, note, ok = NextFire(e, now)
	if !ok || when.Format("2006-01-02") != "2026-08-18" {
		t.Fatalf("after skip next should be Tue, got %v ok=%v", when, ok)
	}
	if note != "skipped 1" {
		t.Fatalf("skip note: %q", note)
	}

	e.Next = nil
	moved, err := BuildPatches(e, now, Patch{At: "09:30"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	e.Next = moved
	when, note, ok = NextFire(e, now)
	if !ok || when.Format("2006-01-02 15:04") != "2026-08-17 09:30" {
		t.Fatalf("moved: %v", when)
	}
	if note == "" {
		t.Fatal("expected moved note")
	}

	monMorning := time.Date(2026, 8, 17, 7, 0, 20, 0, loc)
	if Classify(e, monMorning).Kind != ActNone {
		t.Fatal("patched 09:30 must not fire at 07:00")
	}
	monLate := time.Date(2026, 8, 17, 9, 30, 20, 0, loc)
	act := Classify(e, monLate)
	if act.Kind != ActFire || act.Patch == nil || act.Patch.At != "09:30" {
		t.Fatalf("expected fire at 09:30, got %+v", act)
	}
}

func TestSkipWindow(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 17, 7, 0, 20, 0, loc)
	e := Entry{
		Enabled:  true,
		At:       "07:00",
		Timezone: "UTC",
		Days:     []string{"mon"},
		Next:     []Patch{{Date: "2026-08-17", Skip: true}},
	}
	act := Classify(e, now)
	if act.Kind != ActSkip {
		t.Fatalf("want skip, got %+v", act)
	}
}

func TestSleepNextDay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 14, 20, 0, 0, 0, loc) // Friday evening
	e := Entry{
		Enabled:  true,
		Kind:     KindSleep,
		At:       "23:00",
		Timezone: "America/New_York",
		Days:     []string{"mon", "tue", "wed", "thu", "fri"},
	}
	patches, err := BuildPatches(e, now, Patch{At: "01:00", NextDay: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	e.Next = patches
	when, _, ok := NextFire(e, now)
	if !ok {
		t.Fatal("expected next fire")
	}
	if when.Format("2006-01-02 15:04") != "2026-08-15 01:00" {
		t.Fatalf("want Sat 01:00 got %s", when)
	}
	sat := time.Date(2026, 8, 15, 1, 0, 10, 0, loc)
	if Classify(e, sat).Kind != ActFire {
		t.Fatalf("should fire at 01:00, %+v", Classify(e, sat))
	}
}

func TestBuildCount(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, loc) // Sunday
	e := Entry{
		Enabled:  true,
		At:       "07:00",
		Timezone: "UTC",
		Days:     []string{"mon", "tue", "wed", "thu", "fri"},
	}
	patches, err := BuildPatches(e, now, Patch{At: "09:00"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 3 {
		t.Fatalf("count: %d", len(patches))
	}
	if patches[0].Date != "2026-08-17" || patches[2].Date != "2026-08-19" {
		t.Fatalf("dates %v", patches)
	}
}
