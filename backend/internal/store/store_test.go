package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ticketracer/internal/catalog"
)

// testStore connects to TEST_DATABASE_URL and resets it. Tests are skipped when it is unset.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Reset(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	// every test gets a fresh 200-seat show (a 4,000-capacity theater scales to 200 seats)
	sh, err := s.CreateShow(context.Background(), "TS", testPlan())
	if err != nil {
		t.Fatal(err)
	}
	testShow = sh.ID
	return s
}

// testShow is the show the current test books against (tests in a package run one at a time).
var testShow int

func testPlan() catalog.Plan {
	return catalog.Plan{Venue: catalog.Venue{Key: "test-hall", Capacity: 4000, Kind: catalog.Theater}, Act: "The Test Pattern", StartsAt: time.Now().Add(720 * time.Hour)}
}

func assertInvariants(t *testing.T, s *Store) Stats {
	t.Helper()
	st, err := s.Stats(context.Background(), testShow)
	if err != nil {
		t.Fatal(err)
	}
	if st.SoldWithoutPaidOrder != 0 || st.DuplicateOrders != 0 || !st.Reconciled {
		t.Fatalf("invariants broken: %+v", st)
	}
	if st.Available+st.Held+st.Sold != 200 {
		t.Fatalf("seat count drifted: %+v", st)
	}
	return st
}

// The headline guarantee: 100 fans race for one seat, exactly one wins.
func TestReserveRaceSingleSeat(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	var wins, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Reserve(ctx, testShow, fmt.Sprintf("fan-%d", i), []int{5}, time.Minute)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrConflict):
				conflicts.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || conflicts.Load() != 99 {
		t.Fatalf("wins=%d conflicts=%d, want 1/99", wins.Load(), conflicts.Load())
	}
	assertInvariants(t, s)
}

// A request that includes one taken seat must hold none of them.
func TestReserveAllOrNothing(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	if _, err := s.Reserve(ctx, testShow, "a", []int{10}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, testShow, "b", []int{9, 10, 11}, time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	st := assertInvariants(t, s)
	if st.Held != 1 {
		t.Fatalf("partial hold leaked: held=%d", st.Held)
	}
}

// Many overlapping multi-seat requests: every seat ends up in at most one hold,
// and every successful hold got exactly the seats it asked for.
func TestReserveOverlappingRace(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	var mu sync.Mutex
	held := map[int]string{}
	var wg sync.WaitGroup
	for i := range 80 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := []int{1 + rand.N(20), 21 + rand.N(20), 41 + rand.N(20)} // distinct ranges => no duplicate ids
			h, err := s.Reserve(ctx, testShow, fmt.Sprintf("fan-%d", i), ids, time.Minute)
			if errors.Is(err, ErrConflict) {
				return
			} else if err != nil {
				t.Error(err)
				return
			}
			if len(h.Seats) != 3 {
				t.Errorf("hold %s has %d seats, want 3", h.ID, len(h.Seats))
			}
			mu.Lock()
			defer mu.Unlock()
			for _, x := range h.Seats {
				if prev, dup := held[x.ID]; dup {
					t.Errorf("seat %d held by %s and %s", x.ID, prev, h.ID)
				}
				held[x.ID] = h.ID
			}
		}()
	}
	wg.Wait()
	if st := assertInvariants(t, s); st.Held != len(held) {
		t.Fatalf("db says %d held, holds report %d", st.Held, len(held))
	}
}

// A fan picking again replaces their previous hold instead of hoarding seats.
func TestReserveReplacesPreviousHold(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	first, _ := s.Reserve(ctx, testShow, "a", []int{1}, time.Minute)
	if _, err := s.Reserve(ctx, testShow, "a", []int{2}, time.Minute); err != nil {
		t.Fatal(err)
	}
	h, _ := s.Hold(ctx, first.ID)
	if h.Status != "cancelled" || len(h.Seats) != 0 {
		t.Fatalf("old hold not released: %+v", h)
	}
}

