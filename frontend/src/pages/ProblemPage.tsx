import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import confetti from 'canvas-confetti'
import {
  CheckCircle2,
  Clock,
  Code2,
  Cpu,
  FileText,
  History,
  Loader2,
  MemoryStick,
  Play,
  Plus,
  RotateCcw,
  Send,
  Terminal,
  Trophy,
  X,
  XCircle,
} from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { DifficultyPill, StatusBadge, VerdictText } from '@/components/badges'
import { Markdown } from '@/components/Markdown'
import { ErrorState, Loading } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { useAuth } from '@/auth/useAuth'
import { useCodeDraft } from '@/hooks/useCodeDraft'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { useMyStanding } from '@/hooks/useStanding'
import { useSubmissionStream } from '@/hooks/useSubmissionStream'
import { problems, runs, submissions } from '@/lib/endpoints'
import { formatArguments, formatMemory, formatRelative, prettyJson } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { ProblemView, Run, RunCase } from '@/lib/types'

const CodeEditor = lazy(() => import('@/components/CodeEditor'))

const MAX_INPUTS = 10

export default function ProblemPage() {
  const { slug = '' } = useParams()
  const problem = useQuery({ queryKey: ['problem', slug], queryFn: () => problems.get(slug) })

  if (problem.isPending) return <WorkspaceSkeleton />
  if (problem.isError) return <ErrorState error={problem.error} title="Problem not found" />
  // Keyed by problem: moving to another problem starts fresh state (inputs, results, draft).
  return <Workspace key={problem.data.id} problem={problem.data} />
}

function WorkspaceSkeleton() {
  return (
    <div className="grid flex-1 gap-2 bg-muted/40 p-2 lg:grid-cols-[42fr_58fr]">
      <Skeleton className="min-h-80 rounded-lg" />
      <Skeleton className="min-h-80 rounded-lg" />
    </div>
  )
}

/** Each pane is a rounded card on a tinted backdrop, LeetCode style; the gaps are the resize handles. */
function Pane({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('flex h-full flex-col overflow-hidden rounded-lg border bg-card', className)}>
      {children}
    </div>
  )
}

const handleClass =
  'bg-transparent w-2 after:w-2 hover:bg-primary/15 transition-colors aria-[orientation=horizontal]:h-2 aria-[orientation=horizontal]:after:h-2'

