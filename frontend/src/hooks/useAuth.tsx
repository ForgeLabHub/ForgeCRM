import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { authApi } from '../api/auth'
import { api, csrfTokenStore } from '../api/client'
import type { MeResponse } from '../types/api'

export interface AuthState {
  /** Null while loading or when signed out. */
  session: MeResponse | null
  status: 'loading' | 'authenticated' | 'anonymous'
  /** True when the signed-in user holds the permission. */
  can: (permission: string) => boolean
  signIn: (email: string, password: string) => Promise<{ mfaRequired: boolean; ticket?: string }>
  completeMFA: (ticket: string, code: string) => Promise<void>
  signOut: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

/**
 * Holds the session for the whole application.
 *
 * The session is discovered from the server on mount rather than stored in the
 * browser, because the cookie is HttpOnly and unreadable by design. That means
 * a reload always costs one request, which is the correct trade: no credential
 * ever sits in JavaScript-accessible storage.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<MeResponse | null>(null)
  const [status, setStatus] = useState<AuthState['status']>('loading')

  const refresh = useCallback(async () => {
    try {
      const me = await authApi.me()
      setSession(me)
      setStatus('authenticated')
    } catch {
      setSession(null)
      csrfTokenStore.clear()
      setStatus('anonymous')
    }
  }, [])

  useEffect(() => {
    // A 401 anywhere means the session ended. Dropping to anonymous here keeps
    // the UI consistent instead of leaving stale data on screen.
    api.setUnauthorizedHandler(() => {
      setSession(null)
      setStatus('anonymous')
    })
    void refresh()
  }, [refresh])

  const signIn = useCallback(async (email: string, password: string) => {
    const result = await authApi.login(email, password)
    if (result.mfa_required) {
      return { mfaRequired: true, ticket: result.mfa_ticket }
    }
    await refresh()
    return { mfaRequired: false }
  }, [refresh])

  const completeMFA = useCallback(async (ticket: string, code: string) => {
    await authApi.verifyMFA(ticket, code)
    await refresh()
  }, [refresh])

  const signOut = useCallback(async () => {
    await authApi.logout()
    setSession(null)
    setStatus('anonymous')
  }, [])

  const can = useCallback(
    (permission: string) => session?.permissions.includes(permission) ?? false,
    [session],
  )

  const value = useMemo<AuthState>(
    () => ({ session, status, can, signIn, completeMFA, signOut, refresh }),
    [session, status, can, signIn, completeMFA, signOut, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside an AuthProvider')
  return ctx
}