func TestSweepReleasesExpiredHolds(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	h, err := s.Reserve(ctx, testShow, "a", []int{1, 2, 3}, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	freed, err := s.SweepExpired(ctx)
	if err != nil || freed[testShow] != 3 {
		t.Fatalf("freed %v (err %v), want 3 for show %d", freed, err, testShow)
	}
	got, _ := s.Hold(ctx, h.ID)
	if got.Status != "expired" {
		t.Fatalf("status %s, want expired", got.Status)
	}
	// the freed seats can be booked again
	if _, err := s.Reserve(ctx, testShow, "b", []int{1, 2, 3}, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func paymentFor(h Hold, eventID string) PaymentEvent {
	return PaymentEvent{EventID: eventID, HoldID: h.ID, PaymentRef: "pi_" + h.ID, SessionRef: "cs_" + h.ID, AmountCents: h.TotalCents}
}

// The same event delivered 5 times at once, plus a different event for the same payment,
// still produces exactly one order.
func TestCompleteOrderIsIdempotent(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	h, _ := s.Reserve(ctx, testShow, "a", []int{7, 8}, time.Minute)

	outcomes := make(chan Outcome, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, _, err := s.CompleteOrder(ctx, paymentFor(h, "evt_1"))
			if err != nil {
				t.Error(err)
			}
			outcomes <- o
		}()
	}
	wg.Wait()
	close(outcomes)
	count := map[Outcome]int{}
	for o := range outcomes {
		count[o]++
	}
	if count[OutcomePaid] != 1 || count[OutcomeDuplicate] != 4 {
		t.Fatalf("outcomes %v, want 1 paid + 4 duplicate", count)
	}
	if o, _, _ := s.CompleteOrder(ctx, paymentFor(h, "evt_2")); o != OutcomeDuplicate {
		t.Fatalf("second event for same hold: %s, want duplicate", o)
	}
	st := assertInvariants(t, s)
	if st.OrdersPaid != 1 || st.Sold != 2 || st.RevenueCents != h.TotalCents {
		t.Fatalf("unexpected stats %+v", st)
	}
}

// Payment landing after the hold expired: never sells the seats, queues a refund.
func TestLatePaymentIsRefunded(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	h, _ := s.Reserve(ctx, testShow, "a", []int{1}, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	s.SweepExpired(ctx)
	other, err := s.Reserve(ctx, testShow, "b", []int{1}, time.Minute) // someone else takes the seat
	if err != nil {
		t.Fatal(err)
	}
	if o, _, _ := s.CompleteOrder(ctx, paymentFor(h, "evt_late")); o != OutcomeRefund {
		t.Fatalf("outcome %s, want refund", o)
	}
	pending, _ := s.PendingRefunds(ctx)
	if len(pending) != 1 {
		t.Fatalf("pending refunds %d, want 1", len(pending))
	}
	if err := s.MarkRefunded(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Hold(ctx, other.ID)
	if got.Status != "active" || got.Seats[0].Status != "held" {
		t.Fatalf("late payment disturbed the new holder: %+v", got)
	}
	if st := assertInvariants(t, s); st.Sold != 0 || st.OrdersRefunded != 1 {
		t.Fatalf("unexpected stats %+v", st)
	}
}

// The amount comes from our database; a mismatching payment is refunded, not fulfilled.
func TestAmountMismatchIsRefunded(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	h, _ := s.Reserve(ctx, testShow, "a", []int{1}, time.Minute)
	ev := paymentFor(h, "evt_cheap")
	ev.AmountCents = 1
	if o, _, _ := s.CompleteOrder(ctx, ev); o != OutcomeRefund {
		t.Fatalf("outcome %s, want refund", o)
	}
	assertInvariants(t, s)
}

// A second, different payment for an already-paid hold is flagged, not counted as a harmless duplicate.
func TestSecondPaymentIsFlagged(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	h, _ := s.Reserve(ctx, testShow, "a", []int{1}, time.Minute)
	if o, _, _ := s.CompleteOrder(ctx, paymentFor(h, "evt_a")); o != OutcomePaid {
		t.Fatalf("first payment: %s", o)
	}
	second := paymentFor(h, "evt_b")
	second.PaymentRef = "pi_other"
	if o, _, _ := s.CompleteOrder(ctx, second); o != OutcomeSecondPayment {
		t.Fatalf("second payment: %s, want second_payment", o)
	}
	assertInvariants(t, s)
}

func TestUnknownHold(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	o, _, err := s.CompleteOrder(ctx, PaymentEvent{EventID: "evt_x", HoldID: "00000000-0000-0000-0000-000000000000", AmountCents: 1})
	if err != nil || o != OutcomeUnknown {
		t.Fatalf("got %s %v, want unknown", o, err)
	}
}

// The same seat number in two shows is two different seats: holding one never touches the other.
func TestHoldsCannotCrossShows(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	a := testShow
	b, err := s.CreateShow(ctx, "TS", testPlan())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, a, "fan", []int{5}, time.Minute); err != nil {
		t.Fatal(err)
	}
	hb, err := s.Reserve(ctx, b.ID, "fan", []int{5}, time.Minute)
	if err != nil {
		t.Fatalf("seat 5 of show b should be free: %v", err)
	}
	if hb.ShowID != b.ID || len(hb.Seats) != 1 {
		t.Fatalf("hold %+v", hb)
	}
	// a fan's second hold in show b must not cancel their hold in show a
	if st, _ := s.Stats(ctx, a); st.Held != 1 {
		t.Fatalf("show a held=%d, want 1", st.Held)
	}
	// an id that only exists in a bigger show cannot be claimed in a smaller one
	if _, err := s.Reserve(ctx, a, "fan2", []int{999}, time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("seat 999 in a 200-seat show: %v", err)
	}
}

// Many first visitors to a state at once still create exactly one lineup.
func TestLineupCreatedOnce(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	state, _ := catalog.StateByCode("VT")
	s.pool.Exec(ctx, `DELETE FROM shows WHERE state = 'VT'`)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.EnsureLineup(ctx, state, false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	shows, err := s.Lineup(ctx, "VT")
	if err != nil || len(shows) != ShowsPerState {
		t.Fatalf("%d shows (err %v), want %d", len(shows), err, ShowsPerState)
	}
}

// General admission: 60 fans race for 4 floor tickets each; the floor never oversells and every
// winner gets exactly 4.
func TestReserveGANeverOversells(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	sh, err := s.CreateShow(ctx, "TS", catalog.Plan{Venue: catalog.Venue{Key: "test-club", Capacity: 4000, Kind: catalog.Ballroom},
		Act: "The Floor Test", StartsAt: time.Now().Add(720 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	seats, _ := s.Seats(ctx, sh.ID)
	floor := 0
	for _, x := range seats {
		if x.Section == "Floor" {
			floor++
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := s.ReserveGA(ctx, sh.ID, fmt.Sprintf("fan-%d", i), "Floor", 4, time.Minute)
			if errors.Is(err, ErrConflict) {
				return
			} else if err != nil {
				t.Error(err)
				return
			}
			if len(h.Seats) != 4 {
				t.Errorf("hold got %d floor tickets, want 4", len(h.Seats))
			}
			wins.Add(1)
		}()
	}
	wg.Wait()
	st, _ := s.Stats(ctx, sh.ID)
	if st.Held != int(wins.Load())*4 || st.Held > floor || int(wins.Load()) != floor/4 {
		t.Fatalf("floor %d, held %d, winners %d", floor, st.Held, wins.Load())
	}
}
