import { lazy, Suspense } from 'react'
import { Link, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Sparkles } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { StatusBadge } from '@/components/badges'
import { Markdown } from '@/components/Markdown'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { ApiError } from '@/lib/api'
import { feedback, submissions } from '@/lib/endpoints'
import { formatDateTime, formatMemory } from '@/lib/format'
import type { Feedback, Submission } from '@/lib/types'
import { useProblemTitles } from '@/hooks/useProblemTitles'

const CodeEditor = lazy(() => import('@/components/CodeEditor'))

export default function SubmissionPage() {
  const { id = '' } = useParams()
  const titles = useProblemTitles()
  const submission = useQuery({
    queryKey: ['submissions', id],
    queryFn: () => submissions.get(id, true),
    refetchInterval: (q) => (q.state.data?.status === 'COMPLETED' ? false : 2000),
  })

  if (submission.isPending) return <Loading />
  if (submission.isError) return <ErrorState error={submission.error} title="Submission not found" />
  const s = submission.data
  const problem = titles?.get(s.problem_id)

  return (
    <PageContainer wide>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-semibold">
          {problem ? <Link to={`/problems/${problem.slug}`}>{problem.title}</Link> : 'Submission'}
        </h1>
        <StatusBadge status={s.status} verdict={s.verdict} />
      </div>
      <div className="mb-4 flex flex-wrap gap-6 text-sm text-muted-foreground">
        <span>Submitted {formatDateTime(s.created_at)}</span>
        {s.total_test_cases !== null && (
          <span>
            {s.test_cases_passed} / {s.total_test_cases} test cases
          </span>
        )}
        {s.execution_time_ms !== null && <span>{s.execution_time_ms} ms</span>}
        <span>{formatMemory(s.memory_kb)}</span>
      </div>
      {s.error_message && (
        <pre className="mb-4 overflow-x-auto rounded bg-muted p-3 text-sm text-rose-600">
          {s.error_message}
        </pre>
      )}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="overflow-hidden py-0">
          <div className="h-[28rem]">
            <Suspense fallback={<Loading label="Loading code…" />}>
              <CodeEditor value={s.code ?? ''} readOnly />
            </Suspense>
          </div>
        </Card>
        <FeedbackCard submission={s} />
      </div>
    </PageContainer>
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
    mutationFn: () => feedback.request(submission.submission_id),
    onSuccess: (f) => queryClient.setQueryData(key, f),
    onError: (e) => toast.error(errorMessage(e)),
  })

  const f = current.data
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Sparkles className="size-4" /> AI feedback
        </CardTitle>
        <CardDescription>
          Hints on your code. The verdict always comes from the judge, never from the AI.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!judged ? (
          <p className="text-sm text-muted-foreground">Available once the submission is judged.</p>
        ) : current.isPending ? (
          <Loading />
        ) : current.isError ? (
          <ErrorState error={current.error} />
        ) : f === null || f === undefined ? (
          <Button onClick={() => request.mutate()} disabled={request.isPending}>
            {request.isPending && <Loader2 className="animate-spin" />} Get feedback
          </Button>
        ) : f.status === 'ready' && f.feedback ? (
          <Markdown>{f.feedback}</Markdown>
        ) : f.status === 'unavailable' ? (
          <p className="text-sm text-muted-foreground">
            Feedback is unavailable{f.error_reason ? `: ${f.error_reason}` : '.'}
          </p>
        ) : (
          <Loading label="Generating feedback…" />
        )}
      </CardContent>
    </Card>
  )
}
