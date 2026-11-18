package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"ticketracer/internal/catalog"
	"ticketracer/internal/queue"
	"ticketracer/internal/store"
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

// config is the booking rules that apply to every show.
func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hold_ttl_seconds": int(s.cfg.HoldTTL.Seconds()), "max_seats": s.cfg.MaxSeatsPerHold, "payment_mode": s.cfg.PaymentMode,
	})
}

// states lists every state with its time zone, for the picker and the browser's first guess.
func (s *Server) states(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, catalog.States)
}

// venueView is the public part of a catalog venue.
type venueView struct {
	Name     string       `json:"name"`
	City     string       `json:"city"`
	State    string       `json:"state"`
	Capacity int          `json:"capacity"`
	Kind     catalog.Kind `json:"kind"`
}

func viewOf(v catalog.Venue) venueView {
	return venueView{Name: v.Name, City: v.City, State: v.State, Capacity: v.Capacity, Kind: v.Kind}
}

// lineup returns a state's shows, creating the lineup on the first request for that state.
func (s *Server) lineup(w http.ResponseWriter, r *http.Request) {
	state, ok := catalog.StateByCode(r.URL.Query().Get("state"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown state")
		return
	}
	if err := s.st.EnsureLineup(r.Context(), state, false); err != nil {
		s.fail(w, r, err)
		return
	}
	shows, err := s.st.Lineup(r.Context(), state.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type item struct {
		store.ShowSummary
		Venue venueView `json:"venue"`
	}
	out := make([]item, 0, len(shows))
	for _, sh := range shows {
		v, _ := catalog.VenueByKey(sh.VenueKey)
		out = append(out, item{sh, viewOf(v)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "shows": out})
}

// showDetail is everything the event page and seat picker need besides the seats themselves.
func (s *Server) showDetail(w http.ResponseWriter, r *http.Request) {
	id := showID(r)
	sh, err := s.st.Show(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || id <= 0 {
		writeErr(w, http.StatusNotFound, "show not found")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	v, _ := catalog.VenueByKey(sh.VenueKey)
	_, sections := catalog.Layout(v)
	state, _ := catalog.StateByCode(sh.State)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": sh.ID, "act": sh.Act, "starts_at": sh.StartsAt, "zone": state.Zone, "venue": viewOf(v), "sections": sections,
		"hold_ttl_seconds": int(s.cfg.HoldTTL.Seconds()), "max_seats": s.cfg.MaxSeatsPerHold, "payment_mode": s.cfg.PaymentMode,
	})
}

// joinQueue puts a fan in a show's line. Rate limited per IP so one client cannot flood the line.
func (s *Server) joinQueue(w http.ResponseWriter, r *http.Request) {
	ctx, show := r.Context(), showID(r)
	if _, err := s.st.Show(ctx, show); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "show not found")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
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
		token, err := s.q.JoinAdmitted(ctx, show)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.q.Incr(ctx, show, "joined", 1)
		s.q.Incr(ctx, show, "admitted", 1)
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "status": queue.Status{Admitted: true}})
		return
	}
	token, st, err := s.q.Join(ctx, show)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.q.Incr(ctx, show, "joined", 1) // drives the dashboard pipeline
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "status": st})
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.q.Status(r.Context(), showID(r), r.Header.Get("X-Queue-Token"))
	if errors.Is(err, queue.ErrUnknownToken) {
		writeErr(w, http.StatusNotFound, "unknown or expired queue token")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// queueLength is public: how many fans are in a show's line right now (one ZCARD, no token needed).
func (s *Server) queueLength(w http.ResponseWriter, r *http.Request) {
	n, err := s.q.Waiting(r.Context(), showID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"waiting": n})
}

