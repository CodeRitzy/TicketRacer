// Small React hooks: routing, live seats over SSE, a ticking clock and polling.
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { api } from '../api'
import { applyFrame, type Seat } from './seats'

// --- routing: every step owns a URL, and Back retraces the trail -------------------------

const subscribePath = (cb: () => void) => {
  window.addEventListener('popstate', cb)
  return () => window.removeEventListener('popstate', cb)
}

export function useRoute(): URL {
  const href = useSyncExternalStore(subscribePath, () => window.location.href)
  return new URL(href)
}

export function navigate(to: string, replace = false) {
  if (replace) history.replaceState(null, '', to)
  else history.pushState(null, '', to)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

// --- live status: the rail lamp reads whether any page's seat stream is connected ------------

let liveNow = false
const liveSubs = new Set<() => void>()
function setLiveGlobal(v: boolean) {
  if (v === liveNow) return
  liveNow = v
  liveSubs.forEach((cb) => cb())
}
export function useLiveStatus(): boolean {
  return useSyncExternalStore((cb) => { liveSubs.add(cb); return () => { liveSubs.delete(cb) } }, () => liveNow)
}

// --- live seats: one SSE stream; falls back to polling if the stream drops ---------------

export function useLiveSeats(): { seats: Seat[]; live: boolean; error: string } {
  const [seats, setSeats] = useState<Seat[]>([])
  const [live, setLiveLocal] = useState(false)
  const setLive = useCallback((v: boolean) => { setLiveLocal(v); setLiveGlobal(v) }, [])
  const [error, setError] = useState('')
  const seatsRef = useRef<Seat[]>([])

  useEffect(() => {
    let cancelled = false
    let es: EventSource | null = null
    let poll: number | undefined

    const update = (next: Seat[]) => {
      seatsRef.current = next
      setSeats(next)
    }
    const refetch = () =>
      api.seats().then((s) => !cancelled && update(s), () => !cancelled && setError('Seat map unavailable, retrying…'))

    refetch().then(() => {
      if (cancelled) return
      es = new EventSource('/api/seats/stream')
      es.onopen = () => { setLive(true); setError(''); window.clearInterval(poll) }
      es.onmessage = (e) => {
        const frame = (JSON.parse(e.data) as { s: string }).s
        const next = applyFrame(seatsRef.current, frame)
        if (next !== seatsRef.current) update(next)
      }
      // EventSource reconnects on its own; poll meanwhile so the map never freezes
      es.onerror = () => {
        setLive(false)
        window.clearInterval(poll)
        poll = window.setInterval(refetch, 2000)
      }
    })
    return () => { cancelled = true; es?.close(); window.clearInterval(poll); setLiveGlobal(false) }
  }, [setLive])

  return { seats, live, error }
}

// --- time ------------------------------------------------------------------------------------

export function useNow(intervalMs = 250): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(t)
  }, [intervalMs])
  return now
}

// usePoll calls fn every ms while enabled; fn returning true stops polling.
export function usePoll(fn: () => Promise<boolean | void>, ms: number, enabled = true) {
  const saved = useRef(fn)
  saved.current = fn
  const [tick, setTick] = useState(0)
  const restart = useCallback(() => setTick((t) => t + 1), [])
  useEffect(() => {
    if (!enabled) return
    let stop = false
    let timer: number | undefined
    const run = async () => {
      try {
        if ((await saved.current()) === true) stop = true
      } catch { /* the next tick retries */ }
      if (!stop) timer = window.setTimeout(run, ms)
    }
    run()
    return () => { stop = true; window.clearTimeout(timer) }
  }, [ms, enabled, tick])
  return restart
}

export const prefersReducedMotion = () =>
  typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
