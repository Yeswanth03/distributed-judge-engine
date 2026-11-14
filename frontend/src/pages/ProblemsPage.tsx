import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { CheckCircle2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { DifficultyLabel } from '@/components/badges'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { useAuth } from '@/auth/useAuth'
import { leaderboard, problems } from '@/lib/endpoints'
import type { Difficulty } from '@/lib/types'

export default function ProblemsPage() {
  const [params, setParams] = useSearchParams()
  const difficulty = (params.get('difficulty') as Difficulty | null) ?? undefined
  const { state } = useAuth()
  const userId = state.status === 'signedIn' ? state.user.user_id : null

  const list = useInfiniteQuery({
    queryKey: ['problems', difficulty],
    queryFn: ({ pageParam }) => problems.list(difficulty, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  // Which problems the signed-in user has solved; a user with no verdicts yet answers 404.
  const standing = useQuery({
    queryKey: ['standing', userId],
    queryFn: () => leaderboard.standing(userId!),
    enabled: userId !== null,
    retry: false,
  })
  const solved = new Set(standing.data?.solved_problem_ids ?? [])

  return (
    <PageContainer>
      <div className="mb-4 flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Problems</h1>
        <Select
          value={difficulty ?? 'all'}
          onValueChange={(v) => setParams(v === 'all' ? {} : { difficulty: v })}
        >
          <SelectTrigger className="w-40" aria-label="Difficulty">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All difficulties</SelectItem>
            <SelectItem value="easy">Easy</SelectItem>
            <SelectItem value="medium">Medium</SelectItem>
            <SelectItem value="hard">Hard</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {list.isPending ? (
        <Loading />
      ) : list.isError ? (
        <ErrorState error={list.error} />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10" />
                <TableHead>Title</TableHead>
                <TableHead className="w-32">Difficulty</TableHead>
                <TableHead className="w-24 text-right">Points</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.data.pages
                .flatMap((p) => p.items)
                .map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      {solved.has(p.id) && (
                        <CheckCircle2 className="size-4 text-emerald-600" aria-label="Solved" />
                      )}
                    </TableCell>
                    <TableCell>
                      <Link to={`/problems/${p.slug}`} className="font-medium hover:underline">
                        {p.title}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <DifficultyLabel difficulty={p.difficulty} />
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{p.points}</TableCell>
                  </TableRow>
                ))}
              {list.data.pages[0].items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    No problems yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          {list.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                onClick={() => list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            </div>
          )}
        </>
      )}
    </PageContainer>
  )
}
