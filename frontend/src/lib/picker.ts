// Seat-picker rules (pure, unit-tested): Best available and per-section availability.
import type { Seat } from './seats'

export interface SectionSummary {
  name: string
  priceCents: number
  total: number
  left: number
}

// sectionSummaries groups seats by section in layout order (nearest the stage first).
export function sectionSummaries(seats: Seat[]): SectionSummary[] {
  const map = new Map<string, SectionSummary>()
  for (const s of seats) {
    const sec = map.get(s.section) ?? { name: s.section, priceCents: s.price_cents, total: 0, left: 0 }
    sec.total++
    if (s.status === 'available') sec.left++
    map.set(s.section, sec)
  }
  return [...map.values()]
}

// bestAvailable finds `qty` side-by-side free seats, trying sections in the given order and rows
// front to back, and choosing the run closest to the centre of its row. Seats in `exclude` (the
// fan's current picks) are treated as taken. Returns null when no row has a long enough run.
export function bestAvailable(seats: Seat[], qty: number, sectionOrder: string[], exclude: number[] = []): Seat[] | null {
  const free = (s: Seat) => s.status === 'available' && !exclude.includes(s.id)
  for (const section of sectionOrder) {
    // rows keep layout order; seats within a row are ordered by number
    const rows = new Map<string, Seat[]>()
    for (const s of seats) if (s.section === section) rows.set(s.row, [...(rows.get(s.row) ?? []), s])
    for (const row of rows.values()) {
      const centre = (row.length - 1) / 2
      let best: Seat[] | null = null
      let bestDistance = Infinity
      for (let start = 0; start + qty <= row.length; start++) {
        const run = row.slice(start, start + qty)
        if (!run.every(free)) continue
        const distance = Math.abs(start + (qty - 1) / 2 - centre)
        if (distance < bestDistance) [best, bestDistance] = [run, distance]
      }
      if (best) return best
    }
  }
  return null
}

// remark speaks the departures board's own vocabulary for a show's status.
export function remark(left: number, held: number, total: number): { text: string; tone?: 'amber' | 'muted' } {
  if (!total) return { text: '' }
  if (left === 0 && held === 0) return { text: 'Sold out', tone: 'muted' }
  if (left === 0) return { text: 'All held', tone: 'amber' }
  if (left <= Math.max(20, total * 0.1)) return { text: 'Final seats', tone: 'amber' }
  return { text: 'On sale' }
}

export interface TicketLine { key: string; label: string; count: number; priceCents: number }

// ticketLines is what a cart or receipt lists: general-admission tickets grouped by section
// ("Pit · General admission ×2"), reserved seats one line each.
export function ticketLines(seats: Seat[], gaSections: Set<string>): TicketLine[] {
  const lines: TicketLine[] = []
  const ga = new Map<string, TicketLine>()
  for (const s of seats) {
    if (gaSections.has(s.section)) {
      const line = ga.get(s.section) ?? { key: `ga-${s.section}`, label: `${s.section} · General admission`, count: 0, priceCents: 0 }
      if (!ga.has(s.section)) { ga.set(s.section, line); lines.push(line) }
      line.count++
      line.priceCents += s.price_cents
    } else {
      lines.push({ key: `s-${s.id}`, label: `${s.section} · Row ${s.row} · Seat ${s.number}`, count: 1, priceCents: s.price_cents })
    }
  }
  return lines
}

export interface SectionBreakdown { name: string; total: number; available: number; held: number; sold: number }

// sectionBreakdown is the house at section level, for the dashboard (no per-seat drawing).
export function sectionBreakdown(seats: Seat[]): SectionBreakdown[] {
  const map = new Map<string, SectionBreakdown>()
  for (const s of seats) {
    const b = map.get(s.section) ?? { name: s.section, total: 0, available: 0, held: 0, sold: 0 }
    b.total++
    b[s.status]++
    map.set(s.section, b)
  }
  return [...map.values()]
}
