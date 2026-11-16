// Authored arrow icon (one stroke weight), used instead of a Unicode glyph.
export function Arrow({ dir = 'right' }: { dir?: 'right' | 'left' }) {
  return (
    <svg width="18" height="12" viewBox="0 0 18 12" aria-hidden="true" focusable="false"
      style={{ transform: dir === 'left' ? 'scaleX(-1)' : undefined, flex: 'none' }}>
      <path d="M1 6h15M11 1.5 16 6l-5 4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="square" />
    </svg>
  )
}
