// Package api exposes the booking HTTP API, the live seat stream and the admin dashboard API.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"ticketracer/internal/config"
	"ticketracer/internal/pay"
	"ticketracer/internal/queue"
	"ticketracer/internal/sim"
	"ticketracer/internal/store"
)

// Server wires the stores, payment provider and background loops together.
type Server struct {
	cfg  config.Config
	st   *store.Store
	q    *queue.Queue
	pay  pay.Provider
	log  *slog.Logger
	live *live
	self *http.Client // used to deliver demo webhooks through the real webhook endpoint

	simMu       sync.Mutex
	simRunning  bool
	simProgress *sim.Progress
	simReport   *sim.Report
}

func New(cfg config.Config, st *store.Store, q *queue.Queue, p pay.Provider, log *slog.Logger) *Server {
	return &Server{cfg: cfg, st: st, q: q, pay: p, log: log, live: newLive(),
		self: &http.Client{Timeout: 10 * time.Second}}
}

// Routes registers every endpoint. Method-specific patterns reject wrong verbs with 405.
func (s *Server) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", s.health)
	m.HandleFunc("GET /api/config", s.config)
	m.HandleFunc("POST /api/queue/join", s.joinQueue)
	m.HandleFunc("GET /api/queue/status", s.queueStatus)
	m.HandleFunc("GET /api/queue/length", s.queueLength)
	m.HandleFunc("GET /api/seats", s.seats)
	m.HandleFunc("GET /api/seats/stream", s.seatStream)
	m.HandleFunc("POST /api/holds", s.createHold)
	m.HandleFunc("GET /api/holds/{id}", s.getHold)
	m.HandleFunc("DELETE /api/holds/{id}", s.cancelHold)
	m.HandleFunc("POST /api/holds/{id}/checkout", s.checkout)
	m.HandleFunc("GET /api/holds/{id}/order", s.getOrder)
	m.HandleFunc("POST /api/demo/pay/{id}", s.demoPay)
	m.HandleFunc("POST /api/webhooks/stripe", s.stripeWebhook)
	m.HandleFunc("GET /api/admin/stats", s.admin(s.stats))
	m.HandleFunc("POST /api/admin/reset", s.admin(s.reset))
	m.HandleFunc("POST /api/admin/simulate", s.admin(s.startSim))
	return s.middleware(m)
}

// middleware adds panic recovery, request-size limits, security headers and access logs.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "path", r.URL.Path, "err", v)
				writeErr(w, http.StatusInternalServerError, "internal error")
			}
		}()
		limit := int64(16 << 10) // fan requests are tiny
		if r.URL.Path == "/api/webhooks/stripe" {
			limit = 1 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		if p := r.URL.Path; p != "/api/queue/status" && p != "/api/queue/length" && p != "/api/seats" { // skip high-volume polling paths
			s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

// Unwrap lets http.ResponseController reach Flush/SetWriteDeadline for SSE.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// isAdmin checks the bearer token in constant time. No configured token means nobody is admin.
func (s *Server) isAdmin(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.cfg.AdminToken != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.AdminToken)) == 1
}

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdmin(r) {
			writeErr(w, http.StatusUnauthorized, "admin token required")
			return
		}
		h(w, r)
	}
}

// clientIP trusts X-Real-IP because the API is only reachable through our nginx, which sets it.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeErr never includes internal error details; those go to the log only.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

// RunBackground starts the loops every instance runs; all are safe with many instances.
func (s *Server) RunBackground(ctx context.Context) {
	every := func(d time.Duration, name string, f func(context.Context) error) {
		go func() {
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := f(ctx); err != nil && ctx.Err() == nil {
						s.log.Warn("background task failed", "task", name, "err", err)
					}
				}
			}
		}()
	}
	// admit the next fans from the waiting room
	every(time.Second, "admit", func(ctx context.Context) error {
		_, err := s.q.AdmitTick(ctx, s.cfg.AdmitPerSecond)
		return err
	})
	// release expired holds
	every(2*time.Second, "sweep", func(ctx context.Context) error {
		n, err := s.st.SweepExpired(ctx)
		if n > 0 {
			s.q.Incr(ctx, "seats_released_expired", int64(n))
		}
		return err
	})
	// send refunds for payments we could not honour; Stripe idempotency keys make retries safe
	every(5*time.Second, "refunds", s.processRefunds)
	// one seat snapshot per second for every connected browser
	every(time.Second, "live", func(ctx context.Context) error { return s.live.refresh(ctx, s.st) })
}

func (s *Server) processRefunds(ctx context.Context) error {
	orders, err := s.st.PendingRefunds(ctx)
	for _, o := range orders {
		if err := s.pay.Refund(ctx, o.PaymentRef, o.ID); err != nil {
			s.log.Error("refund failed, will retry", "order", o.ID, "err", err)
			continue
		}
		if err := s.st.MarkRefunded(ctx, o.ID); err != nil {
			return err
		}
		s.q.Incr(ctx, "refunds_issued", 1)
		s.log.Info("refund issued", "order", o.ID)
	}
	return err
}
