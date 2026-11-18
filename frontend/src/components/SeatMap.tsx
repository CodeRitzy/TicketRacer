// One section's seats as flap tiles. Arrow keys move between seats (roving tabindex),
// Enter/Space selects. Status is carried by fill, lamp and strike-through, never colour alone.
import { memo, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { formatPrice, groupRows, type Seat } from '../lib/seats'
import { Arrow } from './Arrow'

interface Props {
  seats: Seat[] // one section, in layout order
  selected: number[]
  /** seats in the fan's own hold: shown as selected, not clickable */
  locked?: boolean
  onToggle?: (seat: Seat) => void
}

const SeatTile = memo(function SeatTile({ seat, pressed, focusable, locked, onToggle, column }: {
  seat: Seat; pressed: boolean; focusable: boolean; locked: boolean; onToggle?: (s: Seat) => void; column?: number
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
      style={column ? { gridColumnStart: column } : undefined}
      aria-label={`${seat.section}, row ${seat.row}, seat ${seat.number}, ${formatPrice(seat.price_cents)}, ${state}`}
      onClick={() => !taken && !locked && onToggle?.(seat)}
    >
      <span className="seat__n" key={flip}>{seat.number}</span>
    </button>
  )
})

export function SeatMap({ seats, selected, locked = false, onToggle }: Props) {
  const rows = useMemo(() => groupRows(seats), [seats])
  const [focusId, setFocusId] = useState(0)
  const ref = useRef<HTMLDivElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const widest = Math.max(1, ...rows.map(([, r]) => r.length))

  // where each seat sits, for arrow-key moves across rows of any length
  const pos = useMemo(() => {
    const m = new Map<number, [number, number]>()
    rows.forEach(([, r], ri) => r.forEach((s, ci) => m.set(s.id, [ri, ci])))
    return m
  }, [rows])
  const current = pos.has(focusId) ? focusId : rows[0]?.[1][0]?.id ?? 0

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const moves: Record<string, [number, number]> = { ArrowLeft: [0, -1], ArrowRight: [0, 1], ArrowUp: [-1, 0], ArrowDown: [1, 0] }
    const m = moves[e.key]
    const at = pos.get(current)
    if (!m || !at) return
    e.preventDefault()
    const r = Math.min(rows.length - 1, Math.max(0, at[0] + m[0]))
    const row = rows[r][1]
    const next = row[Math.min(row.length - 1, Math.max(0, at[1] + m[1]))]
    setFocusId(next.id)
    ref.current?.querySelector<HTMLButtonElement>(`[data-id="${next.id}"]`)?.focus()
  }

  // on narrow screens start centred, where the best seats are
  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollLeft = (el.scrollWidth - el.clientWidth) / 2
  }, [rows.length])

  if (!seats.length) return <p className="dim">Loading seats…</p>

  return (
    <div>
      <p className="swipe-hint" aria-hidden="true"><Arrow dir="left" /> Swipe to see the whole section <Arrow /></p>
      <div className="seatmap-scroll" ref={scroller}>
        <div
          className="seatmap"
          ref={ref}
          role="group"
          aria-label={`${seats[0].section} seats. Use arrow keys to move between seats and Enter to select.`}
          onKeyDown={onKeyDown}
          onFocus={(e) => { const id = Number((e.target as HTMLElement).dataset.id); if (id) setFocusId(id) }}
          style={{ ['--cols' as string]: widest }}
        >
          {rows.map(([row, rowSeats]) => (
            <div className="seatrow" key={row} role="group" aria-label={`Row ${row}`}>
              <span className="seatrow__label" aria-hidden="true">{row}</span>
              {rowSeats.map((s, i) => (
                // a short row is centred under the full rows, as in a real house
                <SeatTile key={s.id} column={i === 0 && rowSeats.length < widest ? 2 + Math.floor((widest - rowSeats.length) / 2) : undefined} seat={s} pressed={selected.includes(s.id)} focusable={s.id === current}
                  locked={locked} onToggle={onToggle} />
              ))}
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
    </div>
  )
}
