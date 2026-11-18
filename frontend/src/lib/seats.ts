// Pure seat-map rules shared by the booking page; no React, so they are unit-tested directly.

export type SeatStatus = 'available' | 'held' | 'sold'

export interface Seat {
  id: number
  section: string
  row: string
  number: number
  price_cents: number
  status: SeatStatus
}

const letters: Record<string, SeatStatus> = { a: 'available', h: 'held', s: 'sold' }

// toggleSeat applies one click to the current selection and explains any refusal.
export function toggleSeat(selected: number[], seat: Seat, max: number): { selected: number[]; error?: string } {
  if (selected.includes(seat.id)) return { selected: selected.filter((id) => id !== seat.id) }
  if (seat.status !== 'available') return { selected, error: 'That seat is no longer available.' }
  if (selected.length >= max) return { selected, error: `You can hold up to ${max} seats.` }
  return { selected: [...selected, seat.id] }
}

// applyFrame merges a live SSE frame ("aahs…", one letter per seat in id order) into the seats.
// It returns the same array when nothing changed, so React skips the re-render.
export function applyFrame(seats: Seat[], frame: string): Seat[] {
  if (frame.length !== seats.length) return seats
  let changed = false
  const next = seats.map((s, i) => {
    const status = letters[frame[i]] ?? s.status
    if (status === s.status) return s
    changed = true
    return { ...s, status }
  })
  return changed ? next : seats
}

// groupRows keeps API order (by id), which is row-major; pass one section's seats.
export function groupRows(seats: Seat[]): [string, Seat[]][] {
  const rows = new Map<string, Seat[]>()
  for (const s of seats) rows.set(s.row, [...(rows.get(s.row) ?? []), s])
  return [...rows]
}

export function formatCountdown(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

// one grouping rule for every number on screen
export const formatCount = (n: number) => n.toLocaleString('en-US')

export function formatPrice(cents: number): string {
  const digits = cents % 100 === 0 ? 0 : 2
  return `$${(cents / 100).toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })}`
}
