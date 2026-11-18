// Venue overview: the stage (a hairline arc) and every section as a soft tinted band that fades
// as it sells, with its seats-left count updating live (plain text: animated content inside an
// SVG foreignObject repaints unreliably). Shapes follow the venue type: a fan for
// clubs, ballrooms and theaters, wide arcs for amphitheaters, a bowl around a floor for arenas.
import type { KeyboardEvent } from 'react'
import type { VenueKind } from '../api'
import type { SectionSummary } from '../lib/picker'
import { formatPrice } from '../lib/seats'

interface Props {
  kind: VenueKind
  sections: SectionSummary[]
  /** general-admission sections are labelled GA */
  gaSections?: Set<string>
  selected?: string
  onSelect?: (name: string) => void
}

type Pt = [number, number]
const SPAN: Record<VenueKind, number> = { club: 70, ballroom: 84, theater: 116, amphitheater: 156, arena: 260 }
const BAND = 72
const GAP = 8

// point at radius r and angle a (degrees, 0 = straight down from the stage)
const at = (c: Pt, r: number, a: number): Pt => [c[0] + r * Math.sin((a * Math.PI) / 180), c[1] + r * Math.cos((a * Math.PI) / 180)]
const f = (p: Pt) => `${p[0].toFixed(1)} ${p[1].toFixed(1)}`

// a curved band between radii ri..ro spanning the given angle, centred on "down"
function band(c: Pt, ri: number, ro: number, span: number): string {
  const h = span / 2
  const large = span > 180 ? 1 : 0
  return `M ${f(at(c, ro, -h))} A ${ro} ${ro} 0 ${large} 0 ${f(at(c, ro, h))} L ${f(at(c, ri, h))} A ${ri} ${ri} 0 ${large} 1 ${f(at(c, ri, -h))} Z`
}

interface Shape {
  name: string
  outline: string
  label: Pt
}

function layout(kind: VenueKind, names: string[]): { shapes: Shape[]; stage: { arc: string; label: Pt }; view: string } {
  if (kind === 'arena') {
    // floor in the middle, bowls wrap around it, open at the stage end
    const c: Pt = [400, 330]
    const shapes: Shape[] = [{
      name: names[0], outline: 'M 300 236 H 500 V 424 H 300 Z', label: [400, 330],
    }]
    names.slice(1).forEach((name, i) => {
      const ri = 172 + i * (BAND + GAP)
      const mid = ri + BAND / 2
      shapes.push({ name, outline: band(c, ri, ri + BAND, SPAN.arena), label: at(c, mid, 0) })
    })
    const outer = 172 + (names.length - 1) * (BAND + GAP)
    return {
      shapes, stage: { arc: 'M 320 214 Q 400 178 480 214', label: [400, 202] },
      view: `${400 - outer - 20} ${330 - outer - 10} ${2 * outer + 40} ${2 * outer + 30}`,
    }
  }
  const c: Pt = [400, 64]
  const span = SPAN[kind]
  const shapes = names.map((name, i) => {
    const ri = 92 + i * (BAND + GAP)
    const mid = ri + BAND / 2
    return { name, outline: band(c, ri, ri + BAND, span), label: at(c, mid, 0) }
  })
  const outer = 92 + names.length * (BAND + GAP)
  const half = outer * Math.sin(((span / 2) * Math.PI) / 180)
  return {
    shapes, stage: { arc: 'M 300 64 Q 400 92 500 64', label: [400, 52] },
    view: `${400 - half - 24} 0 ${2 * half + 48} ${64 + outer + 16}`,
  }
}

export function VenueMap({ kind, sections, gaSections, selected, onSelect }: Props) {
  const { shapes, stage, view } = layout(kind, sections.map((s) => s.name))
  const byName = new Map(sections.map((s) => [s.name, s]))
  const interactive = !!onSelect
  const key = (name: string) => (e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect?.(name) }
  }

  return (
    <svg className="vmap" viewBox={view} role={interactive ? 'group' : 'img'}
      aria-label={interactive ? 'Venue map: choose a section' : `Venue map: ${sections.map((s) => `${s.name} ${s.left} of ${s.total} left`).join(', ')}`}>
      <path className="vmap-stage" d={stage.arc} />
      <text className="vmap-stage-label" x={stage.label[0]} y={stage.label[1]} textAnchor="middle">STAGE</text>
      {shapes.map(({ name, outline, label }) => {
        const s = byName.get(name)!
        const ratio = s.total ? s.left / s.total : 0
        // a full section glows violet; it dims toward the dark ground as it sells
        const lit = Math.round(14 + ratio * 34)
        const desc = `${name}, ${formatPrice(s.priceCents)}, ${s.left === 0 ? 'sold out' : `${s.left} of ${s.total} left`}`
        return (
          <g key={name} className="vmap-section" data-selected={selected === name || undefined} data-soldout={s.left === 0 || undefined}
            {...(interactive
              ? { role: 'button', tabIndex: 0, 'aria-label': desc, 'aria-pressed': selected === name, onClick: () => onSelect?.(name), onKeyDown: key(name) }
              : { 'aria-hidden': true })}>
            <path className="vmap-lower" d={outline} style={s.left ? { fill: `color-mix(in srgb, var(--accent) ${lit}%, var(--surface))` } : undefined} />
            <path className="vmap-frame" d={outline} />
            <text x={label[0]} y={label[1] - 8} textAnchor="middle" className="vmap-name">{name}{gaSections?.has(name) ? ' · GA' : ''}</text>
            <foreignObject className="vmap-meta" x={label[0] - 110} y={label[1] + 2} width={220} height={30}>
              <div className="vmap-meta__row">
                {s.left === 0 ? <span className="vmap-meta__sold">Sold out</span> : (
                  <>
                    <span>{formatPrice(s.priceCents)}</span>
                    <b>{s.left}</b>
                    <span>left</span>
                  </>
                )}
              </div>
            </foreignObject>
          </g>
        )
      })}
    </svg>
  )
}
