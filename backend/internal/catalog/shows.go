package catalog

import (
	"math/rand/v2"
	"time"
	_ "time/tzdata" // the distroless image has no zoneinfo; embed it so venue-local times work
)

// SeatSpec is one seat to create for a show. IDs run 1..n, nearest section first, row-major.
type SeatSpec struct {
	ID         int
	Section    string
	Row        string
	Number     int
	PriceCents int
}

// SectionInfo summarizes one section for the API and the venue map.
type SectionInfo struct {
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Seats      int    `json:"seats"`
	GA         bool   `json:"ga"` // general admission: fans buy entry to the area, not a seat
}

// IsGA reports whether a section is general admission (standing floor, pit, lawn), where a ticket
// is admission to the area rather than a numbered seat, as on Ticketmaster and AXS.
func IsGA(section string) bool {
	switch section {
	case "Floor", "Pit", "Lawn", "General Admission":
		return true
	}
	return false
}

// DemoSeats is how many seats a show sells: a scaled block of the real house that keeps
// the map readable and the stampede dramatic.
func DemoSeats(capacity int) int { return max(200, min(capacity/20, 600)) }

// rowWidth is seats per row: wider rooms get wider rows.
func rowWidth(k Kind) int {
	if k == Amphitheater || k == Arena {
		return 24
	}
	return 20
}

// Layout builds every seat for a venue. Sections further from the stage get more seats
// (weights 1, 1.25, 1.5, ...) and lower prices (front = top of the kind's range).
func Layout(v Venue) ([]SeatSpec, []SectionInfo) {
	sections := SectionsOf(v)
	total := DemoSeats(v.Capacity)
	lo, hi := priceRange[v.Kind][0], priceRange[v.Kind][1]
	width := rowWidth(v.Kind)

	// split the total by weight; the last section absorbs rounding
	var weightSum float64
	for i := range sections {
		weightSum += 1 + 0.25*float64(i)
	}
	seats := make([]SeatSpec, 0, total)
	infos := make([]SectionInfo, 0, len(sections))
	remaining := total
	for i, name := range sections {
		n := int(float64(total) * (1 + 0.25*float64(i)) / weightSum)
		if i == len(sections)-1 {
			n = remaining
		}
		remaining -= n
		price := hi
		if len(sections) > 1 {
			price = hi - (hi-lo)*i/(len(sections)-1)
		}
		price = (price + 250) / 500 * 500 // whole $5 steps
		ga := IsGA(name)
		for k := range n {
			// a GA ticket has no row or seat: it is admission number k+1 to the area
			row, num := rowLabel(k/width), k%width+1
			if ga {
				row, num = "GA", k+1
			}
			seats = append(seats, SeatSpec{ID: len(seats) + 1, Section: name, Row: row, Number: num, PriceCents: price})
		}
		infos = append(infos, SectionInfo{Name: name, PriceCents: price, Seats: n, GA: ga})
	}
	return seats, infos
}

// rowLabel: A..Z, then AA, AB, ... (row letters restart in every section).
func rowLabel(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return string(rune('A'+i/26-1)) + string(rune('A'+i%26))
}

// Invented act names. Word lists, not artists: no real act is named anywhere in this file.
var (
	adjectives = []string{"Velvet", "Paper", "Neon", "Hollow", "Silver", "Midnight", "Copper", "Electric", "Glass", "Wild",
		"Static", "Golden", "Lunar", "Crimson", "Quiet", "Sunday", "Analog", "Feral", "Cosmic", "Northern"}
	nouns = []string{"Comets", "Harbors", "Satellites", "Wolves", "Lanterns", "Machines", "Rivers", "Ghosts", "Parades", "Signals",
		"Orchards", "Engines", "Tides", "Sparrows", "Circuits", "Echoes", "Highways", "Monarchs", "Embers", "Atlases"}
	firstNames = []string{"Nova", "June", "Ellis", "Marlo", "Wren", "Theo", "Sable", "Remy", "Ivy", "Cass"}
	crews      = []string{"the Night Shift", "the Low Tide", "the Wire", "the Late Arrivals", "the Static Choir", "the Long Weekend"}
)

// NewAct invents an act name in one of three common band-name shapes.
func NewAct(r *rand.Rand) string {
	pick := func(xs []string) string { return xs[r.IntN(len(xs))] }
	switch r.IntN(3) {
	case 0:
		return "The " + pick(adjectives) + " " + pick(nouns)
	case 1:
		return pick(adjectives) + " " + pick(nouns)
	default:
		return pick(firstNames) + " & " + pick(crews)
	}
}

// ShowTime picks a Thursday–Saturday evening 2–12 weeks after now, in the venue's zone.
func ShowTime(r *rand.Rand, now time.Time, zone string) time.Time {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	day := now.In(loc).AddDate(0, 0, 14+r.IntN(70))
	for day.Weekday() < time.Thursday { // Sunday..Wednesday roll forward to Thursday
		day = day.AddDate(0, 0, 1)
	}
	minutes := []int{19 * 60, 19*60 + 30, 20 * 60}[r.IntN(3)]
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, loc)
}

// Lineup plans n shows for a state, rotating through its venues, with distinct act names.
func Lineup(r *rand.Rand, state State, n int, now time.Time) []Plan {
	venues := VenuesIn(state.Code)
	if len(venues) == 0 {
		return nil
	}
	seen := map[string]bool{}
	plans := make([]Plan, 0, n)
	for i := range n {
		act := NewAct(r)
		for seen[act] {
			act = NewAct(r)
		}
		seen[act] = true
		plans = append(plans, Plan{Venue: venues[i%len(venues)], Act: act, StartsAt: ShowTime(r, now, state.Zone)})
	}
	return plans
}

// Plan is one show to create.
type Plan struct {
	Venue    Venue
	Act      string
	StartsAt time.Time
}
