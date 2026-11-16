package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ticketracer/internal/config"
	"ticketracer/internal/pay"
	"ticketracer/internal/queue"
	"ticketracer/internal/store"
)

const adminToken = "test-admin-token"

// testAPI starts the real handler stack against the test Postgres and Redis.
func testAPI(t *testing.T) (*httptest.Server, *Server) {
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
	if err := st.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	rdb.FlushDB(ctx)

	cfg := config.Config{AdminToken: adminToken, PaymentMode: "demo", DemoWebhookSecret: "whsec_test_demo",
		HoldTTL: time.Minute, MaxSeatsPerHold: 6, AdmitPerSecond: 50, JoinRatePerMin: 1000, PublicURL: "http://web"}
	s := New(cfg, st, q, pay.Demo{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	s.cfg.SelfURL = ts.URL // demo webhooks loop back into this test server
	return ts, s
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
func admittedFan(t *testing.T, ts *httptest.Server) string {
	_, out := call(t, ts, "POST", "/api/queue/join?skip=1", map[string]string{"Authorization": "Bearer " + adminToken}, nil)
	return out["token"].(string)
}

func TestAdminRequiresToken(t *testing.T) {
	ts, _ := testAPI(t)
	for _, h := range []map[string]string{nil, {"Authorization": "Bearer wrong"}, {"Authorization": adminToken}} {
		if code, _ := call(t, ts, "GET", "/api/admin/stats", h, nil); code != http.StatusUnauthorized {
			t.Fatalf("headers %v: got %d, want 401", h, code)
		}
	}
	if code, _ := call(t, ts, "GET", "/api/admin/stats", map[string]string{"Authorization": "Bearer " + adminToken}, nil); code != 200 {
		t.Fatalf("valid token: got %d", code)
	}
}

// Fans still in line, or with no token, cannot reserve; skip=1 is ignored for non-admins.
func TestHoldRequiresAdmission(t *testing.T) {
	ts, _ := testAPI(t)
	_, out := call(t, ts, "POST", "/api/queue/join?skip=1", nil, nil)
	tok := out["token"].(string)
	for _, h := range []map[string]string{nil, {"X-Queue-Token": tok}} {
		if code, _ := call(t, ts, "POST", "/api/holds", h, map[string]any{"seat_ids": []int{1}}); code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", code)
		}
	}
}

func TestHoldValidation(t *testing.T) {
	ts, _ := testAPI(t)
	h := map[string]string{"X-Queue-Token": admittedFan(t, ts)}
	for _, body := range []any{
		map[string]any{"seat_ids": []int{}},
		map[string]any{"seat_ids": []int{1, 2, 3, 4, 5, 6, 7}},
		map[string]any{"seat_ids": []int{1, 1}},
		map[string]any{"seat_ids": []int{-1}},
		map[string]any{"seat_ids": []int{1}, "extra": true},
	} {
		if code, _ := call(t, ts, "POST", "/api/holds", h, body); code != http.StatusBadRequest {
			t.Fatalf("body %v: got %d, want 400", body, code)
		}
	}
}

// Another fan's hold looks like it does not exist.
func TestHoldIsPrivateToOwner(t *testing.T) {
	ts, _ := testAPI(t)
	_, hold := call(t, ts, "POST", "/api/holds", map[string]string{"X-Queue-Token": admittedFan(t, ts)}, map[string]any{"seat_ids": []int{3}})
	path := "/api/holds/" + hold["id"].(string)
	if code, _ := call(t, ts, "GET", path, map[string]string{"X-Queue-Token": admittedFan(t, ts)}, nil); code != http.StatusNotFound {
		t.Fatalf("stranger got %d, want 404", code)
	}
	if code, _ := call(t, ts, "DELETE", path, nil, nil); code != http.StatusNotFound {
		t.Fatalf("anonymous cancel got %d, want 404", code)
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	ts, s := testAPI(t)
	tok := admittedFan(t, ts)
	_, hold := call(t, ts, "POST", "/api/holds", map[string]string{"X-Queue-Token": tok}, map[string]any{"seat_ids": []int{4}})
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
	if st, _ := s.st.Stats(context.Background()); st.OrdersPaid != 0 {
		t.Fatal("forged webhook created an order")
	}
}

// End to end: hold, pay with the same webhook delivered 5 times concurrently,
// get exactly one order and a consistent database.
func TestDuplicateWebhooksCreateOneOrder(t *testing.T) {
	ts, s := testAPI(t)
	tok := admittedFan(t, ts)
	fan := map[string]string{"X-Queue-Token": tok}
	code, hold := call(t, ts, "POST", "/api/holds", fan, map[string]any{"seat_ids": []int{1, 2}})
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

	code, order := call(t, ts, "GET", "/api/holds/"+id+"/order", fan, nil)
	if code != 200 || order["status"] != "paid" || len(order["seats"].([]any)) != 2 {
		t.Fatalf("order: %d %v", code, order)
	}
	st, _ := s.st.Stats(context.Background())
	if st.OrdersPaid != 1 || st.Sold != 2 || st.DuplicateOrders != 0 || !st.Reconciled {
		t.Fatalf("stats %+v", st)
	}
	// a sold seat cannot be held again
	if code, _ := call(t, ts, "POST", "/api/holds", map[string]string{"X-Queue-Token": admittedFan(t, ts)}, map[string]any{"seat_ids": []int{1}}); code != http.StatusConflict {
		t.Fatalf("re-holding a sold seat: %d, want 409", code)
	}
}

// Without an admin token, a fan cannot replay webhooks or use demo payment in stripe mode.
func TestDemoPayLockedDownInStripeMode(t *testing.T) {
	ts, s := testAPI(t)
	s.cfg.PaymentMode = "stripe"
	tok := admittedFan(t, ts)
	_, hold := call(t, ts, "POST", "/api/holds", map[string]string{"X-Queue-Token": tok}, map[string]any{"seat_ids": []int{9}})
	if code, _ := call(t, ts, "POST", "/api/demo/pay/"+hold["id"].(string), map[string]string{"X-Queue-Token": tok}, nil); code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", code)
	}
}
