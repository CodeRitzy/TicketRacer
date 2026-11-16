// Operator dashboard: invariants read from Postgres, the live pipeline, and the traffic simulator.
import { useState, type FormEvent } from 'react'
import { api, ApiError, session, type AdminStats, type SimConfig } from '../api'
import { Flap } from '../components/Flap'
import { Pipeline } from '../components/Pipeline'
import { useLiveSeats, usePoll } from '../lib/hooks'
import { formatCount, formatPrice } from '../lib/seats'

const DEFAULTS: SimConfig = { users: 2000, concurrency: 200, stampede: true, abandon_pct: 0.2, duplicate_pct: 0.3, max_seats: 4, abandon_ttl: 15 }

export function Dashboard() {
  const [authed, setAuthed] = useState(!!session.admin())
  return authed ? <Ops onSignOut={() => { session.setAdmin(''); setAuthed(false) }} /> : <Gate onOk={() => setAuthed(true)} />
}

// The admin token lives in this tab's sessionStorage only and is checked by the server.
function Gate({ onOk }: { onOk: () => void }) {
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    session.setAdmin(token.trim())
    try {
      await api.stats()
      onOk()
    } catch (err) {
      session.setAdmin('')
      setError(err instanceof ApiError && err.status === 401 ? 'That token was not accepted.' : 'The API is not reachable.')
    }
  }
  return (
    <main id="main" className="ticket">
      <h1 className="section__title">Operations</h1>
      <p className="dim">The dashboard and simulator need the admin token, the <span className="mono">ADMIN_TOKEN</span> value in your <span className="mono">.env</span>.</p>
      <form className="form" onSubmit={submit} style={{ maxWidth: 480 }}>
        <div className="field">
          <label htmlFor="admin-token">Admin token</label>
          <input id="admin-token" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} required />
        </div>
        <button className="btn" type="submit">Open the dashboard</button>
        {error && <p className="notice" data-tone="error" role="alert"><strong>Denied</strong> {error}</p>}
      </form>
    </main>
  )
}

