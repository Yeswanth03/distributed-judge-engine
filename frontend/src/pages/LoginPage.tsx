import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, Navigate, useLocation, useNavigate, useSearchParams } from 'react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Loading } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { OAUTH_ERRORS } from '@/lib/oauth'
import { AuthCard, OAuthButtons } from './auth-shared'

export default function LoginPage() {
  const { state, login, completeOAuth } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [params, setParams] = useSearchParams()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const exchanged = useRef(false)

  const from = (location.state as { from?: string } | null)?.from ?? '/problems'
  const loginCode = params.get('login_code')
  const loginError = params.get('login_error')

  // Back from Google/GitHub: trade the one-minute, single-use code for tokens (once, even in StrictMode).
  useEffect(() => {
    if (!loginCode || exchanged.current) return
    exchanged.current = true
    completeOAuth(loginCode)
      .then(() => navigate('/problems', { replace: true }))
      .catch((e) => {
        setError(errorMessage(e))
        setParams({}, { replace: true })
      })
  }, [loginCode, completeOAuth, navigate, setParams])

  if (state.status === 'signedIn' && !loginCode) return <Navigate to={from} replace />
  if (loginCode && !error) return <Loading label="Signing you in…" />

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(email, password)
      navigate(from, { replace: true })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const shownError = error ?? (loginError ? (OAUTH_ERRORS[loginError] ?? 'Sign-in failed.') : null)

  return (
    <AuthCard
      title="Sign in"
      description={
        <>
          No account?{' '}
          <Link to="/register" className="underline underline-offset-4">
            Create one
          </Link>
        </>
      }
    >
      {shownError && (
        <Alert variant="destructive">
          <AlertDescription>{shownError}</AlertDescription>
        </Alert>
      )}
      <form onSubmit={submit} className="grid gap-4">
        <div className="grid gap-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <Button type="submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
      <OAuthButtons />
    </AuthCard>
  )
}
