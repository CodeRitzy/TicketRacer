// Built-in test checkout (demo payment mode). Paying sends a Stripe-format, HMAC-signed webhook
// to the same endpoint Stripe uses, so the order path is the production one.
import { useEffect, useState } from 'react'
import { api, type Hold } from '../api'
import { navigate } from '../lib/hooks'
import { formatPrice } from '../lib/seats'

export function DemoPay({ holdId }: { holdId: string }) {
  const [hold, setHold] = useState<Hold | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => { api.getHold(holdId).then(setHold, () => setError('This hold does not exist for this browser tab.')) }, [holdId])

  const pay = async () => {
    setBusy(true)
    try {
      await api.demoPay(holdId)
      navigate(`/confirmation?hold=${holdId}`, true)
    } catch {
      setError('Payment failed. Your seats are still held; try again.')
      setBusy(false)
    }
  }

  return (
    <main id="main" className="ticket">
      <h1 className="section__title">Test checkout</h1>
      <p className="dim">No card, no money. Paying here sends a signed <span className="mono">checkout.session.completed</span> event
        to the same webhook endpoint Stripe calls.</p>
      {error && <p className="notice" data-tone="error" role="alert"><strong>Error</strong> {error}</p>}
      {hold && (
        <>
          <ul className="ticket-list">
            {hold.seats.map((s) => <li key={s.id}><b>Row {s.row} · Seat {s.number}</b><span>{formatPrice(s.price_cents)}</span></li>)}
          </ul>
          <div className="total"><span className="dim">Total</span><b>{formatPrice(hold.total_cents)}</b></div>
          <div className="hero__action">
            <button className="btn" onClick={pay} disabled={busy || hold.status !== 'active'}>{busy ? 'Paying…' : `Pay ${formatPrice(hold.total_cents)}`}</button>
            <button className="btn btn--ghost btn--small" onClick={() => navigate(`/hold/${holdId}`)}>Back</button>
          </div>
        </>
      )}
    </main>
  )
}
