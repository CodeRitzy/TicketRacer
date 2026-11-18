// Live pipeline visualizer: every fan request drawn as a dot travelling the real path
// queue → admission → reserve transaction → hold → webhook → order, with the failure branches.
// Dots are spawned from counter deltas on each one-second stats tick, so the picture is the data.
import { useEffect, useRef } from 'react'
import type { AdminStats } from '../api'
import { prefersReducedMotion } from '../lib/hooks'
import { Ticker } from './Ticker'

type Pt = [number, number]
interface Node { id: string; x: number; y: number; label: string; tone?: string }
interface Edge { id: string; path: Pt[]; color: string }

const W = 1280
const H = 430
const TOP = 130
const LOW = 330

const NODES: Node[] = [
  { id: 'arrive', x: 112, y: TOP, label: 'Fans joined' },
  { id: 'queue', x: 288, y: TOP, label: 'In line now' },
  { id: 'admit', x: 464, y: TOP, label: 'Admitted' },
  { id: 'reserve', x: 640, y: TOP, label: 'Reserve attempts' },
  { id: 'hold', x: 816, y: TOP, label: 'Seats held now' },
  { id: 'webhook', x: 992, y: TOP, label: 'Webhooks in' },
  { id: 'order', x: 1168, y: TOP, label: 'Paid orders', tone: 'green' },
  { id: 'conflict', x: 640, y: LOW, label: '409 conflicts', tone: 'red' },
  { id: 'expired', x: 816, y: LOW, label: 'Seats released', tone: 'amber' },
  { id: 'duplicate', x: 992, y: LOW, label: 'Duplicates blocked', tone: 'green' },
  { id: 'refund', x: 1168, y: LOW, label: 'Refunded', tone: 'amber' },
]
// node boxes are 156 x 76; edges run border to border so dots never cross the numbers
const HW = 78
const HH = 38
const node = (id: string) => NODES.find((x) => x.id === id)!
const right = (id: string): Pt => [node(id).x + HW, node(id).y]
const left = (id: string): Pt => [node(id).x - HW, node(id).y]
const bottom = (id: string): Pt => [node(id).x, node(id).y + HH]
const top = (id: string): Pt => [node(id).x, node(id).y - HH - 34] // stop above the label over the box

// colours are the shared styles.css tokens, so the pipeline always matches the rest of the app
const C = { main: 'var(--ink)', red: 'var(--red)', amber: 'var(--amber)', green: 'var(--green)' }
const EDGES: Edge[] = [
  { id: 'arrive>queue', path: [right('arrive'), left('queue')], color: C.main },
  { id: 'queue>admit', path: [right('queue'), left('admit')], color: C.main },
  { id: 'admit>reserve', path: [right('admit'), left('reserve')], color: C.main },
  { id: 'reserve>hold', path: [right('reserve'), left('hold')], color: C.main },
  { id: 'reserve>conflict', path: [bottom('reserve'), top('conflict')], color: C.red },
  { id: 'hold>webhook', path: [right('hold'), left('webhook')], color: C.main },
  { id: 'hold>expired', path: [bottom('hold'), top('expired')], color: C.amber },
  { id: 'webhook>order', path: [right('webhook'), left('order')], color: C.green },
  { id: 'webhook>duplicate', path: [bottom('webhook'), top('duplicate')], color: C.green },
  { id: 'webhook>refund', path: [[1080, TOP], [1080, LOW], left('refund')], color: C.amber },
]

const LEGEND: [string, string][] = [
  [C.main, 'Request moving forward'],
  [C.red, 'Lost the race: seat already taken (409)'],
  [C.amber, 'Hold expired or payment refunded'],
  [C.green, 'Order created, or duplicate webhook absorbed'],
]

const m = (s: AdminStats, k: string) => s.metrics[k] ?? 0
const webhooks = (s: AdminStats) => m(s, 'webhook_paid') + m(s, 'webhook_duplicate') + m(s, 'webhook_refund') + m(s, 'webhook_unknown')

// node values straight from the stats payload
function values(s: AdminStats): Record<string, number> {
  return {
    arrive: m(s, 'joined'), queue: s.waiting, admit: m(s, 'admitted'),
    reserve: m(s, 'reserve_ok') + m(s, 'reserve_conflicts'), hold: s.db.held, webhook: webhooks(s),
    order: s.db.orders_paid, conflict: m(s, 'reserve_conflicts'), expired: m(s, 'seats_released_expired'),
    duplicate: m(s, 'webhook_duplicate'), refund: s.db.orders_refunded,
  }
}

