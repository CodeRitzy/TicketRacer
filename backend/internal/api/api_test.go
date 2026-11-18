package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ticketracer/internal/catalog"
	"ticketracer/internal/config"
	"ticketracer/internal/pay"
	"ticketracer/internal/queue"
	"ticketracer/internal/store"
)

const adminToken = "test-admin-token-0123456789abcdef"

// testAPI starts the real handler stack against the test Postgres and Redis, with one fresh
// 200-seat show. It returns the server, the Server and the show's path prefix.
func testAPI(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	dbURL, redisURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("TEST_DATABASE_URL / TEST_REDIS_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	opt, _ := redis.ParseURL(redisURL)
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() })
	q := queue.New(rdb, time.Minute)
	if err := st.Reset(ctx, 0); err != nil {
		t.Fatal(err)
	}
	rdb.FlushDB(ctx)
	venue, _ := catalog.VenueByKey("chicago-theatre") // 3,600 capacity scales to 200 seats
	sh, err := st.CreateShow(ctx, "IL", catalog.Plan{Venue: venue, Act: "The Test Pattern", StartsAt: time.Now().Add(720 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{AdminToken: adminToken, PaymentMode: "demo", DemoWebhookSecret: "whsec_test_demo",
		HoldTTL: time.Minute, MaxSeatsPerHold: 6, AdmitPerSecond: 50, JoinRatePerMin: 1000, PublicURL: "http://web"}
	s := New(cfg, st, q, pay.Demo{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	s.cfg.SelfURL = ts.URL // demo webhooks loop back into this test server
	return ts, s, fmt.Sprintf("/api/shows/%d", sh.ID)
}

func call(t *testing.T, ts *httptest.Server, method, path string, headers map[string]string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, ts.URL+path, &buf)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// admittedFan joins with stampede mode so the test does not wait for the admit loop.
func admittedFan(t *testing.T, ts *httptest.Server, show string) string {
	_, out := call(t, ts, "POST", show+"/queue/join?skip=1", map[string]string{"Authorization": "Bearer " + adminToken}, nil)
	return out["token"].(string)
}

func TestAdminRequiresToken(t *testing.T) {
	ts, _, _ := testAPI(t)
	for _, h := range []map[string]string{nil, {"Authorization": "Bearer wrong"}, {"Authorization": adminToken}} {
		if code, _ := call(t, ts, "GET", "/api/admin/stats", h, nil); code != http.StatusUnauthorized {
			t.Fatalf("headers %v: got %d, want 401", h, code)
		}
	}
	if code, _ := call(t, ts, "GET", "/api/admin/stats", map[string]string{"Authorization": "Bearer " + adminToken}, nil); code != 200 {
		t.Fatalf("valid token: got %d", code)
	}
}

// A state's lineup is created on first request; unknown states and shows are rejected.
func TestLineupAndShowDetail(t *testing.T) {
	ts, _, _ := testAPI(t)
	if code, _ := call(t, ts, "GET", "/api/shows?state=ZZ", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown state: %d", code)
	}
	code, out := call(t, ts, "GET", "/api/shows?state=CO", nil, nil)
	shows, _ := out["shows"].([]any)
	if code != 200 || len(shows) != store.ShowsPerState {
		t.Fatalf("lineup: %d with %d shows", code, len(shows))
	}
	first := shows[0].(map[string]any)
	venue := first["venue"].(map[string]any)
	if venue["state"] != "CO" || venue["capacity"].(float64) < 500 {
		t.Fatalf("venue %v", venue)
	}
	code, detail := call(t, ts, "GET", fmt.Sprintf("/api/shows/%d", int(first["id"].(float64))), nil, nil)
	if code != 200 || len(detail["sections"].([]any)) == 0 {
		t.Fatalf("detail: %d %v", code, detail)
	}
	if code, _ := call(t, ts, "GET", "/api/shows/999999", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing show: %d", code)
	}
	if code, _ := call(t, ts, "GET", "/api/shows/abc/seats", nil, nil); code != http.StatusNotFound {
		t.Fatalf("bad show id: %d", code)
	}
}

// Fans still in line, with no token, or admitted to another show cannot reserve; skip=1 is ignored for non-admins.
func TestHoldRequiresAdmission(t *testing.T) {
	ts, s, show := testAPI(t)
	_, out := call(t, ts, "POST", show+"/queue/join?skip=1", nil, nil)
	tok := out["token"].(string)
	for _, h := range []map[string]string{nil, {"X-Queue-Token": tok}} {
		if code, _ := call(t, ts, "POST", show+"/holds", h, map[string]any{"seat_ids": []int{1}}); code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", code)
		}
	}
	// admitted to a different show is not admitted to this one
	venue, _ := catalog.VenueByKey("ryman")
	other, _ := s.st.CreateShow(context.Background(), "TN", catalog.Plan{Venue: venue, Act: "Other", StartsAt: time.Now().Add(time.Hour)})
	otherTok := admittedFan(t, ts, fmt.Sprintf("/api/shows/%d", other.ID))
	if code, _ := call(t, ts, "POST", show+"/holds", map[string]string{"X-Queue-Token": otherTok}, map[string]any{"seat_ids": []int{1}}); code != http.StatusForbidden {
		t.Fatalf("cross-show token: got %d, want 403", code)
	}
}

func TestHoldValidation(t *testing.T) {
	ts, _, show := testAPI(t)
	h := map[string]string{"X-Queue-Token": admittedFan(t, ts, show)}
	for _, body := range []any{
		map[string]any{"seat_ids": []int{}},
		map[string]any{"seat_ids": []int{1, 2, 3, 4, 5, 6, 7}},
		map[string]any{"seat_ids": []int{1, 1}},
		map[string]any{"seat_ids": []int{-1}},
		map[string]any{"seat_ids": []int{1}, "extra": true},
		map[string]any{"section": "Main Floor", "quantity": 2}, // reserved seating: GA quantity not allowed
		map[string]any{"section": "Pit", "quantity": 2},        // GA name but not in this venue
		map[string]any{"section": "Pit", "quantity": 0},
	} {
		if code, _ := call(t, ts, "POST", show+"/holds", h, body); code != http.StatusBadRequest {
			t.Fatalf("body %v: got %d, want 400", body, code)
		}
	}
}

// Another fan's hold looks like it does not exist.
func TestHoldIsPrivateToOwner(t *testing.T) {
	ts, _, show := testAPI(t)
	_, hold := call(t, ts, "POST", show+"/holds", map[string]string{"X-Queue-Token": admittedFan(t, ts, show)}, map[string]any{"seat_ids": []int{3}})
	path := "/api/holds/" + hold["id"].(string)
	if code, _ := call(t, ts, "GET", path, map[string]string{"X-Queue-Token": admittedFan(t, ts, show)}, nil); code != http.StatusNotFound {
		t.Fatalf("stranger got %d, want 404", code)
	}
	if code, _ := call(t, ts, "DELETE", path, nil, nil); code != http.StatusNotFound {
		t.Fatalf("anonymous cancel got %d, want 404", code)
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	ts, s, show := testAPI(t)
	tok := admittedFan(t, ts, show)
	_, hold := call(t, ts, "POST", show+"/holds", map[string]string{"X-Queue-Token": tok}, map[string]any{"seat_ids": []int{4}})
	payload := pay.DemoEvent("evt_forged", hold["id"].(string), 12000)
	for _, sig := range []string{"", "t=1,v1=deadbeef", pay.SignHeader(payload, "whsec_wrong")} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/webhooks/stripe", bytes.NewReader(payload))
		req.Header.Set("Stripe-Signature", sig)
		res, _ := http.DefaultClient.Do(req)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("signature %q: got %d, want 400", sig, res.StatusCode)
		}
	}
	if st, _ := s.st.Stats(context.Background(), 0); st.OrdersPaid != 0 {
		t.Fatal("forged webhook created an order")
	}
}

// End to end: hold, pay with the same webhook delivered 5 times concurrently,
// get exactly one order and a consistent database.
func TestDuplicateWebhooksCreateOneOrder(t *testing.T) {
	ts, s, show := testAPI(t)
	tok := admittedFan(t, ts, show)
	fan := map[string]string{"X-Queue-Token": tok}
	code, hold := call(t, ts, "POST", show+"/holds", fan, map[string]any{"seat_ids": []int{1, 2}})
	if code != http.StatusCreated {
		t.Fatalf("hold: %d", code)
	}
	id := hold["id"].(string)
	if code, out := call(t, ts, "POST", "/api/holds/"+id+"/checkout", fan, nil); code != 200 || !strings.Contains(out["url"].(string), id) {
		t.Fatalf("checkout: %d %v", code, out)
	}

	adminFan := map[string]string{"X-Queue-Token": tok, "Authorization": "Bearer " + adminToken}
	_, out := call(t, ts, "POST", "/api/demo/pay/"+id, adminFan, map[string]int{"deliveries": 5})
	paid, dup := 0, 0
	for _, d := range out["deliveries"].([]any) {
		switch {
		case strings.Contains(d.(string), `"paid"`):
			paid++
		case strings.Contains(d.(string), `"duplicate"`):
			dup++
		}
	}
	if paid != 1 || dup != 4 {
		t.Fatalf("deliveries %v: want 1 paid + 4 duplicate", out["deliveries"])
	}

	code, res := call(t, ts, "GET", "/api/holds/"+id+"/order", fan, nil)
	order, _ := res["order"].(map[string]any)
	if code != 200 || order["status"] != "paid" || len(order["seats"].([]any)) != 2 {
		t.Fatalf("order: %d %v", code, res)
	}
	st, _ := s.st.Stats(context.Background(), 0)
	if st.OrdersPaid != 1 || st.Sold != 2 || st.DuplicateOrders != 0 || !st.Reconciled {
		t.Fatalf("stats %+v", st)
	}
	// a sold seat cannot be held again
	if code, _ := call(t, ts, "POST", show+"/holds", map[string]string{"X-Queue-Token": admittedFan(t, ts, show)}, map[string]any{"seat_ids": []int{1}}); code != http.StatusConflict {
		t.Fatalf("re-holding a sold seat: %d, want 409", code)
	}
}

// Without an admin token, a fan cannot replay webhooks or use demo payment in stripe mode.
func TestDemoPayLockedDownInStripeMode(t *testing.T) {
	ts, s, show := testAPI(t)
	s.cfg.PaymentMode = "stripe"
	tok := admittedFan(t, ts, show)
	_, hold := call(t, ts, "POST", show+"/holds", map[string]string{"X-Queue-Token": tok}, map[string]any{"seat_ids": []int{9}})
	if code, _ := call(t, ts, "POST", "/api/demo/pay/"+hold["id"].(string), map[string]string{"X-Queue-Token": tok}, nil); code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", code)
	}
}

// fakeStripe answers Lookup like Stripe would for one paid session.
type fakeStripe struct {
	pay.Demo
	ev pay.Event
}

func (f fakeStripe) Lookup(_ context.Context, id string) (pay.Event, error) {
	if id != f.ev.SessionRef {
		return pay.Event{}, fmt.Errorf("no such session")
	}
	return f.ev, nil
}

// Returning from checkout confirms the order even if the webhook never arrives; a webhook that
// lands later is a duplicate, and a session belonging to another hold is ignored.
func TestReturnFromCheckoutConfirmsWithoutWebhook(t *testing.T) {
	ts, s, show := testAPI(t)
	s.cfg.PaymentMode = "stripe"
	tok := admittedFan(t, ts, show)
	fan := map[string]string{"X-Queue-Token": tok}
	_, hold := call(t, ts, "POST", show+"/holds", fan, map[string]any{"seat_ids": []int{11, 12}})
	id := hold["id"].(string)
	session := "cs_test_a1B2c3D4e5F6g7H8"
	s.pay = fakeStripe{ev: pay.Event{ID: "sync_" + session, Type: "checkout.session.completed", HoldID: id, PaymentRef: "pi_test_123",
		SessionRef: session, AmountCents: int(hold["total_cents"].(float64)), Paid: true}}

	// a session id that isn't this hold's (or is malformed) changes nothing
	for _, bad := range []string{"cs_test_zzzzzzzzzzzzzzzz", "not-a-session"} {
		if code, _ := call(t, ts, "GET", "/api/holds/"+id+"/order?session="+bad, fan, nil); code != http.StatusAccepted {
			t.Fatalf("session %q: got %d, want 202 processing", bad, code)
		}
	}
	code, res := call(t, ts, "GET", "/api/holds/"+id+"/order?session="+session, fan, nil)
	order, _ := res["order"].(map[string]any)
	if code != 200 || order["status"] != "paid" {
		t.Fatalf("return from checkout: %d %v", code, res)
	}
	// the real webhook arrives late: one order still
	outcome, _, err := s.st.CompleteOrder(context.Background(), store.PaymentEvent{EventID: "evt_late", HoldID: id,
		PaymentRef: "pi_test_123", SessionRef: session, AmountCents: int(hold["total_cents"].(float64))})
	if err != nil || outcome != store.OutcomeDuplicate {
		t.Fatalf("late webhook: %s %v, want duplicate", outcome, err)
	}
	if st, _ := s.st.Stats(context.Background(), 0); st.OrdersPaid != 1 || !st.Reconciled {
		t.Fatalf("stats %+v", st)
	}
}
