// Confirmation: polls until the payment webhook has been processed, then flips the status in.
import { useState } from 'react'
import { api, showWhen, type Order, type ShowDetail } from '../api'
import { Arrow } from '../components/Arrow'
import { Ticker } from '../components/Ticker'
import { TicketList } from '../components/TicketList'
import { navigate, usePoll } from '../lib/hooks'
import { formatPrice } from '../lib/seats'

export function Confirmation({ holdId, session }: { holdId: string; session?: string }) {
  const [order, setOrder] = useState<Order | null>(null)
  const [attempts, setAttempts] = useState(0)
  const [missing, setMissing] = useState(false)
  const [show, setShow] = useState<ShowDetail | null>(null)

  // the fan can land here before Stripe's webhook does: keep asking (every 1 s, then every 5 s after
  // the first minute) until the order exists, so a late webhook still flips the page to CONFIRMED
  usePoll(async () => {
    setAttempts((a) => a + 1)
    try {
      const res = await api.order(holdId, session)
      if (!show) api.show(res.show_id).then(setShow, () => undefined)
      if ('order' in res) { setOrder(res.order); return true }
    } catch {
      setMissing(true)
      return true
    }
  }, attempts > 60 ? 5000 : 1000, !!holdId)

  const status = order?.status === 'paid' ? 'Confirmed' : order ? 'Refunded' : missing ? 'Not found' : 'Processing'
  // green is reserved for passing checks: a confirmed order is one; red only when something went wrong
  const tone = order?.status === 'paid' ? 'green' : order ? 'amber' : missing ? 'red' : 'amber'
  const when = show ? showWhen(show.starts_at, show.zone) : null

  return (
    <main id="main" className="ticket">
      <h1 className="ticket__status" aria-live="polite">
        <Ticker value={status} width={10} tone={tone} label={`Order ${status.toLowerCase()}`} />
      </h1>
      {when && show && <p className="venue-line"><b>{show.act}</b> · {show.venue.name}, {show.venue.city} · {when.long} · {when.time}</p>}

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
          {/* the order lands on its own glass ticket */}
          <div className="glass receipt">
            {order.seats && order.seats.length > 0 && (
              <TicketList seats={order.seats} gaSections={new Set((show?.sections ?? []).filter((s) => s.ga).map((s) => s.name))} />
            )}
            <div className="total"><span className="dim">Paid</span><b>{formatPrice(order.amount_cents)}</b></div>
            <dl className="kv">
              <dt>Order</dt><dd className="mono">{order.id}</dd>
              {order.email && <><dt>Receipt to</dt><dd>{order.email}</dd></>}
              <dt>Payment ref</dt><dd className="mono">{order.session_ref}</dd>
            </dl>
          </div>
          <div className="actions">
            <a className="pill pill--glass" href={`/dashboard${show ? `?show=${show.id}` : ''}`} onClick={(e) => { e.preventDefault(); navigate(`/dashboard${show ? `?show=${show.id}` : ''}`) }}>See it on the dashboard</a>
            <a className="pill pill--ink" href="/" onClick={(e) => { e.preventDefault(); navigate('/') }}>More shows near you <span className="pill__chip"><Arrow /></span></a>
          </div>
        </>
      )}
    </main>
  )
}
