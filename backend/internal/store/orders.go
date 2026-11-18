package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Outcome of processing one payment event.
type Outcome string

const (
	OutcomePaid      Outcome = "paid"      // order created, seats sold
	OutcomeDuplicate Outcome = "duplicate" // event or hold already processed, nothing changed
	OutcomeRefund    Outcome = "refund"    // payment cannot be honoured; order saved as refund_pending
	OutcomeUnknown   Outcome = "unknown"   // hold does not exist
	// OutcomeSecondPayment: a different payment for a hold that already has an order (needs a manual refund)
	OutcomeSecondPayment Outcome = "second_payment"
)

// PaymentEvent is a verified "payment succeeded" notification.
type PaymentEvent struct {
	EventID, HoldID, PaymentRef, SessionRef, Email string
	AmountCents                                    int
}

type Order struct {
	ID          string    `json:"id"`
	HoldID      string    `json:"hold_id"`
	Email       string    `json:"email"`
	AmountCents int       `json:"amount_cents"`
	Status      string    `json:"status"`
	PaymentRef  string    `json:"-"`
	SessionRef  string    `json:"session_ref"`
	CreatedAt   time.Time `json:"created_at"`
	Seats       []Seat    `json:"seats"`
}

// CompleteOrder is the idempotent core of payment handling. Everything happens in one
// transaction: record the event id (a repeat stops here), lock the hold, then either sell
// its seats or save the order for refund. A failure rolls back all of it so Stripe retries.
func (s *Store) CompleteOrder(ctx context.Context, ev PaymentEvent) (Outcome, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)

	// 1. redelivered event? the primary key makes the second insert a no-op
	tag, err := tx.Exec(ctx, `INSERT INTO webhook_events (id) VALUES ($1) ON CONFLICT DO NOTHING`, ev.EventID)
	if err != nil {
		return "", "", err
	}
	if tag.RowsAffected() == 0 {
		return OutcomeDuplicate, "", nil
	}

	// 2. lock the hold so expiry and payment cannot interleave
	var status string
	var showID int
	err = tx.QueryRow(ctx, `SELECT status, show_id FROM holds WHERE id=$1 FOR UPDATE`, ev.HoldID).Scan(&status, &showID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeUnknown, "", tx.Commit(ctx) // keep the event id so retries stop
	} else if err != nil {
		return "", "", err
	}

	// the amount we trust is the one in our database, never the one in the request
	var total int
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(price_cents),0) FROM seats WHERE hold_id=$1`, ev.HoldID).Scan(&total); err != nil {
		return "", "", err
	}

	// 3. sell if the hold is still ours and the amount matches; otherwise refund
	orderStatus, outcome := "paid", OutcomePaid
	if status != "active" || total == 0 || total != ev.AmountCents {
		orderStatus, outcome = "refund_pending", OutcomeRefund
	}
	var orderID string
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (show_id, hold_id, payment_ref, session_ref, email, amount_cents, status)
		VALUES ($7, $1, NULLIF($2,''), $3, $4, $5, $6) ON CONFLICT DO NOTHING RETURNING id`,
		ev.HoldID, ev.PaymentRef, ev.SessionRef, ev.Email, ev.AmountCents, orderStatus, showID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		// same hold or payment already has an order. If it was a *different* payment, the fan paid twice:
		// flag it so it is refunded by hand rather than silently kept.
		var existing string
		if err := tx.QueryRow(ctx, `SELECT coalesce(payment_ref,'') FROM orders WHERE hold_id=$1`, ev.HoldID).Scan(&existing); err == nil &&
			ev.PaymentRef != "" && existing != "" && existing != ev.PaymentRef {
			return OutcomeSecondPayment, "", tx.Commit(ctx)
		}
		return OutcomeDuplicate, "", tx.Commit(ctx)
	} else if err != nil {
		return "", "", err
	}
	if outcome == OutcomePaid {
		if _, err = tx.Exec(ctx, `UPDATE seats SET status='sold' WHERE hold_id=$1 AND status='held'`, ev.HoldID); err != nil {
			return "", "", err
		}
		if _, err = tx.Exec(ctx, `UPDATE holds SET status='paid' WHERE id=$1`, ev.HoldID); err != nil {
			return "", "", err
		}
	}
	return outcome, orderID, tx.Commit(ctx)
}

