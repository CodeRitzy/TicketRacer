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

const (
	maxStreams = 10000            // per instance; beyond this, browsers fall back to polling
	idleAfter  = 30 * time.Second // stop refreshing a show nobody has looked at for this long
)

// showLive is one show's cached snapshot and its SSE subscribers.
type showLive struct {
	seats   []store.Seat
	frame   []byte // last SSE payload: {"s":"aahs..."} one letter per seat, in id order
	subs    map[chan []byte]struct{}
	touched time.Time
}

// live keeps per-show seat snapshots and pushes changes to every subscriber.
// One DB read per active show per second serves any number of viewers.
type live struct {
	mu      sync.Mutex
	shows   map[int]*showLive
	streams int
}

func newLive() *live { return &live{shows: map[int]*showLive{}} }

func (l *live) get(show int) *showLive {
	sl := l.shows[show]
	if sl == nil {
		sl = &showLive{subs: map[chan []byte]struct{}{}}
		l.shows[show] = sl
	}
	sl.touched = time.Now()
	return sl
}

// refresh reloads every show that has viewers or recent traffic, broadcasting only changes.
func (l *live) refresh(ctx context.Context, st *store.Store) error {
	l.mu.Lock()
	var ids []int
	for id, sl := range l.shows {
		if len(sl.subs) > 0 || time.Since(sl.touched) < idleAfter {
			ids = append(ids, id)
		} else {
			delete(l.shows, id) // idle: forget it, the next request reloads it
		}
	}
	l.mu.Unlock()
	for _, id := range ids {
		seats, err := st.Seats(ctx, id)
		if err != nil {
			return err
		}
		l.publish(id, seats)
	}
	return nil
}

func (l *live) publish(show int, seats []store.Seat) {
	var b strings.Builder
	for _, x := range seats {
		b.WriteByte(x.Status[0]) // a = available, h = held, s = sold
	}
	frame := []byte(fmt.Sprintf(`{"s":%q}`, b.String()))
	l.mu.Lock()
	defer l.mu.Unlock()
	sl := l.shows[show]
	if sl == nil {
		return
	}
	sl.seats = seats
	if string(frame) == string(sl.frame) {
		return
	}
	sl.frame = frame
	for ch := range sl.subs {
		// a slow browser only ever gets the newest frame; we never block on it
		select {
		case <-ch:
		default:
		}
		ch <- frame
	}
}

// snapshot returns a show's seats from the cache, loading them on first use.
func (l *live) snapshot(ctx context.Context, st *store.Store, show int) ([]store.Seat, error) {
	l.mu.Lock()
	seats := l.get(show).seats
	l.mu.Unlock()
	if seats != nil {
		return seats, nil
	}
	seats, err := st.Seats(ctx, show)
	if err != nil || len(seats) == 0 {
		return seats, err
	}
	l.publish(show, seats)
	return seats, nil
}

// anyTaken reports whether any id is unknown or not available in the show's snapshot.
// Seat ids are 1..n and the snapshot is ordered by id, so lookup is O(1) per seat.
func (l *live) anyTaken(ctx context.Context, st *store.Store, show int, ids []int) (bool, error) {
	seats, err := l.snapshot(ctx, st, show)
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

// seatStream is a Server-Sent Events feed of one show's seat statuses.
func (s *Server) seatStream(w http.ResponseWriter, r *http.Request) {
	l, show := s.live, showID(r)
	if seats, err := l.snapshot(r.Context(), s.st, show); err != nil || len(seats) == 0 {
		writeErr(w, http.StatusNotFound, "show not found")
		return
	}
	ch := make(chan []byte, 1)
	l.mu.Lock()
	if l.streams >= maxStreams {
		l.mu.Unlock()
		writeErr(w, http.StatusServiceUnavailable, "too many live viewers, use polling")
		return
	}
	l.streams++
	sl := l.get(show)
	sl.subs[ch] = struct{}{}
	if sl.frame != nil {
		ch <- sl.frame
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.get(show).subs, ch)
		l.streams--
		l.mu.Unlock()
	}()

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
