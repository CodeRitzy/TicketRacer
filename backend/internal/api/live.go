package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ticketracer/internal/store"
)

const maxStreams = 10000 // per instance; beyond this, browsers fall back to polling /api/seats

// live keeps one seat snapshot per instance and pushes changes to every SSE subscriber.
// One DB read per second serves any number of viewers.
type live struct {
	mu    sync.RWMutex
	seats []store.Seat
	frame []byte // last SSE payload: {"s":"aahs..."} one letter per seat, in id order
	subs  map[chan []byte]struct{}
}

func newLive() *live { return &live{subs: map[chan []byte]struct{}{}} }

// refresh reloads the seats and broadcasts only when something changed.
func (l *live) refresh(ctx context.Context, st *store.Store) error {
	seats, err := st.Seats(ctx)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, x := range seats {
		b.WriteByte(x.Status[0]) // a = available, h = held, s = sold
	}
	frame := []byte(fmt.Sprintf(`{"s":%q}`, b.String()))

	l.mu.Lock()
	defer l.mu.Unlock()
	l.seats = seats
	if string(frame) == string(l.frame) {
		return nil
	}
	l.frame = frame
	for ch := range l.subs {
		// a slow browser only ever gets the newest frame; we never block on it
		select {
		case <-ch:
		default:
		}
		ch <- frame
	}
	return nil
}

func (l *live) snapshot(ctx context.Context, st *store.Store) ([]store.Seat, error) {
	l.mu.RLock()
	seats := l.seats
	l.mu.RUnlock()
	if seats != nil {
		return seats, nil
	}
	return st.Seats(ctx) // first request before the first refresh tick
}

// anyTaken reports whether any id is unknown or not available in the snapshot.
// Seat ids are 1..n and the snapshot is ordered by id, so lookup is O(1) per seat.
func (l *live) anyTaken(ctx context.Context, st *store.Store, ids []int) (bool, error) {
	seats, err := l.snapshot(ctx, st)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id > len(seats) || (seats[id-1].ID == id && seats[id-1].Status != "available") {
			return true, nil
		}
	}
	return false, nil
}

// seatStream is a Server-Sent Events feed of seat statuses.
func (s *Server) seatStream(w http.ResponseWriter, r *http.Request) {
	l := s.live
	ch := make(chan []byte, 1)
	l.mu.Lock()
	if len(l.subs) >= maxStreams {
		l.mu.Unlock()
		writeErr(w, http.StatusServiceUnavailable, "too many live viewers, use polling")
		return
	}
	l.subs[ch] = struct{}{}
	if l.frame != nil {
		ch <- l.frame
	}
	l.mu.Unlock()
	defer func() { l.mu.Lock(); delete(l.subs, ch); l.mu.Unlock() }()

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // streams outlive the server's normal write timeout
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no") // tell nginx not to buffer
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case f := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", f); err != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		}
		rc.Flush()
	}
}
