// A cluster of glass cards floating in 3D that tilts toward the pointer, each card drifting by its
// own depth (parallax). The pointer position is eased in a rAF loop and written to CSS variables,
// so React never re-renders per frame. Reduced motion: the cluster holds still.
import { useEffect, useRef, type ReactNode } from 'react'
import { prefersReducedMotion } from '../lib/hooks'

export interface FloatCard {
  key: string
  /** position of the card's centre inside the cluster, in % of its box */
  x: number
  y: number
  /** depth: larger floats nearer and drifts more */
  z: number
  /** resting tilt in degrees, for an organic scatter */
  tilt?: number
  width?: number
  node: ReactNode
}

export function FloatCards({ cards, label }: { cards: FloatCard[]; label: string }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const target = { x: 0, y: 0 }
    const cur = { x: 0, y: 0 }
    const onMove = (e: PointerEvent) => {
      const r = el.getBoundingClientRect()
      // -1..1 relative to the cluster's centre, clamped so far-away pointers do not over-tilt
      target.x = Math.max(-1, Math.min(1, (e.clientX - (r.left + r.width / 2)) / (r.width / 1.2)))
      target.y = Math.max(-1, Math.min(1, (e.clientY - (r.top + r.height / 2)) / (r.height / 1.2)))
    }
    window.addEventListener('pointermove', onMove, { passive: true })
    let raf = 0
    const tick = (t: number) => {
      cur.x += (target.x - cur.x) * 0.06
      cur.y += (target.y - cur.y) * 0.06
      el.style.setProperty('--px', cur.x.toFixed(4))
      el.style.setProperty('--py', cur.y.toFixed(4))
      el.style.setProperty('--t', (t / 1000).toFixed(3))
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => { cancelAnimationFrame(raf); window.removeEventListener('pointermove', onMove) }
  }, [])

  return (
    <div className="float" ref={ref} role="group" aria-label={label}>
      <div className="float__stage">
        {cards.map((c, i) => (
          // --dof: cards set far back (z < 1) drift out of focus, like a camera's depth of field
          <div key={c.key} className="float__card glass"
            style={{ left: `${c.x}%`, top: `${c.y}%`, width: c.width, ['--z' as string]: c.z, ['--tilt' as string]: `${c.tilt ?? 0}deg`, ['--i' as string]: i,
              ['--dof' as string]: `${Math.max(0, (1 - c.z) * 3).toFixed(2)}px` }}>
            {c.node}
          </div>
        ))}
      </div>
    </div>
  )
}
