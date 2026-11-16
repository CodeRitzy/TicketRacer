// The live seat map: 10 rows x 20 flap tiles. Arrow keys move between seats (roving tabindex),
// Enter/Space selects. Status is carried by fill, lamp and strike-through, never colour alone.
import { memo, useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { formatPrice, groupRows, type Seat } from '../lib/seats'
import { Arrow } from './Arrow'

interface Props {
  seats: Seat[]
  selected: number[]
  /** seats in the fan's own hold: shown as selected, not clickable */
  locked?: boolean
  onToggle?: (seat: Seat) => void
}

const SeatTile = memo(function SeatTile({ seat, pressed, focusable, locked, onToggle }: {
  seat: Seat; pressed: boolean; focusable: boolean; locked: boolean; onToggle?: (s: Seat) => void
}) {
  // re-key the number on every status change so the flip animation replays
  const [flip, setFlip] = useState(0)
  const first = useRef(true)
  useEffect(() => {
    if (first.current) { first.current = false; return }
    setFlip((f) => f + 1)
  }, [seat.status, pressed])

  const taken = seat.status !== 'available' && !pressed
  const state = pressed ? 'selected' : seat.status
  return (
    <button
      type="button"
      className="seat"
      data-id={seat.id}
      data-status={seat.status}
      data-flip={flip > 0 ? '' : undefined}
      aria-pressed={pressed}
      aria-disabled={taken || locked || undefined}
      tabIndex={focusable ? 0 : -1}
      aria-label={`Row ${seat.row} seat ${seat.number}, ${formatPrice(seat.price_cents)}, ${state}`}
      onClick={() => !taken && !locked && onToggle?.(seat)}
    >
      <span className="seat__n" key={flip}>{seat.number}</span>
    </button>
  )
})

export function SeatMap({ seats, selected, locked = false, onToggle }: Props) {
  const [focusId, setFocusId] = useState<number>(1)
  const ref = useRef<HTMLDivElement>(null)
  const rows = groupRows(seats)
  const perRow = rows[0]?.[1].length ?? 20

  // arrow keys walk the grid; ids are row-major 1..n
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const moves: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -perRow, ArrowDown: perRow }
    const delta = moves[e.key]
    if (!delta) return
    e.preventDefault()
    const next = Math.min(seats.length, Math.max(1, focusId + delta))
    setFocusId(next)
    ref.current?.querySelector<HTMLButtonElement>(`[data-id="${next}"]`)?.focus()
  }

  // on narrow screens start centred under the stage, where the best seats are
  const scroller = useRef<HTMLDivElement>(null)
  const loaded = seats.length > 0
  useEffect(() => {
    const el = scroller.current
    if (el && loaded) el.scrollLeft = (el.scrollWidth - el.clientWidth) / 2
  }, [loaded])

  if (!seats.length) return <p className="dim">Loading the seat map…</p>

  return (
    <div>
      <div className="stage" aria-hidden="true">Stage</div>
      <p className="swipe-hint" aria-hidden="true"><Arrow dir="left" /> Swipe to see the whole house <Arrow /></p>
      <div className="seatmap-scroll" ref={scroller}>
        <div
          className="seatmap"
          ref={ref}
          role="group"
          aria-label="Seat map. Use arrow keys to move between seats and Enter to select."
          onKeyDown={onKeyDown}
          onFocus={(e) => { const id = Number((e.target as HTMLElement).dataset.id); if (id) setFocusId(id) }}
        >
          {rows.map(([row, rowSeats]) => (
            <div className="seatrow" key={row} role="group" aria-label={`Row ${row}`}>
              <span className="seatrow__label" aria-hidden="true">{row}</span>
              {rowSeats.map((s) => (
                <SeatTile key={s.id} seat={s} pressed={selected.includes(s.id)} focusable={s.id === focusId}
                  locked={locked} onToggle={onToggle} />
              ))}
              <span className="seatrow__label" aria-hidden="true">{row}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="legend" aria-hidden="true">
        <span><i className="seat" data-status="available"><span className="seat__n">7</span></i> Available</span>
        <span><i className="seat" aria-pressed="true"><span className="seat__n">7</span></i> Yours</span>
        <span><i className="seat" data-status="held"><span className="seat__n">7</span></i> Held by another fan</span>
        <span><i className="seat" data-status="sold"><span className="seat__n">7</span></i> Sold</span>
      </div>
      <div className="tiers">
        <span>Rows A–C {formatPrice(12000)}</span>
        <span>Rows D–F {formatPrice(8000)}</span>
        <span>Rows G–J {formatPrice(5000)}</span>
      </div>
    </div>
  )
}
