package api

import (
	"context"
	"net/http"
	"time"

	"ticketracer/internal/sim"
)

// stats feeds the dashboard: database truth, shared counters, queue length and simulator state.
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.st.Stats(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	metrics, err := s.q.Metrics(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	waiting, err := s.q.Waiting(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	s.simMu.Lock()
	simState := map[string]any{"running": s.simRunning, "report": s.simReport}
	if p := s.simProgress; p != nil {
		simState["progress"] = map[string]int64{
			"users": p.Users.Load(), "admitted": p.Admitted.Load(), "holds": p.Holds.Load(),
			"conflicts": p.Conflicts.Load(), "sold_out": p.SoldOut.Load(), "paid": p.Paid.Load(),
			"abandoned": p.Abandoned.Load(), "duplicates_sent": p.DuplicatesSent.Load(),
			"errors": p.Errors.Load(), "requests": p.Requests.Load(),
		}
	}
	s.simMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"db": db, "metrics": metrics, "waiting": waiting, "sim": simState,
		"payment_mode": s.cfg.PaymentMode, "time": time.Now().UTC(),
	})
}

// reset clears bookings and the queue. Refused while a simulation runs.
func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	s.simMu.Lock()
	running := s.simRunning
	s.simMu.Unlock()
	if running {
		writeErr(w, http.StatusConflict, "a simulation is running")
		return
	}
	if err := s.st.Reset(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.q.Reset(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.live.refresh(r.Context(), s.st)
	w.WriteHeader(http.StatusNoContent)
}

// startSim launches one simulation in the background; only one may run at a time.
func (s *Server) startSim(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DemoWebhookSecret == "" {
		writeErr(w, http.StatusConflict, "simulator needs DEMO_WEBHOOK_SECRET")
		return
	}
	var cfg sim.Config
	if err := decode(r, &cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg.BaseURL, cfg.AdminToken = s.cfg.SelfURL, s.cfg.AdminToken

	s.simMu.Lock()
	if s.simRunning {
		s.simMu.Unlock()
		writeErr(w, http.StatusConflict, "a simulation is already running")
		return
	}
	p := &sim.Progress{}
	s.simRunning, s.simProgress, s.simReport = true, p, nil
	s.simMu.Unlock()

	go func() {
		// hard cap so a stuck run can never hold the simulator forever
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		rep, err := sim.Run(ctx, cfg, p)
		if err != nil {
			s.log.Warn("simulation ended early", "err", err)
		}
		s.simMu.Lock()
		s.simRunning, s.simReport = false, &rep
		s.simMu.Unlock()
		s.log.Info("simulation finished", "users", cfg.Users, "seconds", rep.DurationSec)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
