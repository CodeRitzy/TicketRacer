// Home: the next show's seats as a live particle sculpture under huge type, a scroll story in three
// steps (the sculpture re-forms for each), the state's lineup, and a closing call to the dashboard.
// The state is guessed from the browser's time zone and can be changed; the guess never leaves the
// browser.
import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { api, showWhen, type ShowSummary } from '../api'
import { Arrow } from '../components/Arrow'
import { FloatCards, type FloatCard } from '../components/FloatCards'
import { SeatSwarm, type SwarmShape } from '../components/SeatSwarm'
import { Ticker } from '../components/Ticker'
import { navigate, useLiveSeats, usePoll } from '../lib/hooks'
import { rememberedState, rememberState, type StateInfo } from '../lib/location'
import { remark } from '../lib/picker'
import { formatCount, formatPrice } from '../lib/seats'

// the scroll story: each step names a formation for the sculpture
const STEPS: { shape: SwarmShape; title: [string, string, string]; body: string }[] = [
  { shape: 'line', title: ['Wait', 'your', 'turn.'],
    body: 'When a show goes on sale, every fan joins one line. The waiting room lets people in a few each second, so the seat map never buckles under the stampede.' },
  { shape: 'hold', title: ['Hold', 'your', 'seats.'],
    body: 'Pick seats and they are yours for ten minutes. The database hands each seat to exactly one fan; anyone else who reached for it hears so instantly.' },
  { shape: 'ticket', title: ['Pay', 'once', '.'],
    body: 'Stripe confirms the payment. If that confirmation arrives five times, you still get one order, and a hold someone walks away from goes straight back on sale.' },
]

// where the show cards sit around the sculpture: [x %, y %, depth, tilt deg, width px]
const ORBIT: [number, number, number, number, number][] = [
  [12, 20, 1.4, -4, 280],
  [86, 12, 0.8, 5, 230],
  [84, 82, 1.2, -3, 240],
  [18, 86, 0.7, 6, 220],
  [52, 3, 0.55, -5, 210],
]

