import { describe, expect, it } from 'vitest'
import { bestAvailable, sectionSummaries } from './picker'
import { guessState } from './location'
import type { Seat } from './seats'

// a tiny venue: section A has two rows of 6, section B one row of 6
function venue(taken: number[] = []): Seat[] {
  const seats: Seat[] = []
  const add = (section: string, row: string, n: number, price: number) => {
    for (let i = 1; i <= n; i++) {
      const id = seats.length + 1
      seats.push({ id, section, row, number: i, price_cents: price, status: taken.includes(id) ? 'sold' : 'available' })
    }
  }
  add('A', 'A', 6, 10000)
  add('A', 'B', 6, 10000)
  add('B', 'A', 6, 5000)
  return seats
}

describe('bestAvailable', () => {
  it('picks contiguous seats nearest the centre of the front row', () => {
    expect(bestAvailable(venue(), 2, ['A', 'B'])?.map((s) => s.number)).toEqual([3, 4])
  })

  it('skips a row without a long enough run', () => {
    // row A of section A has seats 1,2,5,6 free: no run of 3, so row B is used
    const picked = bestAvailable(venue([3, 4]), 3, ['A', 'B'])
    expect(picked?.every((s) => s.row === 'B' && s.section === 'A')).toBe(true)
    expect(picked).toHaveLength(3)
  })

  it('follows the given section order (e.g. cheapest first)', () => {
    expect(bestAvailable(venue(), 2, ['B', 'A'])?.[0].section).toBe('B')
  })

  it('returns null when no row has enough contiguous seats', () => {
    expect(bestAvailable(venue([2, 4, 8, 10, 14, 16]), 3, ['A', 'B'])).toBeNull()
  })

  it('ignores the fan’s own selection when choosing', () => {
    expect(bestAvailable(venue(), 1, ['A'], [3, 4])?.[0].number).not.toBe(3)
  })
})

describe('sectionSummaries', () => {
  it('counts what is left per section in layout order', () => {
    expect(sectionSummaries(venue([1, 13]))).toEqual([
      { name: 'A', priceCents: 10000, total: 12, left: 11 },
      { name: 'B', priceCents: 5000, total: 6, left: 5 },
    ])
  })
})

describe('guessState', () => {
  const states = [
    { code: 'CO', name: 'Colorado', zone: 'America/Denver' },
    { code: 'MT', name: 'Montana', zone: 'America/Denver' },
    { code: 'IL', name: 'Illinois', zone: 'America/Chicago' },
    { code: 'NY', name: 'New York', zone: 'America/New_York' },
    { code: 'IN', name: 'Indiana', zone: 'America/Indiana/Indianapolis' },
  ]
  it('maps a shared zone to its most populous state', () => {
    expect(guessState(states, 'America/Denver')).toBe('CO')
    expect(guessState(states, 'America/Chicago')).toBe('IL')
  })
  it('handles sub-zones', () => {
    expect(guessState(states, 'America/Indiana/Knox')).toBe('IN')
  })
  it('returns null when the location is unknown, so the page can fall back to California', () => {
    expect(guessState(states, 'Europe/Paris')).toBeNull()
    expect(guessState(states, 'UTC')).toBeNull()
    expect(guessState(states, undefined)).toBeNull()
  })
})

describe('ticketLines', () => {
  it('groups general admission by section and lists reserved seats individually', async () => {
    const { ticketLines } = await import('./picker')
    const seats: Seat[] = [
      { id: 1, section: 'Pit', row: 'GA', number: 1, price_cents: 9000, status: 'held' },
      { id: 2, section: 'Pit', row: 'GA', number: 2, price_cents: 9000, status: 'held' },
      { id: 9, section: 'Balcony', row: 'C', number: 4, price_cents: 5000, status: 'held' },
    ]
    expect(ticketLines(seats, new Set(['Pit']))).toEqual([
      { key: 'ga-Pit', label: 'Pit · General admission', count: 2, priceCents: 18000 },
      { key: 's-9', label: 'Balcony · Row C · Seat 4', count: 1, priceCents: 5000 },
    ])
  })
})

describe('sectionBreakdown', () => {
  it('counts sold, held and available per section', async () => {
    const { sectionBreakdown } = await import('./picker')
    expect(sectionBreakdown(venue([1, 13]).map((s) => (s.id === 2 ? { ...s, status: 'held' as const } : s)))).toEqual([
      { name: 'A', total: 12, available: 10, held: 1, sold: 1 },
      { name: 'B', total: 6, available: 5, held: 0, sold: 1 },
    ])
  })
})
