// One show: the act at display scale, date, venue and real capacity, live numbers on a glass
// panel, the one action (join the queue), and the house on a soft venue map below.
import { useEffect, useState } from 'react'
import { api, session, showWhen, type ShowDetail } from '../api'
import { Arrow } from '../components/Arrow'
import { Ticker } from '../components/Ticker'
import { VenueMap } from '../components/VenueMap'
import { navigate, useLiveSeats, usePoll } from '../lib/hooks'
import { remark, sectionSummaries } from '../lib/picker'
import { formatCount } from '../lib/seats'

export function Event({ showId }: { showId: number }) {
  const [show, setShow] = useState<ShowDetail | null>(null)
  const [missing, setMissing] = useState(false)
  const { seats } = useLiveSeats(showId)
  const [waiting, setWaiting] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => { api.show(showId).then(setShow, () => setMissing(true)) }, [showId])
  // the line length is the pressure behind the pitch; one cheap public read every two seconds
  usePoll(async () => { setWaiting((await api.queueLength(showId)).waiting) }, 2000)

  const left = seats.filter((s) => s.status === 'available').length
  const held = seats.filter((s) => s.status === 'held').length
  const status = remark(left, held, seats.length)
  const when = show ? showWhen(show.starts_at, show.zone) : null
  const mine = !!session.tokenFor(showId)
  const dashboard = `/dashboard?show=${showId}`

  // joining issues a queue token for this show in this tab; the waiting room takes it from there
  const join = async () => {
    setBusy(true)
    setError('')
    try {
      const { token, status: st } = await api.join(showId)
      session.setFan(showId, token)
      navigate(st.admitted ? `/shows/${showId}/seats` : `/shows/${showId}/queue`)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not join the queue.')
      setBusy(false)
    }
  }

  if (missing) {
    return (
      <main id="main" className="ticket">
        <h1 className="section__title">Show not found</h1>
        <p>This show may have been replaced by a new lineup. <a href="/" onClick={(e) => { e.preventDefault(); navigate('/') }}>See shows near you</a></p>
      </main>
    )
  }

  return (
    <main id="main">
      <section className="event-hero" aria-labelledby="event-title">
        <a className="back-link" href="/" onClick={(e) => { e.preventDefault(); navigate('/') }}><Arrow dir="left" /> All shows</a>
        <h1 id="event-title" className="display reveal">{show?.act ?? 'Loading…'}</h1>
        {show && when && (
          <p className="lede reveal" style={{ ['--d' as string]: '120ms' }}>
            {when.long} at {when.time} · <b>{show.venue.name}</b>, {show.venue.city}, {show.venue.state}.
            <br />Real capacity {formatCount(show.venue.capacity)}; this demo sells {formatCount(seats.length)} seats.
          </p>
        )}
        <div className="actions reveal" style={{ ['--d' as string]: '220ms' }}>
          {mine ? (
            <button className="pill pill--ink" onClick={() => navigate(`/shows/${showId}/queue`)}>Back to your place <span className="pill__chip"><Arrow /></span></button>
          ) : (
            <button className="pill pill--ink" onClick={join} disabled={busy || status.text === 'Sold out' || !show}>
              {busy ? 'Joining…' : 'Join the queue'} <span className="pill__chip"><Arrow /></span>
            </button>
          )}
          <a className="pill pill--glass" href={dashboard} onClick={(e) => { e.preventDefault(); navigate(dashboard) }}>Watch it under load</a>
        </div>
        {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}

        <dl className="glass stats reveal" style={{ ['--d' as string]: '320ms' }}>
          <div><dt>Seats left</dt><dd><Ticker value={seats.length ? formatCount(left) : '–'} label={`${left} seats left`} /></dd></div>
          <div><dt>In line</dt><dd><Ticker value={waiting === null ? '–' : formatCount(waiting)} label={`${waiting ?? 0} fans in line`} /></dd></div>
          <div><dt>Held now</dt><dd><Ticker value={seats.length ? formatCount(held) : '–'} tone="amber" label={`${held} seats held`} /></dd></div>
          <div><dt>Status</dt><dd><span className={`status status--${status.tone ?? 'ok'}`}>{status.text || '–'}</span></dd></div>
        </dl>
        <p className="dim fine">
          Demo show: the act is fictional. {show?.payment_mode === 'stripe'
            ? 'Payments run in Stripe test mode: use card 4242 4242 4242 4242, any future date, any CVC.'
            : 'Payments use the built-in test checkout; no card needed.'}
        </p>
      </section>

      {show && seats.length > 0 && (
        <section className="section" aria-labelledby="house-title">
          <div className="section__head">
            <h2 id="house-title" className="section__title">The house, <em className="serif">live</em></h2>
            <p className="dim">Sections fade as they sell. Join the queue to pick your seats.</p>
          </div>
          <div className="vmap-wrap glass"><VenueMap kind={show.venue.kind} sections={sectionSummaries(seats)} /></div>
        </section>
      )}
    </main>
  )
}
