import { cn } from '@/lib/utils'
import type { Difficulty } from '@/lib/types'
import type { DifficultyCount } from '@/lib/progress'

const BAR: Record<Difficulty, string> = { easy: 'bg-easy', medium: 'bg-medium', hard: 'bg-hard' }
const TRACK: Record<Difficulty, string> = { easy: 'bg-easy/15', medium: 'bg-medium/15', hard: 'bg-hard/15' }
const TEXT: Record<Difficulty, string> = { easy: 'text-easy', medium: 'text-medium', hard: 'text-hard' }

/** A circular progress ring with the solved count in the middle. */
export function ProgressRing({
  solved,
  total,
  size = 112,
}: {
  solved: number
  total: number
  size?: number
}) {
  const r = 44
  const c = 2 * Math.PI * r
  const fraction = total === 0 ? 0 : solved / total
  return (
    <div className="relative grid place-items-center" style={{ width: size, height: size }}>
      <svg viewBox="0 0 100 100" className="size-full -rotate-90">
        <circle cx="50" cy="50" r={r} fill="none" strokeWidth="7" className="stroke-muted" />
        <circle
          cx="50"
          cy="50"
          r={r}
          fill="none"
          strokeWidth="7"
          strokeLinecap="round"
          className="stroke-primary transition-[stroke-dashoffset] duration-700 ease-out"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - fraction)}
        />
      </svg>
      <div className="absolute text-center">
        <div className="text-2xl font-semibold tabular-nums">
          {solved}
          <span className="text-sm font-normal text-muted-foreground">/{total}</span>
        </div>
        <div className="text-xs text-muted-foreground">Solved</div>
      </div>
    </div>
  )
}

export function DifficultyBars({ counts, className }: { counts: DifficultyCount[]; className?: string }) {
  return (
    <div className={cn('grid gap-3', className)}>
      {counts.map(({ difficulty, solved, total }) => (
        <div key={difficulty} className="grid gap-1.5">
          <div className="flex items-baseline justify-between text-sm">
            <span className={cn('font-medium capitalize', TEXT[difficulty])}>{difficulty}</span>
            <span className="tabular-nums">
              <span className="font-semibold">{solved}</span>
              <span className="text-muted-foreground">/{total}</span>
            </span>
          </div>
          <div className={cn('h-1.5 overflow-hidden rounded-full', TRACK[difficulty])}>
            <div
              className={cn('h-full rounded-full transition-[width] duration-700 ease-out', BAR[difficulty])}
              style={{ width: `${total === 0 ? 0 : (solved / total) * 100}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}
