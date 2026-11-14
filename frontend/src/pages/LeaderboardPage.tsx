import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { useAuth } from '@/auth/useAuth'
import { leaderboard } from '@/lib/endpoints'
import { formatRelative, shortId } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { LeaderboardEntry } from '@/lib/types'

/** The name Catalog learned from user events; a short id until it has one. */
function displayName(e: Pick<LeaderboardEntry, 'user_id' | 'username' | 'display_name'>) {
  return e.display_name || e.username || shortId(e.user_id)
}

export default function LeaderboardPage() {
  const { state } = useAuth()
  const me = state.status === 'signedIn' ? state.user : null

  const top = useQuery({
    queryKey: ['leaderboard'],
    queryFn: () => leaderboard.top(100),
    refetchInterval: 15_000,
  })
  const standing = useQuery({
    queryKey: ['standing', me?.user_id],
    queryFn: () => leaderboard.standing(me!.user_id),
    enabled: me !== null,
    retry: false,
  })

  return (
    <PageContainer>
      <div className="mb-1 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Leaderboard</h1>
        <Button variant="ghost" size="sm" onClick={() => top.refetch()} disabled={top.isFetching}>
          <RefreshCw className={cn(top.isFetching && 'animate-spin')} /> Refresh
        </Button>
      </div>
      {/* The leaderboard is eventually consistent; say how fresh it is instead of pretending it is live. */}
      {top.data && (
        <p className="mb-4 text-sm text-muted-foreground">
          {top.data.as_of
            ? `Includes verdicts judged up to ${formatRelative(top.data.as_of)}`
            : 'No verdicts yet.'}
          {top.data.lag_seconds !== null &&
            ` · last update applied ${top.data.lag_seconds.toFixed(1)} s after judging`}
        </p>
      )}

      {me && standing.data && (
        <Card className="mb-4">
          <CardContent className="flex flex-wrap gap-8 py-4 text-sm">
            <div>
              <div className="text-muted-foreground">Your rank</div>
              <div className="text-xl font-semibold">{standing.data.rank ?? '—'}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Score</div>
              <div className="text-xl font-semibold tabular-nums">{standing.data.score}</div>
            </div>
            <div>
              <div className="text-muted-foreground">Solved</div>
              <div className="text-xl font-semibold tabular-nums">{standing.data.solved_count}</div>
            </div>
          </CardContent>
        </Card>
      )}

      {top.isPending ? (
        <Loading />
      ) : top.isError ? (
        <ErrorState error={top.error} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">Rank</TableHead>
              <TableHead>User</TableHead>
              <TableHead className="w-24 text-right">Solved</TableHead>
              <TableHead className="w-24 text-right">Score</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {top.data.entries.map((e) => (
              <TableRow
                key={e.user_id}
                className={cn(e.user_id === me?.user_id && 'bg-muted/60 font-medium')}
              >
                <TableCell className="tabular-nums">{e.rank}</TableCell>
                <TableCell>
                  {displayName(e)}
                  {e.user_id === me?.user_id && (
                    <span className="ml-2 text-xs text-muted-foreground">(you)</span>
                  )}
                </TableCell>
                <TableCell className="text-right tabular-nums">{e.solved_count}</TableCell>
                <TableCell className="text-right tabular-nums">{e.score}</TableCell>
              </TableRow>
            ))}
            {top.data.entries.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                  Nobody has solved a problem yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      )}
    </PageContainer>
  )
}
