package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"ticketracer/internal/pay"
	"ticketracer/internal/store"
)

// stripeWebhook verifies the signature, then hands the event to the idempotent order transaction.
// 2xx tells Stripe "done, stop retrying"; 5xx makes Stripe retry later.
func (s *Server) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	// demo-signed events are accepted only while DEMO_WEBHOOK_SECRET is set; unset it in production
	ev, err := pay.ParseEvent(payload, r.Header.Get("Stripe-Signature"), s.cfg.StripeWebhookSecret, s.cfg.DemoWebhookSecret)
	if err != nil {
		s.q.Incr(ctx, 0, "webhook_rejected", 1)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !uuidRe.MatchString(ev.HoldID) {
		writeJSON(w, http.StatusOK, map[string]string{"result": "ignored"}) // not one of our sessions
		return
	}

	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		if !ev.Paid {
			writeJSON(w, http.StatusOK, map[string]string{"result": "awaiting_payment"})
			return
		}
		outcome, orderID, err := s.st.CompleteOrder(ctx, store.PaymentEvent{EventID: ev.ID, HoldID: ev.HoldID,
			PaymentRef: ev.PaymentRef, SessionRef: ev.SessionRef, Email: ev.Email, AmountCents: ev.AmountCents})
		if err != nil {
			s.fail(w, r, err) // rolled back; Stripe will retry
			return
		}
		show := 0 // count per show when the hold exists
		if h, err := s.st.Hold(ctx, ev.HoldID); err == nil {
			show = h.ShowID
		}
		s.q.Incr(ctx, show, "webhook_"+string(outcome), 1)
		if outcome == store.OutcomeSecondPayment {
			s.log.Error("second payment for an already-paid hold: refund manually", "hold", ev.HoldID, "payment", ev.PaymentRef)
		}
		s.log.Info("payment processed", "event", ev.ID, "hold", ev.HoldID, "outcome", outcome, "order", orderID)
		writeJSON(w, http.StatusOK, map[string]string{"result": string(outcome), "order_id": orderID})
	case "checkout.session.expired":
		// the fan left Stripe's page: free the seats now instead of waiting for the hold to lapse
		if err := s.st.Cancel(ctx, ev.HoldID); err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": "released"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"result": "ignored"})
	}
}

// demoPay simulates a successful Checkout: it builds a Stripe-shaped event, signs it with the
// demo secret, and POSTs it to our own webhook endpoint, exercising the exact production path.
// Public in demo mode; admin-only in stripe mode (the simulator uses it).
func (s *Server) demoPay(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DemoWebhookSecret == "" || (s.cfg.PaymentMode != "demo" && !s.isAdmin(r)) {
		writeErr(w, http.StatusNotFound, "demo payments are disabled")
		return
	}
	h, ok := s.ownedHold(w, r)
	if !ok {
		return
	}
	var body struct {
		Deliveries int `json:"deliveries"`
	}
	if r.ContentLength > 0 {
		if err := decode(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// deliveries > 1 replays the same event concurrently, like Stripe retrying (admin only)
	n := 1
	if s.isAdmin(r) && body.Deliveries > 1 {
		n = min(body.Deliveries, 10)
	}
	payload := pay.DemoEvent(pay.NewID("evt_demo_"), h.ID, h.TotalCents)
	results := s.deliver(r.Context(), payload, n)
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": results})
}

// deliver posts one signed payload n times in parallel and returns each response status.
func (s *Server) deliver(ctx context.Context, payload []byte, n int) []string {
	results := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "POST", s.cfg.SelfURL+"/api/webhooks/stripe", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Stripe-Signature", pay.SignHeader(payload, s.cfg.DemoWebhookSecret))
			res, err := s.self.Do(req)
			if err != nil {
				results[i] = "error"
				return
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			results[i] = fmt.Sprintf("%d %s", res.StatusCode, bytes.TrimSpace(body))
		}()
	}
	wg.Wait()
	return results
}
