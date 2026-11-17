import { lazy, Suspense, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Loader2, Play, Plus, RotateCcw, Send, X } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { DifficultyLabel, StatusBadge, VerdictBadge } from '@/components/badges'
import { Markdown } from '@/components/Markdown'
import { ErrorState, Loading } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { useCodeDraft } from '@/hooks/useCodeDraft'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { useSubmissionStream } from '@/hooks/useSubmissionStream'
import { problems, runs, submissions } from '@/lib/endpoints'
import { formatArguments, formatMemory, prettyJson } from '@/lib/format'
import type { ProblemView, Run } from '@/lib/types'

const CodeEditor = lazy(() => import('@/components/CodeEditor'))

const MAX_INPUTS = 10

export default function ProblemPage() {
  const { slug = '' } = useParams()
  const problem = useQuery({ queryKey: ['problem', slug], queryFn: () => problems.get(slug) })

  if (problem.isPending) return <Loading />
  if (problem.isError) return <ErrorState error={problem.error} title="Problem not found" />
  // Keyed by problem: moving to another problem starts fresh state (inputs, results, draft).
  return <Workspace key={problem.data.id} problem={problem.data} />
}

function Workspace({ problem }: { problem: ProblemView }) {
  const wide = useMediaQuery('(min-width: 1024px)')
  return (
    <ResizablePanelGroup
      orientation={wide ? 'horizontal' : 'vertical'}
      className="min-h-[calc(100svh-3.5rem)] flex-1"
    >
      <ResizablePanel defaultSize="42" minSize="25">
        <Statement problem={problem} />
      </ResizablePanel>
      <ResizableHandle withHandle />
      <ResizablePanel defaultSize="58" minSize="30">
        <EditorPane problem={problem} />
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}

function Statement({ problem }: { problem: ProblemView }) {
  return (
    <ScrollArea className="h-full">
      <div className="space-y-4 p-5">
        <div>
          <h1 className="text-xl font-semibold">{problem.title}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            <DifficultyLabel difficulty={problem.difficulty} />
            <span>{problem.points} points</span>
            <span>
              {problem.time_limit_ms} ms CPU · {problem.memory_limit_mb} MB per test
            </span>
          </div>
        </div>
        <Markdown>{problem.statement_md}</Markdown>
        {problem.samples.map((s, i) => (
          <div key={i} className="space-y-1">
            <div className="text-sm font-semibold">Example {i + 1}</div>
            <pre className="overflow-x-auto rounded-md bg-muted p-3 text-sm">
              <span className="text-muted-foreground">Input:</span>
              {'\n' + formatArguments(s.input)}
              {'\n'}
              <span className="text-muted-foreground">Output:</span> {s.expected_output}
            </pre>
          </div>
        ))}
      </div>
    </ScrollArea>
  )
}

function EditorPane({ problem }: { problem: ProblemView }) {
  const { state } = useAuth()
  const navigate = useNavigate()
  const signedIn = state.status === 'signedIn'
  const starter = problem.starter_code ?? ''
  const [code, setCode] = useCodeDraft(problem.id, starter)
  const [inputs, setInputs] = useState<string[]>(() => problem.samples.map((s) => s.input))
  const [tab, setTab] = useState('testcases')
  const [runId, setRunId] = useState<string | null>(null)
  const [submissionId, setSubmissionId] = useState<string | null>(null)

  const requireSignIn = () => {
    toast.info('Sign in to run or submit code.')
    navigate('/login', { state: { from: `/problems/${problem.slug}` } })
  }

  const runMutation = useMutation({
    mutationFn: () => {
      const parsed = inputs.map((text, i) => {
        try {
          return JSON.stringify(JSON.parse(text))
        } catch {
          throw new Error(`Test case ${i + 1} is not valid JSON.`)
        }
      })
      return runs.create(problem.id, code, parsed)
    },
    onSuccess: (r) => {
      setRunId(r.run_id)
      setTab('run')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const submitMutation = useMutation({
    mutationFn: () => submissions.create(problem.id, code),
    onSuccess: (s) => {
      setSubmissionId(s.submission_id)
      setTab('submission')
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <ResizablePanelGroup orientation="vertical">
      <ResizablePanel defaultSize="62" minSize="25" className="flex flex-col">
        <div className="flex items-center gap-2 border-b px-3 py-2">
          <span className="text-sm text-muted-foreground">Python</span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => {
              if (window.confirm('Replace your code with the starter code?')) setCode(starter)
            }}
          >
            <RotateCcw /> Reset
          </Button>
          <Button
            variant="secondary"
            size="sm"
            disabled={runMutation.isPending}
            onClick={() => (signedIn ? runMutation.mutate() : requireSignIn())}
          >
            {runMutation.isPending ? <Loader2 className="animate-spin" /> : <Play />} Run
          </Button>
          <Button
            size="sm"
            disabled={submitMutation.isPending}
            onClick={() => (signedIn ? submitMutation.mutate() : requireSignIn())}
          >
            {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <Send />} Submit
          </Button>
        </div>
        <div className="min-h-0 flex-1">
          <Suspense fallback={<Loading label="Loading editor…" />}>
            <CodeEditor value={code} onChange={setCode} />
          </Suspense>
        </div>
      </ResizablePanel>
      <ResizableHandle withHandle />
      <ResizablePanel defaultSize="38" minSize="15">
        <Tabs value={tab} onValueChange={setTab} className="flex h-full flex-col gap-0">
          <TabsList className="m-2">
            <TabsTrigger value="testcases">Test cases</TabsTrigger>
            <TabsTrigger value="run">Run result</TabsTrigger>
            <TabsTrigger value="submission">Submission</TabsTrigger>
          </TabsList>
          <ScrollArea className="min-h-0 flex-1">
            <TabsContent value="testcases" className="space-y-3 p-3">
              <TestCaseInputs inputs={inputs} setInputs={setInputs} />
            </TabsContent>
            <TabsContent value="run" className="p-3">
              {runId ? (
                <RunResult runId={runId} />
              ) : (
                <Hint>Run your code on the test cases to see the output.</Hint>
              )}
            </TabsContent>
            <TabsContent value="submission" className="p-3">
              {submissionId ? (
                <SubmissionResult key={submissionId} submissionId={submissionId} />
              ) : (
                <Hint>Submit to judge your code on every test case, hidden ones included.</Hint>
              )}
            </TabsContent>
          </ScrollArea>
        </Tabs>
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}

function Hint({ children }: { children: React.ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>
}

function TestCaseInputs({ inputs, setInputs }: { inputs: string[]; setInputs: (v: string[]) => void }) {
  return (
    <>
      <Hint>
        Each test case is a JSON object with one value per argument. Run uses these; Submit uses the hidden
        set.
      </Hint>
      {inputs.map((text, i) => (
        <div key={i} className="flex gap-2">
          <Textarea
            aria-label={`Test case ${i + 1}`}
            className="min-h-16 font-mono text-xs"
            value={text}
            onChange={(e) => setInputs(inputs.map((t, j) => (j === i ? e.target.value : t)))}
          />
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Remove test case ${i + 1}`}
            disabled={inputs.length === 1}
            onClick={() => setInputs(inputs.filter((_, j) => j !== i))}
          >
            <X />
          </Button>
        </div>
      ))}
      <Button
        variant="outline"
        size="sm"
        disabled={inputs.length >= MAX_INPUTS}
        onClick={() => setInputs([...inputs, inputs.at(-1) ?? '{}'])}
      >
        <Plus /> Add test case
      </Button>
    </>
  )
}

function RunResult({ runId }: { runId: string }) {
  const run = useQuery({
    queryKey: ['run', runId],
    queryFn: () => runs.get(runId),
    // Poll until the judge answers; runs are short.
    refetchInterval: (q) => (isDone(q.state.data) ? false : 1000),
  })

  if (run.isError) return <ErrorState error={run.error} />
  if (!run.data || !isDone(run.data)) return <Loading label="Running on the judge…" />
  if (run.data.status === 'FAILED' || !run.data.result) {
    return (
      <ErrorState error={new Error(run.data.error_message ?? 'The run did not finish.')} title="Run failed" />
    )
  }
  const { result } = run.data
  return (
    <div className="space-y-3">
      {result.verdict && <VerdictBadge verdict={result.verdict} />}
      {result.cases.map((c, i) => (
        <div key={i} className="space-y-1 rounded-md border p-3 text-xs">
          <div className="flex items-center gap-2">
            <span className="font-medium">Case {i + 1}</span>
            <VerdictBadge verdict={c.verdict} />
            {c.time_ms !== null && <span className="text-muted-foreground">{c.time_ms} ms</span>}
            {c.memory_kb !== null && (
              <span className="text-muted-foreground">{formatMemory(c.memory_kb)}</span>
            )}
          </div>
          <Field label="Input" value={formatArguments(c.input)} />
          {c.output !== null && <Field label="Output" value={prettyJson(c.output)} />}
          {c.expected_output !== null && <Field label="Expected" value={prettyJson(c.expected_output)} />}
          {c.stdout && <Field label="Your prints" value={c.stdout} />}
          {c.error && <Field label="Error" value={c.error} tone="error" />}
        </div>
      ))}
    </div>
  )
}

function isDone(run: Run | undefined) {
  return run?.status === 'COMPLETED' || run?.status === 'FAILED'
}

function Field({ label, value, tone }: { label: string; value: string; tone?: 'error' }) {
  return (
    <div>
      <div className="text-muted-foreground">{label}</div>
      <pre className={`overflow-x-auto rounded bg-muted p-2 ${tone === 'error' ? 'text-rose-600' : ''}`}>
        {value}
      </pre>
    </div>
  )
}

function SubmissionResult({ submissionId }: { submissionId: string }) {
  const { state, error } = useSubmissionStream(submissionId)
  if (error) return <ErrorState error={new Error(error)} />
  if (!state) return <Loading />
  const done = state.status === 'COMPLETED'
  return (
    <div className="space-y-2 text-sm">
      <div className="flex items-center gap-3">
        <StatusBadge status={state.status} verdict={state.verdict ?? null} />
        {!done && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </div>
      {done && (
        <>
          {state.total_test_cases != null && (
            <p>
              {state.test_cases_passed} / {state.total_test_cases} test cases passed
            </p>
          )}
          {state.execution_time_ms != null && (
            <p className="text-muted-foreground">
              {state.execution_time_ms} ms · {formatMemory(state.memory_kb ?? null)}
            </p>
          )}
          {state.error_message && <Field label="Error" value={state.error_message} tone="error" />}
          <Link
            to={`/submissions/${submissionId}`}
            className="inline-block text-sm underline underline-offset-4"
          >
            View submission{state.verdict && state.verdict !== 'ACCEPTED' ? ' and AI feedback' : ''}
          </Link>
        </>
      )}
    </div>
  )
}
