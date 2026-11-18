// Typed client for the Ticketracer API. The queue token is the fan's identity for one show in this tab.
import type { StateInfo } from './lib/location'
import type { Seat } from './lib/seats'

export type VenueKind = 'club' | 'ballroom' | 'theater' | 'amphitheater' | 'arena'

export interface Venue {
  name: string
  city: string
  state: string
  capacity: number
  kind: VenueKind
}

export interface ShowSummary {
  id: number
  act: string
  starts_at: string
  available: number
  held: number
  sold: number
  min_price_cents: number
  venue: Venue
}

export interface SectionInfo { name: string; price_cents: number; seats: number; ga: boolean }

export interface ShowDetail {
  id: number
  act: string
  starts_at: string
  zone: string
  venue: Venue
  sections: SectionInfo[]
  hold_ttl_seconds: number
  max_seats: number
  payment_mode: 'demo' | 'stripe'
}

export interface QueueStatus { admitted: boolean; position: number; waiting: number }

export interface Hold {
  id: string
  show_id: number
  status: 'active' | 'paid' | 'expired' | 'cancelled'
  expires_at: string
  seats: Seat[]
  total_cents: number
}

export interface Order {
  id: string
  hold_id: string
  email: string
  amount_cents: number
  status: 'paid' | 'refund_pending' | 'refunded'
  session_ref: string
  created_at: string
  seats: Seat[] | null
}

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

// sessionStorage keeps the fan's token per tab, so two tabs are two fans (handy for demos).
// One journey at a time: joining another show replaces the token.
const FAN_KEY = 'tr.fan'
const ADMIN_KEY = 'tr.admin'
const read = (k: string) => { try { return sessionStorage.getItem(k) ?? '' } catch { return '' } }
const write = (k: string, v: string) => { try { v ? sessionStorage.setItem(k, v) : sessionStorage.removeItem(k) } catch { /* private mode */ } }

export const session = {
  fan: (): { show: number; token: string } | null => {
    try { return JSON.parse(read(FAN_KEY)) } catch { return null }
  },
  tokenFor: (show: number) => (session.fan()?.show === show ? session.fan()!.token : ''),
  setFan: (show: number, token: string) => write(FAN_KEY, JSON.stringify({ show, token })),
  admin: () => read(ADMIN_KEY),
  setAdmin: (t: string) => write(ADMIN_KEY, t),
}

async function request<T>(method: string, path: string, body?: unknown, admin = false): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = session.fan()?.token
  if (token) headers['X-Queue-Token'] = token
  if (admin && session.admin()) headers.Authorization = `Bearer ${session.admin()}`
  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? res.statusText)
  return data as T
}

const show = (id: number) => `/api/shows/${id}`

export const api = {
  states: () => request<StateInfo[]>('GET', '/api/states'),
  lineup: (state: string) => request<{ state: StateInfo; shows: ShowSummary[] }>('GET', `/api/shows?state=${encodeURIComponent(state)}`),
  show: (id: number) => request<ShowDetail>('GET', show(id)),
  join: (id: number) => request<{ token: string; status: QueueStatus }>('POST', `${show(id)}/queue/join`),
  queueStatus: (id: number) => request<QueueStatus>('GET', `${show(id)}/queue/status`),
  queueLength: (id: number) => request<{ waiting: number }>('GET', `${show(id)}/queue/length`),
  seats: (id: number) => request<Seat[]>('GET', `${show(id)}/seats`),
  hold: (id: number, seatIds: number[]) => request<Hold>('POST', `${show(id)}/holds`, { seat_ids: seatIds }),
  // general admission: a quantity of entries to a standing / lawn / pit section, no seat choice
  holdGA: (id: number, section: string, quantity: number) => request<Hold>('POST', `${show(id)}/holds`, { section, quantity }),
  getHold: (id: string) => request<Hold>('GET', `/api/holds/${id}`),
  cancelHold: (id: string) => request<void>('DELETE', `/api/holds/${id}`),
  checkout: (id: string) => request<{ url: string }>('POST', `/api/holds/${id}/checkout`),
  // session is the Stripe Checkout Session id from the return URL; it lets the server confirm without the webhook
  order: (id: string, session?: string) => request<{ order: Order; show_id: number } | { status: 'processing'; hold_status: Hold['status']; show_id: number }>('GET', `/api/holds/${id}/order${session ? `?session=${encodeURIComponent(session)}` : ''}`),
  demoPay: (id: string) => request<{ deliveries: string[] }>('POST', `/api/demo/pay/${id}`),
  stats: (showId: number) => request<AdminStats>('GET', `/api/admin/stats?show=${showId}`, undefined, true),
  reset: (showId: number) => request<void>('POST', `/api/admin/reset?show=${showId}`, undefined, true),
  regenerate: (state: string) => request<void>('POST', `/api/admin/lineup?state=${state}`, undefined, true),
  simulate: (cfg: SimConfig) => request<{ status: string }>('POST', '/api/admin/simulate', cfg, true),
}

export interface SimConfig {
  show_id: number
  users: number
  concurrency: number
  stampede: boolean
  abandon_pct: number
  duplicate_pct: number
  max_seats: number
  abandon_ttl: number
}

export interface DBStats {
  available: number
  held: number
  sold: number
  orders_paid: number
  orders_refunded: number
  revenue_cents: number
  webhook_events: number
  sold_without_paid_order: number
  duplicate_orders: number
  reconciled: boolean
}

export interface SimReport {
  config: SimConfig
  duration_sec: number
  requests: number
  requests_per_sec: number
  counts: Record<string, number>
  reserve_latency_ms: Record<string, number>
  invariants: DBStats | null
}

export interface AdminStats {
  show: number
  db: DBStats
  metrics: Record<string, number>
  waiting: number
  payment_mode: string
  time: string
  sim: { running: boolean; report: SimReport | null; progress?: Record<string, number> }
}

// formatShowDate renders a show's date and time in the venue's own time zone.
export function showWhen(iso: string, zone?: string) {
  const d = new Date(iso)
  const opts = zone ? { timeZone: zone } : {}
  return {
    day: d.toLocaleDateString('en-US', { weekday: 'short', ...opts }),
    date: d.toLocaleDateString('en-US', { month: 'short', day: '2-digit', ...opts }),
    time: d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false, ...opts }),
    long: d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', ...opts }),
  }
}
