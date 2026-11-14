import { useState, type FormEvent } from 'react'
import { Link, Navigate, useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { AuthCard, OAuthButtons } from './auth-shared'

export default function RegisterPage() {
  const { state, register } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState({ email: '', username: '', display_name: '', password: '' })
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (state.status === 'signedIn') return <Navigate to="/problems" replace />

  const set = (key: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }))

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await register({ ...form, display_name: form.display_name || undefined })
      navigate('/problems', { replace: true })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard
      title="Create an account"
      description={
        <>
          Already have one?{' '}
          <Link to="/login" className="underline underline-offset-4">
            Sign in
          </Link>
        </>
      }
    >
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
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
            value={form.email}
            onChange={set('email')}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="username">Username</Label>
          <Input
            id="username"
            autoComplete="username"
            required
            value={form.username}
            onChange={set('username')}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="display_name">Display name (optional)</Label>
          <Input
            id="display_name"
            autoComplete="name"
            value={form.display_name}
            onChange={set('display_name')}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            required
            value={form.password}
            onChange={set('password')}
          />
        </div>
        <Button type="submit" disabled={busy}>
          {busy ? 'Creating…' : 'Create account'}
        </Button>
      </form>
      <OAuthButtons />
    </AuthCard>
  )
}
