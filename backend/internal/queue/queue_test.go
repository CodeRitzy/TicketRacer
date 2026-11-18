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
		tok, st, err := q.Join(ctx, 1)
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
		st, err := q.Status(ctx, 1, tok)
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

// Two shows have independent lines: admission to one is no admission to the other.
func TestShowsHaveIndependentLines(t *testing.T) {
	q, ctx := testQueue(t), context.Background()
	a, _, _ := q.Join(ctx, 1)
	b, st, _ := q.Join(ctx, 2)
	if st.Position != 1 {
		t.Fatalf("first fan of show 2 is at position %d", st.Position)
	}
	if n, _ := q.AdmitTick(ctx, 5); n != 2 {
		t.Fatalf("admitted %d across two shows, want 2", n)
	}
	if ok, _ := q.Admitted(ctx, 2, a); ok {
		t.Fatal("a show-1 token was admitted to show 2")
	}
	if ok, _ := q.Admitted(ctx, 2, b); !ok {
		t.Fatal("show-2 fan not admitted")
	}
}

// Only one admission batch per show per second, however many instances call AdmitTick.
func TestAdmitTickOncePerSecond(t *testing.T) {
	q, ctx := testQueue(t), context.Background()
	for range 10 {
		q.Join(ctx, 1)
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
	for _, tok := range []string{"nope", "0123456789abcdef0123456789abcdef"} {
		if _, err := q.Status(context.Background(), 1, tok); err != ErrUnknownToken {
			t.Fatalf("%q: got %v, want ErrUnknownToken", tok, err)
		}
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