// OrderByHold returns the order and seats for a hold. Seats of a refunded order are not included.
func (s *Store) OrderByHold(ctx context.Context, holdID string) (Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx, `SELECT id, hold_id, email, amount_cents, status, coalesce(payment_ref,''), coalesce(session_ref,''), created_at
		FROM orders WHERE hold_id=$1`, holdID).Scan(&o.ID, &o.HoldID, &o.Email, &o.AmountCents, &o.Status, &o.PaymentRef, &o.SessionRef, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	} else if err != nil {
		return Order{}, err
	}
	if o.Status == "paid" {
		o.Seats, _, err = holdSeats(ctx, s.pool, holdID)
	}
	return o, err
}

// PendingRefunds lists orders whose refund still has to be sent to the payment provider.
func (s *Store) PendingRefunds(ctx context.Context) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, hold_id, coalesce(payment_ref,'') FROM orders WHERE status='refund_pending' LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (o Order, err error) {
		err = r.Scan(&o.ID, &o.HoldID, &o.PaymentRef)
		return
	})
}

func (s *Store) MarkRefunded(ctx context.Context, orderID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orders SET status='refunded' WHERE id=$1`, orderID)
	return err
}

// Stats is what the dashboard shows, including the consistency checks.
type Stats struct {
	Available      int `json:"available"`
	Held           int `json:"held"`
	Sold           int `json:"sold"`
	OrdersPaid     int `json:"orders_paid"`
	OrdersRefunded int `json:"orders_refunded"`
	RevenueCents   int `json:"revenue_cents"`
	WebhookEvents  int `json:"webhook_events"`
	// Invariants: all three must be 0 / true at all times.
	SoldWithoutPaidOrder int  `json:"sold_without_paid_order"` // seat sold but no paid order covers it
	DuplicateOrders      int  `json:"duplicate_orders"`        // holds with more than one order
	Reconciled           bool `json:"reconciled"`              // paid revenue == price of sold seats
}

// Stats reads one show's numbers and invariants; showID 0 means every show.
// webhook_events has no show, so that count is always global.
func (s *Store) Stats(ctx context.Context, showID int) (st Stats, err error) {
	err = s.pool.QueryRow(ctx, `
	SELECT
	  count(*) FILTER (WHERE status='available'), count(*) FILTER (WHERE status='held'), count(*) FILTER (WHERE status='sold'),
	  (SELECT count(*) FROM orders WHERE status='paid' AND ($1 = 0 OR show_id = $1)),
	  (SELECT count(*) FROM orders WHERE status IN ('refunded','refund_pending') AND ($1 = 0 OR show_id = $1)),
	  (SELECT coalesce(sum(amount_cents),0) FROM orders WHERE status='paid' AND ($1 = 0 OR show_id = $1)),
	  (SELECT count(*) FROM webhook_events),
	  (SELECT count(*) FROM seats x WHERE x.status='sold' AND ($1 = 0 OR x.show_id = $1) AND NOT EXISTS
	      (SELECT 1 FROM orders o WHERE o.hold_id=x.hold_id AND o.status='paid')),
	  (SELECT count(*) FROM (SELECT hold_id FROM orders WHERE ($1 = 0 OR show_id = $1) GROUP BY hold_id HAVING count(*) > 1) d),
	  coalesce(sum(price_cents) FILTER (WHERE status='sold'),0) =
	    (SELECT coalesce(sum(amount_cents),0) FROM orders WHERE status='paid' AND ($1 = 0 OR show_id = $1))
	FROM seats WHERE ($1 = 0 OR show_id = $1)`, showID).Scan(&st.Available, &st.Held, &st.Sold, &st.OrdersPaid, &st.OrdersRefunded,
		&st.RevenueCents, &st.WebhookEvents, &st.SoldWithoutPaidOrder, &st.DuplicateOrders, &st.Reconciled)
	return
}

// Reset clears a show's bookings (showID 0: every show, plus the webhook log). The shows stay.
func (s *Store) Reset(ctx context.Context, showID int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := []string{
		`UPDATE seats SET status='available', hold_id=NULL WHERE ($1 = 0 OR show_id = $1) AND status <> 'available'`,
		`DELETE FROM orders WHERE ($1 = 0 OR show_id = $1)`,
		`DELETE FROM holds WHERE ($1 = 0 OR show_id = $1)`,
	}
	if showID == 0 {
		queries = append(queries, `DELETE FROM webhook_events WHERE $1 = 0`)
	}
	for _, q := range queries {
		if _, err = tx.Exec(ctx, q, showID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
