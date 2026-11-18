// Entry: self-hosted fonts (no third-party requests), global styles, then the app.
import '@fontsource-variable/geist'

import '@fontsource-variable/geist-mono'
// italic serif for single accent words in the big headings
import '@fontsource/bodoni-moda/latin-400-italic.css'
import './styles.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
