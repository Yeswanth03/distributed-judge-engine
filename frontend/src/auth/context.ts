import { createContext } from 'react'
import type { User } from '@/lib/types'

export type AuthState =
  { status: 'loading'; user: null } | { status: 'signedOut'; user: null } | { status: 'signedIn'; user: User }

export interface AuthContextValue {
  state: AuthState
  login: (email: string, password: string) => Promise<void>
  register: (body: {
    email: string
    username: string
    password: string
    display_name?: string
  }) => Promise<void>
  completeOAuth: (loginCode: string) => Promise<void>
  logout: () => Promise<void>
  reloadUser: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)
