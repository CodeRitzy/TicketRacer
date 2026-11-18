// The home page's centrepiece: the next show's real seats as a particle sculpture. Each dot is one
// seat, coloured by its live status. The sculpture turns slowly in 3D toward the pointer, dots
// scatter away from the cursor and spring back, and when `shape` changes (as the story scrolls)
// every dot flies to its place in the next formation: the house, the queue, a held block, a ticket.
// Drawn on one canvas with no per-frame React renders; paused while off screen or in a hidden tab.
// Reduced motion: no drift, no scatter, formations snap into place.
import { useEffect, useRef } from 'react'
import { prefersReducedMotion } from '../lib/hooks'
import type { SeatStatus } from '../lib/seats'

export type SwarmShape = 'house' | 'line' | 'hold' | 'ticket'
type V3 = [number, number, number]
type Tone = 'accent' | 'amber' | 'ink' | 'faint' | 'mine'

const PLACEHOLDER = 420 // dots to show before the seat stream arrives

// a stable pseudo-random 0..1 per index, so formations look organic but never jump between renders
const hash = (i: number) => ((Math.sin(i * 127.1 + 311.7) * 43758.5453) % 1 + 1) % 1

// ---------- formations: n points in a unit space (x, y in about -1..1, z is depth) ----------

// raked arcs fanning out from a stage at the top, like the amphitheatre seat maps
function house(n: number): V3[] {
  const rows = Math.max(6, Math.round(Math.sqrt(n / 2.4)))
  const radii = Array.from({ length: rows }, (_, r) => 0.34 + 0.66 * (r / (rows - 1)))
  const total = radii.reduce((a, b) => a + b, 0)
  const pts: V3[] = []
  radii.forEach((rr, r) => {
    // longer back rows get more seats; the last row takes whatever is left
    const count = r === rows - 1 ? n - pts.length : Math.round((n * rr) / total)
    for (let i = 0; i < count; i++) {
      const a = -1.15 + 2.3 * (count === 1 ? 0.5 : i / (count - 1))
      pts.push([rr * Math.sin(a), -0.62 + rr * Math.cos(a) * 0.95, -0.3 + 0.6 * (r / (rows - 1))])
    }
  })
  return pts.slice(0, n)
}

// a waiting-room queue snaking back and forth between barriers; index 0 is the front of the line
function line(n: number): V3[] {
  const lanes = 6
  return Array.from({ length: n }, (_, i) => {
    const s = i / n
    const lane = Math.floor(s * lanes), u = s * lanes - lane
    const x = (lane % 2 ? 1 : -1) * (0.85 - 1.7 * u)
    return [x + (hash(i) - 0.5) * 0.03, -0.62 + lane * 0.25 + (hash(i + 9) - 0.5) * 0.025, (hash(i + 3) - 0.5) * 0.12]
  })
}

// one block of seats, row by row; the two in the middle are "yours"
function hold(n: number): V3[] {
  const cols = Math.ceil(Math.sqrt(n * 1.9))
  const rows = Math.ceil(n / cols)
  return Array.from({ length: n }, (_, i) => {
    const c = i % cols, r = Math.floor(i / cols)
    return [-0.95 + 1.9 * (c / (cols - 1)), -0.55 + 1.1 * (r / Math.max(1, rows - 1)), Math.sin(c / cols * Math.PI) * 0.18]
  })
}
const mineOf = (n: number) => {
  const cols = Math.ceil(Math.sqrt(n * 1.9))
  const mid = Math.floor(Math.ceil(n / cols) / 2) * cols + Math.floor(cols / 2)
  return new Set([mid - 1, mid])
}

// m points spaced evenly along a polyline
function along(poly: [number, number][], m: number): V3[] {
  const seg = poly.slice(1).map((p, k) => Math.hypot(p[0] - poly[k][0], p[1] - poly[k][1]))
  const total = seg.reduce((a, b) => a + b, 0)
  const out: V3[] = []
  let k = 0, acc = 0
  for (let j = 0; j < m; j++) {
    const d = (j / m) * total
    while (k < seg.length - 1 && acc + seg[k] < d) acc += seg[k++]
    const u = seg[k] ? (d - acc) / seg[k] : 0
    out.push([poly[k][0] + (poly[k + 1][0] - poly[k][0]) * u, poly[k][1] + (poly[k + 1][1] - poly[k][1]) * u, 0])
  }
  return out
}

