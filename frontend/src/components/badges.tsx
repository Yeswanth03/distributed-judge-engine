import { CheckCircle2, Clock, Loader2, XCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Difficulty, RunCaseVerdict, SubmissionStatus, Verdict } from '@/lib/types'
import { verdictText } from '@/lib/verdict'

const DIFFICULTY_TEXT: Record<Difficulty, string> = {
  easy: 'text-easy',
  medium: 'text-medium',
  hard: 'text-hard',
}

const DIFFICULTY_PILL: Record<Difficulty, string> = {
  easy: 'bg-easy/12 text-easy',
  medium: 'bg-medium/14 text-medium',
  hard: 'bg-hard/12 text-hard',
}

/** Difficulty as LeetCode shows it in lists: coloured text. */
export function DifficultyLabel({ difficulty, className }: { difficulty: Difficulty; className?: string }) {
  return (
    <span className={cn('text-sm font-medium capitalize', DIFFICULTY_TEXT[difficulty], className)}>
      {difficulty}
    </span>
  )
}

/** Difficulty as a tinted pill, for headers. */
export function DifficultyPill({ difficulty }: { difficulty: Difficulty }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium capitalize',
        DIFFICULTY_PILL[difficulty],
      )}
    >
      {difficulty}
    </span>
  )
}

function tone(verdict: Verdict | RunCaseVerdict) {
  if (verdict === 'ACCEPTED') return 'success'
  if (verdict === 'FINISHED' || verdict === 'INTERNAL_ERROR') return 'neutral'
  return 'danger'
}

/** A verdict as coloured text with an icon, LeetCode style. */
export function VerdictText({
  verdict,
  className,
}: {
  verdict: Verdict | RunCaseVerdict
  className?: string
}) {
  const t = tone(verdict)
  const Icon = t === 'success' ? CheckCircle2 : t === 'danger' ? XCircle : Clock
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 font-medium',
        t === 'success' && 'text-success',
        t === 'danger' && 'text-danger',
        t === 'neutral' && 'text-muted-foreground',
        className,
      )}
    >
      <Icon className="size-4 shrink-0" />
      {verdictText(verdict)}
    </span>
  )
}

/** A verdict as a compact pill, for small spaces such as test-case headers. */
export function VerdictBadge({ verdict }: { verdict: Verdict | RunCaseVerdict }) {
  const t = tone(verdict)
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium',
        t === 'success' && 'bg-success/14 text-success',
        t === 'danger' && 'bg-danger/12 text-danger',
        t === 'neutral' && 'bg-muted text-muted-foreground',
      )}
    >
      {verdictText(verdict)}
    </span>
  )
}

export function StatusBadge({ status, verdict }: { status: SubmissionStatus; verdict: Verdict | null }) {
  if (status === 'COMPLETED' && verdict) return <VerdictText verdict={verdict} />
  return (
    <span className="inline-flex items-center gap-1.5 font-medium text-muted-foreground">
      <Loader2 className="size-4 animate-spin" />
      {status === 'RUNNING' ? 'Running' : 'Queued'}
    </span>
  )
}