function PaneTabs<T extends string>({
  tabs,
  value,
  onChange,
  right,
}: {
  tabs: { value: T; label: string; Icon: typeof Code2 }[]
  value: T
  onChange: (v: T) => void
  right?: React.ReactNode
}) {
  return (
    <div className="flex h-10 shrink-0 items-center gap-1 border-b bg-muted/30 px-2">
      {tabs.map(({ value: v, label, Icon }) => (
        <button
          key={v}
          type="button"
          onClick={() => onChange(v)}
          className={cn(
            'flex items-center gap-1.5 rounded-md px-2.5 py-1 text-sm transition-colors',
            v === value
              ? 'bg-background font-medium text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          <Icon className="size-3.5" />
          {label}
        </button>
      ))}
      <div className="ml-auto flex items-center gap-1.5">{right}</div>
    </div>
  )
}

function Workspace({ problem }: { problem: ProblemView }) {
  const wide = useMediaQuery('(min-width: 1024px)')
  return (
    <div className="flex flex-1 bg-muted/50 p-2 dark:bg-background">
      <ResizablePanelGroup
        orientation={wide ? 'horizontal' : 'vertical'}
        className="min-h-[calc(100svh-4.5rem)] flex-1"
      >
        <ResizablePanel defaultSize="42" minSize="25">
          <LeftPane problem={problem} />
        </ResizablePanel>
        <ResizableHandle className={handleClass} />
        <ResizablePanel defaultSize="58" minSize="30">
          <EditorPane problem={problem} />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  )
}

// ---- left: description and this problem's submissions ----

function LeftPane({ problem }: { problem: ProblemView }) {
  const [tab, setTab] = useState<'description' | 'submissions'>('description')
  return (
    <Pane>
      <PaneTabs
        value={tab}
        onChange={setTab}
        tabs={[
          { value: 'description', label: 'Description', Icon: FileText },
          { value: 'submissions', label: 'Submissions', Icon: History },
        ]}
      />
      <ScrollArea className="min-h-0 flex-1">
        {tab === 'description' ? <Description problem={problem} /> : <ProblemSubmissions problem={problem} />}
      </ScrollArea>
    </Pane>
  )
}

function Description({ problem }: { problem: ProblemView }) {
  const standing = useMyStanding()
  const solved = standing.solved.has(problem.id)
  return (
    <div className="space-y-5 p-5">
      <div className="space-y-3">
        <div className="flex items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{problem.title}</h1>
          {solved && (
            <span className="flex shrink-0 items-center gap-1 text-sm text-success">
              Solved <CheckCircle2 className="size-4" />
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <DifficultyPill difficulty={problem.difficulty} />
          <Chip Icon={Trophy}>{problem.points} points</Chip>
          <Chip Icon={Cpu}>{problem.time_limit_ms} ms CPU</Chip>
          <Chip Icon={MemoryStick}>{problem.memory_limit_mb} MB</Chip>
        </div>
      </div>
      <Markdown>{problem.statement_md}</Markdown>
      {problem.samples.map((s, i) => (
        <div key={i} className="space-y-2">
          <div className="text-sm font-semibold">Example {i + 1}</div>
          <div className="space-y-1.5 border-l-2 border-border pl-4 font-mono text-[13px] leading-6">
            <div>
              <span className="font-sans font-semibold">Input: </span>
              <span className="text-muted-foreground">{formatArguments(s.input).replaceAll('\n', ', ')}</span>
            </div>
            <div>
              <span className="font-sans font-semibold">Output: </span>
              <span className="text-muted-foreground">{s.expected_output}</span>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function Chip({ Icon, children }: { Icon: typeof Cpu; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2.5 py-0.5 text-muted-foreground">
      <Icon className="size-3" />
      {children}
    </span>
  )
}

function ProblemSubmissions({ problem }: { problem: ProblemView }) {
  const { state } = useAuth()
  const signedIn = state.status === 'signedIn'
  const list = useQuery({
    queryKey: ['submissions', 'problem', problem.id],
    queryFn: () => submissions.list(problem.id),
    enabled: signedIn,
  })
  if (!signedIn) {
    return (
      <p className="p-5 text-sm text-muted-foreground">
        <Link to="/login" className="text-brand hover:underline">
          Sign in
        </Link>{' '}
        to see your submissions.
      </p>
    )
  }
  if (list.isPending) return <Loading />
  if (list.isError) return <ErrorState error={list.error} />
  if (list.data.items.length === 0)
    return <p className="p-5 text-sm text-muted-foreground">No submissions yet.</p>
  return (
    <div className="divide-y">
      {list.data.items.map((s) => (
        <Link
          key={s.submission_id}
          to={`/submissions/${s.submission_id}`}
          className="flex items-center gap-3 px-5 py-3 text-sm transition-colors hover:bg-muted/50"
        >
          <div className="min-w-0 flex-1">
            <StatusBadge status={s.status} verdict={s.verdict} />
            <div className="mt-0.5 text-xs text-muted-foreground">{formatRelative(s.created_at)}</div>
          </div>
          <span className="flex items-center gap-1 text-xs text-muted-foreground tabular-nums">
            <Clock className="size-3" />
            {s.execution_time_ms !== null ? `${s.execution_time_ms} ms` : '—'}
          </span>
          <span className="flex w-20 items-center justify-end gap-1 text-xs text-muted-foreground tabular-nums">
            <MemoryStick className="size-3" />
            {formatMemory(s.memory_kb)}
          </span>
        </Link>
      ))}
    </div>
  )
}

// ---- right: editor and console ----

/** Argument names in order, taken from the first sample (the problem view doesn't list parameters). */
function argumentNames(problem: ProblemView): string[] {
  try {
    return Object.keys(JSON.parse(problem.samples[0]?.input ?? '{}') as object)
  } catch {
    return []
  }
}

/** A test case being edited: each argument as its own JSON text. */
type CaseDraft = Record<string, string>

function toDraft(input: string, names: string[]): CaseDraft {
  try {
    const obj = JSON.parse(input) as Record<string, unknown>
    return Object.fromEntries(names.map((n) => [n, JSON.stringify(obj[n] ?? null)]))
  } catch {
    return Object.fromEntries(names.map((n) => [n, '']))
  }
}

function toInput(draft: CaseDraft, names: string[], index: number): string {
  const args: Record<string, unknown> = {}
  for (const n of names) {
    try {
      args[n] = JSON.parse(draft[n] ?? '')
    } catch {
      throw new Error(`Case ${index + 1}: ${n} is not valid JSON.`)
    }
  }
  return JSON.stringify(args)
}

function EditorPane({ problem }: { problem: ProblemView }) {
  const { state } = useAuth()
  const navigate = useNavigate()
  const signedIn = state.status === 'signedIn'
  const starter = problem.starter_code ?? ''
  const [code, setCode] = useCodeDraft(problem.id, starter)
  const names = useMemo(() => argumentNames(problem), [problem])
  const [cases, setCases] = useState<CaseDraft[]>(() => problem.samples.map((s) => toDraft(s.input, names)))
  const [tab, setTab] = useState<'testcase' | 'result' | 'submission'>('testcase')
  const [runId, setRunId] = useState<string | null>(null)
  const [submissionId, setSubmissionId] = useState<string | null>(null)

  const requireSignIn = () => {
    toast.info('Sign in to run or submit code.')
    navigate('/login', { state: { from: `/problems/${problem.slug}` } })
  }

  const runMutation = useMutation({
    mutationFn: () =>
      runs.create(
        problem.id,
        code,
        cases.map((c, i) => toInput(c, names, i)),
      ),
    onSuccess: (r) => {
      setRunId(r.run_id)
      setTab('result')
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

  const busy = runMutation.isPending || submitMutation.isPending

  return (
    <ResizablePanelGroup orientation="vertical">
      <ResizablePanel defaultSize="62" minSize="25">
        <Pane>
          <PaneTabs
            value="code"
            onChange={() => {}}
            tabs={[{ value: 'code', label: 'Code', Icon: Code2 }]}
            right={
              <>
                <span className="hidden rounded-md bg-muted px-2 py-0.5 font-mono text-xs whitespace-nowrap text-muted-foreground sm:inline">
                  Python 3
                </span>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7"
                  aria-label="Reset to starter code"
                  title="Reset to starter code"
                  onClick={() => {
                    if (window.confirm('Replace your code with the starter code?')) setCode(starter)
                  }}
                >
                  <RotateCcw />
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  className="h-7"
                  disabled={busy}
                  onClick={() => (signedIn ? runMutation.mutate() : requireSignIn())}
                >
                  {runMutation.isPending ? <Loader2 className="animate-spin" /> : <Play />} Run
                </Button>
                <Button
                  size="sm"
                  className="h-7 bg-success text-white hover:bg-success/90"
                  disabled={busy}
                  onClick={() => (signedIn ? submitMutation.mutate() : requireSignIn())}
                >
                  {submitMutation.isPending ? <Loader2 className="animate-spin" /> : <Send />} Submit
                </Button>
              </>
            }
          />
          <div className="min-h-0 flex-1">
            <Suspense fallback={<Loading label="Loading editor…" />}>
              <CodeEditor value={code} onChange={setCode} />
            </Suspense>
          </div>
        </Pane>
      </ResizablePanel>
      <ResizableHandle className={handleClass} />
      <ResizablePanel defaultSize="38" minSize="15">
        <Pane>
          <PaneTabs
            value={tab}
            onChange={setTab}
            tabs={[
              { value: 'testcase', label: 'Testcase', Icon: CheckCircle2 },
              { value: 'result', label: 'Test Result', Icon: Terminal },
              { value: 'submission', label: 'Submission', Icon: Send },
            ]}
          />
          <ScrollArea className="min-h-0 flex-1">
            <div className="p-4">
              {tab === 'testcase' && <TestCases names={names} cases={cases} setCases={setCases} />}
              {tab === 'result' &&
                (runId ? <RunResult runId={runId} /> : <Hint>You must run your code first.</Hint>)}
              {tab === 'submission' &&
                (submissionId ? (
                  <SubmissionResult key={submissionId} submissionId={submissionId} />
                ) : (
                  <Hint>Submit to judge your code on every test case, hidden ones included.</Hint>
                ))}
            </div>
          </ScrollArea>
        </Pane>
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}

function Hint({ children }: { children: React.ReactNode }) {
  return <p className="py-6 text-center text-sm text-muted-foreground">{children}</p>
}

function CaseChips({
  count,
  selected,
  onSelect,
  status,
  onRemove,
}: {
  count: number
  selected: number
  onSelect: (i: number) => void
  status?: (i: number) => 'pass' | 'fail' | undefined
  onRemove?: (i: number) => void
}) {
  return (
    <>
      {Array.from({ length: count }, (_, i) => {
        const s = status?.(i)
        return (
          <span key={i} className="group relative">
            <button
              type="button"
              onClick={() => onSelect(i)}
              className={cn(
                'flex items-center gap-1.5 rounded-md px-3 py-1 text-sm font-medium transition-colors',
                i === selected ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60',
              )}
            >
              {s && (
                <span className={cn('size-1.5 rounded-full', s === 'pass' ? 'bg-success' : 'bg-danger')} />
              )}
              Case {i + 1}
            </button>
            {onRemove && count > 1 && (
              <button
                type="button"
                aria-label={`Remove case ${i + 1}`}
                onClick={() => onRemove(i)}
                className="absolute -top-1.5 -right-1.5 hidden size-4 place-items-center rounded-full bg-muted-foreground/80 text-background group-hover:grid"
              >
                <X className="size-3" />
              </button>
            )}
          </span>
        )
      })}
    </>
  )
}

function TestCases({
  names,
  cases,
  setCases,
}: {
  names: string[]
  cases: CaseDraft[]
  setCases: (v: CaseDraft[]) => void
}) {
  const [selected, setSelected] = useState(0)
  const current = cases[Math.min(selected, cases.length - 1)]
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-1">
        <CaseChips
          count={cases.length}
          selected={selected}
          onSelect={setSelected}
          onRemove={(i) => {
            setCases(cases.filter((_, j) => j !== i))
            setSelected(0)
          }}
        />
        <Button
          variant="ghost"
          size="icon"
          className="size-7"
          aria-label="Add test case"
          disabled={cases.length >= MAX_INPUTS}
          onClick={() => {
            setCases([...cases, { ...(cases.at(-1) ?? {}) }])
            setSelected(cases.length)
          }}
        >
          <Plus />
        </Button>
      </div>
      {current &&
        names.map((n) => (
          <div key={n} className="space-y-1.5">
            <label htmlFor={`arg-${n}`} className="font-mono text-xs text-muted-foreground">
              {n} =
            </label>
            <Input
              id={`arg-${n}`}
              className="bg-muted/50 font-mono text-sm"
              value={current[n] ?? ''}
              onChange={(e) =>
                setCases(
                  cases.map((c, j) =>
                    j === Math.min(selected, cases.length - 1) ? { ...c, [n]: e.target.value } : c,
                  ),
                )
              }
            />
          </div>
        ))}
    </div>
  )
}

function RunResult({ runId }: { runId: string }) {
  const [selected, setSelected] = useState(0)
  const run = useQuery({
    queryKey: ['run', runId],
    queryFn: () => runs.get(runId),
    // Poll until the judge answers; runs are short.
    refetchInterval: (q) => (isDone(q.state.data) ? false : 1000),
  })

  if (run.isError) return <ErrorState error={run.error} />
  if (!run.data || !isDone(run.data)) return <Pending label="Running on the judge" />
  if (run.data.status === 'FAILED' || !run.data.result) {
    return (
      <ErrorState error={new Error(run.data.error_message ?? 'The run did not finish.')} title="Run failed" />
    )
  }
  const { result } = run.data
  const c = result.cases[Math.min(selected, result.cases.length - 1)]
  const maxTime = Math.max(0, ...result.cases.map((x) => x.time_ms ?? 0))
  return (
    <div className="space-y-4">
      <div className="flex items-baseline gap-3">
        {result.verdict ? (
          <VerdictText verdict={result.verdict} className="text-xl" />
        ) : (
          <span className="text-xl font-medium">Finished</span>
        )}
        <span className="text-sm text-muted-foreground">Runtime: {maxTime} ms</span>
      </div>
      <div className="flex flex-wrap gap-1">
        <CaseChips
          count={result.cases.length}
          selected={selected}
          onSelect={setSelected}
          status={(i) => caseStatus(result.cases[i]!)}
        />
      </div>
      {c && <CaseDetail c={c} />}
    </div>
  )
}

function caseStatus(c: RunCase): 'pass' | 'fail' | undefined {
  if (c.verdict === 'ACCEPTED') return 'pass'
  if (c.verdict === 'FINISHED') return undefined
  return 'fail'
}

function CaseDetail({ c }: { c: RunCase }) {
  const wrong = c.verdict !== 'ACCEPTED' && c.verdict !== 'FINISHED'
  return (
    <div className="space-y-3">
      {wrong && c.verdict !== 'WRONG_ANSWER' && <VerdictText verdict={c.verdict} />}
      <Block label="Input" value={formatArguments(c.input)} />
      {c.stdout && <Block label="Stdout" value={c.stdout} />}
      {c.output !== null && (
        <Block label="Output" value={prettyJson(c.output)} tone={wrong ? 'danger' : undefined} />
      )}
      {c.expected_output !== null && <Block label="Expected" value={prettyJson(c.expected_output)} />}
      {c.error && <Block label="Error" value={c.error} tone="danger" />}
      <div className="flex gap-4 text-xs text-muted-foreground tabular-nums">
        {c.time_ms !== null && <span>{c.time_ms} ms</span>}
        {c.memory_kb !== null && <span>{formatMemory(c.memory_kb)}</span>}
      </div>
    </div>
  )
}

function Block({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return (
    <div className="space-y-1.5">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <pre
        className={cn(
          'overflow-x-auto rounded-lg bg-muted/60 px-3 py-2 font-mono text-[13px] leading-6',
          tone === 'danger' && 'text-danger',
        )}
      >
        {value}
      </pre>
    </div>
  )
}

function isDone(run: Run | undefined) {
  return run?.status === 'COMPLETED' || run?.status === 'FAILED'
}

function Pending({ label }: { label: string }) {
  return (
    <div className="flex flex-col items-center gap-3 py-8 text-sm text-muted-foreground">
      <Loader2 className="size-6 animate-spin text-brand" />
      {label}…
    </div>
  )
}

function celebrate() {
  const colors = ['#8b5cf6', '#22c55e', '#a78bfa', '#facc15']
  const shoot = (x: number) =>
    confetti({
      particleCount: 70,
      spread: 70,
      startVelocity: 45,
      origin: { x, y: 0.9 },
      colors,
      disableForReducedMotion: true,
    })
  shoot(0.6)
  setTimeout(() => shoot(0.85), 150)
}

function SubmissionResult({ submissionId }: { submissionId: string }) {
  const { state, error } = useSubmissionStream(submissionId)
  const celebrated = useRef(false)
  const accepted = state?.status === 'COMPLETED' && state.verdict === 'ACCEPTED'

  useEffect(() => {
    if (accepted && !celebrated.current) {
      celebrated.current = true
      celebrate()
    }
  }, [accepted])

  if (error) return <ErrorState error={new Error(error)} />
  if (!state || state.status !== 'COMPLETED') {
    return (
      <Pending
        label={state?.status === 'RUNNING' ? 'Judging on hidden test cases' : 'Waiting for a judge worker'}
      />
    )
  }
  return (
    <div className="space-y-4 animate-in fade-in-0 zoom-in-95 duration-300">
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        {state.verdict && <VerdictText verdict={state.verdict} className="text-2xl" />}
        {state.total_test_cases != null && (
          <span className="text-sm text-muted-foreground">
            {state.test_cases_passed} / {state.total_test_cases} testcases passed
          </span>
        )}
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Stat
          Icon={Clock}
          label="Runtime"
          value={state.execution_time_ms != null ? `${state.execution_time_ms} ms` : '—'}
        />
        <Stat Icon={MemoryStick} label="Memory" value={formatMemory(state.memory_kb ?? null)} />
      </div>
      {state.error_message && <Block label="Error" value={state.error_message} tone="danger" />}
      <Button variant="outline" size="sm" asChild>
        <Link to={`/submissions/${submissionId}`}>
          {accepted ? <CheckCircle2 /> : <XCircle />}
          View submission{state.verdict && !accepted ? ' and AI hints' : ''}
        </Link>
      </Button>
    </div>
  )
}

function Stat({ Icon, label, value }: { Icon: typeof Clock; label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-muted/30 p-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div className="mt-1 text-lg font-semibold tabular-nums">{value}</div>
    </div>
  )
}
