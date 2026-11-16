// Package queue is the Redis waiting room plus small shared counters and rate limits.
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

const (
	waitingKey = "wr:waiting" // sorted set: token -> arrival sequence
	seqKey     = "wr:seq"
	admittedNS = "wr:admitted:" // + token, expires after AdmittedTTL
	metricsKey = "metrics"
)

// admitScript pops the next N fans and admits them atomically, so a crash can never lose a fan
// between "removed from queue" and "admitted".
// shortcut: builds keys inside Lua, fine for single-node Redis; use hash tags before moving to Redis Cluster.
var admitScript = redis.NewScript(`
local popped = redis.call('ZPOPMIN', KEYS[1], ARGV[1])
for i = 1, #popped, 2 do redis.call('SET', ARGV[3] .. popped[i], '1', 'EX', ARGV[2]) end
redis.call('HINCRBY', KEYS[2], 'admitted', #popped / 2)
return #popped / 2`)

type Queue struct {
	rdb         *redis.Client
	admittedTTL time.Duration
}

func New(rdb *redis.Client, admittedTTL time.Duration) *Queue {
	return &Queue{rdb: rdb, admittedTTL: admittedTTL}
}

// Status of one fan in the waiting room.
type Status struct {
	Admitted bool  `json:"admitted"`
	Position int64 `json:"position"` // 1-based place in line, 0 when admitted
	Waiting  int64 `json:"waiting"`  // total people in line
}

var ErrUnknownToken = errors.New("unknown or expired queue token")

// Join puts a new fan at the back of the line and returns their secret token.
func (q *Queue) Join(ctx context.Context) (string, Status, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", Status{}, err
	}
	token := hex.EncodeToString(b)
	// INCR gives a strict arrival order even with many API instances
	seq, err := q.rdb.Incr(ctx, seqKey).Result()
	if err != nil {
		return "", Status{}, err
	}
	if err = q.rdb.ZAdd(ctx, waitingKey, redis.Z{Score: float64(seq), Member: token}).Err(); err != nil {
		return "", Status{}, err
	}
	st, err := q.Status(ctx, token)
	return token, st, err
}

// JoinAdmitted issues a token that skips the line (simulator "stampede" mode; admin only).
func (q *Queue) JoinAdmitted(ctx context.Context) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	return token, q.rdb.Set(ctx, admittedNS+token, 1, q.admittedTTL).Err()
}

// Ping checks the Redis connection.
func (q *Queue) Ping(ctx context.Context) error { return q.rdb.Ping(ctx).Err() }

// validToken: tokens are 32 hex chars; anything else is rejected before touching Redis.
func validToken(t string) bool {
	if len(t) != 32 {
		return false
	}
	_, err := hex.DecodeString(t)
	return err == nil
}

// Status reports whether a token is admitted, or where it is in line.
func (q *Queue) Status(ctx context.Context, token string) (Status, error) {
	if !validToken(token) {
		return Status{}, ErrUnknownToken
	}
	if ok, err := q.Admitted(ctx, token); err != nil || ok {
		return Status{Admitted: ok}, err
	}
	pipe := q.rdb.Pipeline()
	rank := pipe.ZRank(ctx, waitingKey, token)
	total := pipe.ZCard(ctx, waitingKey)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return Status{}, err
	}
	if errors.Is(rank.Err(), redis.Nil) {
		return Status{}, ErrUnknownToken
	}
	return Status{Position: rank.Val() + 1, Waiting: total.Val()}, nil
}

func (q *Queue) Admitted(ctx context.Context, token string) (bool, error) {
	if !validToken(token) {
		return false, nil
	}
	n, err := q.rdb.Exists(ctx, admittedNS+token).Result()
	return n == 1, err
}

// AdmitTick admits up to n fans. Only the first instance to claim this second's slot admits,
// so the admission rate stays n/second no matter how many API instances run.
func (q *Queue) AdmitTick(ctx context.Context, n int) (int, error) {
	slot := "wr:tick:" + strconv.FormatInt(time.Now().Unix(), 10)
	won, err := q.rdb.SetNX(ctx, slot, 1, 5*time.Second).Result()
	if err != nil || !won {
		return 0, err
	}
	admitted, err := admitScript.Run(ctx, q.rdb, []string{waitingKey, metricsKey},
		n, int(q.admittedTTL.Seconds()), admittedNS).Int()
	return admitted, err
}

// Waiting is the current queue length.
func (q *Queue) Waiting(ctx context.Context) (int64, error) {
	return q.rdb.ZCard(ctx, waitingKey).Result()
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

// Incr bumps a dashboard counter shared by all instances.
func (q *Queue) Incr(ctx context.Context, name string, by int64) {
	q.rdb.HIncrBy(ctx, metricsKey, name, by) // best effort: a lost metric never affects bookings
}

// Metrics returns all counters.
func (q *Queue) Metrics(ctx context.Context) (map[string]int64, error) {
	raw, err := q.rdb.HGetAll(ctx, metricsKey).Result()
	out := make(map[string]int64, len(raw))
	for k, v := range raw {
		out[k], _ = strconv.ParseInt(v, 10, 64)
	}
	return out, err
}

// Reset clears the waiting room, admissions and counters.
func (q *Queue) Reset(ctx context.Context) error {
	keys := []string{waitingKey, seqKey, metricsKey}
	iter := q.rdb.Scan(ctx, 0, admittedNS+"*", 500).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	return q.rdb.Del(ctx, keys...).Err()
}
