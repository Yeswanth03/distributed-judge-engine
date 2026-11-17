import { useQuery } from '@tanstack/react-query'
import { Crown, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { UserAvatar } from '@/components/brand'
import { ErrorState } from '@/components/states'
import { useAuth } from '@/auth/useAuth'
import { useMyStanding } from '@/hooks/useStanding'
import { leaderboard } from '@/lib/endpoints'
import { displayNameOf, formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LeaderboardEntry } from '@/lib/types'

const MEDAL: Record<number, string> = {
  1: 'bg-amber-400/20 text-amber-500 ring-amber-400/40',
  2: 'bg-slate-300/25 text-slate-500 ring-slate-300/50 dark:text-slate-300',
  3: 'bg-orange-400/20 text-orange-500 ring-orange-400/40',
}

export default function LeaderboardPage() {
  const { state } = useAuth()
  const me = state.status === 'signedIn' ? state.user : null
  const top = useQuery({
    queryKey: ['leaderboard'],
    queryFn: () => leaderboard.top(100),
    refetchInterval: 15_000,
  })
  const standing = useMyStanding()
  const entries = top.data?.entries ?? []

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Leaderboard</h1>
          {/* Eventually consistent: say how fresh it is instead of pretending it is live. */}
          <p className="text-sm text-muted-foreground">
            {top.data?.as_of
              ? `Includes verdicts judged up to ${formatRelative(top.data.as_of)}`
              : 'Ranked by score; ties go to whoever got there first.'}
            {top.data?.lag_seconds != null && (
              <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs">
                updated {top.data.lag_seconds.toFixed(1)} s after judging
              </span>
            )}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => top.refetch()} disabled={top.isFetching}>
          <RefreshCw className={cn(top.isFetching && 'animate-spin')} /> Refresh
        </Button>
      </div>

      {top.isPending ? (
        <Skeleton className="mb-6 h-56 rounded-xl" />
      ) : top.isError ? (
        <ErrorState error={top.error} />
      ) : entries.length === 0 ? (
        <div className="rounded-xl border bg-card px-4 py-16 text-center text-sm text-muted-foreground">
          Nobody has solved a problem yet. Be the first.
        </div>
      ) : (
        <>
          <Podium entries={entries.slice(0, 3)} meId={me?.user_id} />

          {me && standing.data && (
            <div className="mb-4 flex flex-wrap items-center gap-x-8 gap-y-2 rounded-xl border border-primary/30 bg-brand-soft px-5 py-3 text-sm">
              <span className="font-medium">You</span>
              <span>
                Rank <b className="tabular-nums">{standing.data.rank ?? '—'}</b>
              </span>
              <span>
                Score <b className="tabular-nums">{standing.data.score}</b>
              </span>
              <span>
                Solved <b className="tabular-nums">{standing.data.solved_count}</b>
              </span>
            </div>
          )}

          <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
            <div className="grid grid-cols-[4rem_1fr_5rem_5rem] border-b px-4 py-2.5 text-xs font-medium tracking-wide text-muted-foreground uppercase">
              <span>Rank</span>
              <span>Coder</span>
              <span className="text-right">Solved</span>
              <span className="text-right">Score</span>
            </div>
            {entries.map((e) => (
              <Row key={e.user_id} e={e} isMe={e.user_id === me?.user_id} />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function RankBadge({ rank }: { rank: number }) {
  return (
    <span
      className={cn(
        'grid size-7 place-items-center rounded-full text-xs font-semibold tabular-nums',
        MEDAL[rank] ? cn('ring-1', MEDAL[rank]) : 'text-muted-foreground',
      )}
    >
      {rank}
    </span>
  )
}

function Row({ e, isMe }: { e: LeaderboardEntry; isMe: boolean }) {
  const name = displayNameOf(e)
  return (
    <div
      className={cn(
        'grid grid-cols-[4rem_1fr_5rem_5rem] items-center px-4 py-2.5 text-sm odd:bg-muted/40',
        isMe && 'bg-brand-soft! font-medium',
      )}
    >
      <RankBadge rank={e.rank} />
      <span className="flex min-w-0 items-center gap-3">
        <UserAvatar name={name} className="size-7 text-[10px]" />
        <span className="truncate">
          {name}
          {e.username && e.display_name && (
            <span className="ml-2 text-xs text-muted-foreground">@{e.username}</span>
          )}
          {isMe && <span className="ml-2 text-xs text-brand">(you)</span>}
        </span>
      </span>
      <span className="text-right tabular-nums">{e.solved_count}</span>
      <span className="text-right font-semibold tabular-nums">{e.score}</span>
    </div>
  )
}

/** The top three on steps: second, first, third. */
function Podium({ entries, meId }: { entries: LeaderboardEntry[]; meId?: string }) {
  const order = [entries[1], entries[0], entries[2]]
  const heights = ['h-24', 'h-32', 'h-20']
  return (
    <div className="mb-6 flex items-end justify-center gap-3 rounded-xl border bg-card bg-brand-glow px-4 pt-8 shadow-sm sm:gap-6">
      {order.map((e, i) => {
        if (!e) return <div key={i} className="w-24 sm:w-36" />
        const name = displayNameOf(e)
        return (
          <div key={e.user_id} className="flex w-24 flex-col items-center sm:w-36">
            {e.rank === 1 && <Crown className="mb-1 size-5 text-amber-400" />}
            <UserAvatar
              name={name}
              className={cn(
                'size-12 text-sm ring-2 ring-offset-2 ring-offset-card',
                MEDAL[e.rank]?.split(' ').at(-1),
              )}
            />
            <div
              className={cn(
                'mt-2 w-full truncate text-center text-sm font-medium',
                e.user_id === meId && 'text-brand',
              )}
            >
              {name}
            </div>
            <div className="mb-2 text-xs text-muted-foreground tabular-nums">{e.score} pts</div>
            <div
              className={cn(
                'grid w-full place-items-center rounded-t-lg text-2xl font-bold',
                heights[i],
                MEDAL[e.rank] ?? 'bg-muted text-muted-foreground',
              )}
            >
              {e.rank}
            </div>
          </div>
        )
      })}
    </div>
  )
}
