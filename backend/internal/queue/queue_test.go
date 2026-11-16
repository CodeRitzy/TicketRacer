package queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testQueue uses TEST_REDIS_URL (a dedicated DB, it is flushed). Skipped when unset.
func testQueue(t *testing.T) *Queue {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() })
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return New(rdb, time.Minute)
}

// Fans are admitted strictly in arrival order, and the rest see their updated position.
func TestAdmitsInArrivalOrder(t *testing.T) {
	q, ctx := testQueue(t), context.Background()
	tokens := make([]string, 5)
	for i := range tokens {
		tok, st, err := q.Join(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.Position != int64(i+1) {
			t.Fatalf("fan %d got position %d", i, st.Position)
		}
		tokens[i] = tok
	}
	if n, err := q.AdmitTick(ctx, 2); err != nil || n != 2 {
		t.Fatalf("admitted %d (err %v), want 2", n, err)
	}
	for i, tok := range tokens {
		st, err := q.Status(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		if wantAdmitted := i < 2; st.Admitted != wantAdmitted {
			t.Fatalf("fan %d admitted=%v", i, st.Admitted)
		}
		if i >= 2 && st.Position != int64(i-1) {
			t.Fatalf("fan %d position %d, want %d", i, st.Position, i-1)
		}
	}
}

// Only one admission batch per second, however many instances call AdmitTick.
func TestAdmitTickOncePerSecond(t *testing.T) {
	q, ctx := testQueue(t), context.Background()
	for range 10 {
		q.Join(ctx)
	}
	// align to the start of a second so both calls share one slot
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	a, _ := q.AdmitTick(ctx, 3)
	b, _ := q.AdmitTick(ctx, 3)
	if a+b != 3 {
		t.Fatalf("admitted %d+%d in one second, want 3 total", a, b)
	}
}

func TestUnknownToken(t *testing.T) {
	q := testQueue(t)
	if _, err := q.Status(context.Background(), "nope"); err != ErrUnknownToken {
		t.Fatalf("got %v, want ErrUnknownToken", err)
	}
}

func TestRateLimit(t *testing.T) {
	q, ctx := testQueue(t), context.Background()
	for i := range 4 {
		ok, err := q.Allow(ctx, "ip", 3)
		if err != nil || ok != (i < 3) {
			t.Fatalf("call %d allowed=%v err=%v", i, ok, err)
		}
	}
}
