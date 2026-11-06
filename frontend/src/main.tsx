import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from './App'
import { AuthProvider } from './hooks/useAuth'
import { I18nProvider } from './i18n'
import { AccountThemeProvider } from './theme/AccountThemeProvider'
import { WorkspaceProvider } from './workspace/WorkspaceProvider'
import { ApiError } from './api/client'
// Self-hosted so the app keeps working offline and in air-gapped installs; the
// variable axis also gives the fractional weights the tokens ask for (550, 650).
import '@fontsource-variable/inter'
import './index.css'
// The interface experiences: their token sets must land after the base sheet
// so a :root[data-layout='…'] block wins at equal specificity.
import './shells/shells.css'
// The public site. It reads the same tokens the shells define, so the marketing
// pages follow the visitor's palette without a second theme system.
import './site/site.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx will not fix itself, so retrying only delays the error reaching
      // the user. A 401 in particular must not be retried.
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false
        return failureCount < 2
      },
      staleTime: 15_000,
      refetchOnWindowFocus: false,
    },
  },
})

const rootElement = document.getElementById('root')
if (!rootElement) throw new Error('Root element #root is missing from index.html')

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <BrowserRouter>
          <AuthProvider>
            <AccountThemeProvider>
              <WorkspaceProvider>
                <App />
              </WorkspaceProvider>
            </AccountThemeProvider>
          </AuthProvider>
        </BrowserRouter>
      </I18nProvider>
    </QueryClientProvider>
  </StrictMode>,
)
