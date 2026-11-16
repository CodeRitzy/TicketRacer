// Package pay talks to Stripe (test mode) and provides a demo provider that produces
// identically signed webhooks, so the simulator and CI run without real Stripe calls.
package pay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"ticketracer/internal/store"
)

// demoPrefix marks payment ids created by the demo provider; they are never sent to Stripe.
const demoPrefix = "pi_demo_"

// Event is the part of a Stripe Checkout webhook we act on.
type Event struct {
	ID, Type, HoldID, PaymentRef, SessionRef, Email string
	AmountCents                                     int
	Paid                                            bool
}

// stripeEvent mirrors only the fields we read from the Stripe event JSON.
type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID                string `json:"id"`
			ClientReferenceID string `json:"client_reference_id"`
			PaymentIntent     string `json:"payment_intent"`
			PaymentStatus     string `json:"payment_status"`
			AmountTotal       int    `json:"amount_total"`
			CustomerDetails   *struct {
				Email string `json:"email"`
			} `json:"customer_details"`
		} `json:"object"`
	} `json:"data"`
}

// ParseEvent verifies the Stripe-Signature header against any of the given secrets
// (with Stripe's 5-minute replay tolerance) and only then decodes the payload.
func ParseEvent(payload []byte, sigHeader string, secrets ...string) (Event, error) {
	verified := false
	for _, s := range secrets {
		if s != "" && webhook.ValidatePayload(payload, sigHeader, s) == nil {
			verified = true
			break
		}
	}
	if !verified {
		return Event{}, errors.New("invalid webhook signature")
	}
	var se stripeEvent
	if err := json.Unmarshal(payload, &se); err != nil || se.ID == "" {
		return Event{}, errors.New("malformed webhook payload")
	}
	o := se.Data.Object
	ev := Event{ID: se.ID, Type: se.Type, HoldID: o.ClientReferenceID, PaymentRef: o.PaymentIntent,
		SessionRef: o.ID, AmountCents: o.AmountTotal, Paid: o.PaymentStatus == "paid"}
	if o.CustomerDetails != nil {
		ev.Email = o.CustomerDetails.Email
	}
	return ev, nil
}

// SignHeader builds a Stripe-Signature header using Stripe's own signing scheme.
func SignHeader(payload []byte, secret string) string {
	now := time.Now()
	return fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(webhook.ComputeSignature(now, payload, secret)))
}

// DemoEvent builds a checkout.session.completed payload shaped exactly like Stripe's.
func DemoEvent(eventID, holdID string, amountCents int) []byte {
	b, _ := json.Marshal(map[string]any{
		"id": eventID, "type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"id": "cs_demo_" + holdID, "client_reference_id": holdID, "payment_intent": demoPrefix + holdID,
			"payment_status": "paid", "amount_total": amountCents,
			"customer_details": map[string]string{"email": "demo@ticketracer.test"},
		}},
	})
	return b
}

// NewID returns a random id with a prefix, e.g. evt_demo_ab12...
func NewID(prefix string) string {
	b := make([]byte, 12)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// Provider creates checkout pages and refunds payments.
type Provider interface {
	Checkout(ctx context.Context, h store.Hold, publicURL string) (string, error)
	Refund(ctx context.Context, paymentRef, orderID string) error
}

// Demo sends fans to the built-in demo payment page; refunds are no-ops.
type Demo struct{}

func (Demo) Checkout(_ context.Context, h store.Hold, publicURL string) (string, error) {
	return publicURL + "/pay/demo?hold=" + h.ID, nil
}
func (Demo) Refund(context.Context, string, string) error { return nil }

// Stripe uses Checkout Sessions in test mode.
type Stripe struct{ c *stripe.Client }

func NewStripe(secretKey string) *Stripe { return &Stripe{c: stripe.NewClient(secretKey)} }

func (s *Stripe) Checkout(ctx context.Context, h store.Hold, publicURL string) (string, error) {
	p := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String("payment"),
		ClientReferenceID: stripe.String(h.ID),
		SuccessURL:        stripe.String(publicURL + "/confirmation?hold=" + h.ID),
		CancelURL:         stripe.String(publicURL + "/?cancelled=" + h.ID),
		// No custom expires_at: params must be identical for the idempotency key below to return the
		// same session on a repeat click. A payment landing after our 10-minute hold is refunded.
	}
	for _, seat := range h.Seats {
		p.LineItems = append(p.LineItems, &stripe.CheckoutSessionCreateLineItemParams{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:    stripe.String("usd"),
				UnitAmount:  stripe.Int64(int64(seat.PriceCents)),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{Name: stripe.String(fmt.Sprintf("Row %s · Seat %d", seat.Row, seat.Number))},
			},
		})
	}
	// same hold => same session, even if the fan double-clicks checkout
	p.SetIdempotencyKey("checkout-" + h.ID)
	sess, err := s.c.V1CheckoutSessions.Create(ctx, p)
	if err != nil {
		return "", fmt.Errorf("stripe checkout: %w", err)
	}
	return sess.URL, nil
}

func (s *Stripe) Refund(ctx context.Context, paymentRef, orderID string) error {
	if paymentRef == "" || strings.HasPrefix(paymentRef, demoPrefix) {
		return nil // simulator payments never touched Stripe
	}
	p := &stripe.RefundCreateParams{PaymentIntent: stripe.String(paymentRef)}
	p.SetIdempotencyKey("refund-" + orderID) // retries can never refund twice
	_, err := s.c.V1Refunds.Create(ctx, p)
	return err
}
