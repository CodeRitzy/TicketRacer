// The cursor's fluid wake. Each frame is cleared and redrawn from a short history of the eased
// pointer, so nothing ghosts: older points fade, widen and stretch along their direction of
// travel, with a gentle sideways curl, which is what reads as fluid. A quarter-resolution canvas
// blurred by CSS keeps it cheap. Off the home page the wake fades away when the mouse rests.
// The ambient violet/blue field behind it is CSS (styles.css body). Reduced motion: no wake.
import { useEffect, useRef } from 'react'
import { prefersReducedMotion } from '../lib/hooks'

const SCALE = 0.25 // draw at a quarter of the screen size; the blur hides the low resolution
const TRAIL = 36   // recent pointer samples that make up the wake

export function FluidTrail({ intensity = 1 }: { intensity?: number }) {
  const ref = useRef<HTMLCanvasElement>(null)
  const strength = useRef(intensity)
  strength.current = intensity

  useEffect(() => {
    const canvas = ref.current!
    const ctx = canvas.getContext('2d')!
    let w = 0, h = 0
    const resize = () => {
      w = canvas.width = Math.ceil(window.innerWidth * SCALE)
      h = canvas.height = Math.ceil(window.innerHeight * SCALE)
    }
    resize()
    window.addEventListener('resize', resize)

    // a soft blob, optionally stretched along an angle
    const blob = (x: number, y: number, r: number, rgb: string, a: number, angle = 0, stretch = 1) => {
      ctx.save()
      ctx.translate(x, y)
      ctx.rotate(angle)
      ctx.scale(stretch, 1)
      const g = ctx.createRadialGradient(0, 0, 0, 0, 0, r)
      g.addColorStop(0, `rgba(${rgb}, ${a})`)
      g.addColorStop(1, `rgba(${rgb}, 0)`)
      ctx.fillStyle = g
      ctx.beginPath()
      ctx.arc(0, 0, r, 0, Math.PI * 2)
      ctx.fill()
      ctx.restore()
    }
    // the ambient field lives in CSS (full-resolution body gradients), so the canvas only carries
    // the wake; reduced motion keeps the field and skips the wake entirely
    if (prefersReducedMotion()) return () => window.removeEventListener('resize', resize)

    const target = { x: w * 0.7, y: h * 0.45 }
    const head = { ...target }
    const trail: { x: number; y: number }[] = []
    let moved = -Infinity // no movement yet: start at rest, so a fresh page shows no wake
    let presence = 0 // 0..1: how visible the wake is right now (fades out at rest off home)
    const onMove = (e: PointerEvent) => {
      target.x = e.clientX * SCALE
      target.y = e.clientY * SCALE
      moved = performance.now()
    }
    window.addEventListener('pointermove', onMove, { passive: true })

    let raf = 0
    let hidden = false
    const onVis = () => { hidden = document.hidden; if (!hidden) raf = requestAnimationFrame(frame) }
    document.addEventListener('visibilitychange', onVis)

    function frame(t: number) {
      if (hidden) return
      ctx.clearRect(0, 0, w, h)

      // idle: on home a slow figure-eight keeps the page breathing; elsewhere the wake fades away
      // so it never parks beside content
      const idle = t - moved > 2500
      const home = strength.current >= 1
      presence += ((idle && !home ? 0 : 1) - presence) * 0.04
      if (idle && home) {
        target.x = w * (0.62 + 0.2 * Math.sin(t / 3400))
        target.y = h * (0.46 + 0.18 * Math.sin(t / 2500) * Math.cos(t / 4300))
      }
      head.x += (target.x - head.x) * 0.09
      head.y += (target.y - head.y) * 0.09
      trail.unshift({ x: head.x, y: head.y })
      if (trail.length > TRAIL) trail.pop()

      // draw oldest first so the newest sits on top
      const k = strength.current * presence * (idle ? 0.55 : 1)
      for (let i = trail.length - 1; i > 0; i--) {
        const p = trail[i], q = trail[i - 1]
        const age = i / TRAIL                       // 0 new .. 1 old
        const dx = q.x - p.x, dy = q.y - p.y
        const speed = Math.min(1, Math.hypot(dx, dy) / 5)
        const angle = Math.atan2(dy, dx)
        // a sideways curl that grows with age
        const curl = Math.sin(t / 700 + i * 0.35) * age * w * 0.012
        const x = p.x - Math.sin(angle) * curl, y = p.y + Math.cos(angle) * curl
        const r = w * (0.035 + 0.06 * age + 0.03 * speed)
        const a = (1 - age) * (0.16 + 0.22 * speed) * k
        // on the dark ground the wake is a violet glow with a cooler blue halo
        const violet = `${120 + Math.round(30 * Math.sin(t / 2100 + i * 0.1))}, 96, 255`
        blob(x, y, r * 1.5, '60, 110, 255', a * 0.55, angle, 1 + speed * 1.6)
        blob(x, y, r, violet, a, angle, 1 + speed * 1.2)
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)

    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      window.removeEventListener('pointermove', onMove)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [])

  return <canvas ref={ref} className="fluid" aria-hidden="true" />
}
