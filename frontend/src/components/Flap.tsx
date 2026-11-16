// Split-flap text: each character is a tile that clacks through the board's character drum
// to its new value. The signature interaction of the whole app.
import { memo, useEffect, useRef, useState } from 'react'
import { prefersReducedMotion } from '../lib/hooks'

// The drum orders a real Solari board turns through: numeric modules carry digits only.
const ALPHA = ' ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789:-.$/&'
const DIGITS = ' 0123456789'
const drumFor = (from: string, to: string) => (/^[ \d]$/.test(from) && /^[ \d]$/.test(to) ? DIGITS : ALPHA)
const STEP_MS = 55
const MAX_STEPS = 8 // cap the spin so long jumps settle within ~0.5 s

type Tone = 'signal' | 'amber' | 'green' | 'red' | undefined

const Cell = memo(function Cell({ ch, delay, tone }: { ch: string; delay: number; tone: Tone }) {
  const [shown, setShown] = useState(ch)
  const [prev, setPrev] = useState(ch) // the character whose top leaf is falling
  const [flipKey, setFlipKey] = useState(0)
  const shownRef = useRef(ch)

  useEffect(() => {
    if (ch === shownRef.current) return
    if (prefersReducedMotion()) {
      shownRef.current = ch
      setShown(ch)
      return
    }
    // walk forward through the drum from the current glyph toward the target
    const DRUM = drumFor(shownRef.current, ch)
    const from = Math.max(0, DRUM.indexOf(shownRef.current.toUpperCase()))
    const to = DRUM.indexOf(ch.toUpperCase())
    const distance = to < 0 ? 1 : (to - from + DRUM.length) % DRUM.length
    const steps = Math.min(distance, MAX_STEPS)
    let i = 0
    let timer: number
    const tick = () => {
      i++
      const next = i >= steps ? ch : DRUM[(from + Math.round((distance * i) / steps)) % DRUM.length]
      setPrev(shownRef.current)
      shownRef.current = next
      setShown(next)
      setFlipKey((k) => k + 1)
      if (i < steps) timer = window.setTimeout(tick, STEP_MS)
    }
    timer = window.setTimeout(tick, delay)
    return () => window.clearTimeout(timer)
  }, [ch, delay])

  const glyph = (c: string) => (c === ' ' ? ' ' : c)
  // the new character is already printed underneath; the old top leaf falls over the seam to reveal it
  return (
    <span className={`flap${tone ? ` flap--${tone}` : ''}`} aria-hidden="true">
      <span className="flap__glyph">{glyph(shown)}</span>
      {flipKey > 0 && (
        <span className="flap__leaf" key={flipKey}><span className="flap__glyph">{glyph(prev)}</span></span>
      )}
    </span>
  )
})

interface FlapProps {
  value: string | number
  /** pad to a fixed width so the board never reflows */
  width?: number
  align?: 'left' | 'right'
  tone?: Tone
  /** stagger the first flip by this many ms per tile (title entrance) */
  stagger?: number
  className?: string
  label?: string
}

export function Flap({ value, width, align = 'left', tone, stagger = 18, className = '', label }: FlapProps) {
  let text = String(value).toUpperCase()
  if (width) text = align === 'right' ? text.padStart(width, ' ').slice(-width) : text.padEnd(width, ' ').slice(0, width)
  return (
    <span className={`flaps ${className}`} role="text" aria-label={label ?? String(value)}>
      {[...text].map((ch, i) => (
        <Cell key={i} ch={ch} delay={i * stagger} tone={tone} />
      ))}
    </span>
  )
}

// FlapIntro starts blank and flips to its value once mounted: the board "arriving".
export function FlapIntro(props: FlapProps) {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    const t = window.setTimeout(() => setReady(true), 120)
    return () => window.clearTimeout(t)
  }, [])
  const blank = ' '.repeat(String(props.value).length)
  return <Flap {...props} value={ready ? props.value : blank} label={String(props.value)} />
}
