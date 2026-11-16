// Seat selection (/seats) and the active hold (/hold/:id) share one layout:
// the live map on the left, the fan's ticket panel on the right.
import { useEffect, useState } from 'react'
import { api, ApiError, session, type EventConfig, type Hold } from '../api'
import { Flap } from '../components/Flap'
import { SeatMap } from '../components/SeatMap'
import { navigate, useLiveSeats, useNow } from '../lib/hooks'
import { formatCountdown, formatPrice, toggleSeat, type Seat } from '../lib/seats'

export function Booking({ config, holdId }: { config: EventConfig | null; holdId?: string }) {
  const { seats, live, error: liveError } = useLiveSeats()
  const [selected, setSelected] = useState<number[]>([])
  const [hold, setHold] = useState<Hold | null>(null)
  const [message, setMessage] = useState<{ tone: 'error' | 'ok'; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const max = config?.max_seats ?? 6

  // without a queue token there is nothing to book with
  useEffect(() => { if (!session.token()) navigate('/', true) }, [])

  // load the hold on /hold/:id; a paid hold goes straight to its confirmation
  useEffect(() => {
    if (!holdId) { setHold(null); return }
    api.getHold(holdId).then((h) => {
      if (h.status === 'paid') navigate(`/confirmation?hold=${h.id}`, true)
      setHold(h)
    }, () => navigate('/seats', true))
  }, [holdId])

  // a seat someone else just took drops out of the selection, with a word about it
  useEffect(() => {
    const lost = selected.filter((id) => seats[id - 1] && seats[id - 1].status !== 'available')
    if (lost.length) {
      setSelected((sel) => sel.filter((id) => !lost.includes(id)))
      setMessage({ tone: 'error', text: `Seat ${lost.map((id) => label(seats[id - 1])).join(', ')} was just taken by another fan.` })
    }
  }, [seats, selected])

  const onToggle = (seat: Seat) => {
    const r = toggleSeat(selected, seat, max)
    setSelected(r.selected)
    setMessage(r.error ? { tone: 'error', text: r.error } : null)
  }

  const reserve = async () => {
    setBusy(true)
    setMessage(null)
    try {
      const h = await api.hold(selected)
      setSelected([])
      navigate(`/hold/${h.id}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) return navigate('/queue', true)
      setMessage({ tone: 'error', text: e instanceof ApiError && e.status === 409
        ? 'Someone got to one of those seats first. Nothing was held, so pick again.'
        : 'Could not hold seats. Try again.' })
    } finally {
      setBusy(false)
    }
  }

  const chosen = hold ? hold.seats : selected.map((id) => seats[id - 1]).filter(Boolean)
  const total = chosen.reduce((sum, s) => sum + s.price_cents, 0)

  return (
    <main id="main">
      <div className="section__head" style={{ padding: '40px var(--gutter) 0', marginBottom: 0 }}>
        <h1 className="section__title">{hold ? 'Your seats are held' : 'Pick your seats'}</h1>
        {!live && <span className="lamp">Reconnecting</span>}
      </div>
      <div className="booking" style={{ marginTop: 28 }}>
        <section className="booking__map" aria-label="Seat map">
          {liveError && <p className="notice" data-tone="error"><strong>Offline</strong> {liveError}</p>}
          <SeatMap seats={seats} selected={hold ? hold.seats.map((s) => s.id) : selected} locked={!!hold} onToggle={onToggle} />
        </section>

        <aside className="booking__side" aria-label="Your tickets">
          {hold ? <HoldPanel hold={hold} total={total} onExpired={() => setHold({ ...hold, status: 'expired' })} />
            : (
              <>
                <h2 className="line__label">Selected · up to {max}</h2>
                {chosen.length ? <TicketList seats={chosen} /> : <p className="dim">Choose seats on the map. Other fans’ holds appear in amber as they happen.</p>}
                <div className="total"><span className="dim">Total</span><b>{formatPrice(total)}</b></div>
                <button className="btn" disabled={!chosen.length || busy} onClick={reserve}>
                  {busy ? 'Holding…' : chosen.length ? `Hold ${chosen.length} seat${chosen.length > 1 ? 's' : ''}` : 'Hold seats'}
                </button>
                <p className="dim" style={{ fontSize: '0.92rem' }}>Held seats are yours for {Math.round((config?.hold_ttl_seconds ?? 600) / 60)} minutes while you pay.</p>
              </>
            )}
          {message && <p className="notice" data-tone={message.tone} role="status"><strong>{message.tone === 'error' ? 'Heads up' : 'Done'}</strong> {message.text}</p>}
        </aside>
      </div>
    </main>
  )
}

function HoldPanel({ hold, total, onExpired }: { hold: Hold; total: number; onExpired: () => void }) {
  const now = useNow(250)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const left = new Date(hold.expires_at).getTime() - now
  const expired = hold.status !== 'active' || left <= 0

  useEffect(() => { if (left <= 0 && hold.status === 'active') onExpired() }, [left, hold.status, onExpired])

  // checkout returns Stripe's hosted page (or the demo page); the browser leaves for it
  const pay = async () => {
    setBusy(true)
    setError('')
    try {
      const { url } = await api.checkout(hold.id)
      window.location.assign(url)
    } catch (e) {
      setError(e instanceof ApiError && e.status === 410 ? 'This hold has expired.' : 'Checkout is unavailable right now. Try again.')
      setBusy(false)
    }
  }
  const release = async () => {
    await api.cancelHold(hold.id).catch(() => undefined)
    navigate('/seats')
  }

  if (expired) {
    return (
      <>
        <h2 className="line__label">Hold expired</h2>
        <p>Your seats went back on the board for other fans.</p>
        <button className="btn" onClick={() => navigate('/seats')}>Pick again</button>
      </>
    )
  }
  return (
    <>
      <h2 className="line__label">Time left to pay</h2>
      <div className="countdown" data-low={left < 60_000} aria-live="off">
        <Flap value={formatCountdown(left)} width={5} stagger={0} label={`${Math.ceil(left / 1000)} seconds left`} />
      </div>
      <TicketList seats={hold.seats} />
      <div className="total"><span className="dim">Total</span><b>{formatPrice(total)}</b></div>
      <button className="btn" onClick={pay} disabled={busy}>{busy ? 'Opening checkout…' : `Pay ${formatPrice(total)}`}</button>
      <button className="btn btn--ghost btn--small" onClick={release}>Release these seats</button>
      {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
    </>
  )
}

const label = (s: Seat) => `${s.row}${s.number}`

function TicketList({ seats }: { seats: Seat[] }) {
  return (
    <ul className="ticket-list">
      {seats.map((s) => (
        <li key={s.id}><b>Row {s.row} · Seat {s.number}</b><span>{formatPrice(s.price_cents)}</span></li>
      ))}
    </ul>
  )
}
