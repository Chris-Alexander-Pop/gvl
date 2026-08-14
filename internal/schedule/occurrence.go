package schedule

import (
	"fmt"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
)

const fireWindow = 90 * time.Second

// Patch is a one-shot override for a single upcoming occurrence.
// Recurring At / Days stay unchanged.
type Patch struct {
	Date        string     `json:"date"` // YYYY-MM-DD of the occurrence this replaces
	Skip        bool       `json:"skip,omitempty"`
	At          string     `json:"at,omitempty"`
	NextDay     bool       `json:"next_day,omitempty"`
	DurationMin int        `json:"duration_min,omitempty"`
	From        *mode.Look `json:"from,omitempty"`
	To          *mode.Look `json:"to,omitempty"`
	EndOff      *bool      `json:"end_off,omitempty"`
}

// ActKind is what Tick should do for an entry right now.
type ActKind int

const (
	ActNone ActKind = iota
	ActFire
	ActSkip
)

// Act is the Tick decision for one entry.
type Act struct {
	Kind    ActKind
	Patch   *Patch
	FireAt  time.Time
	OccDate string
}

func locOf(e Entry) *time.Location {
	if e.Timezone != "" {
		if l, err := time.LoadLocation(e.Timezone); err == nil {
			return l
		}
	}
	return time.Local
}

func matchesDay(e Entry, t time.Time) bool {
	if len(e.Days) == 0 {
		return true
	}
	want := weekdayNames[t.Weekday()]
	for _, d := range e.Days {
		if equalFold(d, want) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func parseClock(at string) (h, m int, err error) {
	var hh, mm int
	n, err := fmt.Sscanf(at, "%d:%d", &hh, &mm)
	if err != nil || n != 2 || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("invalid time %q", at)
	}
	return hh, mm, nil
}

func parseDate(date string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", date)
	}
	return t, nil
}

// ClockOn returns HH:MM on the calendar day of day, in loc.
func ClockOn(day time.Time, at string, loc *time.Location) (time.Time, error) {
	h, m, err := parseClock(at)
	if err != nil {
		return time.Time{}, err
	}
	local := day.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc), nil
}

func inWindow(now, target time.Time) bool {
	d := now.Sub(target)
	return d >= 0 && d < fireWindow
}

func patchForDate(e Entry, date string) *Patch {
	for i := range e.Next {
		if e.Next[i].Date == date {
			p := e.Next[i]
			return &p
		}
	}
	return nil
}

