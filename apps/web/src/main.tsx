import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './i18n'
import './index.css'
import App from './App.tsx'
import { ErrorBoundary } from './components/ErrorBoundary'

const root = document.getElementById('root')
if (!root) {
  throw new Error('root element not found')
}

// Top-level boundary catches crashes in the provider/shell/login chrome that
// render outside the in-shell route boundary, so nothing white-screens.
createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>,
)