// a ticket drawn in dots: the outline with a notch bitten from each edge, and a dotted perforation
const PERF = 0.42
function ticket(n: number): V3[] {
  const w = 0.95, h = 0.52, notch = 0.1
  const bite = (cy: number, a0: number, a1: number): [number, number][] =>
    Array.from({ length: 9 }, (_, k) => { const a = a0 + ((a1 - a0) * k) / 8; return [PERF + notch * Math.cos(a), cy + notch * Math.sin(a)] })
  const outline: [number, number][] = [
    [-w, -h], ...bite(-h, Math.PI, 0), [w, -h], [w, h], ...bite(h, 0, -Math.PI), [-w, h], [-w, -h],
  ]
  // share dots by length, so the perforation is as evenly spaced as the outline
  const perfLen = 2 * (h - notch), edgeLen = 4 * (w + h) + 2 * notch * (Math.PI - 2)
  const edge = Math.round((n * edgeLen) / (edgeLen + perfLen))
  return [...along(outline, edge), ...along([[PERF, -h + notch], [PERF, h - notch]], n - edge)]
}

const FORMS: Record<SwarmShape, (n: number) => V3[]> = { house, line, hold, ticket }

// what colour each dot takes in each formation
function toneOf(shape: SwarmShape, i: number, status: SeatStatus | undefined, mine: Set<number>, pos: V3): Tone {
  if (shape === 'house') return status === 'sold' ? 'faint' : status === 'held' ? 'amber' : 'accent'
  if (shape === 'line') return i < 10 ? 'accent' : 'ink'                       // the front is being let in
  if (shape === 'hold') return mine.has(i) ? 'mine' : hash(i + 2) < 0.12 ? 'amber' : 'faint'
  return pos[0] >= PERF - 0.001 ? 'ink' : 'accent'                             // ticket body; stub and perforation
}