export function Shows() {
  const [states, setStates] = useState<StateInfo[]>([])
  const [code, setCode] = useState('')
  const [shows, setShows] = useState<ShowSummary[] | null>(null)
  const [zone, setZone] = useState<string>()
  const [error, setError] = useState('')
  const [shape, setShape] = useState<SwarmShape>('house')
  const [tick, setTick] = useState(0)

  const [fallback, setFallback] = useState(false)

  // load the state list, then start from the remembered choice, the time-zone guess, or California.
  // A failed request (say the API is still starting) retries every 3 s instead of leaving the page empty.
  usePoll(async () => {
    try {
      const list = await api.states()
      const start = rememberedState(list)
      setStates(list)
      setCode(start.code)
      setFallback(start.fallback)
      setError('')
      return true
    } catch (e) {
      setError('Could not reach the server. Retrying…')
      throw e
    }
  }, 3000, !states.length)

  // the lineup refreshes every 5 s so availability and remarks stay honest
  usePoll(async () => {
    if (!code) return
    const res = await api.lineup(code)
    setShows(res.shows)
    setZone(res.state.zone)
    setError('')
  }, 5000, !!code)

  // whichever block crosses the middle of the screen picks the sculpture's formation and the tick
  useEffect(() => {
    const blocks = document.querySelectorAll<HTMLElement>('[data-tick]')
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue
        const el = e.target as HTMLElement
        setTick(Number(el.dataset.tick))
        if (el.dataset.shape) setShape(el.dataset.shape as SwarmShape)
      }
    }, { rootMargin: '-50% 0px -50% 0px' })
    blocks.forEach((b) => io.observe(b))
    return () => io.disconnect()
  }, [])

  const change = (next: string) => {
    rememberState(next)
    setShows(null)
    setCode(next)
    setFallback(false)
  }
  const stateName = states.find((s) => s.code === code)?.name ?? ''
  // the featured show: the soonest one still on sale (a sold-out house makes a dull sculpture)
  const next = shows?.find((s) => s.available > 0) ?? shows?.[0]
  const { seats: nextSeats } = useLiveSeats(next?.id ?? 0)
  const statuses = useMemo(() => nextSeats.map((s) => s.status), [nextSeats])
  const open = (id: number) => (e: MouseEvent) => { e.preventDefault(); navigate(`/shows/${id}`) }
  const go = (to: string) => (e: MouseEvent) => { e.preventDefault(); navigate(to) }

  // glass cards orbiting the sculpture: only the shows near you. The featured show sits closest
  // and largest; the rest of the state's lineup floats further back, drifting out of focus.
  const cards = useMemo<FloatCard[]>(() => {
    if (!next || !shows) return []
    const total = (s: ShowSummary) => s.available + s.held + s.sold
    const nearby = [next, ...shows.filter((s) => s.id !== next.id)].slice(0, ORBIT.length)
    return nearby.map((sh, i) => {
      const [x, y, z, tilt, width] = ORBIT[i]
      const when = showWhen(sh.starts_at, zone)
      const lead = i === 0
      return {
        key: `show-${sh.id}`, x, y, z, tilt, width, node: (
          <a className="fcard" href={`/shows/${sh.id}`} onClick={open(sh.id)}>
            <b className={`fcard__title${lead ? '' : ' fcard__title--sm'}`}>{sh.act}</b>
            <span className="fcard__meta">{when.day} {when.date} · {sh.venue.name}</span>
            {sh.available > 0
              ? <span className={`fcard__big${lead ? '' : ' fcard__big--sm'}`}><Ticker value={formatCount(sh.available)} /> <small>seats left of {formatCount(total(sh))}</small></span>
              : <span className="fcard__soldout">Sold out</span>}
          </a>
        ),
      }
    })
  }, [next, shows, zone])

  return (
    <main id="main" className="home">
      {/* the sculpture is pinned behind the hero and the story, and re-forms as they scroll past */}
      <div className="scene">
        <div className="scene__pin" aria-hidden="true">
          <div className="scene__canvas"><SeatSwarm statuses={statuses} shape={shape} /></div>
        </div>

        <section className="home-hero" aria-labelledby="shows-title" data-tick="0" data-shape="house">
          <div className="home-hero__top reveal">
            {shows && shows.length > 0 && (
              <div className="marquee" aria-hidden="true">
                <div className="marquee__track">
                  {[...shows, ...shows].map((s, i) => <span key={i} className="chip">{s.act}</span>)}
                </div>
              </div>
            )}
            <div className="home-hero__intro">
              <p className="lede">
                Six upcoming shows at real {stateName || 'local'} venues. Thousands of fans, a few hundred seats,
                and not one seat ever sold twice.
              </p>
              {fallback && <p className="scene__caption">We couldn't tell where you are, so these are California shows. Tap the state name to see any other state.</p>}
              {next && <p className="scene__caption">Each dot is a seat at {next.act}, live.</p>}
            </div>
          </div>

          <FloatCards cards={cards} label={`Live facts for ${stateName} shows`} />

          <div className="home-hero__bottom">
            <h1 id="shows-title" className="display display--xl">
              <span className="reveal">Shows <em className="serif">in</em></span>{' '}
              <label className="state-pick reveal" style={{ ['--d' as string]: '120ms' }}>
                <span className="sr-only">Choose your state</span>
                <select value={code} onChange={(e) => change(e.target.value)} disabled={!states.length}>
                  {states.map((s) => <option key={s.code} value={s.code}>{s.name}</option>)}
                </select>
              </label>
            </h1>
            <a className="pill pill--ink reveal" style={{ ['--d' as string]: '260ms' }} href="#lineup"
              onClick={(e) => { e.preventDefault(); document.getElementById('lineup')?.scrollIntoView({ behavior: 'smooth' }) }}>
              See the lineup <span className="pill__chip"><Arrow /></span>
            </a>
          </div>        </section>

        <section className="story" aria-labelledby="story-title">
          <h2 id="story-title" className="sr-only">How one seat is won</h2>
          {STEPS.map((s, i) => (
            <article key={s.shape} className="step" data-tick={i + 1} data-shape={s.shape}>
              <ol className="step__rail" aria-hidden="true">
                {STEPS.map((_, j) => <li key={j} data-on={j === i || undefined}>{j === i ? `Step 0${j + 1}` : `0${j + 1}`}</li>)}
              </ol>
              <div className="step__copy">
                <h3 className="step__title">
                  {s.title[0]} <em className="serif">{s.title[1]}</em>{s.title[2] === '.' ? '.' : ` ${s.title[2]}`}
                </h3>
                <p className="lede">{s.body}</p>
              </div>
            </article>
          ))}
        </section>
      </div>

      <section id="lineup" className="section" aria-labelledby="lineup-title" data-tick="4">
        <div className="section__head">
          <h2 id="lineup-title" className="section__title">Upcoming <em className="serif">in</em> {stateName}</h2>
          <p className="dim">Live availability, refreshed every few seconds.</p>
        </div>
        {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
        <div className="glass lineup">
          <table className="departures" aria-label={`Upcoming shows in ${stateName}`}>
            <thead>
              <tr><th scope="col">Date</th><th scope="col">Show</th><th scope="col">Venue</th>
                <th scope="col" className="col-from">From</th><th scope="col">Status</th><th scope="col"><span className="sr-only">Open</span></th></tr>
            </thead>
            <tbody>
              {!shows && <tr><td colSpan={6} className="dim">Loading shows…</td></tr>}
              {shows?.map((sh) => {
                const when = showWhen(sh.starts_at, zone)
                const r = remark(sh.available, sh.held, sh.available + sh.held + sh.sold)
                return (
                  <tr key={sh.id} className="departures__row">
                    <td className="departures__date"><b>{when.date}</b><span>{when.day} · {when.time}</span></td>
                    <td className="departures__act"><a href={`/shows/${sh.id}`} onClick={open(sh.id)}>{sh.act}</a></td>
                    <td className="departures__venue">{sh.venue.name}<span>{sh.venue.city}</span></td>
                    <td className="col-from">{formatPrice(sh.min_price_cents)}</td>
                    <td><span className={`status status--${r.tone ?? 'ok'}`}>{r.text}</span></td>
                    <td className="departures__go" aria-hidden="true"><Arrow /></td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <p className="dim fine">
          Demo shows: every show is fictional. Venues and capacities are real; section layouts use the venue's published
          names where they exist and the venue type's usual layout otherwise; prices are typical for the venue type.
          Your state is guessed from your device's time zone and never leaves this browser.
        </p>

        {/* the closing bar: one step from here to watching a stampede */}
        {next && (
          <div className="cta-bar glass">
            <p className="cta-bar__title">See it under <em className="serif">real</em> load.</p>
            <p className="dim">Release thousands of simulated fans at {next.act} and watch every seat sell exactly once.</p>
            <a className="pill pill--ink" href={`/dashboard?show=${next.id}`} onClick={go(`/dashboard?show=${next.id}`)}>
              Watch it under load <span className="pill__chip"><Arrow /></span>
            </a>
          </div>
        )}
      </section>

      {/* where you are on the page, autopilot-style */}
      <ol className="ticks" aria-hidden="true">
        {[0, 1, 2, 3, 4].map((i) => <li key={i} data-on={tick === i || undefined} />)}
      </ol>
    </main>
  )
}
