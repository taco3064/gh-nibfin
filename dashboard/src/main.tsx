import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from '@/App'
import { applyCspNonce } from '@/lib/nonce'
import { applyTheme } from '@/lib/theme'
import '@/styles/theme.css'

applyCspNonce()
applyTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