export function SeatSwarm({ statuses, shape }: { statuses: SeatStatus[]; shape: SwarmShape }) {
  const ref = useRef<HTMLCanvasElement>(null)
  // the animation loop reads the latest props from here, so prop changes never restart it
  const props = useRef({ statuses, shape })
  props.current = { statuses, shape }

  useEffect(() => {
    const canvas = ref.current!
    const ctx = canvas.getContext('2d')!
    const still = prefersReducedMotion()

    // palette from the styles.css tokens, as rgb triples
    const css = getComputedStyle(canvas)
    const rgb = (name: string): V3 => {
      const hex = css.getPropertyValue(name).trim().replace('#', '')
      return [0, 2, 4].map((k) => parseInt(hex.slice(k, k + 2), 16)) as V3
    }
    const accent = rgb('--accent'), amber = rgb('--amber'), ink = rgb('--ink')
    const PAINT: Record<Tone, [V3, number]> = {
      accent: [accent, 0.85], amber: [amber, 0.95], ink: [ink, 0.55], faint: [ink, 0.14], mine: [accent, 1],
    }

    // particle state, rebuilt whenever the count or the formation changes
    let n = 0, form: SwarmShape | '' = ''
    let pos: V3[] = [], target: V3[] = [], col: number[][] = [], tone: Tone[] = []
    let off: number[][] = [], vel: number[][] = [], start: number[] = []
    let mine = new Set<number>()
    const rebuild = (now: number) => {
      const { statuses: st, shape: sh } = props.current
      const count = st.length || PLACEHOLDER
      if (count !== n) {
        // new seat count: start every dot scattered softly around the centre
        n = count
        pos = Array.from({ length: n }, (_, i) => [(hash(i) - 0.5) * 2.4, (hash(i + 1) - 0.5) * 2, (hash(i + 2) - 0.5)])
        col = Array.from({ length: n }, () => [...accent, 0])
        off = Array.from({ length: n }, () => [0, 0]); vel = Array.from({ length: n }, () => [0, 0])
        form = ''
      }
      if (sh !== form) {
        form = sh
        target = FORMS[sh](n)
        mine = mineOf(n)
        // dots leave in a sweep so the morph reads as a wave, not a jump
        start = Array.from({ length: n }, (_, i) => now + (still ? 0 : (i / n) * 420 + hash(i) * 180))
        if (still) pos = target.map((p) => [...p] as V3)
      }
      tone = Array.from({ length: n }, (_, i) => toneOf(sh, i, st[i], mine, target[i]))
    }

    let w = 0, h = 0
    const dpr = Math.min(2, window.devicePixelRatio || 1)
    const resize = () => {
      const r = canvas.getBoundingClientRect()
      w = r.width; h = r.height
      canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr)
    }
    resize()
    const ro = new ResizeObserver(resize)
    ro.observe(canvas)

    // the pointer, eased, as -1..1 around the canvas centre (for the turn) and in canvas px (for scatter)
    const pointer = { x: -1e4, y: -1e4, tx: 0, ty: 0, cx: 0, cy: 0 }
    const onMove = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect()
      pointer.x = e.clientX - r.left; pointer.y = e.clientY - r.top
      pointer.tx = Math.max(-1, Math.min(1, (pointer.x - r.width / 2) / (r.width / 2)))
      pointer.ty = Math.max(-1, Math.min(1, (pointer.y - r.height / 2) / (r.height / 2)))
    }
    const onLeave = () => { pointer.x = pointer.y = -1e4 }
    if (!still) {
      window.addEventListener('pointermove', onMove, { passive: true })
      document.addEventListener('pointerleave', onLeave)
    }

    let raf = 0, visible = true
    const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; if (visible) loop() })
    io.observe(canvas)
    const onVis = () => { if (!document.hidden) loop() }
    document.addEventListener('visibilitychange', onVis)
    const loop = () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(frame) }

    function frame(t: number) {
      if (!visible || document.hidden) return
      rebuild(t)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, w, h)

      // a slow turn toward the pointer plus a gentle idle sway
      pointer.cx += (pointer.tx - pointer.cx) * 0.05
      pointer.cy += (pointer.ty - pointer.cy) * 0.05
      const ry = still ? 0 : pointer.cx * 0.45 + Math.sin(t / 5200) * 0.12
      const rx = 0.32 + (still ? 0 : pointer.cy * 0.18)
      const sy = Math.sin(ry), cy = Math.cos(ry), sx = Math.sin(rx), cx = Math.cos(rx)
      const scale = Math.min(w * 0.42, h * 0.6)
      const ox = w / 2, oy = h / 2
      // small houses get bigger dots so the sculpture still reads as solid; small screens cap them
      const dot = Math.min(2.3 * Math.min(1.6, Math.max(1, Math.sqrt(500 / Math.max(1, n)))), scale * 0.014)

      // the stage, a faint arc just above the front rows
      if (form === 'house') {
        ctx.strokeStyle = `rgba(${ink.join(',')}, 0.18)`
        ctx.lineWidth = 1.5
        ctx.beginPath()
        ctx.ellipse(ox, oy - scale * 0.52 * cx, scale * 0.3, scale * 0.06, 0, 0.15 * Math.PI, 0.85 * Math.PI)
        ctx.stroke()
      }

      for (let i = 0; i < n; i++) {
        // move toward the formation once this dot's turn in the sweep has come
        if (t >= start[i]) {
          const p = pos[i], q = target[i]
          p[0] += (q[0] - p[0]) * 0.075; p[1] += (q[1] - p[1]) * 0.075; p[2] += (q[2] - p[2]) * 0.075
        }
        // rotate (y then x) and project with a soft perspective
        const [x, y, z] = pos[i]
        const x1 = x * cy + z * sy, z1 = -x * sy + z * cy
        const y2 = y * cx - z1 * sx, z2 = y * sx + z1 * cx
        const persp = 3 / (3 - z2)
        let px = ox + x1 * scale * persp, py = oy + y2 * scale * persp

        // scatter: dots near the cursor are pushed away and spring back
        const o = off[i], v = vel[i]
        if (!still) {
          const dx = px + o[0] - pointer.x, dy = py + o[1] - pointer.y, d = Math.hypot(dx, dy)
          if (d < 110 && d > 0.01) { const f = (1 - d / 110) ** 2 * 7; v[0] += (dx / d) * f; v[1] += (dy / d) * f }
          v[0] = (v[0] - o[0] * 0.05) * 0.84; v[1] = (v[1] - o[1] * 0.05) * 0.84
          o[0] += v[0]; o[1] += v[1]
        }
        px += o[0]; py += o[1]

        // ease the colour toward this dot's tone, so status changes and morphs fade rather than flash
        const [rgbT, aT] = PAINT[tone[i]]
        const c = col[i]
        for (let k = 0; k < 3; k++) c[k] += (rgbT[k] - c[k]) * 0.08
        c[3] += (aT - c[3]) * 0.06
        // nearer dots are larger and stronger: depth without a blur pass
        const depth = Math.max(0.35, Math.min(1.25, persp))
        const r = (tone[i] === 'mine' ? 5.5 : dot) * depth
        ctx.fillStyle = `rgba(${c[0] | 0}, ${c[1] | 0}, ${c[2] | 0}, ${(c[3] * Math.min(1, depth)).toFixed(3)})`
        ctx.beginPath()
        ctx.arc(px, py, r, 0, Math.PI * 2)
        ctx.fill()
        if (tone[i] === 'mine') {
          // your seats get a halo
          ctx.strokeStyle = `rgba(${accent.join(',')}, ${(0.25 * c[3]).toFixed(3)})`
          ctx.lineWidth = 6
          ctx.stroke()
        }
      }
      raf = requestAnimationFrame(frame)
    }
    loop()

    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect(); io.disconnect()
      window.removeEventListener('pointermove', onMove)
      document.removeEventListener('pointerleave', onLeave)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [])

  return <canvas ref={ref} className="swarm" aria-hidden="true" />
}
