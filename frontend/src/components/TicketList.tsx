// Cart / receipt lines: GA tickets grouped per section, reserved seats one per line.
import { ticketLines } from '../lib/picker'
import { formatPrice, type Seat } from '../lib/seats'

export function TicketList({ seats, gaSections }: { seats: Seat[]; gaSections: Set<string> }) {
  return (
    <ul className="ticket-list">
      {ticketLines(seats, gaSections).map((l) => (
        <li key={l.key}>
          <b>{l.label}{l.count > 1 ? ` ×${l.count}` : ''}</b>
          <span>{formatPrice(l.priceCents)}</span>
        </li>
      ))}
    </ul>
  )
}
