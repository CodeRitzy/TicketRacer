// Route table and the top rail. Every step of the flow has its own URL.
import { useEffect, useState, type MouseEvent } from 'react'
import { api, type EventConfig } from './api'
import { navigate, useLiveStatus, useRoute } from './lib/hooks'
import { Booking } from './pages/Booking'
import { Confirmation } from './pages/Confirmation'
import { Dashboard } from './pages/Dashboard'
import { DemoPay } from './pages/DemoPay'
import { Home } from './pages/Home'
import { Queue } from './pages/Queue'

export function App() {
  const url = useRoute()
  const live = useLiveStatus()
  const [config, setConfig] = useState<EventConfig | null>(null)
  useEffect(() => { api.config().then(setConfig, () => setConfig(null)) }, [])
  useEffect(() => { window.scrollTo(0, 0) }, [url.pathname])

  const path = url.pathname
  const holdMatch = path.match(/^\/hold\/([0-9a-f-]{36})$/)
  let page
  if (path === '/queue') page = <Queue />
  else if (path === '/seats') page = <Booking config={config} />
  else if (holdMatch) page = <Booking config={config} holdId={holdMatch[1]} />
  else if (path === '/pay/demo') page = <DemoPay holdId={url.searchParams.get('hold') ?? ''} />
  else if (path === '/confirmation') page = <Confirmation holdId={url.searchParams.get('hold') ?? ''} />
  else if (path === '/dashboard') page = <Dashboard />
  else page = <Home config={config} />

  // internal links use the history API instead of full reloads
  const link = (to: string) => ({
    href: to,
    'aria-current': path === to ? ('page' as const) : undefined,
    onClick: (e: MouseEvent) => { e.preventDefault(); navigate(to) },
  })

  return (
    <>
      <a className="sr-only" href="#main">Skip to content</a>
      <header className="rail">
        <a className="rail__mark" {...link('/')}>Ticket<span>racer</span></a>
        <nav className="rail__nav" aria-label="Main">
          {live && <span className="lamp rail__lamp" data-on="true" role="status">Live</span>}
          <a className="rail__home" {...link('/')}>Tonight</a>
          <a {...link('/seats')}>Seats</a>
          <a {...link('/dashboard')}>Dashboard</a>
        </nav>
      </header>
      {page}
    </>
  )
}
