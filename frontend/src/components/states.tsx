import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { AlertCircle, Loader2 } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useAuth } from '@/auth/useAuth'
import { errorMessage } from '@/lib/errors'

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
      <Loader2 className="size-4 animate-spin" />
      {label}
    </div>
  )
}

export function ErrorState({ error, title = 'Could not load this' }: { error: unknown; title?: string }) {
  return (
    <Alert variant="destructive" className="m-4 max-w-2xl">
      <AlertCircle />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{errorMessage(error)}</AlertDescription>
    </Alert>
  )
}

export function PageContainer({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  return (
    <div className={wide ? 'mx-auto w-full max-w-6xl p-6' : 'mx-auto w-full max-w-4xl p-6'}>{children}</div>
  )
}

/** Sends signed-out visitors to /login and back here afterwards. */
export function RequireAuth({ children, admin = false }: { children: ReactNode; admin?: boolean }) {
  const { state } = useAuth()
  const location = useLocation()
  if (state.status === 'loading') return <Loading />
  if (state.status === 'signedOut') {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }
  if (admin && state.user.role !== 'admin') {
    return <ErrorState error={new Error('This page needs the admin role.')} title="Not allowed" />
  }
  return children
}
