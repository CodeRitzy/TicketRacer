// The night's board: event title flips in, the live line (seats left, in line, held, status),
// the one action — join the queue — and the house itself below the fold.
import { useState } from 'react'
import { api, session, type EventConfig } from '../api'
import { Arrow } from '../components/Arrow'
import { Flap, FlapIntro } from '../components/Flap'
import { SeatMap } from '../components/SeatMap'
import { navigate, useLiveSeats, usePoll } from '../lib/hooks'
import { formatCount } from '../lib/seats'

// departures-board remarks: the status line speaks the board's own vocabulary
function remark(left: number, held: number, total: number): { text: string; tone?: 'amber' } {
  if (!total) return { text: '' }
  if (left === 0 && held === 0) return { text: 'Sold out' }
  if (left === 0) return { text: 'All held', tone: 'amber' }
  if (left <= 20) return { text: 'Final seats', tone: 'amber' }
  return { text: 'On sale' }
}

export function Home({ config }: { config: EventConfig | null }) {
  const { seats } = useLiveSeats()
  const [waiting, setWaiting] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // the queue length is the pressure behind the pitch; one cheap public read every two seconds
  usePoll(async () => { setWaiting((await api.queueLength()).waiting) }, 2000)

  const left = seats.filter((s) => s.status === 'available').length
  const held = seats.filter((s) => s.status === 'held').length
  const status = remark(left, held, seats.length)
  const soldOut = status.text === 'Sold out'
  const date = config ? new Date(config.starts_at) : null

  // joining issues a queue token for this tab; the waiting room takes it from there
  const join = async () => {
    setBusy(true)
    setError('')
    try {
      const { token, status: st } = await api.join()
      session.setToken(token)
      navigate(st.admitted ? '/seats' : '/queue')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not join the queue.')
      setBusy(false)
    }
  }

  return (
    <main id="main">
      <section className="hero" aria-labelledby="event-title">
        <div className="bezel" style={{ display: 'grid', gap: 'clamp(24px, 3.5vw, 40px)' }}>
          <h1 id="event-title" className="hero__title">
            <FlapIntro value="Midnight" />
            <FlapIntro value="Static" stagger={26} />
          </h1>
          <p className="hero__meta">
            <Flap value="The Foundry" stagger={14} />
            {date && <Flap value={date.toLocaleDateString('en-US', { day: '2-digit', month: 'short', timeZone: 'America/Chicago' })} stagger={14} />}
            {date && <Flap value="20:00" stagger={14} />}
          </p>
          <dl className="line line--home" style={{ ["--cols" as string]: 4 }}>
            <div><dt>Seats left</dt><dd><Flap value={seats.length ? left : '---'} width={3} align="right" label={`${left} seats left`} /></dd></div>
            <div><dt>In line</dt><dd><Flap value={waiting === null ? '----' : formatCount(waiting)} width={5} align="right" label={`${waiting ?? 0} fans in line`} /></dd></div>
            <div><dt>Held</dt><dd><Flap value={seats.length ? held : '---'} width={3} align="right" tone="amber" label={`${held} seats held`} /></dd></div>
            <div><dt>Status</dt><dd><Flap value={status.text} tone={status.tone} /></dd></div>
          </dl>
        </div>

        <p className="hero__lede">
          200 seats, thousands of fans. <strong>Ticketracer cannot sell a seat twice</strong>: the database decides every hold,
          abandoned holds return to the board, and a payment confirmed twice still makes one order.
        </p>

        <div className="hero__action">
          {session.token() ? (
            <button className="btn" onClick={() => navigate('/queue')}>Back to your place</button>
          ) : (
            <button className="btn" onClick={join} disabled={busy || soldOut}>{busy ? 'Joining…' : 'Join the queue'}</button>
          )}
          <a href="/dashboard" style={{ display: 'inline-flex', gap: 10, alignItems: 'center' }}
            onClick={(e) => { e.preventDefault(); navigate('/dashboard') }}>Watch it under load <Arrow /></a>
        </div>
        {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
        <p className="dim" style={{ fontSize: '0.9rem' }}>
          Demo event. {config?.payment_mode === 'stripe'
            ? 'Payments run in Stripe test mode: use card 4242 4242 4242 4242, any future date, any CVC.'
            : 'Payments use the built-in test checkout; no card needed.'}
        </p>
      </section>

      <section className="section" aria-labelledby="house-title">
        <div className="section__head">
          <h2 id="house-title" className="section__title">The house, live</h2>
          <p className="dim">Amber flaps are other fans holding seats right now. Join the queue to pick yours.</p>
        </div>
        <SeatMap seats={seats} selected={[]} locked />
      </section>
    </main>
  )
}
