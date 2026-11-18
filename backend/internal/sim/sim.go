// Package sim generates realistic fan traffic against a running Ticketracer API over HTTP.
// The dashboard simulator and the load-test CLI both use it, so both measure the real system.
package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Config describes one simulation run.
type Config struct {
	ShowID       int     `json:"show_id"`
	BaseURL      string  `json:"-"`
	AdminToken   string  `json:"-"`
	Users        int     `json:"users"`
	Concurrency  int     `json:"concurrency"`   // max simultaneous HTTP connections
	Stampede     bool    `json:"stampede"`      // skip the waiting room: everyone hits reserve at once
	AbandonPct   float64 `json:"abandon_pct"`   // share of holds never paid (they expire)
	DuplicatePct float64 `json:"duplicate_pct"` // share of payments whose webhook is delivered 3 times
	MaxSeats     int     `json:"max_seats"`     // seats per fan: 1..MaxSeats
	AbandonTTL   int     `json:"abandon_ttl"`   // hold seconds for abandoned holds, so expiry is visible in a short run
}

// Progress is updated live while a run is in flight.
type Progress struct {
	Users, Admitted, Holds, Conflicts, SoldOut, Paid, Abandoned, DuplicatesSent, Errors atomic.Int64
	Requests                                                                            atomic.Int64
}

// Report is the summary of a finished run.
type Report struct {
	Config           Config             `json:"config"`
	DurationSec      float64            `json:"duration_sec"`
	Requests         int64              `json:"requests"`
	RequestsPerSec   float64            `json:"requests_per_sec"`
	Counts           map[string]int     `json:"counts"`
	ReserveLatencyMs map[string]float64 `json:"reserve_latency_ms"`
	Invariants       map[string]any     `json:"invariants"`
}

type runner struct {
	cfg  Config
	http *http.Client
	p    *Progress
	mu   sync.Mutex
	lat  []time.Duration // reserve latencies
}

// Run executes the simulation and blocks until every virtual fan is done or ctx ends.
func Run(ctx context.Context, cfg Config, p *Progress) (Report, error) {
	cfg.Users = clamp(cfg.Users, 1, 20000)
	cfg.Concurrency = clamp(cfg.Concurrency, 1, 1000)
	cfg.MaxSeats = clamp(cfg.MaxSeats, 1, 6)
	cfg.AbandonTTL = clamp(cfg.AbandonTTL, 5, 600)
	r := &runner{cfg: cfg, p: p, http: &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{MaxConnsPerHost: cfg.Concurrency, MaxIdleConnsPerHost: cfg.Concurrency},
	}}

	start := time.Now()
	var wg sync.WaitGroup
	for range cfg.Users {
		wg.Add(1)
		go func() { defer wg.Done(); r.fan(ctx) }()
	}
	wg.Wait()
	dur := time.Since(start)

	rep := Report{
		Config: cfg, DurationSec: round(dur.Seconds()), Requests: p.Requests.Load(),
		RequestsPerSec: round(float64(p.Requests.Load()) / dur.Seconds()),
		Counts: map[string]int{
			"users": int(p.Users.Load()), "admitted": int(p.Admitted.Load()), "holds": int(p.Holds.Load()),
			"conflicts_409": int(p.Conflicts.Load()), "sold_out": int(p.SoldOut.Load()), "paid": int(p.Paid.Load()),
			"abandoned": int(p.Abandoned.Load()), "duplicate_webhooks_sent": int(p.DuplicatesSent.Load()), "errors": int(p.Errors.Load()),
		},
		ReserveLatencyMs: r.percentiles(),
	}
	// read the database invariants after the run
	var stats struct {
		DB map[string]any `json:"db"`
	}
	if err := r.do(ctx, "GET", "/api/admin/stats?show="+strconv.Itoa(r.cfg.ShowID), "", nil, &stats); err == nil {
		rep.Invariants = stats.DB
	}
	return rep, ctx.Err()
}

