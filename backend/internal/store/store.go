// Package store owns all PostgreSQL access. Every booking guarantee (no double
// booking, expiry, one order per payment) is enforced here by the database.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrConflict means at least one requested seat was not available; nothing was held.
	ErrConflict = errors.New("seat unavailable")
	// ErrNotFound means the hold or order does not exist.
	ErrNotFound = errors.New("not found")
)

const schema = `
CREATE TABLE IF NOT EXISTS holds (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token      text NOT NULL,
  status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paid','expired','cancelled')),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS holds_active_expiry ON holds (expires_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS holds_token ON holds (token);

CREATE TABLE IF NOT EXISTS seats (
  id          int PRIMARY KEY,
  row_label   text NOT NULL,
  number      int  NOT NULL,
  price_cents int  NOT NULL CHECK (price_cents > 0),
  status      text NOT NULL DEFAULT 'available' CHECK (status IN ('available','held','sold')),
  hold_id     uuid REFERENCES holds(id),
  UNIQUE (row_label, number),
  -- a seat that is not available must belong to a hold
  CHECK ((status = 'available') = (hold_id IS NULL))
);
CREATE INDEX IF NOT EXISTS seats_hold ON seats (hold_id);

CREATE TABLE IF NOT EXISTS orders (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  hold_id     uuid NOT NULL UNIQUE REFERENCES holds(id), -- one order per hold, ever
  payment_ref text UNIQUE,                               -- one order per payment, ever
  session_ref text,
  email       text NOT NULL DEFAULT '',
  amount_cents int NOT NULL,
  status      text NOT NULL CHECK (status IN ('paid','refund_pending','refunded')),
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- every processed webhook event id; the primary key makes redelivery a no-op
CREATE TABLE IF NOT EXISTS webhook_events (
  id          text PRIMARY KEY,
  received_at timestamptz NOT NULL DEFAULT now()
);
`

// One event, one layout: 10 rows (A-J) x 20 seats = 200 seats, three price tiers.
const seed = `
INSERT INTO seats (id, row_label, number, price_cents)
SELECT r*20+n+1, chr(65+r), n+1, CASE WHEN r<3 THEN 12000 WHEN r<6 THEN 8000 ELSE 5000 END
FROM generate_series(0,9) r, generate_series(0,19) n
ON CONFLICT DO NOTHING;`

type Store struct{ pool *pgxpool.Pool }

// Open connects and migrates under an advisory lock so several API instances can start together.
func Open(ctx context.Context, url string) (*Store, error) {
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	// pgx defaults to one connection per CPU, which starves a burst of reservations;
	// a pool_max_conns URL parameter still overrides this
	if !strings.Contains(url, "pool_max_conns") {
		pcfg.MaxConns = 32
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		return nil, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727274)`)
	if _, err = conn.Exec(ctx, schema+seed); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

type Seat struct {
	ID         int    `json:"id"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Status     string `json:"status"`
}

type Hold struct {
	ID         string    `json:"id"`
	Token      string    `json:"-"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Seats      []Seat    `json:"seats"`
	TotalCents int       `json:"total_cents"`
}

// Seats returns the whole layout with current statuses.
func (s *Store) Seats(ctx context.Context) ([]Seat, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,row_label,number,price_cents,status FROM seats ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (x Seat, err error) {
		err = r.Scan(&x.ID, &x.Row, &x.Number, &x.PriceCents, &x.Status)
		return
	})
}

// Reserve holds all seats or none. A fan's previous active hold is replaced in the same transaction.
// SKIP LOCKED means a seat another transaction is claiming right now counts as unavailable
// instead of making us wait, so concurrent requests never block or deadlock each other.
func (s *Store) Reserve(ctx context.Context, token string, seatIDs []int, ttl time.Duration) (Hold, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback(ctx)

	// replace this fan's earlier hold, if any
	if _, err = tx.Exec(ctx, `
		WITH c AS (UPDATE holds SET status='cancelled' WHERE token=$1 AND status='active' RETURNING id)
		UPDATE seats SET status='available', hold_id=NULL WHERE hold_id IN (SELECT id FROM c) AND status='held'`, token); err != nil {
		return Hold{}, err
	}

	var h Hold
	if err = tx.QueryRow(ctx, `INSERT INTO holds (token, expires_at) VALUES ($1, now() + make_interval(secs => $2))
		RETURNING id, token, status, expires_at`, token, ttl.Seconds()).Scan(&h.ID, &h.Token, &h.Status, &h.ExpiresAt); err != nil {
		return Hold{}, err
	}

	// the booking statement: claim only seats that are still available
	tag, err := tx.Exec(ctx, `
		WITH c AS (SELECT id FROM seats WHERE id = ANY($2) AND status='available' ORDER BY id FOR UPDATE SKIP LOCKED)
		UPDATE seats s SET status='held', hold_id=$1 FROM c WHERE s.id = c.id`, h.ID, seatIDs)
	if err != nil {
		return Hold{}, err
	}
	if int(tag.RowsAffected()) != len(seatIDs) {
		return Hold{}, ErrConflict // deferred Rollback undoes the partial claim
	}
	if h.Seats, h.TotalCents, err = holdSeats(ctx, tx, h.ID); err != nil {
		return Hold{}, err
	}
	return h, tx.Commit(ctx)
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func holdSeats(ctx context.Context, q querier, holdID string) (seats []Seat, total int, err error) {
	rows, err := q.Query(ctx, `SELECT id,row_label,number,price_cents,status FROM seats WHERE hold_id=$1 ORDER BY id`, holdID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	seats = []Seat{}
	for rows.Next() {
		var x Seat
		if err = rows.Scan(&x.ID, &x.Row, &x.Number, &x.PriceCents, &x.Status); err != nil {
			return nil, 0, err
		}
		total += x.PriceCents
		seats = append(seats, x)
	}
	return seats, total, rows.Err()
}

// Hold loads a hold with its seats. Seats are empty once a hold has expired or been cancelled.
func (s *Store) Hold(ctx context.Context, id string) (Hold, error) {
	var h Hold
	err := s.pool.QueryRow(ctx, `SELECT id, token, status, expires_at FROM holds WHERE id=$1`, id).
		Scan(&h.ID, &h.Token, &h.Status, &h.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Hold{}, ErrNotFound
	} else if err != nil {
		return Hold{}, err
	}
	h.Seats, h.TotalCents, err = holdSeats(ctx, s.pool, id)
	return h, err
}

// Cancel releases an active hold immediately.
func (s *Store) Cancel(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		WITH c AS (UPDATE holds SET status='cancelled' WHERE id=$1 AND status='active' RETURNING id)
		UPDATE seats SET status='available', hold_id=NULL WHERE hold_id IN (SELECT id FROM c) AND status='held'`, id)
	return err
}

// SweepExpired frees seats of holds past their deadline and returns how many seats were freed.
func (s *Store) SweepExpired(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH e AS (UPDATE holds SET status='expired' WHERE status='active' AND expires_at < now() RETURNING id)
		UPDATE seats SET status='available', hold_id=NULL WHERE hold_id IN (SELECT id FROM e) AND status='held'`)
	return int(tag.RowsAffected()), err
}
