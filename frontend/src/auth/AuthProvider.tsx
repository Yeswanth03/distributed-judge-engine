import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { onAccessTokenChange, refreshSession, setAccessToken } from '@/lib/api'
import { auth, users } from '@/lib/endpoints'
import type { TokenResponse } from '@/lib/types'
import { AuthContext, type AuthContextValue, type AuthState } from './context'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading', user: null })
  const queryClient = useQueryClient()

  const signIn = useCallback(async (tokens: TokenResponse) => {
    setAccessToken(tokens.access_token)
    const user = tokens.user ?? (await users.me())
    setState({ status: 'signedIn', user })
  }, [])

  // On page load the access token is gone (memory only); the refresh cookie brings the session back.
  useEffect(() => {
    let cancelled = false
    refreshSession().then(async (tokens) => {
      if (cancelled) return
      if (!tokens) {
        setState({ status: 'signedOut', user: null })
        return
      }
      try {
        await signIn(tokens)
      } catch {
        setState({ status: 'signedOut', user: null })
      }
    })
    return () => {
      cancelled = true
    }
  }, [signIn])

  // A failed refresh anywhere (expired or revoked session) signs the user out.
  useEffect(
    () =>
      onAccessTokenChange((token) => {
        if (token === null)
          setState((s) => (s.status === 'signedIn' ? { status: 'signedOut', user: null } : s))
      }),
    [],
  )

  const value = useMemo<AuthContextValue>(
    () => ({
      state,
      login: async (email, password) => signIn(await auth.login(email, password)),
      register: async (body) => signIn(await auth.register(body)),
      completeOAuth: async (code) => signIn(await auth.exchange(code)),
      logout: async () => {
        try {
          await auth.logout()
        } finally {
          setAccessToken(null)
          queryClient.clear()
          setState({ status: 'signedOut', user: null })
        }
      },
      reloadUser: async () => {
        const user = await users.me()
        setState({ status: 'signedIn', user })
      },
    }),
    [state, signIn, queryClient],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
