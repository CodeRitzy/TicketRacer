// Package store owns all PostgreSQL access. Every booking guarantee (no double
// booking, expiry, one order per payment) is enforced here by the database, per show.
package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ticketracer/internal/catalog"
)

var (
	// ErrConflict means at least one requested seat was not available; nothing was held.
	ErrConflict = errors.New("seat unavailable")
	// ErrNotFound means the show, hold or order does not exist.
	ErrNotFound = errors.New("not found")
)

const schemaVersion = 2

// v1 had a single event; its tables lack show_id. They held demo data only, so v2 replaces them.
const dropV1 = `DROP TABLE IF EXISTS orders, seats, holds CASCADE;`

const schema = `
CREATE TABLE IF NOT EXISTS shows (
  id         serial PRIMARY KEY,
  state      text NOT NULL,
  venue_key  text NOT NULL,
  act        text NOT NULL,
  starts_at  timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS shows_state ON shows (state, starts_at);

CREATE TABLE IF NOT EXISTS holds (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  show_id    int  NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
  token      text NOT NULL,
  status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paid','expired','cancelled')),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS holds_active_expiry ON holds (expires_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS holds_token ON holds (show_id, token);

-- seat ids run 1..n inside each show, so a show's live frame is one letter per seat in id order
CREATE TABLE IF NOT EXISTS seats (
  show_id     int  NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
  id          int  NOT NULL,
  section     text NOT NULL,
  row_label   text NOT NULL,
  number      int  NOT NULL,
  price_cents int  NOT NULL CHECK (price_cents > 0),
  status      text NOT NULL DEFAULT 'available' CHECK (status IN ('available','held','sold')),
  hold_id     uuid REFERENCES holds(id),
  PRIMARY KEY (show_id, id),
  -- a seat that is not available must belong to a hold
  CHECK ((status = 'available') = (hold_id IS NULL))
);
CREATE INDEX IF NOT EXISTS seats_hold ON seats (hold_id);

CREATE TABLE IF NOT EXISTS orders (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  show_id      int  NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
  hold_id      uuid NOT NULL UNIQUE REFERENCES holds(id) ON DELETE CASCADE, -- one order per hold, ever
  payment_ref  text UNIQUE,                                                 -- one order per payment, ever
  session_ref  text,
  email        text NOT NULL DEFAULT '',
  amount_cents int  NOT NULL,
  status       text NOT NULL CHECK (status IN ('paid','refund_pending','refunded')),
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- every processed webhook event id; the primary key makes redelivery a no-op
CREATE TABLE IF NOT EXISTS webhook_events (
  id          text PRIMARY KEY,
  received_at timestamptz NOT NULL DEFAULT now()
);
`

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

	// one-time upgrade: v1 had no show_id, so its demo tables are replaced
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_meta (version int NOT NULL)`); err != nil {
		return nil, err
	}
	var version int
	if err = conn.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM schema_meta`).Scan(&version); err != nil {
		return nil, err
	}
	if version < schemaVersion {
		if _, err = conn.Exec(ctx, dropV1); err != nil {
			return nil, fmt.Errorf("upgrade: %w", err)
		}
	}
	if _, err = conn.Exec(ctx, schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if version < schemaVersion {
		if _, err = conn.Exec(ctx, `DELETE FROM schema_meta; INSERT INTO schema_meta VALUES (2)`); err != nil {
			return nil, err
		}
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

type Seat struct {
	ID         int    `json:"id"`
	Section    string `json:"section"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Status     string `json:"status"`
}

type Hold struct {
	ID         string    `json:"id"`
	ShowID     int       `json:"show_id"`
	Token      string    `json:"-"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	Seats      []Seat    `json:"seats"`
	TotalCents int       `json:"total_cents"`
}

// Show is one concert. Venue facts come from the catalog by VenueKey.
type Show struct {
	ID       int       `json:"id"`
	State    string    `json:"state"`
	VenueKey string    `json:"-"`
	Act      string    `json:"act"`
	StartsAt time.Time `json:"starts_at"`
}

// ShowSummary adds live availability for listings.
type ShowSummary struct {
	Show
	Available     int `json:"available"`
	Held          int `json:"held"`
	Sold          int `json:"sold"`
	MinPriceCents int `json:"min_price_cents"`
}

// ShowsPerState is the lineup size for every state.
const ShowsPerState = 6

// CreateShow inserts one show and all its seats (bulk COPY) inside tx.
func createShow(ctx context.Context, tx pgx.Tx, state string, p catalog.Plan) (Show, error) {
	sh := Show{State: state, VenueKey: p.Venue.Key, Act: p.Act, StartsAt: p.StartsAt}
	if err := tx.QueryRow(ctx, `INSERT INTO shows (state, venue_key, act, starts_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		state, sh.VenueKey, sh.Act, sh.StartsAt).Scan(&sh.ID); err != nil {
		return Show{}, err
	}
	specs, _ := catalog.Layout(p.Venue)
	rows := make([][]any, len(specs))
	for i, x := range specs {
		rows[i] = []any{sh.ID, x.ID, x.Section, x.Row, x.Number, x.PriceCents}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"seats"},
		[]string{"show_id", "id", "section", "row_label", "number", "price_cents"}, pgx.CopyFromRows(rows))
	return sh, err
}

// CreateShow creates a single show (used by tests and tools).
func (s *Store) CreateShow(ctx context.Context, state string, p catalog.Plan) (Show, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Show{}, err
	}
	defer tx.Rollback(ctx)
	sh, err := createShow(ctx, tx, state, p)
	if err != nil {
		return Show{}, err
	}
	return sh, tx.Commit(ctx)
}

// EnsureLineup creates a state's shows on first request. The transaction-scoped advisory lock
// means concurrent first visitors create exactly one lineup.
func (s *Store) EnsureLineup(ctx context.Context, state catalog.State, regenerate bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('lineup:' || $1))`, state.Code); err != nil {
		return err
	}
	if regenerate {
		if _, err = tx.Exec(ctx, `DELETE FROM shows WHERE state = $1`, state.Code); err != nil {
			return err
		}
	} else {
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM shows WHERE state = $1`, state.Code).Scan(&n); err != nil || n > 0 {
			return err
		}
	}
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for _, p := range catalog.Lineup(r, state, ShowsPerState, time.Now()) {
		if _, err = createShow(ctx, tx, state.Code, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Lineup lists a state's shows by date with live availability and the lowest price.
func (s *Store) Lineup(ctx context.Context, state string) ([]ShowSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sh.id, sh.state, sh.venue_key, sh.act, sh.starts_at,
		  count(*) FILTER (WHERE se.status='available'), count(*) FILTER (WHERE se.status='held'),
		  count(*) FILTER (WHERE se.status='sold'), coalesce(min(se.price_cents),0)
		FROM shows sh JOIN seats se ON se.show_id = sh.id
		WHERE sh.state = $1 GROUP BY sh.id ORDER BY sh.starts_at`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (x ShowSummary, err error) {
		err = r.Scan(&x.ID, &x.State, &x.VenueKey, &x.Act, &x.StartsAt, &x.Available, &x.Held, &x.Sold, &x.MinPriceCents)
		return
	})
}

// Show loads one show.
func (s *Store) Show(ctx context.Context, id int) (Show, error) {
	var sh Show
	err := s.pool.QueryRow(ctx, `SELECT id, state, venue_key, act, starts_at FROM shows WHERE id=$1`, id).
		Scan(&sh.ID, &sh.State, &sh.VenueKey, &sh.Act, &sh.StartsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Show{}, ErrNotFound
	}
	return sh, err
}

// Seats returns a show's whole layout with current statuses, in id order.
func (s *Store) Seats(ctx context.Context, showID int) ([]Seat, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,section,row_label,number,price_cents,status FROM seats WHERE show_id=$1 ORDER BY id`, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (x Seat, err error) {
		err = r.Scan(&x.ID, &x.Section, &x.Row, &x.Number, &x.PriceCents, &x.Status)
		return
	})
}

// Reserve holds all seats or none, for one show. A fan's previous active hold for the same
// show is replaced in the same transaction. SKIP LOCKED means a seat another transaction is
// claiming right now counts as unavailable instead of making us wait, so concurrent requests
// never block or deadlock each other. show_id in the WHERE clause means a hold can never
// contain another show's seats.
func (s *Store) Reserve(ctx context.Context, showID int, token string, seatIDs []int, ttl time.Duration) (Hold, error) {
	return s.reserve(ctx, showID, token, ttl, len(seatIDs), `
		WITH c AS (SELECT id FROM seats WHERE show_id=$3 AND id = ANY($2) AND status='available' ORDER BY id FOR UPDATE SKIP LOCKED)
		UPDATE seats s SET status='held', hold_id=$1 FROM c WHERE s.show_id=$3 AND s.id = c.id`, seatIDs)
}

// ReserveGA holds qty general-admission tickets in a section, all or none. It claims any free
// tickets of that section with the same SKIP LOCKED pattern, so GA can never oversell either.
func (s *Store) ReserveGA(ctx context.Context, showID int, token, section string, qty int, ttl time.Duration) (Hold, error) {
	return s.reserve(ctx, showID, token, ttl, qty, `
		WITH c AS (SELECT id FROM seats WHERE show_id=$3 AND section=$2 AND status='available' ORDER BY id LIMIT $4 FOR UPDATE SKIP LOCKED)
		UPDATE seats s SET status='held', hold_id=$1 FROM c WHERE s.show_id=$3 AND s.id = c.id`, section, qty)
}

// reserve is the shared transaction: replace the fan's earlier hold, create the hold, run the claim
// ($1 hold id, $2.. claim args, $3 show id), and commit only if exactly want seats were claimed.
func (s *Store) reserve(ctx context.Context, showID int, token string, ttl time.Duration, want int, claim string, arg any, extra ...any) (Hold, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback(ctx)

	// replace this fan's earlier hold for this show, if any
	if _, err = tx.Exec(ctx, `
		WITH c AS (UPDATE holds SET status='cancelled' WHERE show_id=$1 AND token=$2 AND status='active' RETURNING id)
		UPDATE seats SET status='available', hold_id=NULL WHERE hold_id IN (SELECT id FROM c) AND status='held'`, showID, token); err != nil {
		return Hold{}, err
	}

	h := Hold{ShowID: showID}
	if err = tx.QueryRow(ctx, `INSERT INTO holds (show_id, token, expires_at) VALUES ($1, $2, now() + make_interval(secs => $3))
		RETURNING id, token, status, expires_at`, showID, token, ttl.Seconds()).Scan(&h.ID, &h.Token, &h.Status, &h.ExpiresAt); err != nil {
		return Hold{}, err
	}

	// the booking statement: claim only seats of this show that are still available
	tag, err := tx.Exec(ctx, claim, append([]any{h.ID, arg, showID}, extra...)...)
	if err != nil {
		return Hold{}, err
	}
	if int(tag.RowsAffected()) != want {
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
	rows, err := q.Query(ctx, `SELECT id,section,row_label,number,price_cents,status FROM seats WHERE hold_id=$1 ORDER BY id`, holdID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	seats = []Seat{}
	for rows.Next() {
		var x Seat
		if err = rows.Scan(&x.ID, &x.Section, &x.Row, &x.Number, &x.PriceCents, &x.Status); err != nil {
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
	err := s.pool.QueryRow(ctx, `SELECT id, show_id, token, status, expires_at FROM holds WHERE id=$1`, id).
		Scan(&h.ID, &h.ShowID, &h.Token, &h.Status, &h.ExpiresAt)
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

// SweepExpired frees seats of holds past their deadline (every show) and returns the number of
// seats freed per show.
func (s *Store) SweepExpired(ctx context.Context) (map[int]int, error) {
	rows, err := s.pool.Query(ctx, `
		WITH e AS (UPDATE holds SET status='expired' WHERE status='active' AND expires_at < now() RETURNING id),
		     f AS (UPDATE seats SET status='available', hold_id=NULL WHERE hold_id IN (SELECT id FROM e) AND status='held' RETURNING show_id)
		SELECT show_id, count(*) FROM f GROUP BY show_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	freed := map[int]int{}
	for rows.Next() {
		var show, n int
		if err := rows.Scan(&show, &n); err != nil {
			return nil, err
		}
		freed[show] = n
	}
	return freed, rows.Err()
}