// how many dots travel each edge this tick
function flows(prev: AdminStats, next: AdminStats): Record<string, number> {
  const d = (f: (s: AdminStats) => number) => Math.max(0, f(next) - f(prev))
  return {
    'arrive>queue': d((s) => m(s, 'joined')),
    'queue>admit': d((s) => m(s, 'admitted')),
    'admit>reserve': d((s) => m(s, 'reserve_ok') + m(s, 'reserve_conflicts')),
    'reserve>hold': d((s) => m(s, 'reserve_ok')),
    'reserve>conflict': d((s) => m(s, 'reserve_conflicts')),
    'hold>webhook': d(webhooks),
    'hold>expired': d((s) => m(s, 'seats_released_expired')),
    'webhook>order': d((s) => m(s, 'webhook_paid')),
    'webhook>duplicate': d((s) => m(s, 'webhook_duplicate')),
    'webhook>refund': d((s) => m(s, 'webhook_refund')),
  }
}

interface Dot { edge: Edge; start: number; dur: number }

function pointOn(path: Pt[], t: number): Pt {
  const lens = path.slice(1).map((p, i) => Math.hypot(p[0] - path[i][0], p[1] - path[i][1]))
  let d = t * lens.reduce((a, b) => a + b, 0)
  for (let i = 0; i < lens.length; i++) {
    if (d <= lens[i]) {
      const k = d / lens[i]
      return [path[i][0] + (path[i + 1][0] - path[i][0]) * k, path[i][1] + (path[i + 1][1] - path[i][1]) * k]
    }
    d -= lens[i]
  }
  return path[path.length - 1]
}

const MAX_DOTS_PER_EDGE = 36 // a burst of 900 requests still reads as a stream, not a smear

