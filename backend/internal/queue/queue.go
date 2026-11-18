// Package queue is the per-show Redis waiting room plus shared counters and rate limits.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Keys are namespaced by show so every show has its own line and admissions.
const activeKey = "wr:active" // set of show ids with fans waiting

func waitingKey(show int) string       { return fmt.Sprintf("wr:%d:waiting", show) }
func seqKey(show int) string           { return fmt.Sprintf("wr:%d:seq", show) }
func admittedNS(show int) string       { return fmt.Sprintf("wr:%d:admitted:", show) }
func metricsKey(show int) string       { return fmt.Sprintf("metrics:%d", show) }
func tickKey(show int, s int64) string { return fmt.Sprintf("wr:%d:tick:%d", show, s) }

const globalMetrics = "metrics"

// admitScript pops the next N fans of one show and admits them atomically, so a crash can never
// lose a fan between "removed from queue" and "admitted". It also counts them per show and globally.
// shortcut: builds keys inside Lua, fine for single-node Redis; use hash tags before moving to Redis Cluster.
var admitScript = redis.NewScript(`
local popped = redis.call('ZPOPMIN', KEYS[1], ARGV[1])
for i = 1, #popped, 2 do redis.call('SET', ARGV[3] .. popped[i], '1', 'EX', ARGV[2]) end
local n = #popped / 2
redis.call('HINCRBY', KEYS[2], 'admitted', n)
redis.call('HINCRBY', KEYS[3], 'admitted', n)
return n`)

type Queue struct {
	rdb         *redis.Client
	admittedTTL time.Duration
}

func New(rdb *redis.Client, admittedTTL time.Duration) *Queue {
	return &Queue{rdb: rdb, admittedTTL: admittedTTL}
}

// Status of one fan in a show's waiting room.
type Status struct {
	Admitted bool  `json:"admitted"`
	Position int64 `json:"position"` // 1-based place in line, 0 when admitted
	Waiting  int64 `json:"waiting"`  // total people in line
}

var ErrUnknownToken = errors.New("unknown or expired queue token")

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validToken: tokens are 32 hex chars; anything else is rejected before touching Redis.
func validToken(t string) bool {
	if len(t) != 32 {
		return false
	}
	_, err := hex.DecodeString(t)
	return err == nil
}

// Join puts a new fan at the back of a show's line and returns their secret token.
func (q *Queue) Join(ctx context.Context, show int) (string, Status, error) {
	token, err := newToken()
	if err != nil {
		return "", Status{}, err
	}
	// INCR gives a strict arrival order even with many API instances
	seq, err := q.rdb.Incr(ctx, seqKey(show)).Result()
	if err != nil {
		return "", Status{}, err
	}
	pipe := q.rdb.TxPipeline()
	pipe.ZAdd(ctx, waitingKey(show), redis.Z{Score: float64(seq), Member: token})
	pipe.SAdd(ctx, activeKey, show)
	if _, err = pipe.Exec(ctx); err != nil {
		return "", Status{}, err
	}
	st, err := q.Status(ctx, show, token)
	return token, st, err
}

// JoinAdmitted issues a token that skips the line (simulator "stampede" mode; admin only).
func (q *Queue) JoinAdmitted(ctx context.Context, show int) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	return token, q.rdb.Set(ctx, admittedNS(show)+token, 1, q.admittedTTL).Err()
}

// Ping checks the Redis connection.
func (q *Queue) Ping(ctx context.Context) error { return q.rdb.Ping(ctx).Err() }

// Status reports whether a token is admitted to a show, or where it is in line.
func (q *Queue) Status(ctx context.Context, show int, token string) (Status, error) {
	if !validToken(token) {
		return Status{}, ErrUnknownToken
	}
	if ok, err := q.Admitted(ctx, show, token); err != nil || ok {
		return Status{Admitted: ok}, err
	}
	pipe := q.rdb.Pipeline()
	rank := pipe.ZRank(ctx, waitingKey(show), token)
	total := pipe.ZCard(ctx, waitingKey(show))
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return Status{}, err
	}
	if errors.Is(rank.Err(), redis.Nil) {
		return Status{}, ErrUnknownToken
	}
	return Status{Position: rank.Val() + 1, Waiting: total.Val()}, nil
}

