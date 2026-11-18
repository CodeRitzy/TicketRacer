// The seat picker (/shows/:id/seats) and the active hold (/hold/:id) share one layout, following
// the ticket-site flow: venue overview -> section -> seats, with quantity, Best available, a price
// list and a cart rail, all drawn in the split-flap board style.
import { useEffect, useMemo, useState } from 'react'
import { api, ApiError, session, showWhen, type Hold, type ShowDetail } from '../api'
import { Arrow, PlusMinus } from '../components/Arrow'
import { Ticker } from '../components/Ticker'
import { SeatMap } from '../components/SeatMap'
import { TicketList } from '../components/TicketList'
import { VenueMap } from '../components/VenueMap'
import { navigate, useLiveSeats, useNow } from '../lib/hooks'
import { bestAvailable, sectionSummaries } from '../lib/picker'
import { formatCountdown, formatPrice, toggleSeat, type Seat } from '../lib/seats'

export function Picker({ showId: routeShow, holdId }: { showId?: number; holdId?: string }) {
  const [hold, setHold] = useState<Hold | null>(null)
  const showId = routeShow ?? hold?.show_id ?? 0
  const [show, setShow] = useState<ShowDetail | null>(null)
  const { seats, live, error: liveError } = useLiveSeats(showId)
  const [open, setOpen] = useState<string | null>(null)
  const [selected, setSelected] = useState<number[]>([])
  // a general-admission pick is a quantity in a section, not seat ids
  const [gaPick, setGaPick] = useState<{ section: string; qty: number } | null>(null)
  const [qty, setQty] = useState(2)
  const [byPrice, setByPrice] = useState(false)
  const [message, setMessage] = useState<{ tone: 'error' | 'ok'; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const max = show?.max_seats ?? 6

  // without this show's queue token there is nothing to book with
  useEffect(() => {
    if (routeShow && !session.tokenFor(routeShow)) navigate(`/shows/${routeShow}`, true)
  }, [routeShow])

  // on /hold/:id load the hold (a paid one goes straight to its confirmation) and open its section
  useEffect(() => {
    if (!holdId) { setHold(null); return }
    api.getHold(holdId).then((h) => {
      if (h.status === 'paid') navigate(`/confirmation?hold=${h.id}`, true)
      setHold(h)
      if (h.seats[0]) setOpen(h.seats[0].section)
    }, () => navigate('/', true))
  }, [holdId])

  useEffect(() => { if (showId) api.show(showId).then(setShow, () => navigate('/', true)) }, [showId])

  // a seat someone else just took drops out of the selection, with a word about it
  useEffect(() => {
    const lost = selected.filter((id) => seats[id - 1] && seats[id - 1].status !== 'available')
    if (lost.length) {
      setSelected((sel) => sel.filter((id) => !lost.includes(id)))
      setMessage({ tone: 'error', text: `${lost.map((id) => label(seats[id - 1])).join(', ')} was just taken by another fan.` })
    }
  }, [seats, selected])

  const summaries = useMemo(() => sectionSummaries(seats), [seats])
  const listed = byPrice ? [...summaries].sort((a, b) => a.priceCents - b.priceCents) : summaries
  const openSeats = useMemo(() => seats.filter((s) => s.section === open), [seats, open])
  const openInfo = summaries.find((s) => s.name === open)
  const gaSections = useMemo(() => new Set((show?.sections ?? []).filter((s) => s.ga).map((s) => s.name)), [show])
  const openIsGA = !!open && gaSections.has(open)

  const onToggle = (seat: Seat) => {
    const r = toggleSeat(selected, seat, max)
    setGaPick(null)
    setSelected(r.selected)
    setMessage(r.error ? { tone: 'error', text: r.error } : null)
  }

  // Best available: contiguous seats, in the open section or across sections in list order
  const best = () => {
    const order = open ? [open] : listed.filter((s) => s.left > 0).map((s) => s.name)
    for (const name of order) {
      if (gaSections.has(name)) {
        if ((summaries.find((s) => s.name === name)?.left ?? 0) >= qty) return pickGA(name)
        continue
      }
      const run = bestAvailable(seats, qty, [name])
      if (run) {
        setGaPick(null)
        setSelected(run.map((s) => s.id))
        setOpen(run[0].section)
        setMessage({ tone: 'ok', text: `Found ${qty} together: ${run[0].section}, row ${run[0].row}.` })
        return
      }
    }
    setMessage({ tone: 'error', text: `No ${qty} seats together ${open ? `in ${open}` : 'anywhere'} right now. Try fewer tickets${open ? ' or another section' : ''}.` })
  }

  // choosing GA puts a quantity of admissions in the cart; it replaces any seat selection
  const pickGA = (section: string) => {
    setSelected([])
    setGaPick({ section, qty })
    setOpen(section)
    setMessage({ tone: 'ok', text: `${qty} × ${section} general admission in your cart.` })
  }

  const reserve = async () => {
    setBusy(true)
    setMessage(null)
    try {
      const h = gaPick ? await api.holdGA(showId, gaPick.section, gaPick.qty) : await api.hold(showId, selected)
      setSelected([])
      setGaPick(null)
      navigate(`/hold/${h.id}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) return navigate(`/shows/${showId}/queue`, true)
      setMessage({ tone: 'error', text: e instanceof ApiError && e.status === 409
        ? 'Someone got to one of those seats first. Nothing was held, so pick again.'
        : e instanceof ApiError && e.status === 429 ? e.message : 'Could not hold seats. Try again.' })
    } finally {
      setBusy(false)
    }
  }

  const chosen = hold ? hold.seats
    : gaPick ? seats.filter((s) => s.section === gaPick.section && s.status === 'available').slice(0, gaPick.qty)
      : selected.map((id) => seats[id - 1]).filter(Boolean)
  const total = chosen.reduce((sum, s) => sum + s.price_cents, 0)
  const when = show ? showWhen(show.starts_at, show.zone) : null

  return (
    <main id="main">
      <div className="picker-head">
        <a className="back-link" href={`/shows/${showId}`} onClick={(e) => { e.preventDefault(); navigate(`/shows/${showId}`) }}><Arrow dir="left" /> Show details</a>
        <h1 className="section__title">{show?.act ?? (hold ? 'Your seats are held' : 'Pick your seats')}</h1>
        {show && when && <p className="dim">{hold ? 'Your seats are held · ' : ''}{show.venue.name} · {show.venue.city}, {show.venue.state} · {when.long} · {when.time}</p>}
        {!live && seats.length > 0 && <span className="lamp">Reconnecting</span>}
      </div>

      <div className="booking">
        <section className="booking__map" aria-label={open ? `${open} seats` : 'Venue map'}>
          {liveError && <p className="notice" data-tone="error"><strong>Offline</strong> {liveError}</p>}
          {open && openInfo ? (
            <>
              <div className="zoom-head">
                {!hold && <button className="pill pill--glass pill--small" onClick={() => setOpen(null)}><Arrow dir="left" /> All sections</button>}
                <h2 className="zoom-title">{open}</h2>
                <span className="dim">{formatPrice(openInfo.priceCents)} · {openInfo.left} of {openInfo.total} left</span>
              </div>
              <div className="stage stage--small" aria-hidden="true">Stage this way</div>
              {openIsGA ? (
                <div className="ga-panel">
                  <p className="ga-panel__lede">General admission: your ticket gets you into the {open}. There are no assigned spots, so arrive early to stand near the stage.</p>
                  <dl className="ga-panel__facts">
                    <div><dt>Price each</dt><dd>{formatPrice(openInfo.priceCents)}</dd></div>
                    <div><dt>Admissions left</dt><dd><Ticker value={openInfo.left} width={3} align="right" stagger={0} label={`${openInfo.left} left`} /></dd></div>
                    <div><dt>{hold ? 'You hold' : 'Tickets'}</dt><dd>{hold ? hold.seats.length : qty}</dd></div>
                  </dl>
                  {!hold && (
                    <button className="pill pill--glass" onClick={() => pickGA(open)} disabled={openInfo.left < qty}>
                      {openInfo.left < qty ? `Only ${openInfo.left} left` : `Add ${qty} to cart · ${formatPrice(openInfo.priceCents * qty)}`}
                    </button>
                  )}
                </div>
              ) : (
                <SeatMap seats={openSeats} selected={hold ? hold.seats.map((s) => s.id) : selected} locked={!!hold} onToggle={onToggle} />
              )}
            </>
          ) : show && seats.length ? (
            <div className="vmap-wrap"><VenueMap kind={show.venue.kind} sections={summaries} gaSections={gaSections} onSelect={setOpen} /></div>
          ) : <p className="dim">Loading the venue…</p>}
        </section>

        <aside className="booking__side" aria-label="Your tickets">
          {hold ? <HoldPanel hold={hold} total={total} gaSections={gaSections} onExpired={() => setHold({ ...hold, status: 'expired' })} showId={showId} /> : (
            <>
              <div className="rail-row">
                <span className="line__label" id="qty-label">Tickets</span>
                <div className="stepper" role="group" aria-labelledby="qty-label">
                  <button type="button" onClick={() => setQty((q) => Math.max(1, q - 1))} disabled={qty <= 1} aria-label="Fewer tickets"><PlusMinus /></button>
                  <output aria-live="polite">{qty}</output>
                  <button type="button" onClick={() => setQty((q) => Math.min(max, q + 1))} disabled={qty >= max} aria-label="More tickets"><PlusMinus plus /></button>
                </div>
              </div>
              <button className="pill pill--glass" onClick={best} disabled={!seats.length} aria-describedby="best-scope">Best available</button>
              <p id="best-scope" className="dim rail-note">{open ? `Searching ${open}` : 'Searching every section, in list order'}</p>

              <div className="rail-row">
                <h2 className="line__label">Typical prices</h2>
                <button type="button" className="link-button" onClick={() => setByPrice((b) => !b)} aria-pressed={byPrice}>
                  Cheapest first
                </button>
              </div>
              <ul className="section-list">
                {listed.map((s) => (
                  <li key={s.name}>
                    <button type="button" aria-pressed={open === s.name} disabled={s.left === 0} onClick={() => setOpen(s.name)}>
                      <b>{s.name}{gaSections.has(s.name) && <small className="ga-tag">GA</small>}</b>
                      <span>{s.left === 0 ? 'Sold out' : <><Ticker value={s.left} width={3} align="right" stagger={0} label={`${s.left} left`} /> left</>}</span>
                      <em>{formatPrice(s.priceCents)}</em>
                    </button>
                  </li>
                ))}
              </ul>

              <h2 className="line__label">Your seats · up to {max}</h2>
              {chosen.length ? <TicketList seats={chosen} gaSections={gaSections} /> : <p className="dim">Pick a section, then seats (or a quantity for general admission), or use Best available.</p>}
              <div className="total"><span className="dim">Total</span><b>{formatPrice(total)}</b></div>
              <button className="pill pill--ink" disabled={!chosen.length || busy} onClick={reserve}>
                {busy ? 'Holding…' : chosen.length ? `Hold ${chosen.length} seat${chosen.length > 1 ? 's' : ''}` : 'Hold seats'}
              </button>
              <p className="dim" style={{ fontSize: '0.92rem' }}>Held seats are yours for {Math.round((show?.hold_ttl_seconds ?? 600) / 60)} minutes while you pay.</p>
            </>
          )}
          {message && <p className="notice" data-tone={message.tone} role="status"><strong>{message.tone === 'error' ? 'Heads up' : 'Found'}</strong> {message.text}</p>}
        </aside>
      </div>
    </main>
  )
}

function HoldPanel({ hold, total, onExpired, showId, gaSections }: { hold: Hold; total: number; onExpired: () => void; showId: number; gaSections: Set<string> }) {
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
    navigate(`/shows/${showId}/seats`)
  }

  if (expired) {
    return (
      <>
        <h2 className="line__label">Hold expired</h2>
        <p>Your seats went back on the board for other fans.</p>
        <button className="pill pill--ink" onClick={() => navigate(`/shows/${showId}/seats`)}>Pick again</button>
      </>
    )
  }
  return (
    <>
      <h2 className="line__label">Time left to pay</h2>
      <div className="countdown" data-low={left < 60_000} aria-live="off">
        <Ticker value={formatCountdown(left)} width={5} stagger={0} label={`${Math.ceil(left / 1000)} seconds left`} />
      </div>
      <TicketList seats={hold.seats} gaSections={gaSections} />
      <div className="total"><span className="dim">Total</span><b>{formatPrice(total)}</b></div>
      <button className="pill pill--ink" onClick={pay} disabled={busy}>{busy ? 'Opening checkout…' : `Pay ${formatPrice(total)}`}</button>
      <button className="pill pill--glass pill--small" onClick={release}>Release these seats</button>
      {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
    </>
  )
}

const label = (s: Seat) => `${s.section} ${s.row}${s.number}`
