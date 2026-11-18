// Authored arrow icon (one stroke weight), used instead of a Unicode glyph.
export function Arrow({ dir = 'right' }: { dir?: 'right' | 'left' }) {
  return (
    <svg width="18" height="12" viewBox="0 0 18 12" aria-hidden="true" focusable="false"
      style={{ transform: dir === 'left' ? 'scaleX(-1)' : undefined, flex: 'none' }}>
      <path d="M1 6h15M11 1.5 16 6l-5 4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="square" />
    </svg>
  )
}

// Plus / minus for the ticket stepper, same stroke weight as the arrow.
export function PlusMinus({ plus }: { plus?: boolean }) {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true" focusable="false">
      <path d={plus ? 'M1 7h12M7 1v12' : 'M1 7h12'} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="square" />
    </svg>
  )
}
