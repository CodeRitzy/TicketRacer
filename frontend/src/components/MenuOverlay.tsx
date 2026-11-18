// "Menu" pill that opens a full-screen frosted overlay with large navigation links.
// Escape and the close pill shut it; focus moves in on open and back to the pill on close.
import { useEffect, useRef, useState, type MouseEvent } from 'react'
import { session } from '../api'
import { navigate } from '../lib/hooks'
import { Arrow } from './Arrow'

export function MenuOverlay({ path }: { path: string }) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const first = useRef<HTMLAnchorElement>(null)
  const fan = session.fan()

  useEffect(() => {
    if (!open) return
    first.current?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])
  useEffect(() => { setOpen(false) }, [path]) // a navigation closes the menu

  const close = () => { setOpen(false); trigger.current?.focus() }
  const go = (to: string) => (e: MouseEvent) => { e.preventDefault(); setOpen(false); navigate(to) }

  const links: [string, string, string][] = [
    ['/', 'Shows near you', 'Six upcoming shows at real venues in your state'],
    ...(fan ? [[`/shows/${fan.show}`, 'Your show', 'Back to the show you joined'] as [string, string, string]] : []),
    [fan ? `/dashboard?show=${fan.show}` : '/dashboard', 'Dashboard', 'Live guarantees, the request pipeline and the traffic simulator'],
  ]

  return (
    <>
      <button ref={trigger} className="pill pill--glass pill--small" aria-expanded={open} aria-controls="site-menu" onClick={() => setOpen(true)}>
        <span className="burger" aria-hidden="true" /> Menu
      </button>
      {open && (
        <div id="site-menu" className="menu" role="dialog" aria-modal="true" aria-label="Site menu">
          <div className="menu__top">
            <button className="pill pill--glass pill--small" onClick={close}><span className="burger burger--x" aria-hidden="true" /> Close</button>
          </div>
          <div className="menu__body">
          <nav className="menu__nav" aria-label="Main">
            {links.map(([to, title, note], i) => (
              <a key={to} ref={i === 0 ? first : undefined} href={to} onClick={go(to)} aria-current={path === to.split('?')[0] ? 'page' : undefined}
                style={{ ['--i' as string]: i }}>
                <span className="menu__title">{title} <span className="menu__arrow"><Arrow /></span></span>
                <span className="menu__note">{note}</span>
              </a>
            ))}
          </nav>
          <aside className="menu__aside glass">
            <p className="menu__promise">No seat is ever sold twice.</p>
            <p className="dim">Every hold is decided by the database, abandoned holds go back on sale, and a payment confirmed five times still makes one order.</p>
          </aside>
          </div>
        </div>
      )}
    </>
  )
}
