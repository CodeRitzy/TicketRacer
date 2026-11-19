// Command loadtest drives simulated fans against a running API and prints a Markdown report.
//
//	go run ./cmd/loadtest -users 2000 -concurrency 200 -stampede
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"time"

	"ticketracer/internal/config"
	"ticketracer/internal/sim"
)

func main() {
	config.LoadDotEnv("../.env")
	// with no .env the stack runs on the local default token, so use that too
	cfg := sim.Config{AdminToken: cmp.Or(os.Getenv("ADMIN_TOKEN"), config.LocalDefault)}
	flag.StringVar(&cfg.BaseURL, "url", "http://localhost:8080", "API base URL (nginx or the api directly)")
	flag.IntVar(&cfg.Users, "users", 2000, "virtual fans")
	flag.IntVar(&cfg.Concurrency, "concurrency", 200, "max simultaneous connections")
	flag.BoolVar(&cfg.Stampede, "stampede", false, "skip the waiting room: everyone reserves at once")
	flag.Float64Var(&cfg.AbandonPct, "abandon", 0.2, "share of holds never paid")
	flag.Float64Var(&cfg.DuplicatePct, "dup", 0.3, "share of payments whose webhook is delivered 3 times")
	flag.IntVar(&cfg.MaxSeats, "max-seats", 4, "seats per fan, 1..n")
	flag.IntVar(&cfg.AbandonTTL, "abandon-ttl", 15, "hold seconds for abandoned holds")
	flag.IntVar(&cfg.ShowID, "show", 0, "show id to attack (0: the largest show in -state)")
	state := flag.String("state", "CA", "state whose largest show is used when -show is 0")
	reset := flag.Bool("reset", true, "reset the show's bookings before the run")
	asJSON := flag.Bool("json", false, "print the raw JSON report")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if cfg.ShowID == 0 {
		id, err := largestShow(ctx, cfg.BaseURL, *state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not pick a show:", err)
			os.Exit(1)
		}
		cfg.ShowID = id
	}
	if *reset {
		req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/admin/reset?show=%d", cfg.BaseURL, cfg.ShowID), nil)
		req.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != http.StatusNoContent {
			fmt.Fprintln(os.Stderr, "reset failed:", err, statusOf(res))
			os.Exit(1)
		}
		res.Body.Close()
	}

	fmt.Fprintf(os.Stderr, "running %d fans against show %d (concurrency %d, stampede=%v)...\n", cfg.Users, cfg.ShowID, cfg.Concurrency, cfg.Stampede)
	rep, err := sim.Run(ctx, cfg, &sim.Progress{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run interrupted:", err)
	}
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(rep)
		return
	}
	printMarkdown(rep)
	inv := rep.Invariants
	if inv == nil {
		fmt.Println("\n**Could not read the invariants after the run** (stats request failed); rerun or check /api/admin/stats.")
		os.Exit(1)
	}
	if inv["sold_without_paid_order"] != 0.0 || inv["duplicate_orders"] != 0.0 || inv["reconciled"] != true {
		fmt.Println("\n**INVARIANT VIOLATION**")
		os.Exit(1)
	}
}

// largestShow returns the id of the show with the most seats in a state (creating the lineup if needed).
func largestShow(ctx context.Context, base, state string) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/shows?state="+state, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var body struct {
		Shows []struct{ ID, Available, Held, Sold int }
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || len(body.Shows) == 0 {
		return 0, fmt.Errorf("no shows for %s (%v)", state, err)
	}
	best, size := 0, -1
	for _, sh := range body.Shows {
		if n := sh.Available + sh.Held + sh.Sold; n > size {
			best, size = sh.ID, n
		}
	}
	return best, nil
}

func statusOf(res *http.Response) string {
	if res == nil {
		return ""
	}
	return res.Status
}

func printMarkdown(r sim.Report) {
	fmt.Printf("Run: %s · show %d · %d fans · concurrency %d · stampede=%v · abandon %.0f%% · duplicate webhooks %.0f%%\n\n",
		time.Now().Format("2006-01-02 15:04 MST"), r.Config.ShowID, r.Config.Users, r.Config.Concurrency, r.Config.Stampede, r.Config.AbandonPct*100, r.Config.DuplicatePct*100)
	fmt.Printf("| Metric | Value |\n|---|---|\n| Duration | %.2f s |\n| HTTP requests | %d |\n| Throughput | %.0f req/s |\n",
		r.DurationSec, r.Requests, r.RequestsPerSec)
	for _, k := range []string{"p50", "p95", "p99", "max"} {
		fmt.Printf("| Reserve latency %s | %.1f ms |\n", k, r.ReserveLatencyMs[k])
	}
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("| %s | %d |\n", k, r.Counts[k])
	}
	fmt.Println("\n**Database after run**\n\n| Check | Value |\n|---|---|")
	for _, k := range []string{"available", "held", "sold", "orders_paid", "orders_refunded", "webhook_events", "sold_without_paid_order", "duplicate_orders", "reconciled"} {
		fmt.Printf("| %s | %v |\n", k, r.Invariants[k])
	}
}
