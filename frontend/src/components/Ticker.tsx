// A live value that rolls: when a character changes, the new one slides up into place and the
// old one slides out, like an odometer. Tabular figures keep widths steady. Same props as the
// old flap component so pages swap it in place; reduced motion simply swaps the text.
import { memo } from 'react'

type Tone = 'signal' | 'amber' | 'green' | 'red' | undefined

const Char = memo(function Char({ ch }: { ch: string }) {
  // keying by the character remounts it on change, which replays the roll-in animation
  return (
    <span className="tick__cell">
      <span className="tick__ch" key={ch}>{ch === ' ' ? ' ' : ch}</span>
    </span>
  )
})

interface Props {
  value: string | number
  /** pad to a fixed width so the layout never jumps */
  width?: number
  align?: 'left' | 'right'
  tone?: Tone
  stagger?: number
  label?: string
  className?: string
}

export function Ticker({ value, width, align = 'left', tone, label, className = '' }: Props) {
  let text = String(value)
  if (width) text = align === 'right' ? text.padStart(width, ' ').slice(-width) : text.padEnd(width, ' ').slice(0, width)
  // padding spaces are trimmed visually but keep the slot count stable
  return (
    <span className={`tick${tone ? ` tick--${tone}` : ''} ${className}`} role="text" aria-label={label ?? String(value)}>
      {[...text].map((ch, i) => <Char key={i} ch={ch} />)}
    </span>
  )
}