function Ops({ onSignOut }: { onSignOut: () => void }) {
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [offline, setOffline] = useState(false)
  const [notice, setNotice] = useState('')
  const { seats } = useLiveSeats()

  // the one-second tick that drives every number and dot on this page
  usePoll(async () => {
    try {
      setStats(await api.stats())
      setOffline(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) { onSignOut(); return true }
      setOffline(true)
    }
  }, 1000)

  const reset = async () => {
    try { await api.reset(); setNotice('Bookings, queue and counters were reset.') }
    catch (e) { setNotice(e instanceof ApiError ? e.message : 'Reset failed.') }
  }

  const db = stats?.db
  const ok = db ? db.sold_without_paid_order === 0 && db.duplicate_orders === 0 && db.reconciled : true
  const running = stats?.sim.running ?? false

  return (
    <main id="main" className="ops">
      <div className="ops__head">
        <div style={{ display: 'grid', gap: 12 }}>
          <h1 className="section__title">Operations</h1>
          <p className="dim">Every number below is read from PostgreSQL and Redis once a second.
            Payment mode: <span className="mono">{stats?.payment_mode ?? '…'}</span>.</p>
        </div>
        <div className="hero__action">
          {offline && <span className="lamp" role="alert">API offline</span>}
          <button className="btn btn--ghost btn--small" onClick={reset} disabled={running}>Reset event</button>
          <button className="btn btn--ghost btn--small" onClick={onSignOut}>Sign out</button>
        </div>
      </div>
      {notice && <p className="notice" data-tone="ok" role="status"><strong>Done</strong> {notice}</p>}

      <section aria-labelledby="checks-title" style={{ display: 'grid', gap: 20 }}>
        <h2 id="checks-title" className="section__title" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.4rem)', color: ok ? undefined : 'var(--red)' }}>
          {ok ? 'Every guarantee holding' : 'Guarantee violated'}
        </h2>
        <div className="checks">
          <div><span className="line__label">Oversold seats</span>
            <Flap value={db?.sold_without_paid_order ?? 0} width={3} align="right" tone={db?.sold_without_paid_order ? 'red' : 'green'} /></div>
          <div><span className="line__label">Duplicate orders</span>
            <Flap value={db?.duplicate_orders ?? 0} width={3} align="right" tone={db?.duplicate_orders ? 'red' : 'green'} /></div>
          <div><span className="line__label">Revenue reconciled</span>
            <Flap value={db ? (db.reconciled ? 'Yes' : 'No') : '---'} width={3} tone={db && !db.reconciled ? 'red' : 'green'} /></div>
        </div>
        <p className="dim" style={{ fontSize: '0.95rem' }}>Oversold: sold seats with no paid order. Duplicate: holds with more than one order.
          Reconciled: paid revenue equals the price of every sold seat.</p>
      </section>

      <section aria-labelledby="pipe-title" style={{ display: 'grid', gap: 20 }}>
        <div className="section__head" style={{ marginBottom: 0 }}>
          <h2 id="pipe-title" className="section__title" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.4rem)' }}>Request pipeline</h2>
          <span className="dim">Each dot is one real request from the last second.</span>
        </div>
        <Pipeline stats={stats} />
      </section>

      <div className="grid-2">
        <Simulator running={running} progress={stats?.sim.progress} />
        <section aria-labelledby="seats-title" style={{ display: 'grid', gap: 20, alignContent: 'start' }}>
          <h2 id="seats-title" className="section__title" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.4rem)' }}>The house</h2>
          <dl className="line" style={{ ['--cols' as string]: 3 }}>
            <div><dt>Available</dt><dd><Flap value={db?.available ?? 0} width={3} align="right" /></dd></div>
            <div><dt>Held</dt><dd><Flap value={db?.held ?? 0} width={3} align="right" tone="amber" /></dd></div>
            <div><dt>Sold</dt><dd><Flap value={db?.sold ?? 0} width={3} align="right" /></dd></div>
          </dl>
          <div className="heat" role="img" aria-label={`Seat map: ${db?.available ?? 0} available, ${db?.held ?? 0} held, ${db?.sold ?? 0} sold`}>
            {seats.map((s) => <i key={s.id} data-s={s.status[0]} />)}
          </div>
          <dl className="kv">
            <dt>Revenue</dt><dd>{formatPrice(db?.revenue_cents ?? 0)}</dd>
            <dt>Paid orders</dt><dd>{formatCount(db?.orders_paid ?? 0)}</dd>
            <dt>Refunded</dt><dd>{formatCount(db?.orders_refunded ?? 0)}</dd>
            <dt>Webhook events stored</dt><dd>{formatCount(db?.webhook_events ?? 0)}</dd>
          </dl>
        </section>
      </div>

      {stats?.sim.report && <Report report={stats.sim.report} />}
    </main>
  )
}

