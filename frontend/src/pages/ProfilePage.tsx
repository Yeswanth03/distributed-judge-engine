import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Laptop, LogOut } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { UserAvatar } from '@/components/brand'
import { DifficultyBars, ProgressRing } from '@/components/progress'
import { countByDifficulty } from '@/lib/progress'
import { ErrorState } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { useAllProblems } from '@/hooks/useProblemTitles'
import { useMyStanding } from '@/hooks/useStanding'
import { users } from '@/lib/endpoints'
import { formatDateTime, formatRelative } from '@/lib/format'

export default function ProfilePage() {
  const { state, reloadUser } = useAuth()
  const user = state.status === 'signedIn' ? state.user : null
  const [name, setName] = useState(user?.display_name ?? '')
  const all = useAllProblems()
  const standing = useMyStanding()

  const save = useMutation({
    mutationFn: () => users.updateMe(name),
    onSuccess: async () => {
      await reloadUser()
      toast.success('Display name saved.')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  if (!user) return null
  const shown = user.display_name || user.username
  return (
    <div className="mx-auto grid w-full max-w-5xl gap-4 px-4 py-8">
      <div className="flex flex-wrap items-center gap-5 rounded-xl border bg-card bg-brand-glow p-6 shadow-sm">
        <UserAvatar name={shown} className="size-16 text-xl" />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-2xl font-bold tracking-tight">{shown}</h1>
          <div className="text-sm text-muted-foreground">
            @{user.username} · {user.email}
          </div>
          <div className="mt-2 flex flex-wrap gap-2 text-xs">
            <span className="rounded-full bg-brand-soft px-2.5 py-0.5 font-medium text-brand capitalize">
              {user.role}
            </span>
            <span className="rounded-full bg-muted px-2.5 py-0.5 text-muted-foreground">
              Joined {formatDateTime(user.created_at)}
            </span>
          </div>
        </div>
        <div className="flex gap-6 text-center">
          <div>
            <div className="text-2xl font-bold tabular-nums">{standing.data?.rank ?? '—'}</div>
            <div className="text-xs text-muted-foreground">Rank</div>
          </div>
          <div>
            <div className="text-2xl font-bold tabular-nums">{standing.data?.score ?? 0}</div>
            <div className="text-xs text-muted-foreground">Score</div>
          </div>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-[1.2fr_1fr]">
        <div className="rounded-xl border bg-card p-6 shadow-sm">
          <h2 className="mb-4 font-semibold">Solved problems</h2>
          {all.data ? (
            <div className="flex flex-col items-center gap-6 sm:flex-row">
              <ProgressRing solved={standing.solved.size} total={all.data.length} />
              <DifficultyBars
                counts={countByDifficulty(all.data, standing.solved)}
                className="w-full flex-1"
              />
            </div>
          ) : (
            <Skeleton className="h-28" />
          )}
        </div>
        <div className="rounded-xl border bg-card p-6 shadow-sm">
          <h2 className="mb-1 font-semibold">Display name</h2>
          <p className="mb-4 text-sm text-muted-foreground">
            Shown on the leaderboard instead of your username.
          </p>
          <form
            className="grid gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault()
              save.mutate()
            }}
          >
            <Label htmlFor="display_name" className="sr-only">
              Display name
            </Label>
            <Input id="display_name" value={name} onChange={(e) => setName(e.target.value)} />
            <Button type="submit" className="w-fit" disabled={save.isPending || name === user.display_name}>
              Save
            </Button>
          </form>
        </div>
      </div>

      <Sessions />
    </div>
  )
}

function Sessions() {
  const queryClient = useQueryClient()
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: users.sessions })
  const revoke = useMutation({
    mutationFn: users.revokeSession,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      toast.success('Device signed out. It loses access within 10 minutes.')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm">
      <h2 className="font-semibold">Signed-in devices</h2>
      <p className="mb-4 text-sm text-muted-foreground">
        Signing a device out takes effect within 10 minutes, when its access token expires.
      </p>
      {sessions.isPending ? (
        <Skeleton className="h-20" />
      ) : sessions.isError ? (
        <ErrorState error={sessions.error} />
      ) : (
        <div className="divide-y rounded-lg border">
          {sessions.data.items.map((s) => (
            <div key={s.session_id} className="flex items-center gap-4 px-4 py-3 text-sm">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                <Laptop className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium" title={s.user_agent}>
                  {s.user_agent || 'Unknown device'}
                </div>
                <div className="text-xs text-muted-foreground">
                  {s.ip} · last active {formatRelative(s.last_used_at)}
                </div>
              </div>
              {s.current ? (
                <span className="rounded-full bg-success/14 px-2.5 py-0.5 text-xs font-medium text-success">
                  This device
                </span>
              ) : (
                <Button variant="outline" size="sm" onClick={() => revoke.mutate(s.session_id)}>
                  <LogOut /> Sign out
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
