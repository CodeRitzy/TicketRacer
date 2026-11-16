// Typed client for the Ticketracer API. The queue token is the fan's identity for this tab.
import type { Seat } from './lib/seats'

export interface EventConfig {
  event_name: string
  venue: string
  starts_at: string
  hold_ttl_seconds: number
  max_seats: number
  payment_mode: 'demo' | 'stripe'
}

export interface QueueStatus { admitted: boolean; position: number; waiting: number }

export interface Hold {
  id: string
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

// sessionStorage keeps the token per tab, so two tabs are two fans (handy for demos).
const TOKEN_KEY = 'tr.token'
const ADMIN_KEY = 'tr.admin'
const read = (k: string) => { try { return sessionStorage.getItem(k) ?? '' } catch { return '' } }
const write = (k: string, v: string) => { try { v ? sessionStorage.setItem(k, v) : sessionStorage.removeItem(k) } catch { /* private mode */ } }

export const session = {
  token: () => read(TOKEN_KEY),
  setToken: (t: string) => write(TOKEN_KEY, t),
  admin: () => read(ADMIN_KEY),
  setAdmin: (t: string) => write(ADMIN_KEY, t),
}

async function request<T>(method: string, path: string, body?: unknown, admin = false): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = session.token()
  if (token) headers['X-Queue-Token'] = token
  if (admin && session.admin()) headers.Authorization = `Bearer ${session.admin()}`
  const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? res.statusText)
  return data as T
}

export const api = {
  config: () => request<EventConfig>('GET', '/api/config'),
  join: () => request<{ token: string; status: QueueStatus }>('POST', '/api/queue/join'),
  queueStatus: () => request<QueueStatus>('GET', '/api/queue/status'),
  queueLength: () => request<{ waiting: number }>('GET', '/api/queue/length'),
  seats: () => request<Seat[]>('GET', '/api/seats'),
  hold: (seatIds: number[]) => request<Hold>('POST', '/api/holds', { seat_ids: seatIds }),
  getHold: (id: string) => request<Hold>('GET', `/api/holds/${id}`),
  cancelHold: (id: string) => request<void>('DELETE', `/api/holds/${id}`),
  checkout: (id: string) => request<{ url: string }>('POST', `/api/holds/${id}/checkout`),
  order: (id: string) => request<Order | { status: 'processing'; hold_status: Hold['status'] }>('GET', `/api/holds/${id}/order`),
  demoPay: (id: string) => request<{ deliveries: string[] }>('POST', `/api/demo/pay/${id}`),
  stats: () => request<AdminStats>('GET', '/api/admin/stats', undefined, true),
  reset: () => request<void>('POST', '/api/admin/reset', undefined, true),
  simulate: (cfg: SimConfig) => request<{ status: string }>('POST', '/api/admin/simulate', cfg, true),
}

export interface SimConfig {
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
  db: DBStats
  metrics: Record<string, number>
  waiting: number
  payment_mode: string
  time: string
  sim: { running: boolean; report: SimReport | null; progress?: Record<string, number> }
}
