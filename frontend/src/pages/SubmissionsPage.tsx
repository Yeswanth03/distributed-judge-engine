import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { StatusBadge } from '@/components/badges'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { submissions } from '@/lib/endpoints'
import { useProblemTitles } from '@/hooks/useProblemTitles'
import { formatMemory, formatRelative } from '@/lib/format'

export default function SubmissionsPage() {
  const titles = useProblemTitles()
  const list = useInfiniteQuery({
    queryKey: ['submissions', 'mine'],
    queryFn: ({ pageParam }) => submissions.list(undefined, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  return (
    <PageContainer>
      <h1 className="mb-4 text-2xl font-semibold">My submissions</h1>
      {list.isPending ? (
        <Loading />
      ) : list.isError ? (
        <ErrorState error={list.error} />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Problem</TableHead>
                <TableHead>Result</TableHead>
                <TableHead className="text-right">Time</TableHead>
                <TableHead className="text-right">Memory</TableHead>
                <TableHead className="text-right">Submitted</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.data.pages
                .flatMap((p) => p.items)
                .map((s) => (
                  <TableRow key={s.submission_id}>
                    <TableCell>
                      <Link to={`/submissions/${s.submission_id}`} className="font-medium hover:underline">
                        {titles?.get(s.problem_id)?.title ?? 'Problem'}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={s.status} verdict={s.verdict} />
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {s.execution_time_ms !== null ? `${s.execution_time_ms} ms` : '—'}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{formatMemory(s.memory_kb)}</TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {formatRelative(s.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              {list.data.pages[0].items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                    No submissions yet.{' '}
                    <Link to="/problems" className="underline">
                      Pick a problem
                    </Link>
                    .
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
                Load more
              </Button>
            </div>
          )}
        </>
      )}
    </PageContainer>
  )
}
