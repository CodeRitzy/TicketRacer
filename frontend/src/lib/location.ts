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

export function guessState(states: StateInfo[], zone: string): string {
  const has = (code: string) => states.some((s) => s.code === code)
  const preferred = PREFERRED[zone]
  if (preferred && has(preferred)) return preferred
  const prefix = PREFIXES.find(([p]) => zone.startsWith(p))
  if (prefix && has(prefix[1])) return prefix[1]
  return states.find((s) => s.zone === zone)?.code ?? 'NY'
}

const KEY = 'tr.state'

// rememberedState: the visitor's own choice wins over the guess (kept in localStorage only).
export function rememberedState(states: StateInfo[]): string {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved && states.some((s) => s.code === saved)) return saved
  } catch { /* storage blocked: fall through to the guess */ }
  return guessState(states, Intl.DateTimeFormat().resolvedOptions().timeZone)
}

export function rememberState(code: string) {
  try { localStorage.setItem(KEY, code) } catch { /* ignore */ }
}
