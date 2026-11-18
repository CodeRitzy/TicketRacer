import { describe, expect, it } from 'vitest'
import { applyFrame, formatCountdown, formatPrice, groupRows, toggleSeat, type Seat } from './seats'

const seat = (id: number, row: string, number: number, status: Seat['status'] = 'available'): Seat => ({
  id, section: 'Floor', row, number, price_cents: 5000, status,
})

describe('toggleSeat', () => {
  it('adds an available seat and removes it on second click', () => {
    const s = seat(1, 'A', 1)
    expect(toggleSeat([], s, 6)).toEqual({ selected: [1] })
    expect(toggleSeat([1], s, 6)).toEqual({ selected: [] })
  })

  it('refuses taken seats', () => {
    expect(toggleSeat([], seat(2, 'A', 2, 'held'), 6)).toEqual({ selected: [], error: 'That seat is no longer available.' })
    expect(toggleSeat([], seat(3, 'A', 3, 'sold'), 6).error).toBeDefined()
  })

  it('enforces the per-fan limit', () => {
    const r = toggleSeat([1, 2], seat(3, 'A', 3), 2)
    expect(r.selected).toEqual([1, 2])
    expect(r.error).toBe('You can hold up to 2 seats.')
  })
})

describe('applyFrame', () => {
  it('decodes one letter per seat in id order', () => {
    const seats = [seat(1, 'A', 1), seat(2, 'A', 2), seat(3, 'A', 3)]
    expect(applyFrame(seats, 'ahs').map((s) => s.status)).toEqual(['available', 'held', 'sold'])
  })

  it('returns the same array when nothing changed (no re-render)', () => {
    const seats = [seat(1, 'A', 1)]
    expect(applyFrame(seats, 'a')).toBe(seats)
  })

  it('ignores a frame for a different layout', () => {
    const seats = [seat(1, 'A', 1)]
    expect(applyFrame(seats, 'ah')).toBe(seats)
  })
})

describe('formatting', () => {
  it('formats a countdown and never goes negative', () => {
    expect(formatCountdown(600_000)).toBe('10:00')
    expect(formatCountdown(61_500)).toBe('01:01')
    expect(formatCountdown(-5)).toBe('00:00')
  })

  it('formats prices in dollars', () => {
    expect(formatPrice(12000)).toBe('$120')
    expect(formatPrice(8050)).toBe('$80.50')
    expect(formatPrice(1170000)).toBe('$11,700')
  })

  it('groups seats into rows in order', () => {
    const rows = groupRows([seat(1, 'A', 1), seat(2, 'A', 2), seat(21, 'B', 1)])
    expect(rows.map(([r, s]) => [r, s.length])).toEqual([['A', 2], ['B', 1]])
  })
})