// seats serves the show's cached snapshot (at most 1s old) so polling rarely reaches Postgres.
func (s *Server) seats(w http.ResponseWriter, r *http.Request) {
	seats, err := s.live.snapshot(r.Context(), s.st, showID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(seats) == 0 {
		writeErr(w, http.StatusNotFound, "show not found")
		return
	}
	writeJSON(w, http.StatusOK, seats)
}

// createHold validates the request, checks the fan was admitted to this show, then runs the
// all-or-nothing reservation transaction.
func (s *Server) createHold(w http.ResponseWriter, r *http.Request) {
	ctx, show := r.Context(), showID(r)
	token := r.Header.Get("X-Queue-Token")
	if ok, err := s.q.Admitted(ctx, show, token); err != nil {
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
	// either specific seats, or a quantity of general-admission tickets in one section
	var body struct {
		SeatIDs  []int  `json:"seat_ids"`
		Section  string `json:"section"`
		Quantity int    `json:"quantity"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Section != "" {
		s.createGAHold(w, r, show, token, body.Section, body.Quantity)
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
	if taken, err := s.live.anyTaken(ctx, s.st, show, body.SeatIDs); err != nil {
		s.fail(w, r, err)
		return
	} else if taken {
		s.q.Incr(ctx, show, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "some of those seats were just taken")
		return
	}
	hold, err := s.st.Reserve(ctx, show, token, body.SeatIDs, s.holdTTL(r))
	if errors.Is(err, store.ErrConflict) {
		s.q.Incr(ctx, show, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "some of those seats were just taken")
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	s.q.Incr(ctx, show, "reserve_ok", 1)
	writeJSON(w, http.StatusCreated, hold)
}

// holdTTL is the configured hold, which the simulator (admin only) may shorten, never lengthen.
func (s *Server) holdTTL(r *http.Request) time.Duration {
	ttl := s.cfg.HoldTTL
	if v, err := strconv.Atoi(r.Header.Get("X-Hold-TTL")); err == nil && s.isAdmin(r) && v > 0 && time.Duration(v)*time.Second < ttl {
		ttl = time.Duration(v) * time.Second
	}
	return ttl
}

// createGAHold holds qty admissions to a general-admission section (pit, floor, lawn).
func (s *Server) createGAHold(w http.ResponseWriter, r *http.Request, show int, token, section string, qty int) {
	ctx := r.Context()
	if qty < 1 || qty > s.cfg.MaxSeatsPerHold {
		writeErr(w, http.StatusBadRequest, "choose between 1 and "+strconv.Itoa(s.cfg.MaxSeatsPerHold)+" tickets")
		return
	}
	if !catalog.IsGA(section) {
		writeErr(w, http.StatusBadRequest, "that section has reserved seats; choose seats instead")
		return
	}
	// cheap rejection from the ≤1s-old snapshot: not enough left in the section
	seats, err := s.live.snapshot(ctx, s.st, show)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	free, exists := 0, false
	for _, x := range seats {
		if x.Section == section {
			exists = true
			if x.Status == "available" {
				free++
			}
		}
	}
	if !exists {
		writeErr(w, http.StatusBadRequest, "no such section")
		return
	}
	if free < qty {
		s.q.Incr(ctx, show, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "not enough tickets left in "+section)
		return
	}
	hold, err := s.st.ReserveGA(ctx, show, token, section, qty, s.holdTTL(r))
	if errors.Is(err, store.ErrConflict) {
		s.q.Incr(ctx, show, "reserve_conflicts", 1)
		writeErr(w, http.StatusConflict, "not enough tickets left in "+section)
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	s.q.Incr(ctx, show, "reserve_ok", 1)
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
	title := "Ticketracer demo show"
	if sh, err := s.st.Show(r.Context(), h.ShowID); err == nil {
		v, _ := catalog.VenueByKey(sh.VenueKey)
		title = sh.Act + " · " + v.Name
	}
	url, err := s.pay.Checkout(r.Context(), h, title, s.cfg.PublicURL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

var sessionRe = regexp.MustCompile(`^cs_(test|live)_[A-Za-z0-9]{10,200}$`)

// reconcile confirms a payment from the Checkout Session the fan returned with (?session=cs_...),
// for when the webhook is late or never arrives. It runs the same idempotent order transaction,
// so a webhook landing later is a harmless duplicate. Only the hold's owner reaches this, the
// session must belong to this hold, and lookups are rate limited per hold.
func (s *Server) reconcile(r *http.Request, h store.Hold) bool {
	ctx, session := r.Context(), r.URL.Query().Get("session")
	if s.cfg.PaymentMode != "stripe" || !sessionRe.MatchString(session) {
		return false
	}
	if ok, err := s.q.Allow(ctx, "sync:"+h.ID, 12); err != nil || !ok {
		return false
	}
	ev, err := s.pay.Lookup(ctx, session)
	if err != nil {
		s.log.Warn("checkout session lookup failed", "hold", h.ID, "err", err)
		return false
	}
	if ev.HoldID != h.ID || !ev.Paid {
		return false // not this hold's session, or not paid yet
	}
	outcome, orderID, err := s.st.CompleteOrder(ctx, store.PaymentEvent{EventID: ev.ID, HoldID: ev.HoldID,
		PaymentRef: ev.PaymentRef, SessionRef: ev.SessionRef, Email: ev.Email, AmountCents: ev.AmountCents})
	if err != nil {
		s.log.Error("reconcile failed", "hold", h.ID, "err", err)
		return false
	}
	s.q.Incr(ctx, h.ShowID, "webhook_"+string(outcome), 1)
	s.log.Info("payment confirmed from checkout session", "hold", h.ID, "outcome", outcome, "order", orderID)
	return true
}

// getOrder is polled by the confirmation page until the webhook has been processed.
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownedHold(w, r)
	if !ok {
		return
	}
	o, err := s.st.OrderByHold(r.Context(), h.ID)
	// no order yet but the fan came back from checkout: confirm with the provider directly
	if errors.Is(err, store.ErrNotFound) && s.reconcile(r, h) {
		o, err = s.st.OrderByHold(r.Context(), h.ID)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "processing", "hold_status": h.Status, "show_id": h.ShowID})
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": o, "show_id": h.ShowID})
}
