// Entry: self-hosted fonts (no third-party requests), global styles, then the app.
import '@fontsource-variable/big-shoulders-display'
import '@fontsource/barlow/400.css'
import '@fontsource/barlow/600.css'
import '@fontsource-variable/jetbrains-mono'
import './styles.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
