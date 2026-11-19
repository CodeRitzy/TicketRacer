// The visitor's state, guessed from the browser's time zone. No geolocation prompt,
// no IP lookup, no network call: the guess never leaves the browser.

export interface StateInfo {
  code: string
  name: string
  zone: string
}

// A zone shared by several states maps to its most populous one.
const PREFERRED: Record<string, string> = {
  'America/New_York': 'NY',
  'America/Chicago': 'IL',
  'America/Denver': 'CO',
  'America/Los_Angeles': 'CA',
  'America/Phoenix': 'AZ',
  'America/Detroit': 'MI',
  'America/Anchorage': 'AK',
  'Pacific/Honolulu': 'HI',
  'America/Boise': 'ID',
}

const PREFIXES: [string, string][] = [
  ['America/Indiana/', 'IN'],
  ['America/Kentucky/', 'KY'],
  ['America/North_Dakota/', 'ND'],
]

// when the location can't be told (non-US or hidden time zone), the page opens on California
export const FALLBACK_STATE = 'CA'

// guessState maps a time zone to a state, or null when the zone isn't a US one we know.
export function guessState(states: StateInfo[], zone: string | undefined): string | null {
  if (!zone) return null
  const has = (code: string) => states.some((s) => s.code === code)
  const preferred = PREFERRED[zone]
  if (preferred && has(preferred)) return preferred
  const prefix = PREFIXES.find(([p]) => zone.startsWith(p))
  if (prefix && has(prefix[1])) return prefix[1]
  return states.find((s) => s.zone === zone)?.code ?? null
}

const KEY = 'tr.state'

// rememberedState: the visitor's own choice wins over the guess (kept in localStorage only).
// `fallback` is true when neither told us where they are, so the page can say so.
export function rememberedState(states: StateInfo[]): { code: string; fallback: boolean } {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved && states.some((s) => s.code === saved)) return { code: saved, fallback: false }
  } catch { /* storage blocked: fall through to the guess */ }
  let zone: string | undefined
  try { zone = Intl.DateTimeFormat().resolvedOptions().timeZone } catch { /* no Intl time zone support */ }
  const guess = guessState(states, zone)
  if (guess) return { code: guess, fallback: false }
  const fallback = states.some((s) => s.code === FALLBACK_STATE) ? FALLBACK_STATE : states[0]?.code ?? ''
  return { code: fallback, fallback: true }
}

export function rememberState(code: string) {
  try { localStorage.setItem(KEY, code) } catch { /* ignore */ }
}
