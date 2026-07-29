package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
)

// Kind of schedule entry.
type Kind string

const (
	KindWake  Kind = "wake"
	KindSleep Kind = "sleep"
	KindMode  Kind = "mode"
)

// Entry is a scheduled lighting action.
type Entry struct {
	ID          string    `json:"id"`
	Enabled     bool      `json:"enabled"`
	Kind        Kind      `json:"kind"`
	Days        []string  `json:"days"` // mon..sun; empty = every day
	At          string    `json:"at"`   // HH:MM
	Timezone    string    `json:"timezone"`
	DurationMin int       `json:"duration_min"`
	From        mode.Look `json:"from"`
	To          mode.Look `json:"to"`
	EndOff      bool      `json:"end_off,omitempty"` // sleep: turn off at end
	Mode        string    `json:"mode,omitempty"`
	LastFired   string    `json:"last_fired,omitempty"` // YYYY-MM-DD
}

// Store persists schedules as JSON.
type Store struct {
	mu   sync.Mutex
	path string
	list []Entry
}

// NewStore loads or creates a store at path.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.list = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.list)
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns a copy of all entries.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.list))
	copy(out, s.list)
	return out
}

// Get returns an entry by id.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.list {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Upsert inserts or replaces an entry.
func (s *Store) Upsert(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == e.ID {
			s.list[i] = e
			return s.saveLocked()
		}
	}
	s.list = append(s.list, e)
	return s.saveLocked()
}

// Delete removes an entry.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("schedule %q not found", id)
}

// SetEnabled toggles an entry.
func (s *Store) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Enabled = enabled
			return s.saveLocked()
		}
	}
	return fmt.Errorf("schedule %q not found", id)
}

// MarkFired records that an entry fired on a calendar day.
func (s *Store) MarkFired(id, day string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].LastFired = day
			return s.saveLocked()
		}
	}
	return fmt.Errorf("schedule %q not found", id)
}

var weekdayNames = map[time.Weekday]string{
	time.Monday:    "mon",
	time.Tuesday:   "tue",
	time.Wednesday: "wed",
	time.Thursday:  "thu",
	time.Friday:    "fri",
	time.Saturday:  "sat",
	time.Sunday:    "sun",
}

// Due reports whether e should fire at now (within the tick window).
func Due(e Entry, now time.Time) bool {
	if !e.Enabled {
		return false
	}
	loc := time.Local
	if e.Timezone != "" {
		if l, err := time.LoadLocation(e.Timezone); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	dayKey := local.Format("2006-01-02")
	if e.LastFired == dayKey {
		return false
	}
	if len(e.Days) > 0 {
		want := weekdayNames[local.Weekday()]
		ok := false
		for _, d := range e.Days {
			if strings.EqualFold(d, want) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	parts := strings.Split(e.At, ":")
	if len(parts) != 2 {
		return false
	}
	var hh, mm int
	_, _ = fmt.Sscanf(e.At, "%d:%d", &hh, &mm)
	target := time.Date(local.Year(), local.Month(), local.Day(), hh, mm, 0, 0, loc)
	// Fire if we're within 90s after the scheduled time (ticker is ~30s).
	diff := local.Sub(target)
	return diff >= 0 && diff < 90*time.Second
}

// Engine evaluates schedules and runs ramps/modes.
type Engine struct {
	store      *Store
	runner     *mode.Runner
	client     *govee.Client
	BeforeFire func() error // optional; e.g. auto-rediscover device IP
}

// NewEngine creates a schedule engine.
func NewEngine(store *Store, runner *mode.Runner, client *govee.Client) *Engine {
	return &Engine{store: store, runner: runner, client: client}
}

// Tick checks schedules once.
func (e *Engine) Tick(now time.Time) {
	for _, entry := range e.store.List() {
		if !Due(entry, now) {
			continue
		}
		_ = e.Fire(entry)
		loc := time.Local
		if entry.Timezone != "" {
			if l, err := time.LoadLocation(entry.Timezone); err == nil {
				loc = l
			}
		}
		_ = e.store.MarkFired(entry.ID, now.In(loc).Format("2006-01-02"))
	}
}

// Fire runs a schedule entry immediately (does not mark last_fired unless via Tick).
func (e *Engine) Fire(entry Entry) error {
	if e.BeforeFire != nil {
		if err := e.BeforeFire(); err != nil {
			return err
		}
	}
	switch entry.Kind {
	case KindWake:
		dur := time.Duration(entry.DurationMin) * time.Minute
		if dur <= 0 {
			dur = 30 * time.Minute
		}
		e.runner.StartRamp("wake:"+entry.ID, entry.From, entry.To, dur, false)
	case KindSleep:
		dur := time.Duration(entry.DurationMin) * time.Minute
		if dur <= 0 {
			dur = 30 * time.Minute
		}
		e.runner.StartRamp("sleep:"+entry.ID, entry.From, entry.To, dur, entry.EndOff)
	case KindMode:
		cfg := mode.Config{
			Name:          entry.Mode,
			Brightness:    entry.To.Brightness,
			MinBrightness: 15,
			Speed:         1,
		}
		if cfg.Brightness == 0 {
			cfg.Brightness = 80
		}
		e.runner.StartMode(cfg)
	default:
		return fmt.Errorf("unknown kind %q", entry.Kind)
	}
	return nil
}

// Run loops until stop is closed.
func (e *Engine) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	e.Tick(time.Now())
	for {
		select {
		case <-stop:
			return
		case t := <-ticker.C:
			e.Tick(t)
		}
	}
}

// DefaultWake returns a sensible weekday wake look.
func DefaultWake() (from, to mode.Look) {
	blue := govee.NamedColors["blue"]
	from = mode.Look{Color: &blue, Brightness: 5}
	to = mode.Look{Temp: govee.TempPresets["daylight"], Brightness: 55}
	return
}

// DefaultSleep returns a sensible sleep look.
func DefaultSleep() (from, to mode.Look) {
	from = mode.Look{Temp: govee.TempPresets["neutral"], Brightness: 40}
	to = mode.Look{Temp: govee.TempPresets["candle"], Brightness: 5}
	return
}

// ParseDays parses comma-separated day names or keywords weekdays/weekend/everyday.
func ParseDays(s string) ([]string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", "everyday", "daily", "*":
		return nil, nil
	case "weekdays", "weekday":
		return []string{"mon", "tue", "wed", "thu", "fri"}, nil
	case "weekend":
		return []string{"sat", "sun"}, nil
	}
	parts := strings.Split(s, ",")
	valid := map[string]bool{
		"mon": true, "tue": true, "wed": true, "thu": true,
		"fri": true, "sat": true, "sun": true,
	}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !valid[p] {
			return nil, fmt.Errorf("invalid day %q", p)
		}
		out = append(out, p)
	}
	return out, nil
}

// ParseLook builds a Look from color/temp strings.
func ParseLook(colorStr, tempStr string, brightness int) (mode.Look, error) {
	look := mode.Look{Brightness: brightness}
	if tempStr != "" {
		k, err := govee.ParseTemp(tempStr)
		if err != nil {
			return look, err
		}
		look.Temp = k
		return look, nil
	}
	if colorStr != "" {
		c, err := govee.ParseColor(colorStr)
		if err != nil {
			return look, err
		}
		look.Color = &c
		return look, nil
	}
	return look, fmt.Errorf("need color or temp")
}
