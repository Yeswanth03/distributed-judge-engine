import { lazy, Suspense } from 'react'
import { Link, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, CalendarClock, Clock, ListChecks, Loader2, MemoryStick, Sparkles } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/badges'
import { Markdown } from '@/components/Markdown'
import { ErrorState, Loading } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { ApiError } from '@/lib/api'
import { feedback, submissions } from '@/lib/endpoints'
import { formatDateTime, formatMemory } from '@/lib/format'
import { useProblemTitles } from '@/hooks/useProblemTitles'
import { cn } from '@/lib/utils'
import type { Feedback, Submission } from '@/lib/types'

const CodeEditor = lazy(() => import('@/components/CodeEditor'))

export default function SubmissionPage() {
  const { id = '' } = useParams()
  const titles = useProblemTitles()
  const submission = useQuery({
    queryKey: ['submissions', id],
    queryFn: () => submissions.get(id, true),
    refetchInterval: (q) => (q.state.data?.status === 'COMPLETED' ? false : 2000),
  })

  if (submission.isPending) {
    return (
      <div className="mx-auto grid w-full max-w-6xl gap-4 px-4 py-8">
        <Skeleton className="h-28 rounded-xl" />
        <Skeleton className="h-96 rounded-xl" />
      </div>
    )
  }
  if (submission.isError) return <ErrorState error={submission.error} title="Submission not found" />
  const s = submission.data
  const problem = titles?.get(s.problem_id)
  const accepted = s.verdict === 'ACCEPTED'

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-8">
      <Link
        to="/submissions"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> My submissions
      </Link>

      <div
        className={cn(
          'mb-4 rounded-xl border p-5 shadow-sm',
          s.status !== 'COMPLETED'
            ? 'bg-card'
            : accepted
              ? 'border-success/30 bg-success/6'
              : 'border-danger/30 bg-danger/5',
        )}
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <StatusBadge status={s.status} verdict={s.verdict} />
            <h1 className="mt-1 text-xl font-semibold tracking-tight">
              {problem ? (
                <Link to={`/problems/${problem.slug}`} className="hover:text-brand">
                  {problem.title}
                </Link>
              ) : (
                'Submission'
              )}
            </h1>
          </div>
          {problem && (
            <Button variant="outline" size="sm" asChild>
              <Link to={`/problems/${problem.slug}`}>Open problem</Link>
            </Button>
          )}
        </div>
        <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Fact
            Icon={ListChecks}
            label="Test cases"
            value={s.total_test_cases !== null ? `${s.test_cases_passed} / ${s.total_test_cases}` : '—'}
          />
          <Fact
            Icon={Clock}
            label="Runtime"
            value={s.execution_time_ms !== null ? `${s.execution_time_ms} ms` : '—'}
          />
          <Fact Icon={MemoryStick} label="Memory" value={formatMemory(s.memory_kb)} />
          <Fact Icon={CalendarClock} label="Submitted" value={formatDateTime(s.created_at)} />
        </div>
        {s.error_message && (
          <pre className="mt-4 overflow-x-auto rounded-lg bg-danger/8 px-3 py-2 font-mono text-[13px] text-danger">
            {s.error_message}
          </pre>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
        <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
          <div className="flex items-center justify-between border-b bg-muted/30 px-4 py-2.5 text-sm">
            <span className="font-medium">Code</span>
            <span className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground">
              Python 3
            </span>
          </div>
          <div className="h-[28rem]">
            <Suspense fallback={<Loading label="Loading code…" />}>
              <CodeEditor value={s.code ?? ''} readOnly />
            </Suspense>
          </div>
        </div>
        <FeedbackCard submission={s} />
      </div>
    </div>
  )
}

function Fact({ Icon, label, value }: { Icon: typeof Clock; label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-background/60 p-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div className="mt-1 truncate font-semibold tabular-nums">{value}</div>
    </div>
  )
}

/** AI hints for this submission. The AI never decides the verdict; it only explains it. */
function FeedbackCard({ submission }: { submission: Submission }) {
  const queryClient = useQueryClient()
  const judged = submission.status === 'COMPLETED'
  const key = ['feedback', submission.submission_id]

  const current = useQuery({
    queryKey: key,
    queryFn: async (): Promise<Feedback | null> => {
      try {
        return await feedback.get(submission.submission_id)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null // none requested yet
        throw e
      }
    },
    enabled: judged,
    refetchInterval: (q) => {
      const status = q.state.data?.status
      return status === 'queued' || status === 'processing' ? 2000 : false
    },
  })

  const request = useMutation({
    // Accepted: hints are generated right away. Anything else keeps the queued request and polling.
    mutationFn: () =>
      submission.verdict === 'ACCEPTED'
        ? feedback.requestDirect(submission.submission_id)
        : feedback.request(submission.submission_id),
    onSuccess: (f) => queryClient.setQueryData(key, f),
    onError: (e) => toast.error(errorMessage(e)),
  })

  const f = current.data
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
      <div className="flex items-center gap-2 border-b bg-gradient-to-r from-brand-soft to-transparent px-4 py-2.5">
        <span className="grid size-6 place-items-center rounded-md bg-brand-soft text-brand">
          <Sparkles className="size-3.5" />
        </span>
        <span className="text-sm font-medium">AI hints</span>
      </div>
      <div className="p-4">
        <p className="mb-4 text-xs text-muted-foreground">
          Hints about your code. The verdict always comes from the judge's tests, never from the AI.
        </p>
        {!judged ? (
          <p className="text-sm text-muted-foreground">Available once the submission is judged.</p>
        ) : current.isPending ? (
          <Loading />
        ) : current.isError ? (
          <ErrorState error={current.error} />
        ) : f === null || f === undefined ? (
          <Button onClick={() => request.mutate()} disabled={request.isPending}>
            {request.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />} Get hints
          </Button>
        ) : f.status === 'ready' && f.feedback ? (
          <Markdown>{f.feedback}</Markdown>
        ) : f.status === 'unavailable' ? (
          <p className="text-sm text-muted-foreground">
            Hints are unavailable right now{f.error_reason ? ` (${f.error_reason})` : ''}.
          </p>
        ) : (
          <Loading label="Thinking about your code…" />
        )}
      </div>
    </div>
  )
}