// fan is one virtual customer: join, wait, try to hold seats, then pay or walk away.
func (r *runner) fan(ctx context.Context) {
	r.p.Users.Add(1)
	var join struct{ Token string }
	path := r.show("/queue/join")
	if r.cfg.Stampede {
		path += "?skip=1"
	}
	if err := r.do(ctx, "POST", path, "", nil, &join); err != nil {
		r.p.Errors.Add(1)
		return
	}
	if !r.waitAdmitted(ctx, join.Token) {
		return
	}
	r.p.Admitted.Add(1)

	abandon := rand.Float64() < r.cfg.AbandonPct
	var hold struct{ ID string }
	// Like real fans refreshing a sold-out page, a fan who finds nothing free keeps watching for as
	// long as anyone still holds seats: every hold ends paid or expired, and an expired hold puts
	// seats back for a waiting fan. A fan gives up only when nothing is free and nothing is held
	// (truly sold out). The run's own deadline (ctx) still bounds the wait.
	for {
		ids, held, ok := r.pickSeats(ctx, join.Token)
		if !ok {
			if held == 0 || ctx.Err() != nil {
				r.p.SoldOut.Add(1)
				return
			}
			select { // nothing free right now, but holds may still expire
			case <-ctx.Done():
				return
			case <-time.After(time.Second + rand.N(2*time.Second)):
			}
			continue
		}
		ttl := ""
		if abandon {
			ttl = strconv.Itoa(r.cfg.AbandonTTL)
		}
		t := time.Now()
		err := r.do(ctx, "POST", r.show("/holds"), join.Token, map[string]any{"seat_ids": ids}, &hold, "X-Hold-TTL", ttl)
		r.mu.Lock()
		r.lat = append(r.lat, time.Since(t))
		r.mu.Unlock()
		if err == nil {
			break
		}
		if he, ok := err.(httpError); ok && he.code == http.StatusConflict {
			r.p.Conflicts.Add(1) // someone else won those seats; look again
			continue
		}
		r.p.Errors.Add(1)
		return
	}
	r.p.Holds.Add(1)

	if abandon {
		r.p.Abandoned.Add(1)
		return
	}
	dups := 1
	if rand.Float64() < r.cfg.DuplicatePct {
		dups = 3
		r.p.DuplicatesSent.Add(2)
	}
	if err := r.do(ctx, "POST", "/api/demo/pay/"+hold.ID, join.Token, map[string]int{"deliveries": dups}, nil); err != nil {
		r.p.Errors.Add(1)
		return
	}
	r.p.Paid.Add(1)
}

func (r *runner) waitAdmitted(ctx context.Context, token string) bool {
	for {
		var st struct{ Admitted bool }
		if err := r.do(ctx, "GET", r.show("/queue/status"), token, nil, &st); err != nil {
			r.p.Errors.Add(1)
			return false
		}
		if st.Admitted {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400*time.Millisecond + rand.N(400*time.Millisecond)):
		}
	}
}

// pickSeats chooses 1..MaxSeats free seats, biased toward the front rows like real fans,
// which is what creates contention. It also reports how many seats other fans are holding.
func (r *runner) pickSeats(ctx context.Context, token string) (ids []int, held int, ok bool) {
	var seats []struct {
		ID     int
		Status string
	}
	if err := r.do(ctx, "GET", r.show("/seats"), token, nil, &seats); err != nil {
		return nil, 0, false
	}
	free := make([]int, 0, len(seats))
	for _, s := range seats {
		switch s.Status {
		case "available":
			free = append(free, s.ID)
		case "held":
			held++
		}
	}
	if len(free) == 0 {
		return nil, held, false
	}
	want := min(1+rand.N(r.cfg.MaxSeats), len(free))
	picked := map[int]bool{}
	for len(picked) < want {
		x := rand.Float64()
		picked[free[int(x*x*float64(len(free)))]] = true // x² skews toward low ids (front rows)
	}
	ids = make([]int, 0, want)
	for id := range picked {
		ids = append(ids, id)
	}
	return ids, held, true
}

// show builds a path under this run's show.
func (r *runner) show(p string) string { return "/api/shows/" + strconv.Itoa(r.cfg.ShowID) + p }

type httpError struct{ code int }

func (e httpError) Error() string { return "http " + strconv.Itoa(e.code) }

// do sends one JSON request. extra holds optional header key/value pairs (empty values are skipped).
func (r *runner) do(ctx context.Context, method, path, token string, body, out any, extra ...string) error {
	r.p.Requests.Add(1)
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.BaseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.AdminToken)
	if token != "" {
		req.Header.Set("X-Queue-Token", token)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if extra[i+1] != "" {
			req.Header.Set(extra[i], extra[i+1])
		}
	}
	res, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return httpError{res.StatusCode}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (r *runner) percentiles() map[string]float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lat) == 0 {
		return map[string]float64{}
	}
	slices.Sort(r.lat)
	at := func(q float64) float64 {
		return round(float64(r.lat[int(q*float64(len(r.lat)-1))].Microseconds()) / 1000)
	}
	return map[string]float64{"p50": at(0.50), "p95": at(0.95), "p99": at(0.99), "max": at(1), "samples": float64(len(r.lat))}
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }
func round(f float64) float64 { v, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", f), 64); return v }
