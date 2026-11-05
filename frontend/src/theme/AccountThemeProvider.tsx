import type { ReactNode } from 'react'
import { useAuth } from '../hooks/useAuth'
import { useT } from '../i18n'
import { ThemeProvider, useTheme } from './index'

function AppearanceBoundary({ children }: { children: ReactNode }) {
  const { status, signOut } = useAuth()
  const { ready, loadError, retry } = useTheme()
  const t = useT()
  if (status === 'authenticated' && !ready) {
    return (
      <>
      <main className="content" aria-busy={!loadError}>
        {loadError ? (
          <div role="alert" className="stack">
            <p>{t('appearance.loadError')}</p>
            <div className="row">
              <button type="button" className="btn btn--primary" onClick={retry}>{t('appearance.retry')}</button>
              <button type="button" className="btn btn--secondary" onClick={() => void signOut()}>{t('common.signOut')}</button>
            </div>
          </div>
        ) : <p role="status">{t('appearance.loading')}</p>}
      </main>
      {loadError ? children : null}
      </>
    )
  }
  return children
}

export function AccountThemeProvider({ children }: { children: ReactNode }) {
  const { status, session } = useAuth()
  const key = status === 'authenticated' && session
    ? `${session.user.id}:${session.organization.id}`
    : null
  return (
    <ThemeProvider account={{ status, key }}>
      <AppearanceBoundary>{children}</AppearanceBoundary>
    </ThemeProvider>
  )
}
