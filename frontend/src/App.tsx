// Route table and the shell: the fluid cursor wake behind every page, the entry screen on the
// home page, and a floating header with the menu pill. Every step of the flow has its own URL.
import { useEffect, type MouseEvent } from 'react'
import { EntryGate } from './components/EntryGate'
import { FluidTrail } from './components/FluidTrail'
import { MenuOverlay } from './components/MenuOverlay'
import { navigate, useLiveStatus, useRoute } from './lib/hooks'
import { Confirmation } from './pages/Confirmation'
import { Dashboard } from './pages/Dashboard'
import { DemoPay } from './pages/DemoPay'
import { Event } from './pages/Event'
import { Picker } from './pages/Picker'
import { Queue } from './pages/Queue'
import { Shows } from './pages/Shows'

export function App() {
  const url = useRoute()
  const live = useLiveStatus()
  useEffect(() => { window.scrollTo(0, 0) }, [url.pathname])

  // /shows/12, /shows/12/queue, /shows/12/seats, /hold/<uuid>
  const path = url.pathname
  const showMatch = path.match(/^\/shows\/(\d+)(\/queue|\/seats)?$/)
  const holdMatch = path.match(/^\/hold\/([0-9a-f-]{36})$/)
  let page
  if (showMatch) {
    const id = Number(showMatch[1])
    page = showMatch[2] === '/queue' ? <Queue key={id} showId={id} />
      : showMatch[2] === '/seats' ? <Picker key={id} showId={id} />
        : <Event key={id} showId={id} />
  } else if (holdMatch) page = <Picker key={holdMatch[1]} holdId={holdMatch[1]} />
  else if (path === '/pay/demo') page = <DemoPay holdId={url.searchParams.get('hold') ?? ''} />
  else if (path === '/confirmation') page = <Confirmation holdId={url.searchParams.get('hold') ?? ''} session={url.searchParams.get('session') ?? undefined} />
  else if (path === '/dashboard') page = <Dashboard showId={Number(url.searchParams.get('show')) || 0} />
  else page = <Shows />

  const home = (e: MouseEvent) => { e.preventDefault(); navigate('/') }

  return (
    <>
      {/* calmer away from home so the wake never competes with content */}
      <FluidTrail intensity={path === '/' ? 1 : 0.5} />
      <a className="sr-only" href="#main">Skip to content</a>
      <header className="rail">
        <div className="rail__left">
          <MenuOverlay path={path} />
          <a className="rail__mark" href="/" onClick={home}>ticketracer</a>
        </div>
        <div className="rail__right">
          {live && <span className="lamp" role="status">Live</span>}
          <a className="pill pill--glass pill--small rail__cta" href="/" onClick={home} aria-current={path === '/' ? 'page' : undefined}>Shows near you</a>
        </div>
      </header>
      <div className="page" key={path}>{page}</div>
      {path === '/' && <EntryGate />}
    </>
  )
}
