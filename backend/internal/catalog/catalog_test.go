package catalog

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// Every state and DC has at least one venue, and every venue is well formed.
func TestCatalogCoverage(t *testing.T) {
	if len(States) != 51 {
		t.Fatalf("%d states, want 50 + DC", len(States))
	}
	keys := map[string]bool{}
	for _, v := range Venues {
		if keys[v.Key] {
			t.Errorf("duplicate venue key %s", v.Key)
		}
		keys[v.Key] = true
		if _, ok := StateByCode(v.State); !ok {
			t.Errorf("%s: unknown state %s", v.Key, v.State)
		}
		if v.Capacity < 500 || v.Source == "" || len(SectionsOf(v)) == 0 {
			t.Errorf("%s: incomplete entry %+v", v.Key, v)
		}
		if _, err := time.LoadLocation(mustState(v.State).Zone); err != nil {
			t.Errorf("%s: bad zone: %v", v.Key, err)
		}
	}
	for _, s := range States {
		if len(VenuesIn(s.Code)) == 0 {
			t.Errorf("no venues in %s", s.Code)
		}
	}
}

func mustState(code string) State { s, _ := StateByCode(code); return s }

// Layouts are scaled within 200..600 seats, ids are 1..n, front sections cost the most.
func TestLayout(t *testing.T) {
	for _, v := range Venues {
		seats, sections := Layout(v)
		if n := len(seats); n != DemoSeats(v.Capacity) || n < 200 || n > 600 {
			t.Fatalf("%s: %d seats", v.Key, n)
		}
		total := 0
		for i, sec := range sections {
			total += sec.Seats
			if i > 0 && sec.PriceCents > sections[i-1].PriceCents {
				t.Errorf("%s: %s costs more than the section in front of it", v.Key, sec.Name)
			}
			if sec.PriceCents%500 != 0 || sec.Seats == 0 {
				t.Errorf("%s: bad section %+v", v.Key, sec)
			}
		}
		if total != len(seats) {
			t.Errorf("%s: sections sum to %d, seats %d", v.Key, total, len(seats))
		}
		for i, s := range seats {
			if s.ID != i+1 {
				t.Fatalf("%s: seat %d has id %d", v.Key, i, s.ID)
			}
		}
	}
}

// A lineup has distinct acts, evening Thursday–Saturday dates, 2–13 weeks out.
func TestLineup(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	plans := Lineup(r, mustState("CO"), 6, now)
	if len(plans) != 6 {
		t.Fatalf("%d plans", len(plans))
	}
	acts := map[string]bool{}
	for _, p := range plans {
		if acts[p.Act] || strings.TrimSpace(p.Act) == "" {
			t.Errorf("duplicate or empty act %q", p.Act)
		}
		acts[p.Act] = true
		if wd := p.StartsAt.Weekday(); wd < time.Thursday {
			t.Errorf("%s falls on %s", p.Act, wd)
		}
		if h := p.StartsAt.Hour(); h < 19 || h > 20 {
			t.Errorf("%s starts at hour %d", p.Act, h)
		}
		if d := p.StartsAt.Sub(now); d < 13*24*time.Hour || d > 13*7*24*time.Hour {
			t.Errorf("%s is %v away", p.Act, d)
		}
	}
}
