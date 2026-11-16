package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"time"

	"ticketracer/internal/queue"
	"ticketracer/internal/store"
)

// The single V1 event.
const (
	eventName  = "Midnight Static — Live at The Foundry"
	eventVenue = "The Foundry Hall"
	eventDate  = "2027-03-12T20:00:00-05:00"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "postgres unavailable")
		return
	}
	if err := s.q.Ping(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "redis unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"event_name": eventName, "venue": eventVenue, "starts_at": eventDate,
		"hold_ttl_seconds": int(s.cfg.HoldTTL.Seconds()), "max_seats": s.cfg.MaxSeatsPerHold,
		"payment_mode": s.cfg.PaymentMode,
	})
}

// joinQueue puts a fan in line. Rate limited per IP so one client cannot flood the line.
func (s *Server) joinQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	admin := s.isAdmin(r)
	if !admin {
		ok, err := s.q.Allow(ctx, "join:"+clientIP(r), s.cfg.JoinRatePerMin)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			writeErr(w, http.StatusTooManyRequests, "too many attempts, try again in a minute")
			return
		}
	}
	// the simulator's stampede mode skips the line to hammer reservations directly
	if admin && r.URL.Query().Get("skip") == "1" {
		token, err := s.q.JoinAdmitted(ctx)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.q.Incr(ctx, "joined", 1)
		s.q.Incr(ctx, "admitted", 1)
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "status": queue.Status{Admitted: true}})
		return
	}
	token, st, err := s.q.Join(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.q.Incr(ctx, "joined", 1) // drives the dashboard pipeline
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "status": st})
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.q.Status(r.Context(), r.Header.Get("X-Queue-Token"))
	if errors.Is(err, queue.ErrUnknownToken) {
		writeErr(w, http.StatusNotFound, "unknown or expired queue token")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// queueLength is public: how many fans are in line right now (one ZCARD, no token needed).
func (s *Server) queueLength(w http.ResponseWriter, r *http.Request) {
	n, err := s.q.Waiting(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"waiting": n})
}

// seats serves the cached snapshot (at most 1s old) so polling never reaches Postgres.
func (s *Server) seats(w http.ResponseWriter, r *http.Request) {
	seats, err := s.live.snapshot(r.Context(), s.st)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, seats)
}

// createHold validates the request, checks the fan was admitted, then runs the
// all-or-nothing reservation transaction.
func (s *Server) createHold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.Header.Get("X-Queue-Token")
	if ok, err := s.q.Admitted(ctx, token); err != nil {
		s.fail(w, r, err)
		return
	} else if token == "" || !ok {
		writeErr(w, http.StatusForbidden, "not admitted from the waiting room yet")
		return
	}
	// one client cannot churn holds to hoard the house (the simulator, as admin, is exempt)
	// shortcut: a per-IP rate limit, not a cap on seats held per IP; add a client_ip column on holds if hoarding persists
	if !s.isAdmin(r) {
		if ok, err := s.q.Allow(ctx, "hold:"+clientIP(r), s.cfg.JoinRatePerMin/2); err != nil {
			s.fail(w, r, err)
			return
		} else if !ok {
			writeErr(w, http.StatusTooManyRequests, "too many hold attempts, try again in a minute")
			return
		}
	}
	var body struct {
		SeatIDs []int `json:"seat_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if n := len(body.SeatIDs); n == 0 || n > s.cfg.MaxSeatsPerHold {
		writeErr(w, http.StatusBadRequest, "choose between 1 and "+strconv.Itoa(s.cfg.MaxSeatsPerHold)+" seats")
		return
	}
	seen := map[int]bool{}
	for _, id := range body.SeatIDs {
		if id <= 0 || seen[id] {
			writeErr(w, http.StatusBadRequest, "seat ids must be unique positive numbers")
			return
		}
		seen[id] = true
	}
	// cheap rejection: a seat already taken in the ≤1s-old snapshot cannot be won, so skip the DB.
	// Correctness never depends on this; the transaction below is the real check.
	if taken, err := s.live.anyTaken(ctx, s.st, body.SeatIDs); err != nil {
		s.fail(w, r, err)
		return
	} else if taken {
		s.q.Incr(ctx, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "some of those seats were just taken")
		return
	}
	ttl := s.cfg.HoldTTL
	// the simulator may shorten holds so expiry shows up within a short run (admin only, never longer)
	if v, err := strconv.Atoi(r.Header.Get("X-Hold-TTL")); err == nil && s.isAdmin(r) && v > 0 && time.Duration(v)*time.Second < ttl {
		ttl = time.Duration(v) * time.Second
	}

	hold, err := s.st.Reserve(ctx, token, body.SeatIDs, ttl)
	if errors.Is(err, store.ErrConflict) {
		s.q.Incr(ctx, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "some of those seats were just taken")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	s.q.Incr(ctx, "reserve_ok", 1)
	writeJSON(w, http.StatusCreated, hold)
}

// ownedHold loads a hold only for the fan who created it; anyone else gets 404, not 403,
// so hold ids cannot be probed.
func (s *Server) ownedHold(w http.ResponseWriter, r *http.Request) (store.Hold, bool) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "hold not found")
		return store.Hold{}, false
	}
	h, err := s.st.Hold(r.Context(), id)
	token := r.Header.Get("X-Queue-Token")
	if errors.Is(err, store.ErrNotFound) || (err == nil && (token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.Token)) != 1)) {
		writeErr(w, http.StatusNotFound, "hold not found")
		return store.Hold{}, false
	} else if err != nil {
		s.fail(w, r, err)
		return store.Hold{}, false
	}
	return h, true
}

func (s *Server) getHold(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.ownedHold(w, r); ok {
		writeJSON(w, http.StatusOK, h)
	}
}

func (s *Server) cancelHold(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownedHold(w, r)
	if !ok {
		return
	}
	if err := s.st.Cancel(r.Context(), h.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkout returns the payment page URL for an active hold.
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownedHold(w, r)
	if !ok {
		return
	}
	if h.Status != "active" || time.Now().After(h.ExpiresAt) {
		writeErr(w, http.StatusGone, "this hold has expired, please pick seats again")
		return
	}
	// every call reaches Stripe; a looping client must not burn the account's API rate limit
	if ok, err := s.q.Allow(r.Context(), "checkout:"+h.ID, 10); err != nil {
		s.fail(w, r, err)
		return
	} else if !ok {
		writeErr(w, http.StatusTooManyRequests, "too many checkout attempts, try again in a minute")
		return
	}
	url, err := s.pay.Checkout(r.Context(), h, s.cfg.PublicURL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// getOrder is polled by the confirmation page until the webhook has been processed.
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownedHold(w, r)
	if !ok {
		return
	}
	o, err := s.st.OrderByHold(r.Context(), h.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing", "hold_status": h.Status})
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}