// FireTime is when this patch should actually run (or the original slot if Skip).
func (p Patch) FireTime(e Entry) (time.Time, error) {
	loc := locOf(e)
	day, err := parseDate(p.Date, loc)
	if err != nil {
		return time.Time{}, err
	}
	at := p.At
	if at == "" {
		at = e.At
	}
	t, err := ClockOn(day, at, loc)
	if err != nil {
		return time.Time{}, err
	}
	if p.NextDay {
		t = t.Add(24 * time.Hour)
	}
	return t, nil
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// OccurrenceDates returns the next n occurrence calendar days (YYYY-MM-DD in tz).
func OccurrenceDates(e Entry, now time.Time, n int) []string {
	if n < 1 {
		n = 1
	}
	loc := locOf(e)
	local := now.In(loc)
	day := startOfDay(local, loc)
	var out []string
	for i := 0; i < 400 && len(out) < n; i++ {
		if matchesDay(e, day) {
			date := day.Format("2006-01-02")
			if pendingOccurrence(e, now, date) {
				out = append(out, date)
			}
		}
		day = day.Add(24 * time.Hour)
	}
	return out
}

func pendingOccurrence(e Entry, now time.Time, date string) bool {
	loc := locOf(e)
	day, err := parseDate(date, loc)
	if err != nil {
		return false
	}
	slot, err := ClockOn(day, e.At, loc)
	if err != nil {
		return false
	}
	p := patchForDate(e, date)
	if p != nil {
		if p.Skip {
			return !now.After(slot.Add(fireWindow))
		}
		fire, err := p.FireTime(e)
		if err != nil {
			return false
		}
		return !now.After(fire.Add(fireWindow))
	}
	if e.LastFired == date {
		return false
	}
	return slot.After(now) || inWindow(now, slot)
}

// NextFire is the next time the ramp will actually run (skips are walked past).
func NextFire(e Entry, now time.Time) (when time.Time, note string, ok bool) {
	if !e.Enabled {
		return time.Time{}, "disabled", false
	}
	dates := OccurrenceDates(e, now, 14)
	skipped := 0
	for _, date := range dates {
		p := patchForDate(e, date)
		if p != nil && p.Skip {
			skipped++
			continue
		}
		if p != nil {
			t, err := p.FireTime(e)
			if err != nil {
				continue
			}
			note = "moved to " + t.Format("Mon 15:04")
			if p.DurationMin > 0 || p.From != nil || p.To != nil {
				note += " (look override)"
			}
			if skipped > 0 {
				note = fmt.Sprintf("skipped %d; %s", skipped, note)
			}
			return t, note, true
		}
		loc := locOf(e)
		day, err := parseDate(date, loc)
		if err != nil {
			continue
		}
		t, err := ClockOn(day, e.At, loc)
		if err != nil {
			continue
		}
		if skipped > 0 {
			note = fmt.Sprintf("skipped %d", skipped)
		}
		return t, note, true
	}
	return time.Time{}, "", false
}

// Decorate fills computed upcoming fields for API/CLI display.
func Decorate(e Entry, now time.Time) Entry {
	if when, note, ok := NextFire(e, now); ok {
		e.Upcoming = when.Format(time.RFC3339)
		e.UpcomingNote = note
	}
	return e
}

// Classify decides what Tick should do at now.
func Classify(e Entry, now time.Time) Act {
	if !e.Enabled {
		return Act{}
	}
	loc := locOf(e)
	local := now.In(loc)

	for i := range e.Next {
		p := e.Next[i]
		if p.Skip {
			orig, err := ClockOn(mustDay(p.Date, loc), e.At, loc)
			if err != nil {
				continue
			}
			if inWindow(now, orig) {
				return Act{Kind: ActSkip, Patch: &p, OccDate: p.Date, FireAt: orig}
			}
			continue
		}
		fireAt, err := p.FireTime(e)
		if err != nil {
			continue
		}
		if inWindow(now, fireAt) {
			return Act{Kind: ActFire, Patch: &p, OccDate: p.Date, FireAt: fireAt}
		}
	}

	dayKey := local.Format("2006-01-02")
	if e.LastFired == dayKey {
		return Act{}
	}
	if patchForDate(e, dayKey) != nil {
		return Act{}
	}
	if !matchesDay(e, local) {
		return Act{}
	}
	target, err := ClockOn(local, e.At, loc)
	if err != nil {
		return Act{}
	}
	if inWindow(now, target) {
		return Act{Kind: ActFire, OccDate: dayKey, FireAt: target}
	}
	return Act{}
}

func mustDay(date string, loc *time.Location) time.Time {
	t, err := parseDate(date, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

// StalePatchDates are overrides whose window already passed (missed Tick).
func StalePatchDates(e Entry, now time.Time) []string {
	var dates []string
	loc := locOf(e)
	for _, p := range e.Next {
		if p.Skip {
			orig, err := ClockOn(mustDay(p.Date, loc), e.At, loc)
			if err != nil || now.After(orig.Add(fireWindow)) {
				dates = append(dates, p.Date)
			}
			continue
		}
		fireAt, err := p.FireTime(e)
		if err != nil || now.After(fireAt.Add(fireWindow)) {
			dates = append(dates, p.Date)
		}
	}
	return dates
}

// ApplyPatch copies one-shot look/duration onto a fire.
func ApplyPatch(e Entry, p *Patch) Entry {
	if p == nil {
		return e
	}
	if p.At != "" {
		e.At = p.At
	}
	if p.DurationMin > 0 {
		e.DurationMin = p.DurationMin
	}
	if p.From != nil {
		e.From = *p.From
	}
	if p.To != nil {
		e.To = *p.To
	}
	if p.EndOff != nil {
		e.EndOff = *p.EndOff
	}
	return e
}

// BuildPatches creates count occurrence patches from a template (Date optional).
func BuildPatches(e Entry, now time.Time, spec Patch, count int) ([]Patch, error) {
	if spec.Skip && spec.At != "" {
		return nil, fmt.Errorf("skip and --at cannot both be set")
	}
	if !spec.Skip && spec.At != "" {
		if _, _, err := parseClock(spec.At); err != nil {
			return nil, err
		}
	}
	if count < 1 {
		count = 1
	}
	var dates []string
	if spec.Date != "" {
		loc := locOf(e)
		if _, err := parseDate(spec.Date, loc); err != nil {
			return nil, err
		}
		dates = []string{spec.Date}
		if count > 1 {
			day, _ := parseDate(spec.Date, loc)
			rest := OccurrenceDates(e, day.Add(24*time.Hour), count-1)
			dates = append(dates, rest...)
		}
	} else {
		dates = OccurrenceDates(e, now, count)
	}
	if len(dates) == 0 {
		return nil, fmt.Errorf("no upcoming occurrence to patch")
	}
	out := make([]Patch, 0, len(dates))
	for _, d := range dates {
		p := spec
		p.Date = d
		out = append(out, p)
	}
	return out, nil
}
