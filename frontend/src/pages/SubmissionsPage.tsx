import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Clock, MemoryStick } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/badges'
import { ErrorState } from '@/components/states'
import { useProblemTitles } from '@/hooks/useProblemTitles'
import { submissions } from '@/lib/endpoints'
import { formatMemory, formatRelative } from '@/lib/format'

const COLS = 'grid grid-cols-[minmax(0,1fr)_11rem] sm:grid-cols-[minmax(0,1fr)_12rem_6rem_6rem_7rem]'

export default function SubmissionsPage() {
  const titles = useProblemTitles()
  const list = useInfiniteQuery({
    queryKey: ['submissions', 'mine'],
    queryFn: ({ pageParam }) => submissions.list(undefined, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })
  const items = list.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8">
      <h1 className="text-2xl font-bold tracking-tight">My submissions</h1>
      <p className="mb-5 text-sm text-muted-foreground">Every solution you've submitted, newest first.</p>

      <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
        <div
          className={`${COLS} border-b px-4 py-2.5 text-xs font-medium tracking-wide text-muted-foreground uppercase`}
        >
          <span>Problem</span>
          <span>Status</span>
          <span className="hidden text-right sm:block">Runtime</span>
          <span className="hidden text-right sm:block">Memory</span>
          <span className="hidden text-right sm:block">Submitted</span>
        </div>
        {list.isPending ? (
          Array.from({ length: 6 }, (_, i) => (
            <div key={i} className={`${COLS} items-center px-4 py-3.5`}>
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-4 w-28" />
            </div>
          ))
        ) : list.isError ? (
          <ErrorState error={list.error} />
        ) : items.length === 0 ? (
          <div className="px-4 py-14 text-center text-sm text-muted-foreground">
            No submissions yet.{' '}
            <Link to="/problems" className="text-brand hover:underline">
              Pick a problem
            </Link>{' '}
            to get started.
          </div>
        ) : (
          items.map((s) => (
            <Link
              key={s.submission_id}
              to={`/submissions/${s.submission_id}`}
              className={`${COLS} items-center px-4 py-3 text-sm transition-colors odd:bg-muted/40 hover:bg-accent/60`}
            >
              <span className="truncate font-medium">{titles?.get(s.problem_id)?.title ?? 'Problem'}</span>
              <StatusBadge status={s.status} verdict={s.verdict} />
              <span className="hidden items-center justify-end gap-1 text-muted-foreground tabular-nums sm:flex">
                <Clock className="size-3" />
                {s.execution_time_ms !== null ? `${s.execution_time_ms} ms` : '—'}
              </span>
              <span className="hidden items-center justify-end gap-1 text-muted-foreground tabular-nums sm:flex">
                <MemoryStick className="size-3" />
                {formatMemory(s.memory_kb)}
              </span>
              <span className="hidden text-right text-muted-foreground sm:block">
                {formatRelative(s.created_at)}
              </span>
            </Link>
          ))
        )}
      </div>
      {list.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <Button variant="outline" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