func (q *Queue) Admitted(ctx context.Context, show int, token string) (bool, error) {
	if !validToken(token) {
		return false, nil
	}
	n, err := q.rdb.Exists(ctx, admittedNS(show)+token).Result()
	return n == 1, err
}

// AdmitTick admits up to n fans for every show that has a line. Per show, only the first
// instance to claim this second's slot admits, so the rate stays n/second per show no matter
// how many API instances run. Returns the total admitted.
func (q *Queue) AdmitTick(ctx context.Context, n int) (int, error) {
	shows, err := q.rdb.SMembers(ctx, activeKey).Result()
	if err != nil {
		return 0, err
	}
	sec := time.Now().Unix()
	total := 0
	for _, s := range shows {
		show, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		won, err := q.rdb.SetNX(ctx, tickKey(show, sec), 1, 5*time.Second).Result()
		if err != nil || !won {
			continue
		}
		admitted, err := admitScript.Run(ctx, q.rdb, []string{waitingKey(show), metricsKey(show), globalMetrics},
			n, int(q.admittedTTL.Seconds()), admittedNS(show)).Int()
		if err != nil {
			return total, err
		}
		total += admitted
		// an empty line leaves the active set; the next Join puts it back
		if left, _ := q.rdb.ZCard(ctx, waitingKey(show)).Result(); left == 0 {
			q.rdb.SRem(ctx, activeKey, show)
		}
	}
	return total, nil
}

// Waiting is the current queue length of one show.
func (q *Queue) Waiting(ctx context.Context, show int) (int64, error) {
	return q.rdb.ZCard(ctx, waitingKey(show)).Result()
}

// WaitingAll sums the lines of every active show.
func (q *Queue) WaitingAll(ctx context.Context) (int64, error) {
	shows, err := q.rdb.SMembers(ctx, activeKey).Result()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, s := range shows {
		show, _ := strconv.Atoi(s)
		n, err := q.Waiting(ctx, show)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// Allow is a fixed-window rate limiter: at most limit calls per key per minute.
func (q *Queue) Allow(ctx context.Context, key string, limit int) (bool, error) {
	k := fmt.Sprintf("rl:%s:%d", key, time.Now().Unix()/60)
	pipe := q.rdb.TxPipeline()
	n := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, 2*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return n.Val() <= int64(limit), nil
}

// Incr bumps a dashboard counter for a show and for the global total.
func (q *Queue) Incr(ctx context.Context, show int, name string, by int64) {
	pipe := q.rdb.Pipeline() // best effort: a lost metric never affects bookings
	pipe.HIncrBy(ctx, globalMetrics, name, by)
	if show > 0 {
		pipe.HIncrBy(ctx, metricsKey(show), name, by)
	}
	pipe.Exec(ctx)
}

// Metrics returns a show's counters (show 0: global totals).
func (q *Queue) Metrics(ctx context.Context, show int) (map[string]int64, error) {
	key := globalMetrics
	if show > 0 {
		key = metricsKey(show)
	}
	raw, err := q.rdb.HGetAll(ctx, key).Result()
	out := make(map[string]int64, len(raw))
	for k, v := range raw {
		out[k], _ = strconv.ParseInt(v, 10, 64)
	}
	return out, err
}

// Reset clears one show's line, admissions and counters (show 0: everything).
func (q *Queue) Reset(ctx context.Context, show int) error {
	pattern, keys := fmt.Sprintf("wr:%d:*", show), []string{metricsKey(show)}
	if show == 0 {
		pattern, keys = "wr:*", []string{globalMetrics}
	}
	for _, p := range []string{pattern, "metrics:*"} {
		if show > 0 && p == "metrics:*" {
			continue
		}
		iter := q.rdb.Scan(ctx, 0, p, 500).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		if err := iter.Err(); err != nil {
			return err
		}
	}
	if show > 0 {
		q.rdb.SRem(ctx, activeKey, show)
	}
	return q.rdb.Del(ctx, keys...).Err()
}
