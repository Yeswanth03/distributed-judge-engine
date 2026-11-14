import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Difficulty, RunCaseVerdict, SubmissionStatus, Verdict } from '@/lib/types'
import { verdictText } from '@/lib/verdict'

const DIFFICULTY: Record<Difficulty, string> = {
  easy: 'text-emerald-600 dark:text-emerald-400',
  medium: 'text-amber-600 dark:text-amber-400',
  hard: 'text-rose-600 dark:text-rose-400',
}

export function DifficultyLabel({ difficulty }: { difficulty: Difficulty }) {
  return <span className={cn('text-sm font-medium capitalize', DIFFICULTY[difficulty])}>{difficulty}</span>
}

export function VerdictBadge({ verdict }: { verdict: Verdict | RunCaseVerdict }) {
  const tone =
    verdict === 'ACCEPTED'
      ? 'bg-emerald-600 text-white'
      : verdict === 'FINISHED'
        ? 'bg-secondary text-secondary-foreground'
        : verdict === 'INTERNAL_ERROR'
          ? 'bg-muted text-muted-foreground'
          : 'bg-rose-600 text-white'
  return <Badge className={cn('border-transparent', tone)}>{verdictText(verdict)}</Badge>
}

export function StatusBadge({ status, verdict }: { status: SubmissionStatus; verdict: Verdict | null }) {
  if (status === 'COMPLETED' && verdict) return <VerdictBadge verdict={verdict} />
  return <Badge variant="secondary">{status === 'RUNNING' ? 'Running…' : 'Queued'}</Badge>
}
