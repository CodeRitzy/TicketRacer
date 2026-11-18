// Full-screen entry screen shown once per browser session on the home page. The fluid wake
// plays behind it; entering lifts the screen away like a curtain. Escape or the button enters.
import { useEffect, useRef, useState } from 'react'
import { Arrow } from './Arrow'

const KEY = 'tr.entered'
const seen = () => { try { return sessionStorage.getItem(KEY) === '1' } catch { return true } }

export function EntryGate() {
  const [open, setOpen] = useState(() => !seen())
  const [leaving, setLeaving] = useState(false)
  const button = useRef<HTMLButtonElement>(null)

  const enter = () => {
    try { sessionStorage.setItem(KEY, '1') } catch { /* private mode: show again next time */ }
    setLeaving(true)
    window.setTimeout(() => setOpen(false), 900)
  }

  useEffect(() => {
    if (!open) return
    button.current?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') enter() }
    window.addEventListener('keydown', onKey)
    document.documentElement.style.overflow = 'hidden'
    return () => { window.removeEventListener('keydown', onKey); document.documentElement.style.overflow = '' }
  }, [open])

  if (!open) return null
  return (
    <div className="gate" data-leaving={leaving || undefined} role="dialog" aria-modal="true" aria-labelledby="gate-title">
      <div className="gate__inner">
        <h1 id="gate-title" className="gate__mark">ticketracer</h1>
        <p className="gate__tag">Fair tickets when thousands of fans<br />race for a few hundred seats</p>
      </div>
      <div className="gate__actions">
        <button ref={button} className="pill pill--ink" onClick={enter}>
          Find shows near you <span className="pill__chip"><Arrow /></span>
        </button>
        <p className="gate__note">Demo site: fictional acts at real venues. Test payments only.</p>
      </div>
    </div>
  )
}
