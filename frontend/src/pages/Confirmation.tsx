// Confirmation: polls until the payment webhook has been processed, then flips the status in.
import { useState } from 'react'
import { api, type Order } from '../api'
import { Flap } from '../components/Flap'
import { navigate, usePoll } from '../lib/hooks'
import { formatPrice } from '../lib/seats'

export function Confirmation({ holdId }: { holdId: string }) {
  const [order, setOrder] = useState<Order | null>(null)
  const [attempts, setAttempts] = useState(0)
  const [missing, setMissing] = useState(false)

  // the fan can land here before Stripe's webhook does; keep asking for up to a minute
  usePoll(async () => {
    setAttempts((a) => a + 1)
    try {
      const res = await api.order(holdId)
      if ('id' in res) { setOrder(res); return true }
    } catch {
      setMissing(true)
      return true
    }
    return attempts > 60
  }, 1000, !!holdId)

  const status = order?.status === 'paid' ? 'Confirmed' : order ? 'Refunded' : missing ? 'Not found' : 'Processing'
  // green is reserved for passing checks: a confirmed order is one; red only when something went wrong
  const tone = order?.status === 'paid' ? 'green' : order ? 'amber' : missing ? 'red' : 'amber'

  return (
    <main id="main" className="ticket">
      <h1 className="ticket__status" aria-live="polite">
        <Flap value={status} width={10} tone={tone} label={`Order ${status.toLowerCase()}`} />
      </h1>

      {!order && !missing && (
        <p className="dim">Waiting for the payment confirmation from Stripe. This usually takes a second or two.
          {attempts > 15 && ' Still waiting: if you are running locally, make sure `stripe listen` is forwarding webhooks.'}</p>
      )}
      {missing && <p>We could not find this booking for this browser tab. <a href="/" onClick={(e) => { e.preventDefault(); navigate('/') }}>Back to the board</a></p>}

      {order && (
        <>
          {order.status !== 'paid' && (
            <p className="notice" data-tone="error"><strong>Refunded</strong> Your payment arrived after the hold expired and the seats
              had been released, so it was refunded in full. You were not charged.</p>
          )}
          <dl className="kv">
            <dt>Order</dt><dd className="mono">{order.id}</dd>
            <dt>Amount</dt><dd>{formatPrice(order.amount_cents)}</dd>
            {order.email && <><dt>Receipt to</dt><dd>{order.email}</dd></>}
            <dt>Payment ref</dt><dd className="mono">{order.session_ref}</dd>
          </dl>
          {order.seats && order.seats.length > 0 && (
            <ul className="ticket-list">
              {order.seats.map((s) => (
                <li key={s.id}><b>Row {s.row} · Seat {s.number}</b><span>{formatPrice(s.price_cents)}</span></li>
              ))}
            </ul>
          )}
          <div className="hero__action">
            <a className="btn btn--ghost" href="/dashboard" onClick={(e) => { e.preventDefault(); navigate('/dashboard') }}>See it on the dashboard</a>
          </div>
        </>
      )}
    </main>
  )
}
