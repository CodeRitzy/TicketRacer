package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"ticketracer/internal/catalog"
	"ticketracer/internal/sim"
)

// queryShow reads ?show=N; 0 (or absent) means every show.
func queryShow(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("show"))
	return max(n, 0)
}

// stats feeds the dashboard: database truth, shared counters, queue length and simulator state,
// for one show or for all of them.
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	ctx, show := r.Context(), queryShow(r)
	db, err := s.st.Stats(ctx, show)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	metrics, err := s.q.Metrics(ctx, show)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var waiting int64
	if show > 0 {
		waiting, err = s.q.Waiting(ctx, show)
	} else {
		waiting, err = s.q.WaitingAll(ctx)
	}
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
		"show": show, "db": db, "metrics": metrics, "waiting": waiting, "sim": simState,
		"payment_mode": s.cfg.PaymentMode, "time": time.Now().UTC(),
	})
}

func (s *Server) simBusy(w http.ResponseWriter) bool {
	s.simMu.Lock()
	defer s.simMu.Unlock()
	if s.simRunning {
		writeErr(w, http.StatusConflict, "a simulation is running")
	}
	return s.simRunning
}

// reset clears bookings, the line and counters of one show (?show=N) or of all shows.
func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	if s.simBusy(w) {
		return
	}
	show := queryShow(r)
	if err := s.st.Reset(r.Context(), show); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.q.Reset(r.Context(), show); err != nil {
		s.fail(w, r, err)
		return
	}
	s.live.refresh(r.Context(), s.st)
	w.WriteHeader(http.StatusNoContent)
}

// regenerate replaces a state's lineup with six new shows (?state=XX).
func (s *Server) regenerate(w http.ResponseWriter, r *http.Request) {
	if s.simBusy(w) {
		return
	}
	state, ok := catalog.StateByCode(r.URL.Query().Get("state"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown state")
		return
	}
	if err := s.st.EnsureLineup(r.Context(), state, true); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startSim launches one simulation against one show in the background; only one may run at a time.
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
	if _, err := s.st.Show(r.Context(), cfg.ShowID); err != nil {
		writeErr(w, http.StatusBadRequest, "choose an existing show")
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
		s.log.Info("simulation finished", "show", cfg.ShowID, "users", cfg.Users, "seconds", rep.DurationSec)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}
