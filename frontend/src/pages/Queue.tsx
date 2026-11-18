// Waiting room: the position flips down each second until the fan is admitted.
import { useRef, useState } from 'react'
import { api, ApiError, session, type QueueStatus } from '../api'
import { Ticker } from '../components/Ticker'
import { navigate, usePoll } from '../lib/hooks'

export function Queue({ showId }: { showId: number }) {
  const [status, setStatus] = useState<QueueStatus | null>(null)
  const [lost, setLost] = useState(false)
  const firstPosition = useRef(0)

  usePoll(async () => {
    if (!session.tokenFor(showId)) { navigate(`/shows/${showId}`, true); return true }
    try {
      const st = await api.queueStatus(showId)
      setStatus(st)
      if (!firstPosition.current && st.position) firstPosition.current = st.position
      if (st.admitted) {
        window.setTimeout(() => navigate(`/shows/${showId}/seats`, true), 1100) // let "you're in" land first
        return true
      }
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) { setLost(true); return true }
    }
  }, 1000)

  const rejoin = async () => {
    const { token } = await api.join(showId)
    session.setFan(showId, token)
    setLost(false)
    firstPosition.current = 0
  }

  const pos = status?.position ?? 0
  const progress = status?.admitted ? 1 : firstPosition.current ? 1 - (pos - 1) / firstPosition.current : 0

  return (
    <main id="main" className="queue">
      <h1 className="section__title">{status?.admitted ? 'You’re in' : 'You’re in line'}</h1>

      <div aria-live="polite">
        <span className="line__label">Your place</span>
        <div className="queue__position">
          {status?.admitted
            ? <Ticker value="Go" width={4} tone="green" label="Admitted" />
            : <Ticker value={pos || '----'} width={4} align="right" label={pos ? `Position ${pos}` : 'Finding your place'} />}
        </div>
      </div>

      <div className="queue__bar" role="progressbar" aria-label="Progress to the front of the line"
        aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress * 100)}>
        <span style={{ width: `${Math.max(2, progress * 100)}%` }} />
      </div>

      <dl className="line" style={{ ['--cols' as string]: 3 }}>
        <div><dt>Ahead of you</dt><dd><Ticker value={status && !status.admitted ? Math.max(0, pos - 1) : 0} width={4} align="right" /></dd></div>
        <div><dt>In line</dt><dd><Ticker value={status?.waiting ?? 0} width={4} align="right" /></dd></div>
        <div><dt>Status</dt><dd><Ticker value={status?.admitted ? 'Admitted' : 'Waiting'} width={8} tone={status?.admitted ? 'green' : 'amber'} /></dd></div>
      </dl>

      {lost ? (
        <p className="notice" data-tone="error" role="alert"><strong>Expired</strong> Your place in line expired.
          <button className="pill pill--ink pill--small" onClick={rejoin}>Rejoin</button></p>
      ) : (
        <p className="dim">Fans are let in at a steady rate, in arrival order. Keep this tab open; it moves you on automatically.</p>
      )}
    </main>
  )
}
