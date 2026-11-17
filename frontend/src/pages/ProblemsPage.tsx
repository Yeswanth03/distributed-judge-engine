import { useMemo } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Circle, Search, Shuffle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { UserAvatar } from '@/components/brand'
import { DifficultyLabel } from '@/components/badges'
import { DifficultyBars, ProgressRing } from '@/components/progress'
import { countByDifficulty } from '@/lib/progress'
import { ErrorState } from '@/components/states'
import { useAllProblems } from '@/hooks/useProblemTitles'
import { useMyStanding } from '@/hooks/useStanding'
import { leaderboard } from '@/lib/endpoints'
import { displayNameOf } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Difficulty } from '@/lib/types'

const FILTERS: { value: Difficulty | 'all'; label: string; active: string }[] = [
  { value: 'all', label: 'All', active: 'bg-foreground text-background' },
  { value: 'easy', label: 'Easy', active: 'bg-easy/15 text-easy' },
  { value: 'medium', label: 'Medium', active: 'bg-medium/15 text-medium' },
  { value: 'hard', label: 'Hard', active: 'bg-hard/15 text-hard' },
]

export default function ProblemsPage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const difficulty = (params.get('difficulty') as Difficulty | null) ?? 'all'
  const search = params.get('q') ?? ''
  const all = useAllProblems()
  const standing = useMyStanding()

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  // The catalog is small, so filtering happens here; every problem's number is its position in slug order.
  const rows = useMemo(() => {
    const q = search.trim().toLowerCase()
    return (all.data ?? [])
      .map((p, i) => ({ ...p, number: i + 1 }))
      .filter((p) => difficulty === 'all' || p.difficulty === difficulty)
      .filter((p) => !q || p.title.toLowerCase().includes(q) || p.slug.includes(q))
  }, [all.data, difficulty, search])

  const pickRandom = () => {
    const unsolved = rows.filter((p) => !standing.solved.has(p.id))
    const pool = unsolved.length > 0 ? unsolved : rows
    if (pool.length === 0) return
    navigate(`/problems/${pool[Math.floor(Math.random() * pool.length)]!.slug}`)
  }

  return (
    <div className="mx-auto grid w-full max-w-6xl gap-6 px-4 py-8 lg:grid-cols-[1fr_300px]">
      <section className="min-w-0">
        <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Problems</h1>
            <p className="text-sm text-muted-foreground">
              Pick a problem, write a function, get judged on hidden tests.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={pickRandom} disabled={rows.length === 0}>
            <Shuffle /> Pick one
          </Button>
        </div>

        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setParam('q', e.target.value)}
              placeholder="Search problems"
              aria-label="Search problems"
              className="pl-9"
            />
          </div>
          <div className="flex gap-1 rounded-lg border bg-card p-1" role="radiogroup" aria-label="Difficulty">
            {FILTERS.map((f) => (
              <button
                key={f.value}
                type="button"
                role="radio"
                aria-checked={difficulty === f.value}
                onClick={() => setParam('difficulty', f.value)}
                className={cn(
                  'rounded-md px-3 py-1 text-sm font-medium transition-colors',
                  difficulty === f.value
                    ? f.active
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>

        <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
          <div className="grid grid-cols-[2.25rem_1fr_4.5rem] sm:grid-cols-[4rem_1fr_6rem_4.5rem] border-b px-4 py-2.5 text-xs font-medium tracking-wide text-muted-foreground uppercase">
            <span>
              <span className="sr-only sm:not-sr-only">Status</span>
            </span>
            <span>Title</span>
            <span>Difficulty</span>
            <span className="hidden text-right sm:block">Points</span>
          </div>
          {all.isPending ? (
            Array.from({ length: 5 }, (_, i) => (
              <div
                key={i}
                className="grid grid-cols-[2.25rem_1fr_4.5rem] sm:grid-cols-[4rem_1fr_6rem_4.5rem] items-center px-4 py-3.5"
              >
                <Skeleton className="size-4 rounded-full" />
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-4 w-14" />
                <Skeleton className="ml-auto hidden h-4 w-8 sm:block" />
              </div>
            ))
          ) : all.isError ? (
            <ErrorState error={all.error} />
          ) : rows.length === 0 ? (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">No problems match.</div>
          ) : (
            rows.map((p) => {
              const solved = standing.solved.has(p.id)
              return (
                <Link
                  key={p.id}
                  to={`/problems/${p.slug}`}
                  className="group grid grid-cols-[2.25rem_1fr_4.5rem] sm:grid-cols-[4rem_1fr_6rem_4.5rem] items-center px-4 py-3 text-sm transition-colors odd:bg-muted/40 hover:bg-accent/60"
                >
                  <span>
                    {solved ? (
                      <CheckCircle2 className="size-4 text-success" aria-label="Solved" />
                    ) : (
                      <Circle className="size-4 text-muted-foreground/40" aria-label="Not solved" />
                    )}
                  </span>
                  <span className="truncate font-medium group-hover:text-brand">
                    <span className="mr-1.5 text-muted-foreground tabular-nums">{p.number}.</span>
                    {p.title}
                  </span>
                  <DifficultyLabel difficulty={p.difficulty} />
                  <span className="hidden text-right text-muted-foreground tabular-nums sm:block">
                    {p.points}
                  </span>
                </Link>
              )
            })
          )}
        </div>
      </section>

      <aside className="grid content-start gap-4">
        <ProgressCard />
        <TopCoders />
      </aside>
    </div>
  )
}

function ProgressCard() {
  const all = useAllProblems()
  const standing = useMyStanding()
  if (!standing.signedIn) {
    return (
      <div className="rounded-xl border bg-card p-5 shadow-sm">
        <h2 className="font-semibold">Track your progress</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Sign in to save solutions, see what you've solved and climb the leaderboard.
        </p>
        <Button className="mt-4 w-full" asChild>
          <Link to="/register">Create a free account</Link>
        </Button>
      </div>
    )
  }
  if (!all.data) return <Skeleton className="h-56 rounded-xl" />
  return (
    <div className="rounded-xl border bg-card p-5 shadow-sm">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="font-semibold">Progress</h2>
        {standing.data?.rank && (
          <span className="rounded-full bg-brand-soft px-2 py-0.5 text-xs font-medium text-brand">
            Rank #{standing.data.rank}
          </span>
        )}
      </div>
      <div className="flex items-center gap-5">
        <ProgressRing solved={standing.solved.size} total={all.data.length} size={96} />
        <DifficultyBars counts={countByDifficulty(all.data, standing.solved)} className="flex-1" />
      </div>
    </div>
  )
}

function TopCoders() {
  const top = useQuery({ queryKey: ['leaderboard'], queryFn: () => leaderboard.top(100) })
  const entries = top.data?.entries.slice(0, 5) ?? []
  return (
    <div className="rounded-xl border bg-card p-5 shadow-sm">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="font-semibold">Top coders</h2>
        <Link to="/leaderboard" className="text-xs text-brand hover:underline">
          View all
        </Link>
      </div>
      {entries.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nobody has solved a problem yet. Be the first.</p>
      ) : (
        <ol className="grid gap-2.5">
          {entries.map((e) => (
            <li key={e.user_id} className="flex items-center gap-3 text-sm">
              <span className="w-4 text-right text-muted-foreground tabular-nums">{e.rank}</span>
              <UserAvatar name={displayNameOf(e)} className="size-7 text-[10px]" />
              <span className="flex-1 truncate">{displayNameOf(e)}</span>
              <span className="font-medium tabular-nums">{e.score}</span>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}