function Simulator({ running, progress }: { running: boolean; progress?: Record<string, number> }) {
  const [cfg, setCfg] = useState<SimConfig>(DEFAULTS)
  const [error, setError] = useState('')
  const set = <K extends keyof SimConfig>(k: K, v: SimConfig[K]) => setCfg((c) => ({ ...c, [k]: v }))

  const start = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    try { await api.simulate(cfg) } catch (err) { setError(err instanceof ApiError ? err.message : 'Could not start.') }
  }

  return (
    <section aria-labelledby="sim-title" style={{ display: 'grid', gap: 20, alignContent: 'start' }}>
      <h2 id="sim-title" className="section__title" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.4rem)' }}>Traffic simulator</h2>
      <p className="dim">Virtual fans hit the real HTTP API: join, wait, grab seats, pay, abandon, and replay webhooks.</p>
      <form className="form" onSubmit={start}>
        <div className="form__row">
          <div className="field"><label htmlFor="sim-users">Fans</label>
            <input id="sim-users" type="number" min={1} max={20000} value={cfg.users} onChange={(e) => set('users', Number(e.target.value))} /></div>
          <div className="field"><label htmlFor="sim-conc">Connections</label>
            <input id="sim-conc" type="number" min={1} max={1000} value={cfg.concurrency} onChange={(e) => set('concurrency', Number(e.target.value))} /></div>
        </div>
        <div className="field">
          <div className="field__row"><label htmlFor="sim-abandon">Abandon their hold</label><span>{Math.round(cfg.abandon_pct * 100)}%</span></div>
          <input id="sim-abandon" type="range" min={0} max={0.6} step={0.05} value={cfg.abandon_pct} onChange={(e) => set('abandon_pct', Number(e.target.value))} />
        </div>
        <div className="field">
          <div className="field__row"><label htmlFor="sim-dup">Webhook delivered 3×</label><span>{Math.round(cfg.duplicate_pct * 100)}%</span></div>
          <input id="sim-dup" type="range" min={0} max={1} step={0.05} value={cfg.duplicate_pct} onChange={(e) => set('duplicate_pct', Number(e.target.value))} />
        </div>
        <label className="check"><input type="checkbox" checked={cfg.stampede} onChange={(e) => set('stampede', e.target.checked)} />
          Stampede: skip the waiting room so everyone reserves at once</label>
        <button className="btn" type="submit" disabled={running}>{running ? 'Simulation running…' : `Release ${cfg.users.toLocaleString('en-US')} fans`}</button>
        {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
      </form>
      {running && progress && (
        <dl className="kv" aria-live="polite">
          <dt>Fans</dt><dd>{formatCount(progress.users)} · admitted {formatCount(progress.admitted)}</dd>
          <dt>Holds won</dt><dd>{formatCount(progress.holds)} · conflicts {formatCount(progress.conflicts)}</dd>
          <dt>Paid</dt><dd>{formatCount(progress.paid)} · abandoned {formatCount(progress.abandoned)}</dd>
          <dt>Requests</dt><dd>{formatCount(progress.requests)}</dd>
        </dl>
      )}
    </section>
  )
}

function Report({ report }: { report: NonNullable<AdminStats['sim']['report']> }) {
  const lat = report.reserve_latency_ms
  const rows: [string, string | number][] = [
    ['Fans', formatCount(report.counts.users)], ['Duration', `${report.duration_sec} s`], ['HTTP requests', formatCount(report.requests)],
    ['Throughput', `${formatCount(Math.round(report.requests_per_sec))} req/s`],
    ['Reserve latency p50 / p95 / p99', `${lat.p50 ?? '–'} / ${lat.p95 ?? '–'} / ${lat.p99 ?? '–'} ms`],
    ['Holds won', formatCount(report.counts.holds)], ['409 conflicts', formatCount(report.counts.conflicts_409)], ['Paid', formatCount(report.counts.paid)],
    ['Abandoned (left to expire)', formatCount(report.counts.abandoned)], ['Extra webhook deliveries', formatCount(report.counts.duplicate_webhooks_sent)],
    ['Errors', formatCount(report.counts.errors)],
  ]
  return (
    <section aria-labelledby="report-title" style={{ display: 'grid', gap: 20 }}>
      <h2 id="report-title" className="section__title" style={{ fontSize: 'clamp(1.6rem, 3vw, 2.4rem)' }}>Last run</h2>
      <table className="table">
        <thead><tr><th scope="col">Measure</th><th scope="col">Result</th></tr></thead>
        <tbody>{rows.map(([k, v]) => <tr key={k}><td>{k}</td><td>{v}</td></tr>)}</tbody>
      </table>
      <p className="dim" style={{ fontSize: '0.95rem' }}>Latency is measured by the simulator, so it includes time waiting for one of its own connections.</p>
    </section>
  )
}