export function Pipeline({ stats }: { stats: AdminStats | null }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const dots = useRef<Dot[]>([])
  const prev = useRef<AdminStats | null>(null)
  const hot = useRef<Record<string, number>>({})

  // one crank: each stats tick spawns this second's dots, spread across the next second
  useEffect(() => {
    if (!stats) return
    if (prev.current && !prefersReducedMotion()) {
      const now = performance.now()
      for (const [id, n] of Object.entries(flows(prev.current, stats))) {
        const edge = EDGES.find((e) => e.id === id)!
        const count = Math.min(n, MAX_DOTS_PER_EDGE)
        if (count) hot.current[id] = now + 1200
        for (let i = 0; i < count; i++) dots.current.push({ edge, start: now + (i / count) * 950, dur: 850 + Math.random() * 250 })
      }
    }
    prev.current = stats
  }, [stats])

  // one rAF loop draws all dots on a canvas laid over the SVG
  useEffect(() => {
    const el = canvas.current
    if (!el) return
    const ctx = el.getContext('2d')!
    // a canvas can't read CSS variables: resolve each token ('var(--red)') to its value once
    const style = getComputedStyle(el)
    const resolved: Record<string, string> = {}
    const paint = (c: string) => (resolved[c] ??= style.getPropertyValue(c.slice(4, -1)).trim())
    let raf = 0
    const draw = (now: number) => {
      const { width, height } = el.getBoundingClientRect()
      const dpr = window.devicePixelRatio || 1
      if (el.width !== Math.round(width * dpr)) { el.width = Math.round(width * dpr); el.height = Math.round(height * dpr) }
      ctx.setTransform((el.width / W), 0, 0, (el.height / H), 0, 0)
      ctx.clearRect(0, 0, W, H)
      dots.current = dots.current.filter((d) => now < d.start + d.dur)
      for (const d of dots.current) {
        const t = (now - d.start) / d.dur
        if (t < 0) continue
        const eased = 1 - Math.pow(1 - t, 2)
        const [x, y] = pointOn(d.edge.path, eased)
        ctx.globalAlpha = t > 0.85 ? (1 - t) / 0.15 : 1
        ctx.fillStyle = paint(d.edge.color)
        ctx.beginPath()
        ctx.arc(x, y, 3.2, 0, Math.PI * 2)
        ctx.fill()
      }
      ctx.globalAlpha = 1
      raf = requestAnimationFrame(draw)
    }
    raf = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(raf)
  }, [])

  const v = stats ? values(stats) : null
  const now = performance.now()

  return (
    <>
    <figure className="pipeline-scroll" style={{ margin: 0 }}>
      <div className="pipeline">
        <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-labelledby="pipeline-title pipeline-desc">
          <title id="pipeline-title">Request pipeline</title>
          <desc id="pipeline-desc">
            {v ? `${v.arrive} fans joined, ${v.queue} in line, ${v.admit} admitted, ${v.reserve} reserve attempts with ${v.conflict} conflicts, ` +
              `${v.hold} seats held, ${v.webhook} webhooks with ${v.duplicate} duplicates blocked, ${v.order} paid orders, ${v.refund} refunded, ${v.expired} seats released by expiry.`
              : 'Loading.'}
          </desc>
          {EDGES.map((e) => (
            // token colours go through style: CSS variables aren't reliable in SVG presentation attributes
            <polyline key={e.id} points={e.path.map((p) => p.join(',')).join(' ')} fill="none"
              strokeOpacity={(hot.current[e.id] ?? 0) > now ? 0.55 : 1} strokeWidth={2}
              style={{ stroke: (hot.current[e.id] ?? 0) > now ? e.color : 'var(--hair-strong)', transition: 'stroke 400ms' }} />
          ))}
          <g transform={`translate(60 ${LOW - 52})`} aria-hidden="true">
            {LEGEND.map(([color, text], i) => (
              <g key={text} transform={`translate(0 ${i * 30})`}>
                <circle cx={6} cy={-5} r={5} style={{ fill: color }} />
                <text x={22} y={0} style={{ fill: 'var(--ink-3)', font: '400 15px "Geist Variable", sans-serif' }}>{text}</text>
              </g>
            ))}
          </g>
          {NODES.map((n) => (
            <g key={n.id} transform={`translate(${n.x - 78} ${n.y - 38})`}>
              <text x={78} y={-12} textAnchor="middle" style={{ fill: 'var(--ink-3)', font: '500 11px "Geist Mono Variable", monospace', letterSpacing: '0.08em', textTransform: 'uppercase' }}>{n.label}</text>
              <rect width={156} height={76} rx={18} style={{ fill: 'var(--surface)', stroke: 'var(--hair-strong)' }} />
              <text x={78} y={52} textAnchor="middle"
                style={{ fill: toneOf(n.tone), font: '300 36px "Geist Variable", sans-serif', fontVariantNumeric: 'tabular-nums' }}>
                {v ? v[n.id].toLocaleString('en-US') : '—'}
              </text>
            </g>
          ))}
        </svg>
        <canvas ref={canvas} aria-hidden="true" />
      </div>
    </figure>
    <Stack v={v} />
    </>
  )
}

// Phones: the same pipeline as a vertical board. Each stage's number is a flap, so every change
// still clacks; its failure branch sits on the line beneath it.
const STAGES: [string, string, string?, string?][] = [
  ['arrive', 'Fans joined'],
  ['queue', 'In line now'],
  ['admit', 'Admitted'],
  ['reserve', 'Reserve attempts', 'conflict', 'Lost the race (409)'],
  ['hold', 'Seats held now', 'expired', 'Released by expiry'],
  ['webhook', 'Webhooks in', 'duplicate', 'Duplicates blocked'],
  ['order', 'Paid orders', 'refund', 'Refunded'],
]

function Stack({ v }: { v: Record<string, number> | null }) {
  return (
    <ol className="pipeline-stack" aria-label="Request pipeline">
      {STAGES.map(([id, label, branch, branchLabel]) => (
        <li key={id}>
          <span>{label}</span>
          <Ticker value={v ? v[id].toLocaleString('en-US') : '-'} width={6} align="right" label={`${label}: ${v?.[id] ?? 0}`} />
          {branch && (
            <small>
              <span>{branchLabel}</span>
              <b style={{ color: toneOf(NODES.find((n) => n.id === branch)?.tone) }}>{v ? v[branch].toLocaleString('en-US') : '-'}</b>
            </small>
          )}
        </li>
      ))}
    </ol>
  )
}

const toneOf = (t?: string) => (t === 'red' ? C.red : t === 'amber' ? C.amber : t === 'green' ? C.green : C.main)
